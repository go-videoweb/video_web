package biz

import (
	"context"
	"errors"
	"vw_video/internal/data/dal/model"
	"vw_video/internal/pkg/ecode/errdef"

	"gorm.io/gorm"
)

// GetVideoUploadStatus 读取本人视频的上传状态，userId 必须是可信调用方确认的用户。
// 找不到状态记录时，仅已保存原片路径的视频可视为完成，此时更新时间为空。
func (uc *VideoInfoUsecase) GetVideoUploadStatus(ctx context.Context, videoId, userId int64) (*model.VideoUploadStatus, error) {
	if videoId <= 0 || userId <= 0 {
		return nil, errdef.ErrInvalidVideoUploadIDs
	}
	video, err := uc.repo.GetVideoInfoById(ctx, videoId)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errdef.ErrVideoNotFound
	}
	if err != nil {
		return nil, errdef.ErrGetVideoUploadStatusFailed.WithCause(err)
	}
	if video.PublisherID != userId {
		return nil, errdef.ErrVideoNotFound
	}

	uploadStatus, err := uc.repo.GetVideoUploadStatus(ctx, videoId)
	if err == nil {
		return uploadStatus, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errdef.ErrGetVideoUploadStatusFailed.WithCause(err)
	}
	if video.VideoPath == "" {
		return nil, errdef.ErrVideoUploadStatusNotFound
	}
	return &model.VideoUploadStatus{VideoID: videoId, Status: videoUploadCompleted}, nil
}
