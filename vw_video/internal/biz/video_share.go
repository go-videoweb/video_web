package biz

import (
	"context"
	"errors"

	"gorm.io/gorm"
	"vw_video/internal/pkg/ecode/errdef"
)

// RecordVideoShare 校验视频存在，在同一本地事务记录个人分享状态及视频计数。
// 并发或重复请求只有首次成功推进分享状态才增加计数。
func (u *InteractUsecase) RecordVideoShare(ctx context.Context, userID, videoID int64) error {
	if userID <= 0 || videoID <= 0 {
		return ErrInvalidInteractionIdentity
	}
	err := u.tx.WithTx(ctx, func(txCtx context.Context) error {
		if _, err := u.repo.GetPublisherIdByVideoId(txCtx, videoID); err != nil {
			return err
		}
		if err := u.repo.EnsureShareStatus(txCtx, userID, videoID); err != nil {
			return err
		}
		changed, err := u.repo.MarkVideoShared(txCtx, userID, videoID)
		if err != nil || !changed {
			return err
		}
		return u.repo.IncrementVideoShare(txCtx, videoID)
	})
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errdef.ErrVideoNotFound
	}
	if err != nil {
		return errdef.ErrShareVideoFailed.WithCause(err)
	}
	return nil
}
