package data

import (
	"context"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"vw_video/internal/data/dal/model"
)

// EnsureShareStatus 按用户和视频创建默认状态行。已有软删除状态只恢复可见性，保留原分享标记。
func (i *interactRepo) EnsureShareStatus(ctx context.Context, userID, videoID int64) error {
	row := &model.UserVideoStatus{UserID: userID, VideoID: videoID}
	return getQuery(ctx).UserVideoStatus.WithContext(ctx).Clauses(clause.OnConflict{
		DoUpdates: clause.AssignmentColumns([]string{"deleted_at"}),
	}).Create(row)
}

// MarkVideoShared 条件更新个人分享状态。已分享时匹配零行，与 MySQL 的匹配行计数设置无关。
func (i *interactRepo) MarkVideoShared(ctx context.Context, userID, videoID int64) (bool, error) {
	state := getQuery(ctx).UserVideoStatus
	result, err := state.WithContext(ctx).
		Where(state.UserID.Eq(userID), state.VideoID.Eq(videoID), state.IsShared.Neq(1)).
		Update(state.IsShared, 1)
	return result.RowsAffected == 1, err
}

// IncrementVideoShare 在选定事务内原子增加有效视频的计数和热度。
func (i *interactRepo) IncrementVideoShare(ctx context.Context, videoID int64) error {
	video := getQuery(ctx).Video
	result, err := video.WithContext(ctx).Where(video.VideoID.Eq(videoID)).Updates(map[string]any{
		"cnt_shares": gorm.Expr("COALESCE(cnt_shares, 0) + ?", 1),
		"hot":        gorm.Expr("COALESCE(hot, 0) + ?", HotEachShare),
	})
	if err != nil {
		return err
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}
