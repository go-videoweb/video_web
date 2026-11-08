package videodata

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
	"util/helper"
	videov1 "vw_gateway/api/v1/video/videoinfo"
	"vw_gateway/internal/biz/videobiz"
	"vw_gateway/internal/domain"
	"vw_gateway/internal/pkg/codecs"
	video "vw_gateway/internal/service/video_service"
	videov2 "vw_video/api/v1/videoinfo"
)

type metadataClient struct {
	videov2.VideoInfoClient
	response *videov2.UploadVideoInfoResp
	err      error
	request  *videov2.UploadVideoInfoReq
	calls    int
}

type coverErrorClient struct {
	videov2.VideoInfoClient
	stream *rejectedCoverStream
}

// UploadVideoCover 返回会提前拒绝请求的封面流。
func (c *coverErrorClient) UploadVideoCover(context.Context, ...grpc.CallOption) (grpc.ClientStreamingClient[videov2.UploadVideoCoverReq, emptypb.Empty], error) {
	return c.stream, nil
}

type rejectedCoverStream struct {
	grpc.ClientStreamingClient[videov2.UploadVideoCoverReq, emptypb.Empty]
	err error
}

// Send 模拟服务端提前关闭后，发送端只得到 EOF。
func (*rejectedCoverStream) Send(*videov2.UploadVideoCoverReq) error { return io.EOF }

// CloseAndRecv 返回服务端的实际业务错误。
func (s *rejectedCoverStream) CloseAndRecv() (*emptypb.Empty, error) { return nil, s.err }

// TestUploadCoverErrorHTTPBody 验证提前拒绝的 RPC 仍能以 HTTP 200 返回原业务码。
func TestUploadCoverErrorHTTPBody(t *testing.T) {
	want := kerrors.New(5107, "VIDEO_COVER_UPLOAD_STAGE_INVALID", "视频当前阶段不能上传封面")
	rpcErr := status.Convert(helper.WithRPCBusinessCode(want)).Err()
	client := &coverErrorClient{stream: &rejectedCoverStream{err: rpcErr}}
	repo := NewVideoInfoRepo(&Data{videoInfoClient: client}, log.NewStdLogger(io.Discard))
	var form bytes.Buffer
	writer := multipart.NewWriter(&form)
	if _, err := writer.CreateFormFile("cover", "cover.png"); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodPost, "/api/v1/video/cover", &form)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	part, header, err := request.FormFile("cover")
	if err != nil {
		t.Fatal(err)
	}
	_ = part.Close()
	t.Cleanup(func() { _ = request.MultipartForm.RemoveAll() })
	err = repo.UploadVideoCover(request.Context(), 7, 42, header)
	response := httptest.NewRecorder()
	codecs.ErrorEncoder(response, request, err)
	var body kerrors.Status
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || response.Code != http.StatusOK ||
		body.Code != want.Code || body.Reason != want.Reason || body.Message != want.Message || len(body.Metadata) != 0 {
		t.Fatalf("HTTP %d，业务错误响应不符：%s", response.Code, response.Body.String())
	}
}

func (c *metadataClient) UploadVideoInfo(_ context.Context, req *videov2.UploadVideoInfoReq, _ ...grpc.CallOption) (*videov2.UploadVideoInfoResp, error) {
	c.calls++
	c.request = req
	return c.response, c.err
}

// TestUploadVideoInfoRepoResponse 验证上传参数映射、RPC 错误透传及缺失或非正视频 ID 的拒绝行为。
func TestUploadVideoInfoRepoResponse(t *testing.T) {
	rpcErr := errors.New("RPC failed")
	for _, tc := range []struct {
		name     string
		response *videov2.UploadVideoInfoResp
		err      error
		missing  bool
	}{
		{name: "success", response: &videov2.UploadVideoInfoResp{VideoId: 9007199254740993}},
		{name: "RPC failure", response: &videov2.UploadVideoInfoResp{VideoId: 123}, err: rpcErr},
		{name: "old Empty response", response: &videov2.UploadVideoInfoResp{}, missing: true},
		{name: "nil response", missing: true},
		{name: "negative ID", response: &videov2.UploadVideoInfoResp{VideoId: -1}, missing: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &metadataClient{response: tc.response, err: tc.err}
			repo := NewVideoInfoRepo(&Data{videoInfoClient: client}, log.NewStdLogger(io.Discard))
			info := &domain.VideoDetail{
				PublisherId: 42, PublisherName: "publisher", Title: "test video", Description: "description",
				Classes: []string{"class"}, Tags: []string{"tag"}, Hot: 1, Duration: "00:01:00",
			}
			videoID, err := repo.UploadVideoInfo(context.Background(), info)
			wantRequest := &videov2.UploadVideoInfoReq{VideoInfo: &videov2.VideoMetaInfo{
				PublisherId: info.PublisherId, PublisherName: info.PublisherName, Title: info.Title,
				Description: info.Description, Classes: info.Classes, Tags: info.Tags, Hot: info.Hot, Duration: info.Duration,
			}}
			if client.calls != 1 || !proto.Equal(client.request, wantRequest) {
				t.Fatal("metadata mapping or RPC call count changed")
			}
			if tc.err != nil {
				if !errors.Is(err, rpcErr) || videoID != 0 {
					t.Fatal("RPC failure must return the original error and zero ID")
				}
			} else if tc.missing {
				if err == nil || videoID != 0 || kerrors.FromError(err).Reason != "VIDEO_ID_MISSING" {
					t.Fatal("missing or invalid upstream ID must not be accepted")
				}
			} else if err != nil || videoID != tc.response.VideoId {
				t.Fatal("repo did not return the upstream video ID")
			}
		})
	}
}

// TestUploadVideoInfoGatewayResponse 验证上传调用链返回已保存的视频 ID，JSON 不丢失大整数精度且失败不自动重试。
func TestUploadVideoInfoGatewayResponse(t *testing.T) {
	for _, tc := range []struct {
		name    string
		videoID int64
	}{
		{name: "success", videoID: 9007199254740993},
		{name: "old service response"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logger := log.NewStdLogger(io.Discard)
			client := &metadataClient{response: &videov2.UploadVideoInfoResp{VideoId: tc.videoID}}
			repo := NewVideoInfoRepo(&Data{videoInfoClient: client}, logger)
			svc := video.NewVideoInfoService(videobiz.NewVideoInfoUsecase(repo, logger), logger)
			response, err := svc.UploadVideoInfo(context.Background(), &videov1.UploadVideoInfoReq{
				VideoInfo: &videov1.VideoMetaInfo{Title: "test", PublisherId: 42},
			})
			if client.calls != 1 {
				t.Fatal("metadata creation must not be retried automatically")
			}
			if tc.videoID == 0 {
				if err == nil || response != nil {
					t.Fatal("old service must not produce a misleading success response")
				}
				return
			}
			if err != nil || response == nil || response.VideoId != tc.videoID || response.GetResp().GetStatusCode() != 200 || response.Resp.Message != "success" {
				t.Fatal("gateway response must contain the original CommonResp and saved ID")
			}
			request := httptest.NewRequest("POST", "/api/v1/video/info", nil)
			request.Header.Set("Accept", "application/json")
			writer := httptest.NewRecorder()
			if err := codecs.ResponseEncoder(writer, request, response); err != nil {
				t.Fatal(err)
			}
			var body struct {
				VideoID string `json:"videoId"`
			}
			if err := json.Unmarshal(writer.Body.Bytes(), &body); err != nil || body.VideoID != strconv.FormatInt(tc.videoID, 10) {
				t.Fatal("HTTP JSON must preserve the video ID as a string")
			}
		})
	}
}
