package biz

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
	"gorm.io/gorm"
	video "vw_video/api/v1/videoinfo"
	"vw_video/internal/data/dal/model"
	"vw_video/internal/pkg/ecode/errdef"
)

type coverUploadRepo struct {
	VideoInfoRepo
	video        model.Video
	phase        string
	statusErr    error
	errorMessage string
	txCtx        context.Context
	rejectMove   bool
	writeErr     error
}

// GetVideoInfoById 返回测试视频的已提交信息。
func (r *coverUploadRepo) GetVideoInfoById(context.Context, int64) (*model.Video, error) {
	return &r.video, nil
}

// GetPublisherIdByVideoId 返回测试视频的发布者。
func (r *coverUploadRepo) GetPublisherIdByVideoId(context.Context, int64) (int64, error) {
	return r.video.PublisherID, nil
}

// GetVideoUploadStatus 返回测试视频的上传阶段或预设查询错误。
func (r *coverUploadRepo) GetVideoUploadStatus(context.Context, int64) (*model.VideoUploadStatus, error) {
	if r.statusErr != nil {
		return nil, r.statusErr
	}
	return &model.VideoUploadStatus{Status: r.phase}, nil
}

// UpdateVideoUploadStatus 模拟旧状态已被其他请求改变，或在测试事务内更新状态。
func (r *coverUploadRepo) UpdateVideoUploadStatus(ctx context.Context, id int64, from, to, errorMessage string, _ time.Time) (bool, error) {
	if from == videoUploadWaitingCover || to == videoUploadCompleted {
		if r.txCtx == nil || ctx != r.txCtx {
			return false, errors.New("状态更新缺少事务")
		}
	}
	if id != r.video.VideoID || from != r.phase || r.rejectMove {
		return false, nil
	}
	r.phase, r.errorMessage = to, errorMessage
	return true, nil
}

// SetCoverPath 在测试事务内保存封面路径，支持模拟写入失败。
func (r *coverUploadRepo) SetCoverPath(ctx context.Context, id int64, path string) error {
	if ctx != r.txCtx || id != r.video.VideoID {
		return errors.New("封面更新的事务或视频 ID 无效")
	}
	r.video.CoverPath = path
	return r.writeErr
}

// WithTx 模拟本地事务及回滚，使测试可检查回执时的最终状态。
func (r *coverUploadRepo) WithTx(ctx context.Context, fn func(context.Context) error) error {
	txCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	r.txCtx = txCtx
	phase, path, message := r.phase, r.video.CoverPath, r.errorMessage
	err := fn(txCtx)
	if err != nil {
		r.phase, r.video.CoverPath, r.errorMessage = phase, path, message
	}
	r.txCtx = nil
	return err
}

type uploadTestStream[T any] struct {
	grpc.ServerStream
	ctx      context.Context
	requests []*T
	afterEOF error
	reply    func() error
}

// Context 返回测试上传流的上下文。
func (s *uploadTestStream[T]) Context() context.Context {
	if s.ctx == nil {
		return context.Background()
	}
	return s.ctx
}

// Recv 依次返回上传消息，末尾可模拟中断。
func (s *uploadTestStream[T]) Recv() (*T, error) {
	if len(s.requests) == 0 {
		if s.afterEOF != nil {
			return nil, s.afterEOF
		}
		return nil, io.EOF
	}
	req := s.requests[0]
	s.requests = s.requests[1:]
	return req, nil
}

// SendAndClose 调用测试回执函数。
func (s *uploadTestStream[T]) SendAndClose(*emptypb.Empty) error { return s.reply() }

