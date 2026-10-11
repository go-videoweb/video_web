package videodata

import (
	"bytes"
	"context"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-kratos/kratos/v2/log"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	"vw_gateway"
	api "vw_gateway/api/v1/video/videoinfo"
	"vw_gateway/internal/biz/videobiz"
	"vw_gateway/internal/conf"
	"vw_gateway/internal/pkg/ecode/errdef/uerr"
	"vw_gateway/internal/server"
	videoHandler "vw_gateway/internal/service/video_service"
	video "vw_video/api/v1/videoinfo"
)

type dashClientStream[T any] struct {
	grpc.ClientStream
	ctx       context.Context
	messages  []*T
	err       error
	afterRecv func()
}

func (s *dashClientStream[T]) Context() context.Context { return s.ctx }
func (s *dashClientStream[T]) Recv() (*T, error) {
	if len(s.messages) == 0 {
		if s.err != nil {
			return nil, s.err
		}
		return nil, io.EOF
	}
	m := s.messages[0]
	s.messages = s.messages[1:]
	if s.afterRecv != nil {
		s.afterRecv()
	}
	return m, nil
}

type dashVideoClient struct {
	video.VideoInfoClient
	mpd            string
	segments       map[string][]byte
	ctx            context.Context
	segmentReq     *video.GetVideoSegmentReq
	mpdID, coverID int64
	calls          int
}

func (c *dashVideoClient) GetVideoMpd(ctx context.Context, req *video.GetVideoMpdReq, _ ...grpc.CallOption) (grpc.ServerStreamingClient[video.GetVideoMpdResp], error) {
	c.ctx, c.mpdID = ctx, req.VideoId
	c.calls++
	return &dashClientStream[video.GetVideoMpdResp]{ctx: ctx, messages: []*video.GetVideoMpdResp{
		{MpdFile: &video.FileResp{File: &video.FileResp_Filename{Filename: "dash.mpd"}}},
		{MpdFile: &video.FileResp{File: &video.FileResp_FileData{FileData: []byte(c.mpd)}}},
	}}, nil
}

func (c *dashVideoClient) GetVideoSegments(ctx context.Context, req *video.GetVideoSegmentReq, _ ...grpc.CallOption) (grpc.ServerStreamingClient[video.GetVideoSegmentResp], error) {
	c.ctx, c.segmentReq = ctx, req
	c.calls++
	data, ok := c.segments[req.SegmentName]
	if !ok {
		return nil, errors.New("segment not found")
	}
	return &dashClientStream[video.GetVideoSegmentResp]{ctx: ctx, messages: []*video.GetVideoSegmentResp{
		{SegmentFile: &video.FileResp{File: &video.FileResp_Filename{Filename: req.SegmentName}}},
		{SegmentFile: &video.FileResp{File: &video.FileResp_FileData{FileData: data}}},
	}}, nil
}

func (c *dashVideoClient) GetVideoCover(ctx context.Context, req *video.GetVideoCoverReq, _ ...grpc.CallOption) (grpc.ServerStreamingClient[video.GetVideoCoverResp], error) {
	c.ctx, c.coverID = ctx, req.VideoId
	c.calls++
	return &dashClientStream[video.GetVideoCoverResp]{ctx: ctx, messages: []*video.GetVideoCoverResp{
		{CoverFile: &video.FileResp{File: &video.FileResp_Filename{Filename: "cover.png"}}},
		{CoverFile: &video.FileResp{File: &video.FileResp_FileData{FileData: []byte("cover")}}},
	}}, nil
}

// isolateDASHTemp 将网关下载测试的临时文件隔离到测试目录，结束后恢复资源根目录。
// 输出：隔离目录；测试不得并行运行，不创建或清理真实 resources/tmp。
func isolateDASHTemp(t *testing.T) string {
	t.Helper()
	old := vw_gateway.ResourcePath
	vw_gateway.ResourcePath = t.TempDir()
	t.Cleanup(func() { vw_gateway.ResourcePath = old })
	return vw_gateway.ResourcePath
}

