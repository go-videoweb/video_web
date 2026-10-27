package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"vw_gateway/internal/biz/videobiz"
	"vw_gateway/internal/conf"
	"vw_gateway/internal/data/videodata"
	"vw_gateway/internal/pkg/middlewares/auth"
	video "vw_gateway/internal/service/video_service"
	interactv2 "vw_video/api/v1/interact"
)

type favoriteWriteHTTPClient struct {
	interactv2.VideoInteractClient
	t     *testing.T
	calls int
}

// SetVideoFavorite 模拟视频响应，验证真实 HTTP 编解码及鉴权身份，不运行或模拟 Saga。
func (c *favoriteWriteHTTPClient) SetVideoFavorite(_ context.Context, req *interactv2.SetVideoFavoriteReq, _ ...grpc.CallOption) (*interactv2.SetVideoFavoriteResp, error) {
	c.calls++
	if req.UserId != 7 || req.VideoId != 9 || req.FavoriteId != 13 {
		c.t.Fatalf("identity or target altered: %v", req)
	}
	if req.Favorite == 0 {
		return nil, kerrors.BadRequest("INVALID_FAVORITE", "明确操作不能为空")
	}
	if req.Favorite == -1 {
		if req.FavoriteRecordId != 9007199254740993 {
			return nil, kerrors.Conflict("FAVORITE_RECORD_CHANGED", "changed")
		}
		return &interactv2.SetVideoFavoriteResp{Message: "取消收藏成功"}, nil
	}
	return &interactv2.SetVideoFavoriteResp{IsFavorited: true, FavoriteId: 13, FavoriteRecordId: 9007199254740993, Message: "收藏成功"}, nil
}

// TestFavoriteWriteHTTP 验证明确操作、大记录 ID、登录要求、原样转发冲突及不回退旧切换语义。
func TestFavoriteWriteHTTP(t *testing.T) {
	const secret = "local-favorite-http-secret"
	access, refresh, err := (&auth.JWTAuth{Secret: secret, AccessExpireTime: time.Hour, RefreshExpireTime: 24 * time.Hour}).CreateToken(7, "owner", false)
	if err != nil {
		t.Fatal(err)
	}
	cache := redis.NewClusterClient(&redis.ClusterOptions{Addrs: []string{"127.0.0.1:1"}, MaxRetries: -1})
	cache.AddHook(&sessionStoreHook{value: access})
	t.Cleanup(func() { _ = cache.Close() })
	logger := log.NewStdLogger(io.Discard)
	client := &favoriteWriteHTTPClient{t: t}
	// 只装配视频客户端，误调用用户服务或 DTM 会导致测试失败。
	data, cleanup, err := videodata.NewData(logger, nil, nil, client, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	svc := video.NewInteractService(videobiz.NewInteractUsecase(videodata.NewInteractRepo(data, logger), logger), logger)
	srv := NewHTTPServer(&conf.Server{Http: &conf.Server_HTTP{}}, &conf.JWT{Secret: secret, AccessTokenExpiration: 1}, nil, nil, nil, nil, cache, nil, nil, nil, svc, nil, logger)
	for _, tc := range []struct {
		name, body string
		signed     bool
		code       int
		calls      int
	}{
		{name: "未登录", body: `{"favoriteId":"13","favorite":1}`, code: 200},
		{name: "收藏", signed: true, body: `{"favoriteId":"13","favorite":1}`, code: 200, calls: 1},
		{name: "取消", signed: true, body: `{"favoriteId":"13","favorite":-1,"favoriteRecordId":"9007199254740993"}`, code: 200, calls: 1},
		{name: "旧记录", signed: true, body: `{"favoriteId":"13","favorite":-1,"favoriteRecordId":"22"}`, code: 409, calls: 1},
		{name: "省略操作不回退", signed: true, body: `{"favoriteId":"13"}`, code: 400, calls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := client.calls
			req := httptest.NewRequest(http.MethodPut, "/api/v1/video/interaction/9/favorite?user_id=999", strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			if tc.signed {
				req.Header.Set("Authorization", "Bearer "+access)
				req.Header.Set("Refresh-Token", "Bearer "+refresh)
			}
			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)
			if client.calls-before != tc.calls {
				t.Fatal("HTTP issued unexpected RPC count")
			}
			if !tc.signed {
				assertMissingToken(t, w)
				return
			}
			if w.Code != tc.code {
				t.Fatalf("unexpected HTTP: %d %s", w.Code, w.Body.String())
			}
			if tc.code != 200 {
				return
			}
			var body struct {
				IsFavorited bool   `json:"isFavorited"`
				RecordID    string `json:"favoriteRecordId"`
				FavoriteID  string `json:"favoriteId"`
			}
			if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if tc.name == "收藏" && (!body.IsFavorited || body.RecordID != "9007199254740993" || body.FavoriteID != "13") {
				t.Fatalf("record ID not encoded as string: %s", w.Body.String())
			}
			if tc.name == "取消" && (body.IsFavorited || body.RecordID != "0" || body.FavoriteID != "0") {
				t.Fatalf("cancel status mismatch: %s", w.Body.String())
			}
		})
	}
}