// TestUploadVideoCoverCommit 验证提交后才回执，失败只清理本次封面，下载使用提交路径。
func TestUploadVideoCoverCommit(t *testing.T) {
	for _, scenario := range []string{"成功", "回执失败", "保存失败", "阶段冲突"} {
		t.Run(scenario, func(t *testing.T) {
			old := resourcePath
			resourcePath = t.TempDir()
			t.Cleanup(func() { resourcePath = old })
			repo := &coverUploadRepo{video: model.Video{VideoID: 42, PublisherID: 7}, phase: videoUploadWaitingCover}
			fault := errors.New("模拟失败")
			if scenario == "保存失败" {
				repo.writeErr = fault
			}
			repo.rejectMove = scenario == "阶段冲突"
			uc := NewVideoInfoUsecase(repo, repo, log.NewStdLogger(io.Discard))
			replied := false
			stream := &uploadTestStream[video.UploadVideoCoverReq]{
				requests: []*video.UploadVideoCoverReq{
					{CoverFile: &video.UploadVideoCoverReq_CoverMetadata{CoverMetadata: &video.UploadVideoCoverReq_CoverInfo{
						VideoId: 42, PublisherId: 7, CoverFileName: "投稿.png",
					}}},
					{CoverFile: &video.UploadVideoCoverReq_CoverFileData{CoverFileData: []byte("封面数据")}},
				},
				reply: func() error {
					replied = true
					if repo.txCtx != nil || repo.phase != videoUploadWaitingVideo || repo.video.CoverPath == "" {
						t.Fatal("封面提交之前就发送了回执")
					}
					if scenario == "回执失败" {
						return fault
					}
					return nil
				},
			}
			err := uc.UploadVideoCover(stream)
			if (err == nil) != (scenario == "成功") {
				t.Fatalf("上传结果不符：%v", err)
			}
			if scenario == "阶段冲突" && !errors.Is(err, errdef.ErrVideoCoverUploadStage) {
				t.Fatalf("事务返回的阶段错误未保留：%v", err)
			}
			if scenario == "保存失败" || scenario == "阶段冲突" {
				entries, err := os.ReadDir(filepath.Join(resourcePath, "7", videoPath, "42", "covers"))
				if err != nil || len(entries) != 0 || replied || repo.phase != videoUploadWaitingCover || repo.video.CoverPath != "" {
					t.Fatalf("失败后的文件或事务状态不符：files=%v err=%v", entries, err)
				}
				return
			}
			download := &dashStream[video.GetVideoCoverResp]{ctx: context.Background()}
			if err = uc.GetVideoCover(42, download); err != nil || len(download.messages) != 2 ||
				string(download.messages[1].GetCoverFile().GetFileData()) != "封面数据" {
				t.Fatalf("提交后的封面无法下载：%v", err)
			}
		})
	}
}

// TestUploadVideoFileRequiresWaitingVideo 验证发布者必须处于待传原片阶段才可继续发送文件。
func TestUploadVideoFileRequiresWaitingVideo(t *testing.T) {
	dir, _ := newDASHFixture(t)
	for _, tc := range []struct {
		publisher  int64
		phase      string
		statusErr  error
		rejectMove bool
		want       *kerrors.Error
	}{
		{publisher: 7, phase: videoUploadWaitingCover, want: errdef.ErrVideoCoverRequired},
		{publisher: 7, statusErr: gorm.ErrRecordNotFound, want: errdef.ErrVideoFileUploadStage},
		{publisher: 7, phase: videoUploadWaitingVideo, rejectMove: true, want: errdef.ErrVideoFileUploadStage},
		{publisher: 8, phase: videoUploadWaitingVideo, want: errdef.ErrVideoUploadForbidden},
	} {
		repo := &coverUploadRepo{
			video:      model.Video{VideoID: 42, PublisherID: 7, CoverPath: filepath.Join(dir, "cover.png")},
			phase:      tc.phase,
			statusErr:  tc.statusErr,
			rejectMove: tc.rejectMove,
		}
		uc := NewVideoInfoUsecase(repo, nil, log.NewStdLogger(io.Discard))
		stream := &uploadTestStream[video.UploadVideoFileReq]{
			requests: []*video.UploadVideoFileReq{
				{VideoFile: &video.UploadVideoFileReq_VideoMetadata{VideoMetadata: &video.UploadVideoFileReq_VideoFileInfo{
					VideoId: 42, PublisherId: tc.publisher, VideoFileName: "投稿.mp4",
				}}},
				{VideoFile: &video.UploadVideoFileReq_VideoFileData{VideoFileData: []byte("原片数据")}},
			},
		}
		err := uc.UploadVideoFile(stream)
		if !errors.Is(err, tc.want) || len(stream.requests) != 1 {
			t.Fatalf("原片上传没有在元信息阶段拒绝：%v", err)
		}
	}
}

