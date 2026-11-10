package service_test

import (
	"context"
	"errors"
	"io"
	"strconv"
	"testing"
	"time"
	"util/helper"
	videov1 "vw_video/api/v1/videoinfo"
	"vw_video/internal/biz"
	"vw_video/internal/data/dal/model"
	"vw_video/internal/pkg/ecode/errdef"
	"vw_video/internal/service"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"google.golang.org/grpc/status"
	"gorm.io/gorm"
)

type uploadStatusRepo struct {
	biz.VideoInfoRepo
	video           *model.Video
	videoErr        error
	uploadStatus    *model.VideoUploadStatus
	statusErr       error
	statusReadCount int
}

// GetVideoInfoById 返回测试设定的视频归属或查找错误。
func (r *uploadStatusRepo) GetVideoInfoById(context.Context, int64) (*model.Video, error) {
	return r.video, r.videoErr
}

// GetVideoUploadStatus 记录实际状态查询次数。
func (r *uploadStatusRepo) GetVideoUploadStatus(context.Context, int64) (*model.VideoUploadStatus, error) {
	r.statusReadCount++
	return r.uploadStatus, r.statusErr
}

// TestGetVideoUploadStatus 验证发布者鉴权、状态字段、已完成回退及业务码传输。
func TestGetVideoUploadStatus(t *testing.T) {
	updated := time.Date(2026, 11, 10, 8, 9, 10, 123000000, time.UTC)
	for _, tc := range []struct {
		name, videoPath, wantStatus string
		userID                      int64
		videoErr, statusErr         error
		wantError                   *kerrors.Error
		wantStatusReads             int
	}{
		{name: "保留失败原因和更新时间", userID: 7, wantStatus: "failed", wantStatusReads: 1},
		{name: "清理已完成状态后回退", userID: 7, videoPath: "video.mp4", statusErr: gorm.ErrRecordNotFound, wantStatus: "completed", wantStatusReads: 1},
		{name: "未完成视频缺少状态", userID: 7, statusErr: gorm.ErrRecordNotFound, wantError: errdef.ErrVideoUploadStatusNotFound, wantStatusReads: 1},
		{name: "状态查询故障", userID: 7, videoPath: "video.mp4", statusErr: errors.New("database unavailable"), wantError: errdef.ErrGetVideoUploadStatusFailed, wantStatusReads: 1},
		{name: "非发布者", userID: 8, wantError: errdef.ErrVideoNotFound},
		{name: "视频不存在", userID: 7, videoErr: gorm.ErrRecordNotFound, wantError: errdef.ErrVideoNotFound},
		{name: "缺少身份", wantError: errdef.ErrInvalidVideoUploadIDs},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &uploadStatusRepo{
				video:        &model.Video{VideoID: 42, PublisherID: 7, VideoPath: tc.videoPath},
				videoErr:     tc.videoErr,
				uploadStatus: &model.VideoUploadStatus{Status: "failed", UpdatedTime: updated, UploadErrorMsg: "视频处理失败"},
				statusErr:    tc.statusErr,
			}
			logger := log.NewStdLogger(io.Discard)
			svc := service.NewVideoInfo(biz.NewVideoInfoUsecase(repo, nil, logger), logger)
			resp, err := svc.GetVideoUploadStatus(context.Background(), &videov1.GetVideoUploadStatusReq{VideoId: 42, UserId: tc.userID})
			if repo.statusReadCount != tc.wantStatusReads {
				t.Fatalf("状态查询次数不符：%d", repo.statusReadCount)
			}
			if tc.wantError != nil {
				if resp != nil || !errors.Is(err, tc.wantError) {
					t.Fatalf("预期错误 %v，得到 %v 和 %v", tc.wantError, resp, err)
				}
				rpcErr := kerrors.FromError(status.Convert(err).Err())
				if rpcErr.Metadata[helper.RPCBusinessCodeKey] != strconv.Itoa(int(tc.wantError.Code)) {
					t.Fatalf("跨 gRPC 后丢失业务码：%v", rpcErr)
				}
				return
			}
			if err != nil || resp.GetStatus() != tc.wantStatus {
				t.Fatalf("上传状态不符：%v %v", resp, err)
			}
			if tc.wantStatus == "completed" {
				if resp.UpdatedTime != nil || resp.UploadErrorMsg != "" {
					t.Fatalf("完成回退返回了虚构的时间或错误：%v", resp)
				}
			} else if resp.GetUploadErrorMsg() != "视频处理失败" || !resp.GetUpdatedTime().AsTime().Equal(updated) {
				t.Fatalf("失败详情不符：%v", resp)
			}
		})
	}
}
