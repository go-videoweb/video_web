package server

import (
	"bytes"
	"context"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/redis/go-redis/v9"
	"google.golang.org/protobuf/types/known/durationpb"
	"vw_gateway/internal/biz/videobiz"
	"vw_gateway/internal/conf"
	"vw_gateway/internal/pkg/middlewares/auth"
	video "vw_gateway/internal/service/video_service"
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