// TestUploadVideoFileAbortPreservesCover 验证原片中断只删除原片，保留已经提交的封面。
func TestUploadVideoFileAbortPreservesCover(t *testing.T) {
	dir, _ := newDASHFixture(t)
	coverPath := filepath.Join(dir, "cover.png")
	repo := &coverUploadRepo{
		video: model.Video{VideoID: 42, PublisherID: 7},
		phase: videoUploadWaitingVideo,
	}
	uc := NewVideoInfoUsecase(repo, nil, log.NewStdLogger(io.Discard))
	stream := &uploadTestStream[video.UploadVideoFileReq]{
		requests: []*video.UploadVideoFileReq{
			{VideoFile: &video.UploadVideoFileReq_VideoMetadata{VideoMetadata: &video.UploadVideoFileReq_VideoFileInfo{
				VideoId: 42, PublisherId: 7, VideoFileName: "投稿.mp4",
			}}},
			{VideoFile: &video.UploadVideoFileReq_VideoFileData{VideoFileData: []byte("原片数据")}},
		},
		afterEOF: errors.New("传输中断"),
	}
	if err := uc.UploadVideoFile(stream); err == nil || len(stream.requests) != 0 {
		t.Fatalf("封面就绪后未接收原片数据，或传输中断被当作成功：%v", err)
	}
	if repo.phase != videoUploadFailed || repo.errorMessage != "接收原片失败" {
		t.Fatalf("传输中断未记录失败状态：%s %s", repo.phase, repo.errorMessage)
	}
	if _, err := os.Stat(filepath.Join(dir, "投稿.mp4")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("原片残留：%v", err)
	}
	if data, err := os.ReadFile(coverPath); err != nil || string(data) != "cover" {
		t.Fatalf("封面受原片清理影响：%v", err)
	}
}

// TestReceiveVideoFileCreateFailure 验证占用上传阶段后创建文件失败也会记录失败状态，并保留已有目录。
func TestReceiveVideoFileCreateFailure(t *testing.T) {
	dir := t.TempDir()
	repo := &coverUploadRepo{video: model.Video{VideoID: 42}, phase: videoUploadUploading}
	uc := NewVideoInfoUsecase(repo, nil, log.NewStdLogger(io.Discard))
	stream := &uploadTestStream[video.UploadVideoFileReq]{}
	if err := uc.receiveVideoFile(stream, 42, dir); kerrors.Code(err) != int(errdef.ErrUploadVideoFileFailed.Code) {
		t.Fatalf("创建原片失败未返回上传错误：%v", err)
	}
	if repo.phase != videoUploadFailed || repo.errorMessage != "接收原片失败" {
		t.Fatalf("创建原片失败未记录状态：%s %s", repo.phase, repo.errorMessage)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Fatalf("创建失败不应清理已有目录：%v", err)
	}
}

// TestProcessVideoFileFailure 清理无法解析的原片，并把失败原因写入上传状态。
func TestProcessVideoFileFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid.mp4")
	if err := os.WriteFile(path, []byte("invalid video"), 0600); err != nil {
		t.Fatal(err)
	}
	repo := &coverUploadRepo{video: model.Video{VideoID: 42}, phase: videoUploadProcessing}
	uc := NewVideoInfoUsecase(repo, nil, log.NewStdLogger(io.Discard))
	uc.processVideoFile(42, path)
	if repo.phase != videoUploadFailed || repo.errorMessage != "视频处理失败" {
		t.Fatalf("处理失败未记录状态：%s %s", repo.phase, repo.errorMessage)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("失败原片未清理：%v", err)
	}
}