// TestDASHAnonymousHTTP 验证无凭证及无效凭证均可经真实 HTTP 路由获取 MPD、相对分片和封面。
// 路径 user_id 不影响 RPC 视频 ID，成功响应后临时文件应删除；列表、原文件和互动仍要求登录。
func TestDASHAnonymousHTTP(t *testing.T) {
	root := isolateDASHTemp(t)
	client := &dashVideoClient{
		mpd:      `<MPD xmlns="urn:mpeg:dash:schema:mpd:2011"><Period><AdaptationSet><Representation id="0"><SegmentTemplate initialization="init-stream$RepresentationID$.m4s" media="chunk-stream$RepresentationID$-$Number%05d$.m4s"/></Representation></AdaptationSet></Period></MPD>`,
		segments: map[string][]byte{"init-stream0.m4s": {0, 1, 2}, "chunk-stream0-00001.m4s": bytes.Repeat([]byte("x"), 1025)},
	}
	logger := log.NewStdLogger(io.Discard)
	repo := NewVideoInfoRepo(&Data{videoInfoClient: client}, logger)
	svc := videoHandler.NewVideoInfoService(videobiz.NewVideoInfoUsecase(repo, logger), logger)
	srv := server.NewHTTPServer(&conf.Server{Http: &conf.Server_HTTP{}}, &conf.JWT{Secret: "test"}, nil, nil, nil, nil, nil, nil, nil, svc, nil, nil, logger)
	get := func(path string) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Authorization", "Bearer invalid")
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, req)
		return w
	}
	base, _ := url.Parse("http://example/api/v1/999/video/42/dash/mpd")
	w := get(base.String())
	if w.Code != http.StatusOK || w.Body.String() != client.mpd || w.Header().Get("Content-Type") != "application/dash+xml" || client.mpdID != 42 {
		t.Fatalf("MPD failed: %d %s", w.Code, w.Body.String())
	}
	var manifest struct {
		Period struct {
			AdaptationSet struct {
				Representation struct {
					ID       string `xml:"id,attr"`
					Template struct {
						Initialization string `xml:"initialization,attr"`
						Media          string `xml:"media,attr"`
					} `xml:"SegmentTemplate"`
				} `xml:"Representation"`
			} `xml:"AdaptationSet"`
		} `xml:"Period"`
	}
	if err := xml.Unmarshal(w.Body.Bytes(), &manifest); err != nil {
		t.Fatal(err)
	}
	representation := manifest.Period.AdaptationSet.Representation
	for _, template := range []string{representation.Template.Initialization, representation.Template.Media} {
		name := strings.ReplaceAll(strings.ReplaceAll(template, "$RepresentationID$", representation.ID), "$Number%05d$", fmt.Sprintf("%05d", 1))
		relative, err := url.Parse(name)
		if err != nil {
			t.Fatal(err)
		}
		segment := get(base.ResolveReference(relative).String())
		if segment.Code != http.StatusOK || !bytes.Equal(segment.Body.Bytes(), client.segments[name]) || client.segmentReq.VideoId != 42 || client.segmentReq.SegmentName != name || client.segmentReq.VideoSegmentPath != "" {
			t.Fatalf("relative segment failed: %s %s", name, segment.Body.String())
		}
	}
	cover := get("/api/v1/123/video/42/cover")
	if cover.Code != http.StatusOK || cover.Body.String() != "cover" || cover.Header().Get("Content-Type") != "image/png" || client.coverID != 42 {
		t.Fatal("anonymous cover failed")
	}
	// 不带任何凭证的请求同样应直接进入公共媒体调用链。
	plain := httptest.NewRecorder()
	srv.ServeHTTP(plain, httptest.NewRequest(http.MethodGet, base.String(), nil))
	if plain.Body.String() != client.mpd {
		t.Fatal("missing credentials blocked MPD")
	}
	entries, err := os.ReadDir(filepath.Join(root, "tmp"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("successful response leaked temporary files: %v", err)
	}
	for _, path := range []string{"/api/v1/video/list", "/api/v1/999/video/42/file"} {
		response := get(path)
		var body struct {
			Code int32 `json:"code"`
		}
		if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil || body.Code != uerr.ErrParseTokenFailed.Code {
			t.Fatalf("protected route opened: %s", path)
		}
	}
	before := client.calls
	old := get("/api/v1/999/video/42/dash/chunk-stream0-00001.m4s?segment_path=C:/private/file")
	var body struct {
		Reason string `json:"reason"`
	}
	if err := json.Unmarshal(old.Body.Bytes(), &body); err != nil || body.Reason != "SEGMENT_PATH_UNSUPPORTED" || client.calls != before {
		t.Fatal("legacy path reached RPC")
	}
	if oldRoute := get("/api/v1/999/video/42/mpd"); oldRoute.Code != http.StatusNotFound {
		t.Fatal("obsolete route still registered")
	}
}

