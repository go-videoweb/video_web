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
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
	"vw_gateway/internal/biz/videobiz"
	"vw_gateway/internal/conf"
	"vw_gateway/internal/data/videodata"
	"vw_gateway/internal/pkg/middlewares/auth"
	video "vw_gateway/internal/service/video_service"
	videorpc "vw_video/api/v1/interact"
)

type shareHTTPClient struct {
	videorpc.VideoInteractClient
	request *videorpc.RecordVideoShareReq
	err     error
}

// RecordVideoShare 捕获网关提交的分享者和视频 ID。
func (c *shareHTTPClient) RecordVideoShare(_ context.Context, req *videorpc.RecordVideoShareReq, _ ...grpc.CallOption) (*emptypb.Empty, error) {
	c.request = req
	if c.err != nil {
		return nil, c.err
	}
	return &emptypb.Empty{}, nil
}

// TestVideoShareHTTP 验证公开观看页链接、可信身份和下游错误回传。
func TestVideoShareHTTP(t *testing.T) {
	const secret = "share-http-test"
	access, refresh, err := (&auth.JWTAuth{Secret: secret, AccessExpireTime: time.Hour, RefreshExpireTime: 24 * time.Hour}).CreateToken(7, "viewer", false)
	if err != nil {
		t.Fatal(err)
	}
	cache := redis.NewClusterClient(&redis.ClusterOptions{Addrs: []string{"127.0.0.1:1"}, MaxRetries: -1})
	cache.AddHook(&sessionStoreHook{value: access})
	t.Cleanup(func() { _ = cache.Close() })
	logger := log.NewStdLogger(io.Discard)
	client := &shareHTTPClient{}
	data, cleanup, err := videodata.NewData(logger, nil, nil, client, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	repo := videodata.NewInteractRepo(data, logger)
	for _, tc := range []struct {
		name, origin, wantURL string
		signed                bool
		downstreamErr         error
		errorCode             int
	}{
		{name: "匿名请求"},
		{name: "公网 IP", signed: true, origin: "http://203.0.113.8:8080/", wantURL: "http://203.0.113.8:8080/watch/9007199254740993"},
		{name: "域名", signed: true, origin: "https://video.example.com", wantURL: "https://video.example.com/watch/9007199254740993"},
		{name: "未配置", signed: true, errorCode: http.StatusInternalServerError},
		{name: "配置误填 API 路径", signed: true, origin: "http://203.0.113.8:8080/api/v1", errorCode: http.StatusInternalServerError},
		{name: "视频不存在", signed: true, origin: "https://video.example.com", downstreamErr: kerrors.NotFound("VIDEO_NOT_FOUND", "视频不存在"), errorCode: http.StatusNotFound},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client.request, client.err = nil, tc.downstreamErr
			svc := video.NewInteractService(videobiz.NewInteractUsecase(repo, &conf.Service{PublicSiteOrigin: tc.origin}), logger)
			srv := NewHTTPServer(&conf.Server{Http: &conf.Server_HTTP{}}, &conf.JWT{Secret: secret}, nil, nil, nil, nil, cache, nil, nil, nil, svc, nil, logger)
			req := httptest.NewRequest(http.MethodGet, "/api/v1/video/interaction/9007199254740993/share?user_id=999&video_id=999", nil)
			req.Host = "untrusted.example"
			if tc.signed {
				req.Header.Set("Authorization", "Bearer "+access)
				req.Header.Set("Refresh-Token", "Bearer "+refresh)
			}
			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)
			if tc.wantURL != "" || tc.downstreamErr != nil {
				if client.request.GetUserId() != 7 || client.request.GetVideoId() != 9007199254740993 {
					t.Fatalf("下游分享参数不符：%v", client.request)
				}
			} else if client.request != nil {
				t.Fatalf("无鉴权或配置错误时调用了下游：%v", client.request)
			}
			switch {
			case !tc.signed:
				assertMissingToken(t, w)
			case tc.errorCode != 0:
				assertHTTPErrorCode(t, w, tc.errorCode)
			default:
				var body struct {
					URL string `json:"url"`
				}
				if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil || w.Code != http.StatusOK ||
					body.URL != tc.wantURL || w.Header().Get("Cache-Control") != "no-store" {
					t.Fatalf("分享响应不符：%d %s", w.Code, w.Body.String())
				}
			}
		})
	}
}
