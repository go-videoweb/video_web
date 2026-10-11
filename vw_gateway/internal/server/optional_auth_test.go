package server

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/redis/go-redis/v9"
	interactv1 "vw_gateway/api/v1/video/video_interact"
	videov1 "vw_gateway/api/v1/video/videoinfo"
	"vw_gateway/internal/biz/videobiz"
	"vw_gateway/internal/conf"
	"vw_gateway/internal/domain"
	"vw_gateway/internal/pkg/ecode/errdef"
	"vw_gateway/internal/pkg/ecode/errdef/uerr"
	"vw_gateway/internal/pkg/middlewares/auth"
	video "vw_gateway/internal/service/video_service"
)

type optionalDetailRepo struct {
	videobiz.VideoInfoRepo
	userID int64
	calls  int
}

func (r *optionalDetailRepo) GetVideoInfo(_ context.Context, videoID, userID int64) (*domain.VideoDetail, error) {
	r.calls++
	r.userID = userID
	return &domain.VideoDetail{VideoId: videoID, Title: "public detail", Records: &domain.Records{}}, nil
}

// TestVideoDetailOptionalAuthRouting 验证视频详情接入游客访问策略，且不信任 HTTP 参数中的 user_id。
// 无凭证或格式错误时详情应成功返回，MPD、分片和封面跳过鉴权，列表、上传及互动仍须登录。
func TestVideoDetailOptionalAuthRouting(t *testing.T) {
	ctx := context.Background()
	strict, optional := NewWhitelistMatcher(), NewAnonymousAllowedMatcher()
	if !strict(ctx, videov1.OperationVideoInfoGetVideoInfo) || !optional(ctx, videov1.OperationVideoInfoGetVideoInfo) {
		t.Fatal("detail auth policy incorrect")
	}
	for _, op := range []string{
		videov1.OperationVideoInfoGetVideoList, videov1.OperationVideoInfoDownloadVideo,
		videov1.OperationVideoInfoUploadVideoInfo,
		interactv1.OperationVideoInteractVideoUpvote,
		interactv1.OperationVideoInteractVideoFavorite,
		interactv1.OperationVideoInteractVideoThrowShells,
		interactv1.OperationVideoInteractVideoSendBarrage,
	} {
		if !strict(ctx, op) || optional(ctx, op) {
			t.Fatalf("unrelated auth policy changed: %s", op)
		}
	}
	for _, op := range []string{videov1.OperationVideoInfoGetMpd, videov1.OperationVideoInfoGetSegments, videov1.OperationVideoInfoGetVideoCover} {
		if strict(ctx, op) || optional(ctx, op) {
			t.Fatalf("public media did not bypass authentication: %s", op)
		}
	}
	if strict(ctx, "/gateway.api.v1.user.id.Identity/Login") {
		t.Fatal("existing whitelist changed")
	}

	logger := log.NewStdLogger(io.Discard)
	repo := &optionalDetailRepo{}
	service := video.NewVideoInfoService(videobiz.NewVideoInfoUsecase(repo, logger), logger)
	srv := NewHTTPServer(&conf.Server{Http: &conf.Server_HTTP{}}, &conf.JWT{Secret: "test"}, nil, nil, nil, nil, nil, nil, nil, service, nil, nil, logger)
	for _, malformed := range []bool{false, true} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/video/42/info?user_id=999", nil)
		if malformed {
			req.Header.Set("Authorization", "Bearer invalid")
		}
		recorder := httptest.NewRecorder()
		srv.ServeHTTP(recorder, req)
		if recorder.Code != http.StatusOK || repo.userID != 0 {
			t.Fatalf("anonymous detail failed: %d %s", recorder.Code, recorder.Body.String())
		}
		var body struct {
			VideoDetail struct {
				VideoID string `json:"videoId"`
			} `json:"videoDetail"`
		}
		if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil || body.VideoDetail.VideoID != "42" {
			t.Fatalf("not a detail response: %s", recorder.Body.String())
		}
	}
	if repo.calls != 2 {
		t.Fatal("detail not handled once per request")
	}
	for _, path := range []string{"/api/v1/video/list", "/api/v1/7/video/42/file"} {
		recorder := httptest.NewRecorder()
		srv.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
		assertMissingToken(t, recorder)
	}
	for _, path := range []string{"/api/v1/video/interaction/42/upvote", "/api/v1/video/interaction/42/favorite", "/api/v1/video/interaction/42/throwShells"} {
		recorder := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPut, path, strings.NewReader("{}"))
		req.Header.Set("Content-Type", "application/json")
		srv.ServeHTTP(recorder, req)
		assertMissingToken(t, recorder)
	}
}