type dashUploadRepo struct {
	coverUploadRepo
	finish chan struct{}
	result chan error
}

// WithTx 暂停完成事务，验证上传回执可以先返回。
func (r *dashUploadRepo) WithTx(ctx context.Context, fn func(context.Context) error) error {
	<-r.finish
	err := r.coverUploadRepo.WithTx(ctx, fn)
	r.result <- err
	return err
}

// UpdateVideoMediaInfo 核对落盘数据和完成事务的上下文。
func (r *dashUploadRepo) UpdateVideoMediaInfo(ctx context.Context, id int64, path, duration string, size int64) error {
	if ctx != r.txCtx || ctx.Err() != nil || id != r.video.VideoID || duration != "00:00:01" || size <= 0 {
		return errors.New("完成事务的上下文或媒体信息错误")
	}
	r.video.VideoPath = path
	return nil
}

// TestUploadVideoFileProcessesAfterReply 验证回执早于完成事务，取消上传上下文后仍能生成真实 DASH。
func TestUploadVideoFileProcessesAfterReply(t *testing.T) {
	ffmpegPath, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("需要 ffmpeg")
	}
	if _, err = exec.LookPath("ffprobe"); err != nil {
		t.Skip("需要 ffprobe")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	input := filepath.Join(t.TempDir(), "input.mp4")
	cmd := exec.CommandContext(ctx, ffmpegPath, "-v", "error", "-f", "lavfi", "-i",
		"color=c=black:s=64x64:r=25:d=1", "-c:v", "libx264", "-pix_fmt", "yuv420p", input)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("生成测试视频失败：%v %s", err, output)
	}
	content, err := os.ReadFile(input)
	if err != nil {
		t.Fatal(err)
	}
	old := resourcePath
	resourcePath = t.TempDir()
	t.Cleanup(func() { resourcePath = old })
	repo := &dashUploadRepo{
		coverUploadRepo: coverUploadRepo{video: model.Video{VideoID: 42, PublisherID: 7}, phase: videoUploadWaitingVideo},
		finish:          make(chan struct{}),
		result:          make(chan error, 1),
	}
	uc := NewVideoInfoUsecase(repo, repo, log.NewStdLogger(io.Discard))
	reqCtx, cancelRequest := context.WithCancel(context.Background())
	defer cancelRequest()
	replyErr := errors.New("回执传输中断")
	stream := &uploadTestStream[video.UploadVideoFileReq]{
		ctx: reqCtx,
		requests: []*video.UploadVideoFileReq{
			{VideoFile: &video.UploadVideoFileReq_VideoMetadata{VideoMetadata: &video.UploadVideoFileReq_VideoFileInfo{
				VideoId: 42, PublisherId: 7, VideoFileName: "video.mp4",
			}}},
			{VideoFile: &video.UploadVideoFileReq_VideoFileData{VideoFileData: content}},
		},
		reply: func() error {
			cancelRequest()
			return replyErr
		},
	}
	rpcResult := make(chan error, 1)
	go func() { rpcResult <- uc.UploadVideoFile(stream) }()
	select {
	case err := <-rpcResult:
		close(repo.finish)
		if !errors.Is(err, replyErr) {
			t.Fatalf("上传回执错误未透传：%v", err)
		}
	case <-time.After(10 * time.Second):
		close(repo.finish)
		t.Fatal("上传 RPC 等待了后台完成事务")
	}
	select {
	case err := <-repo.result:
		if err != nil || repo.phase != videoUploadCompleted {
			t.Fatalf("请求取消后未完成后台处理：%s %v", repo.phase, err)
		}
		if _, err := os.Stat(filepath.Join(filepath.Dir(repo.video.VideoPath), dashPath, dashName)); err != nil {
			t.Fatalf("完成状态缺少 DASH 清单：%v", err)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("后台 DASH 未完成")
	}
}
