package biz

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-kratos/kratos/v2/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	video "vw_video/api/v1/videoinfo"
)

type dashRepo struct {
	VideoInfoRepo
	ctx       context.Context
	videoID   int64
	calls     int
	publisher int64
	err       error
	videoFile string
}

func (r *dashRepo) GetPublisherIdByVideoId(ctx context.Context, id int64) (int64, error) {
	r.ctx, r.videoID = ctx, id
	r.calls++
	return r.publisher, r.err
}

func (r *dashRepo) GetVideoFilePathById(ctx context.Context, id int64) (string, error) {
	r.ctx, r.videoID = ctx, id
	r.calls++
	return r.videoFile, r.err
}

type dashStream[T any] struct {
	grpc.ServerStream
	ctx       context.Context
	messages  []*T
	sendErr   error
	failAt    int
	afterSend func()
}

func (s *dashStream[T]) Context() context.Context { return s.ctx }
func (s *dashStream[T]) Send(msg *T) error {
	if s.sendErr != nil && len(s.messages) == s.failAt {
		return s.sendErr
	}
	s.messages = append(s.messages, msg)
	if s.afterSend != nil {
		s.afterSend()
	}
	return nil
}

// newDASHFixture 将下载测试隔离到临时资源目录，不读写项目真实媒体文件。
// 输出：视频目录和仓储替身；测试结束后恢复资源根目录，测试不得并行运行。
func newDASHFixture(t *testing.T) (string, *dashRepo) {
	t.Helper()
	old := resourcePath
	resourcePath = t.TempDir()
	t.Cleanup(func() { resourcePath = old })
	dir := filepath.Join(resourcePath, "7", videoPath, "42")
	if err := os.MkdirAll(filepath.Join(dir, dashPath), 0700); err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string]string{
		"dash/dash.mpd": "<MPD/>", "dash/init-stream0.m4s": "init", "dash/chunk-stream0-00001.m4s": "chunk",
		"cover.png": "cover", "video.mp4": "video",
	} {
		if err := os.WriteFile(filepath.Join(dir, filepath.FromSlash(name)), []byte(data), 0600); err != nil {
			t.Fatal(err)
		}
	}
	return dir, &dashRepo{publisher: 7, videoFile: filepath.Join(dir, "video.mp4")}
}

// TestDASHDownloads 验证实际发布者决定存储位置，四种下载均传递流上下文并返回真实文件名和完整内容。
func TestDASHDownloads(t *testing.T) {
	_, repo := newDASHFixture(t)
	uc := NewVideoInfoUsecase(repo, nil, log.NewStdLogger(io.Discard))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	mpd := &dashStream[video.GetVideoMpdResp]{ctx: ctx}
	if err := uc.GetVideoMpd(42, mpd); err != nil {
		t.Fatal(err)
	}
	if len(mpd.messages) != 2 || mpd.messages[0].GetMpdFile().GetFilename() != "dash.mpd" || string(mpd.messages[1].GetMpdFile().GetFileData()) != "<MPD/>" {
		t.Fatal("incorrect MPD stream")
	}
	for _, name := range []string{"init-stream0.m4s", "chunk-stream0-00001.m4s"} {
		s := &dashStream[video.GetVideoSegmentResp]{ctx: ctx}
		if err := uc.GetVideoSegment(42, name, s); err != nil {
			t.Fatal(err)
		}
		want := "chunk"
		if strings.HasPrefix(name, "init-") {
			want = "init"
		}
		if len(s.messages) != 2 || s.messages[0].GetSegmentFile().GetFilename() != name || string(s.messages[1].GetSegmentFile().GetFileData()) != want {
			t.Fatal("incorrect segment stream")
		}
	}
	cover := &dashStream[video.GetVideoCoverResp]{ctx: ctx}
	if err := uc.GetVideoCover(42, cover); err != nil {
		t.Fatal(err)
	}
	if len(cover.messages) != 2 || cover.messages[0].GetCoverFile().GetFilename() != "cover.png" || string(cover.messages[1].GetCoverFile().GetFileData()) != "cover" {
		t.Fatal("incorrect cover stream")
	}
	file := &dashStream[video.GetVideoFileResp]{ctx: ctx}
	if err := uc.GetVideoFile(42, file); err != nil {
		t.Fatal(err)
	}
	if len(file.messages) != 2 || file.messages[0].GetVideoFile().GetFilename() != "video.mp4" || string(file.messages[1].GetVideoFile().GetFileData()) != "video" {
		t.Fatal("incorrect video stream")
	}
	if repo.ctx != ctx || repo.videoID != 42 || repo.calls != 5 {
		t.Fatal("request context or video ID not propagated")
	}
}

