package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/types/known/durationpb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"util/helper"
	"vw_gateway/internal/biz/videobiz"
	"vw_gateway/internal/conf"
	"vw_gateway/internal/data/videodata"
	"vw_gateway/internal/pkg/middlewares/auth"
	video "vw_gateway/internal/service/video_service"
	videorpc "vw_video/api/v1/videoinfo"
)

type originalUploadRepo struct {
	videobiz.VideoInfoRepo
	calls int
}

// UploadVideoFile 检查鉴权身份、原片转发期限，并模拟视频服务拒绝上传。
func (r *originalUploadRepo) UploadVideoFile(ctx context.Context, userID, videoID int64, header *multipart.FileHeader) error {
	r.calls++
	deadline, ok := ctx.Deadline()
	if !ok || time.Until(deadline) < 90*time.Second || userID != 7 || videoID != 42 || header.Filename != "video.mp4" {
		return kerrors.InternalServer("INVALID_UPLOAD", "原片请求的上下文或参数错误")
	}
	return kerrors.New(5109, "VIDEO_COVER_REQUIRED", "请先上传封面")
}

// TestUploadVideoFileHTTPReturnsDownstreamError 验证实际 HTTP 路由等待视频服务并回传错误体。
func TestUploadVideoFileHTTPReturnsDownstreamError(t *testing.T) {
	const secret = "original-upload-test"
	access, refresh, err := (&auth.JWTAuth{Secret: secret, AccessExpireTime: time.Hour, RefreshExpireTime: 24 * time.Hour}).CreateToken(7, "publisher", false)
	if err != nil {
		t.Fatal(err)
	}
	cache := redis.NewClusterClient(&redis.ClusterOptions{Addrs: []string{"127.0.0.1:1"}, MaxRetries: -1})
	cache.AddHook(&sessionStoreHook{value: access})
	t.Cleanup(func() { _ = cache.Close() })
	logger := log.NewStdLogger(io.Discard)
	repo := &originalUploadRepo{}
	service := video.NewVideoInfoService(videobiz.NewVideoInfoUsecase(repo, logger), logger)
	config := &conf.Server{Http: &conf.Server_HTTP{Timeout: durationpb.New(time.Second)}}
	server := NewHTTPServer(config, &conf.JWT{Secret: secret}, nil, nil, nil, nil, cache, nil, nil, service, nil, nil, logger)

	var body bytes.Buffer
	form := multipart.NewWriter(&body)
	part, err := form.CreateFormFile("file", "video.mp4")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(part, "video"); err != nil {
		t.Fatal(err)
	}
	if err := form.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/v1/999/video/42/file", &body)
	req.Header.Set("Content-Type", form.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+access)
	req.Header.Set("Refresh-Token", "Bearer "+refresh)
	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, req)
	assertHTTPErrorCode(t, recorder, 5109)
	if repo.calls != 1 {
		t.Fatalf("下游调用次数为 %d", repo.calls)
	}
}

type uploadStatusHTTPClient struct {
	videorpc.VideoInfoClient
	request *videorpc.GetVideoUploadStatusReq
	err     error
}

// GetVideoUploadStatus 模拟视频 RPC，并记录网关传递的身份与视频 ID。
func (c *uploadStatusHTTPClient) GetVideoUploadStatus(_ context.Context, req *videorpc.GetVideoUploadStatusReq, _ ...grpc.CallOption) (*videorpc.GetVideoUploadStatusResp, error) {
	c.request = req
	if c.err != nil {
		return nil, c.err
	}
	return &videorpc.GetVideoUploadStatusResp{
		Status: "failed", UpdatedTime: timestamppb.New(time.Unix(1, 0)), UploadErrorMsg: "视频处理失败",
	}, nil
}

// TestGetVideoUploadStatusHTTP 验证登录、可信身份、大视频 ID、响应字段和下游业务错误。
func TestGetVideoUploadStatusHTTP(t *testing.T) {
	const secret = "upload-status-http-test"
	access, refresh, err := (&auth.JWTAuth{Secret: secret, AccessExpireTime: time.Hour, RefreshExpireTime: 24 * time.Hour}).CreateToken(7, "publisher", false)
	if err != nil {
		t.Fatal(err)
	}
	cache := redis.NewClusterClient(&redis.ClusterOptions{Addrs: []string{"127.0.0.1:1"}, MaxRetries: -1})
	cache.AddHook(&sessionStoreHook{value: access})
	t.Cleanup(func() { _ = cache.Close() })
	logger := log.NewStdLogger(io.Discard)
	client := &uploadStatusHTTPClient{}
	data, cleanup, err := videodata.NewData(logger, client, nil, nil, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	svc := video.NewVideoInfoService(videobiz.NewVideoInfoUsecase(videodata.NewVideoInfoRepo(data, logger), logger), logger)
	server := NewHTTPServer(&conf.Server{Http: &conf.Server_HTTP{}}, &conf.JWT{Secret: secret}, nil, nil, nil, nil, cache, nil, nil, svc, nil, nil, logger)
	const path = "/api/v1/video/9007199254740993/upload-status?user_id=999&video_id=999"

	recorder := httptest.NewRecorder()
	server.ServeHTTP(recorder, httptest.NewRequest(http.MethodGet, path, nil))
	assertMissingToken(t, recorder)
	if client.request != nil {
		t.Fatal("匿名请求调用了下游")
	}

	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Header.Set("Authorization", "Bearer "+access)
	req.Header.Set("Refresh-Token", "Bearer "+refresh)
	recorder = httptest.NewRecorder()
	server.ServeHTTP(recorder, req)
	if client.request.GetUserId() != 7 || client.request.GetVideoId() != 9007199254740993 {
		t.Fatalf("身份或路径视频 ID 不符：%v", client.request)
	}
	var body struct {
		Status         string `json:"status"`
		UpdatedTime    string `json:"updatedTime"`
		UploadErrorMsg string `json:"uploadErrorMsg"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &body); err != nil || recorder.Code != http.StatusOK ||
		recorder.Header().Get("Cache-Control") != "no-store" || body.Status != "failed" ||
		body.UpdatedTime != "1970-01-01T00:00:01Z" || body.UploadErrorMsg != "视频处理失败" {
		t.Fatalf("上传状态 HTTP 响应不符：%d %s", recorder.Code, recorder.Body.String())
	}

	client.err = status.Convert(helper.WithRPCBusinessCode(kerrors.New(5110, "VIDEO_UPLOAD_STATUS_NOT_FOUND", "找不到视频上传状态"))).Err()
	recorder = httptest.NewRecorder()
	server.ServeHTTP(recorder, req.Clone(context.Background()))
	assertHTTPErrorCode(t, recorder, 5110)
}