func assertMissingToken(t *testing.T, recorder *httptest.ResponseRecorder) {
	t.Helper()
	var body struct {
		Code   int32  `json:"code"`
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil || body.Code != uerr.ErrTokenMissing.Code || body.Reason != uerr.ErrTokenMissing.Reason {
		t.Fatalf("protected route did not reject missing credentials: %s", recorder.Body.String())
	}
}

type sessionStoreHook struct {
	value string
	err   error
	gets  int
}

func (*sessionStoreHook) DialHook(redis.DialHook) redis.DialHook {
	return func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("unexpected network access")
	}
}

func (h *sessionStoreHook) ProcessHook(redis.ProcessHook) redis.ProcessHook {
	return func(_ context.Context, cmd redis.Cmder) error {
		get, ok := cmd.(*redis.StringCmd)
		if !ok || cmd.Name() != "get" {
			return errors.New("unexpected session store command")
		}
		h.gets++
		get.SetVal(h.value)
		get.SetErr(h.err)
		return h.err
	}
}

func (*sessionStoreHook) ProcessPipelineHook(redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(context.Context, []redis.Cmder) error { return errors.New("unexpected pipeline") }
}

// TestVideoDetailViewerIdentityThroughHTTP 验证真实 HTTP 路由将有效会话身份传至详情业务，忽略参数伪造的身份。
// 过期、退出或被替换的会话应按游客访问；缓存故障应返回内部错误，不调用详情业务。
func TestVideoDetailViewerIdentityThroughHTTP(t *testing.T) {
	const secret = "http-auth-test-secret"
	access, refresh, err := (&auth.JWTAuth{Secret: secret, AccessExpireTime: time.Hour, RefreshExpireTime: 24 * time.Hour}).CreateToken(7, "viewer", false)
	if err != nil {
		t.Fatal(err)
	}
	expired, _, err := (&auth.JWTAuth{Secret: secret, AccessExpireTime: -time.Hour, RefreshExpireTime: 24 * time.Hour}).CreateToken(7, "viewer", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, access, cached string
		storeErr             error
		wantUser             int64
		wantGets, wantCalls  int
	}{
		{name: "active viewer", access: access, cached: access, wantUser: 7, wantGets: 1, wantCalls: 1},
		{name: "expired credentials", access: expired, cached: expired, wantCalls: 1},
		{name: "logged out", access: access, storeErr: redis.Nil, wantGets: 1, wantCalls: 1},
		{name: "replaced session", access: access, cached: "other", wantGets: 1, wantCalls: 1},
		{name: "store failure", access: access, storeErr: errors.New("store unavailable"), wantGets: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hook := &sessionStoreHook{value: tc.cached, err: tc.storeErr}
			cache := redis.NewClusterClient(&redis.ClusterOptions{Addrs: []string{"127.0.0.1:1"}, MaxRetries: -1})
			cache.AddHook(hook)
			t.Cleanup(func() { _ = cache.Close() })
			repo := &optionalDetailRepo{}
			logger := log.NewStdLogger(io.Discard)
			service := video.NewVideoInfoService(videobiz.NewVideoInfoUsecase(repo, logger), logger)
			srv := NewHTTPServer(&conf.Server{Http: &conf.Server_HTTP{}}, &conf.JWT{Secret: secret, AccessTokenExpiration: 1}, nil, nil, nil, nil, cache, nil, nil, service, nil, nil, logger)
			req := httptest.NewRequest(http.MethodGet, "/api/v1/video/42/info?user_id=999", nil)
			req.Header.Set("Authorization", "Bearer "+tc.access)
			req.Header.Set("Refresh-Token", "Bearer "+refresh)
			recorder := httptest.NewRecorder()
			srv.ServeHTTP(recorder, req)
			if hook.gets != tc.wantGets || repo.calls != tc.wantCalls || repo.userID != tc.wantUser {
				t.Fatalf("unexpected identity or calls: viewer=%d handler=%d get=%d", repo.userID, repo.calls, hook.gets)
			}
			var body struct {
				Code        int32 `json:"code"`
				VideoDetail *struct {
					VideoID string `json:"videoId"`
				} `json:"videoDetail"`
			}
			if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if tc.wantCalls == 0 {
				if body.Code != errdef.INTERNAL_ERROR.Code || body.VideoDetail != nil {
					t.Fatal("store failure became anonymous success")
				}
			} else if body.Code != 0 || body.VideoDetail == nil || body.VideoDetail.VideoID != "42" {
				t.Fatalf("detail failed: %s", recorder.Body.String())
			}
		})
	}
}
