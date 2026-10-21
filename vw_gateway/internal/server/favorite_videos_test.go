package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/types/known/timestamppb"
	videov1 "vw_gateway/api/v1/video/videoinfo"
	"vw_gateway/internal/biz/videobiz"
	"vw_gateway/internal/conf"
	"vw_gateway/internal/domain"
	"vw_gateway/internal/pkg/middlewares/auth"
	video "vw_gateway/internal/service/video_service"
	videov2 "vw_video/api/v1/videoinfo"
)

type favoriteVideosHTTPRepo struct {
	videobiz.VideoInfoRepo
	userID int64
	calls  int
	page   int32
	size   int32
}

func (r *favoriteVideosHTTPRepo) GetFavoriteVideos(_ context.Context, userID, favoriteID int64, page, size int32) (*videov2.GetFavoriteVideosResp, error) {
	r.userID = userID
	r.calls++
	r.page, r.size = page, size
	if favoriteID == 10 {
		return nil, kerrors.NotFound("FAVORITE_NOT_FOUND", "收藏夹不存在")
	}
	return &videov2.GetFavoriteVideosResp{Total: 1, PageNum: 1, PageSize: 20, Videos: []*videov2.FavoriteVideoItem{{Video: &videov2.VideoSummary{VideoId: 9007199254740993, CreatedTime: timestamppb.New(time.Unix(100, 0))}, CollectedAt: timestamppb.New(time.Unix(200, 0))}}}, nil
}
func (*favoriteVideosHTTPRepo) GetVideoInfo(context.Context, int64, int64) (*domain.VideoDetail, error) {
	return &domain.VideoDetail{VideoId: 42, Records: &domain.Records{}, UserVideoStatus: &domain.UserVideoStatus{IsFavorited: 1, FavoriteId: 9007199254740993}}, nil
}

// TestFavoriteVideosHTTP 验证真实路由强制登录、忽略伪造身份、保留 404，详情状态和大 ID JSON 正确返回。
func TestFavoriteVideosHTTP(t *testing.T) {
	const secret = "favorite-videos-http-test-secret"
	access, refresh, err := (&auth.JWTAuth{Secret: secret, AccessExpireTime: time.Hour, RefreshExpireTime: 24 * time.Hour}).CreateToken(7, "owner", false)
	if err != nil {
		t.Fatal(err)
	}
	hook := &sessionStoreHook{value: access}
	cache := redis.NewClusterClient(&redis.ClusterOptions{Addrs: []string{"127.0.0.1:1"}, MaxRetries: -1})
	cache.AddHook(hook)
	t.Cleanup(func() { _ = cache.Close() })
	logger := log.NewStdLogger(io.Discard)
	repo := &favoriteVideosHTTPRepo{}
	svc := video.NewVideoInfoService(videobiz.NewVideoInfoUsecase(repo, logger), logger)
	srv := NewHTTPServer(&conf.Server{Http: &conf.Server_HTTP{}}, &conf.JWT{Secret: secret, AccessTokenExpiration: 1}, nil, nil, nil, nil, cache, nil, nil, svc, nil, nil, logger)
	if !NewWhitelistMatcher()(context.Background(), videov1.OperationVideoInfoGetFavoriteVideos) || NewAnonymousAllowedMatcher()(context.Background(), videov1.OperationVideoInfoGetFavoriteVideos) {
		t.Fatal("分页接口错误允许匿名")
	}
	for _, tc := range []struct {
		path     string
		signed   bool
		wantCode int
		page     int32
		size     int32
	}{
		{path: "/api/v1/favorites/9/videos", wantCode: 200},
		{path: "/api/v1/favorites/9/videos?user_id=999&page_num=2&page_size=3", signed: true, wantCode: 200, page: 2, size: 3},
		{path: "/api/v1/favorites/9/videos?pageNum=4&pageSize=5", signed: true, wantCode: 200, page: 4, size: 5},
		{path: "/api/v1/favorites/10/videos", signed: true, wantCode: 404},
		{path: "/api/v1/video/42/info", signed: true, wantCode: 200},
		{path: "/api/v1/video/42/info", wantCode: 200},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		if tc.signed {
			req.Header.Set("Authorization", "Bearer "+access)
			req.Header.Set("Refresh-Token", "Bearer "+refresh)
		}
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		if !tc.signed && tc.path != "/api/v1/video/42/info" {
			assertMissingToken(t, w)
			if repo.calls != 0 {
				t.Fatal("游客访问分页业务")
			}
			continue
		}
		if w.Code != tc.wantCode {
			t.Fatalf("HTTP 状态错误：%d %s", w.Code, w.Body.String())
		}
		var body struct {
			Total  string `json:"total"`
			Videos []struct {
				Video struct {
					ID      string `json:"videoId"`
					Created string `json:"createdTime"`
				} `json:"video"`
				Collected string `json:"collectedAt"`
			} `json:"videos"`
			State *struct {
				FavoriteID string `json:"favoriteId"`
				Favorited  int32  `json:"isFavorited"`
			} `json:"userVideoStatus"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if tc.path == "/api/v1/video/42/info" {
			if !tc.signed {
				if body.State != nil {
					t.Fatal("游客获得个人状态")
				}
			} else if body.State == nil || body.State.FavoriteID != "9007199254740993" || body.State.Favorited != 1 {
				t.Fatal("详情收藏状态编码错误")
			}
			continue
		}
		if repo.userID != 7 {
			t.Fatal("请求伪造了访问者身份")
		}
		if repo.page != tc.page || repo.size != tc.size {
			t.Fatalf("分页参数未正确透传：page=%d size=%d", repo.page, repo.size)
		}
		if tc.wantCode == 404 {
			if body.Videos != nil || body.Total != "" {
				t.Fatal("无权响应泄露内容")
			}
			continue
		}
		if body.Total != "1" || len(body.Videos) != 1 || body.Videos[0].Video.ID != "9007199254740993" || body.Videos[0].Video.Created != "1970-01-01T00:01:40Z" || body.Videos[0].Collected != "1970-01-01T00:03:20Z" {
			t.Fatalf("分页 JSON 错误：%s", w.Body.String())
		}
	}
}