// TestDASHRPCContext 验证分片 RPC 只传递视频 ID 和文件名，且保留父请求取消与自定义上下文。
func TestDASHRPCContext(t *testing.T) {
	isolateDASHTemp(t)
	client := &dashVideoClient{segments: map[string][]byte{"init-stream0.m4s": []byte("init")}}
	repo := NewVideoInfoRepo(&Data{videoInfoClient: client}, log.NewStdLogger(io.Discard))
	type key struct{}
	ctx, cancel := context.WithCancel(context.WithValue(context.Background(), key{}, "parent"))
	defer cancel()
	resp, err := repo.GetVideoSegment(ctx, 42, "init-stream0.m4s")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(resp.FilePath)
	if client.ctx.Value(key{}) != "parent" || client.segmentReq.VideoId != 42 || client.segmentReq.VideoSegmentPath != "" {
		t.Fatal("RPC identity or context changed")
	}
	cancel()
	if !errors.Is(client.ctx.Err(), context.Canceled) {
		t.Fatal("RPC detached from parent")
	}
}

// TestDASHReceiveFailureCleanup 验证中途接收失败、非法消息和取消请求不会在临时目录留下残片。
func TestDASHReceiveFailureCleanup(t *testing.T) {
	root := isolateDASHTemp(t)
	meta := &video.FileResp{File: &video.FileResp_Filename{Filename: "chunk.m4s"}}
	data := &video.FileResp{File: &video.FileResp_FileData{FileData: []byte("partial")}}
	getName := func(msg *video.FileResp) (string, error) {
		if name, ok := msg.GetFile().(*video.FileResp_Filename); ok {
			return name.Filename, nil
		}
		return "", errors.New("missing filename")
	}
	getData := func(msg *video.FileResp) ([]byte, error) {
		if data, ok := msg.GetFile().(*video.FileResp_FileData); ok {
			return data.FileData, nil
		}
		return nil, errors.New("missing data")
	}
	for _, tc := range []struct {
		name     string
		messages []*video.FileResp
		err      error
		cancel   bool
	}{
		{"interrupted", []*video.FileResp{meta, data}, errors.New("interrupted"), false},
		{"bad data", []*video.FileResp{meta, data, nil}, nil, false},
		{"bad first message", []*video.FileResp{nil}, nil, false},
		{"bad filename", []*video.FileResp{{File: &video.FileResp_Filename{Filename: "../x.m4s"}}}, nil, false},
		{"canceled", []*video.FileResp{meta, data}, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			s := &dashClientStream[video.FileResp]{ctx: ctx, messages: tc.messages, err: tc.err}
			if tc.cancel {
				s.afterRecv = cancel
			}
			if resp, err := receiveStreamFile(s, getName, getData); err == nil || resp != nil {
				t.Fatal("invalid stream succeeded")
			}
			entries, err := os.ReadDir(filepath.Join(root, "tmp"))
			if err != nil && !os.IsNotExist(err) {
				t.Fatal(err)
			}
			if len(entries) != 0 {
				t.Fatal("failed stream leaked temporary file")
			}
		})
	}
}

// TestDASHGatewayFieldMigration 验证旧路径字段保持编号和类型，新增分片名不覆盖旧契约。
func TestDASHGatewayFieldMigration(t *testing.T) {
	legacy := protowire.AppendString(protowire.AppendTag(nil, 3, protowire.BytesType), "old/path")
	var req api.ProvideSegmentsReq
	if err := proto.Unmarshal(legacy, &req); err != nil || req.SegmentPath != "old/path" || req.SegmentName != "" {
		t.Fatal("old path reinterpreted as new name")
	}
	for _, tc := range []struct {
		name string
		num  protoreflect.FieldNumber
	}{{"user_id", 1}, {"video_id", 2}, {"segment_path", 3}, {"segment_name", 4}} {
		field := req.ProtoReflect().Descriptor().Fields().ByName(protoreflect.Name(tc.name))
		if field == nil || field.Number() != tc.num {
			t.Fatalf("field number changed: %s", tc.name)
		}
	}
}
