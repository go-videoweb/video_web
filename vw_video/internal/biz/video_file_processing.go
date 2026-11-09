package biz

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"time"
	"util/ffmpeg"
	"util/helper"
	"util/helper/file"
	"vw_video/internal/pkg/ecode/errdef"
)

// changeVideoUploadStatus 仅从预期阶段推进上传状态，未命中视为阶段冲突。
func (uc *VideoInfoUsecase) changeVideoUploadStatus(ctx context.Context, videoId int64, from, to, errorMessage string) error {
	updated, err := uc.repo.UpdateVideoUploadStatus(ctx, videoId, from, to, errorMessage, time.Now())
	if err != nil {
		return err
	}
	if !updated {
		return errdef.ErrVideoFileUploadStage
	}
	return nil
}

// failVideoUpload 使用独立的短超时写入失败状态，传输取消或处理超时后仍可记录结果。
// errorMessage 对客户端可见，底层错误仅记录在日志中。确认失败后调用方才清理文件。
func (uc *VideoInfoUsecase) failVideoUpload(videoId int64, from, errorMessage string, cause error) bool {
	uc.logger.Errorf("video %d upload failed at %s: %v", videoId, from, cause)
	ctx, cancel := context.WithTimeout(context.Background(), videoUploadWriteTimeout)
	defer cancel()
	if err := uc.changeVideoUploadStatus(ctx, videoId, from, videoUploadFailed, errorMessage); err != nil {
		uc.logger.Errorf("record upload failure for video %d: %v", videoId, err)
		return false
	}
	return true
}

// processVideoFile 独立于上传请求运行 DASH 任务，处理失败时清理本次原片。
func (uc *VideoInfoUsecase) processVideoFile(videoId int64, videoFilePath string) {
	ctx, cancel := context.WithTimeout(context.Background(), videoProcessingTimeout)
	defer cancel()
	dashDir, err := uc.generateVideoDASH(ctx, videoId, videoFilePath)
	if err != nil {
		message := "视频处理失败"
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			message = "视频处理超时"
		}
		// 提交结果不明确时保留媒体，避免删除已标记为完成的视频。
		if uc.failVideoUpload(videoId, videoUploadProcessing, message, err) {
			_ = os.Remove(videoFilePath)
			if dashDir != "" {
				_ = os.RemoveAll(dashDir)
			}
		}
		return
	}
	uc.logger.Infof("video %d DASH processing completed", videoId)
}

// generateVideoDASH 先在独立目录生成分片，处理成功后发布清单和分片并保存最终状态。
// ffmpeg 与 ffprobe 在事务外执行，原片元信息和完成状态在同一事务中保存。
// 返回值包含已发布的分片目录，供事务失败后的清理使用。
func (uc *VideoInfoUsecase) generateVideoDASH(ctx context.Context, videoId int64, videoFilePath string) (string, error) {
	size, err := file.GetFileSize(videoFilePath)
	if err != nil {
		return "", err
	}
	duration, err := ffmpeg.GetVideoDuration(ctx, videoFilePath)
	if err != nil {
		return "", err
	}
	durationStr, _ := helper.SecondToTime(duration)

	videoDir := filepath.Dir(videoFilePath)
	tmpDashDir, err := os.MkdirTemp(videoDir, ".dash-*")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(tmpDashDir)
	if err := ffmpeg.MakeDASH(ctx, videoFilePath, tmpDashDir, dashName); err != nil {
		return "", err
	}

	dashDir := filepath.Join(videoDir, dashPath)
	if err := os.Rename(tmpDashDir, dashDir); err != nil {
		return "", err
	}
	writeCtx, cancel := context.WithTimeout(ctx, videoUploadWriteTimeout)
	defer cancel()
	return dashDir, uc.tx.WithTx(writeCtx, func(txCtx context.Context) error {
		if err := uc.changeVideoUploadStatus(txCtx, videoId, videoUploadProcessing, videoUploadCompleted, ""); err != nil {
			return err
		}
		return uc.repo.UpdateVideoMediaInfo(txCtx, videoId, videoFilePath, durationStr, size)
	})
}