// TestDASHSegmentValidation 验证路径穿越、平台分隔符、编码路径和非分片文件在访问仓储前被拒绝。
func TestDASHSegmentValidation(t *testing.T) {
	_, repo := newDASHFixture(t)
	uc := NewVideoInfoUsecase(repo, nil, log.NewStdLogger(io.Discard))
	for _, name := range []string{"", ".", "..", "../cover.png", "a/b.m4s", `a\b.m4s`, "/tmp/a.m4s", `C:\a.m4s`, "a:secret.m4s", "%2e%2e%2fa.m4s", "a%252fb.m4s", "a.m4s?x=1", "a.m4s\x00", "dash.mpd", "cover.png", "a.m4s.", "a.m4s ", strings.Repeat("a", 256) + ".m4s"} {
		s := &dashStream[video.GetVideoSegmentResp]{ctx: context.Background()}
		if err := uc.GetVideoSegment(42, name, s); status.Code(err) != codes.InvalidArgument || len(s.messages) != 0 {
			t.Fatalf("accepted invalid name %q: %v", name, err)
		}
	}
	for _, id := range []int64{0, -1} {
		if err := uc.GetVideoSegment(id, "init-stream0.m4s", &dashStream[video.GetVideoSegmentResp]{ctx: context.Background()}); status.Code(err) != codes.InvalidArgument {
			t.Fatal(err)
		}
		if err := uc.GetVideoMpd(id, &dashStream[video.GetVideoMpdResp]{ctx: context.Background()}); status.Code(err) != codes.InvalidArgument {
			t.Fatal(err)
		}
		if err := uc.GetVideoCover(id, &dashStream[video.GetVideoCoverResp]{ctx: context.Background()}); status.Code(err) != codes.InvalidArgument {
			t.Fatal(err)
		}
	}
	if repo.calls != 0 {
		t.Fatal("invalid input reached storage")
	}
}

// TestDASHDownloadErrors 验证查找和传输失败不会变成成功响应，也不发送不存在文件的元信息。
func TestDASHDownloadErrors(t *testing.T) {
	dir, repo := newDASHFixture(t)
	uc := NewVideoInfoUsecase(repo, nil, log.NewStdLogger(io.Discard))
	ctx := context.Background()
	for _, name := range []string{"missing.m4s", "directory.m4s"} {
		if name == "directory.m4s" {
			if err := os.Mkdir(filepath.Join(dir, dashPath, name), 0700); err != nil {
				t.Fatal(err)
			}
		}
		s := &dashStream[video.GetVideoSegmentResp]{ctx: ctx}
		if err := uc.GetVideoSegment(42, name, s); err == nil || len(s.messages) != 0 {
			t.Fatal("missing or nonregular segment was sent")
		}
	}
	if err := os.Remove(filepath.Join(dir, "cover.png")); err != nil {
		t.Fatal(err)
	}
	cover := &dashStream[video.GetVideoCoverResp]{ctx: ctx}
	if err := uc.GetVideoCover(42, cover); err == nil || len(cover.messages) != 0 {
		t.Fatal("missing cover was sent")
	}
	repo.err = errors.New("lookup failed")
	mpd := &dashStream[video.GetVideoMpdResp]{ctx: ctx}
	if err := uc.GetVideoMpd(42, mpd); err == nil || len(mpd.messages) != 0 {
		t.Fatal("lookup error swallowed")
	}
	repo.err = nil
	repo.publisher = 0
	if err := uc.GetVideoSegment(42, "init-stream0.m4s", &dashStream[video.GetVideoSegmentResp]{ctx: ctx}); err == nil {
		t.Fatal("invalid publisher accepted")
	}
}

// TestDASHSegmentSymlinkEscape 验证分片符号链接指向 DASH 目录外时拒绝读取；无创建权限的平台跳过此场景。
func TestDASHSegmentSymlinkEscape(t *testing.T) {
	dir, repo := newDASHFixture(t)
	link := filepath.Join(dir, dashPath, "escape.m4s")
	if err := os.Symlink(filepath.Join(dir, "cover.png"), link); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	uc := NewVideoInfoUsecase(repo, nil, log.NewStdLogger(io.Discard))
	s := &dashStream[video.GetVideoSegmentResp]{ctx: context.Background()}
	if err := uc.GetVideoSegment(42, "escape.m4s", s); status.Code(err) != codes.PermissionDenied || len(s.messages) != 0 {
		t.Fatalf("symlink escaped: %v", err)
	}
}

// TestSendFileStreamErrorsAndCancellation 验证元信息与数据发送失败、请求取消和多块数据传输的边界。
func TestSendFileStreamErrorsAndCancellation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "large.m4s")
	payload := bytes.Repeat([]byte("x"), mb+17)
	if err := os.WriteFile(path, payload, 0600); err != nil {
		t.Fatal(err)
	}
	meta := func(name string) *video.FileResp {
		return &video.FileResp{File: &video.FileResp_Filename{Filename: name}}
	}
	data := func(b []byte) *video.FileResp {
		return &video.FileResp{File: &video.FileResp_FileData{FileData: bytes.Clone(b)}}
	}
	wantErr := errors.New("send failed")
	for _, failAt := range []int{0, 1} {
		s := &dashStream[video.FileResp]{ctx: context.Background(), sendErr: wantErr, failAt: failAt}
		if err := sendFileStream(s, path, meta, data); !errors.Is(err, wantErr) {
			t.Fatalf("send error lost: %v", err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	s := &dashStream[video.FileResp]{ctx: ctx, afterSend: cancel}
	if err := sendFileStream(s, path, meta, data); !errors.Is(err, context.Canceled) || len(s.messages) != 1 {
		t.Fatal("canceled transfer continued")
	}
	canceled := &dashStream[video.FileResp]{ctx: ctx}
	if err := sendFileStream(canceled, path, meta, data); !errors.Is(err, context.Canceled) || len(canceled.messages) != 0 {
		t.Fatal("pre-canceled transfer started")
	}
	success := &dashStream[video.FileResp]{ctx: context.Background()}
	if err := sendFileStream(success, path, meta, data); err != nil {
		t.Fatal(err)
	}
	var got []byte
	for _, msg := range success.messages[1:] {
		got = append(got, msg.GetFileData()...)
	}
	if !bytes.Equal(got, payload) || len(success.messages) != 3 {
		t.Fatal("multi-block file truncated")
	}
}
