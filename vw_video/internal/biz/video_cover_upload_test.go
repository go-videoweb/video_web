package biz

import (
	"context"
	"errors"
	"io"
	"os"
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
	video      model.Video
	phase      string
	statusErr  error
	txCtx      context.Context
	rejectMove bool
	writeErr   error
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
func (r *coverUploadRepo) UpdateVideoUploadStatus(ctx context.Context, id int64, from, to string, _ time.Time) (bool, error) {
	if ctx != r.txCtx || id != r.video.VideoID || from != r.phase || to != videoUploadWaitingVideo {
		return false, errors.New("状态更新的事务或参数无效")
	}
	if r.rejectMove {
		return false, nil
	}
	r.phase = to
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
	phase, path := r.phase, r.video.CoverPath
	err := fn(txCtx)
	if err != nil {
		r.phase, r.video.CoverPath = phase, path
	}
	r.txCtx = nil
	return err
}

type uploadTestStream[T any] struct {
	grpc.ServerStream
	requests []*T
	afterEOF error
	reply    func() error
}

// Context 返回测试上传流的上下文。
func (*uploadTestStream[T]) Context() context.Context { return context.Background() }

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
		publisher int64
		phase     string
		statusErr error
		want      *kerrors.Error
	}{
		{publisher: 7, phase: videoUploadWaitingCover, want: errdef.ErrVideoCoverRequired},
		{publisher: 7, statusErr: gorm.ErrRecordNotFound, want: errdef.ErrVideoFileUploadStage},
		{publisher: 8, phase: videoUploadWaitingVideo, want: errdef.ErrVideoUploadForbidden},
	} {
		repo := &coverUploadRepo{
			video:     model.Video{VideoID: 42, PublisherID: 7, CoverPath: filepath.Join(dir, "cover.png")},
			phase:     tc.phase,
			statusErr: tc.statusErr,
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
	if _, err := os.Stat(filepath.Join(dir, "投稿.mp4")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("原片残留：%v", err)
	}
	if data, err := os.ReadFile(coverPath); err != nil || string(data) != "cover" {
		t.Fatalf("封面受原片清理影响：%v", err)
	}
}
