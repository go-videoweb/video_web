package data

import (
	"context"
	stderr "errors"
	"time"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	"gorm.io/gorm"
	"vw_video/internal/biz"
	"vw_video/internal/data/dal/model"
)

type favoriteVideoRow struct {
	model.Video `gorm:"embedded"`
	CollectedAt *time.Time
}

// GetFavoriteVideos 基于统一业务库联查收藏关系和视频，保持收藏时间及 ID 降序。
// 每条内容查询均包含公开或本人可读条件，排除已软删除关系及视频，计数沿用相同条件。
// 不调用详情用例，不增加播放量或写入观看历史，也不修改现有收藏关系。
func (v *videoInfoRepo) GetFavoriteVideos(ctx context.Context, userId, favoritesId int64, pageNum, pageSize int32) ([]*biz.FavoriteVideo, int64, error) {
	db := getQuery(ctx).Video.WithContext(ctx).UnderlyingDB().Session(&gorm.Session{NewDB: true, Context: ctx})
	var favorite struct{ FavoritesID int64 }
	err := db.Table("favorites").Select("favorites_id").
		Where("favorites_id = ? AND (user_id = ? OR is_private = ?)", favoritesId, userId, 1).
		Take(&favorite).Error
	if stderr.Is(err, gorm.ErrRecordNotFound) {
		return nil, 0, kerrors.NotFound("FAVORITE_NOT_FOUND", "收藏夹不存在")
	}
	if err != nil {
		return nil, 0, err
	}
	base := func() *gorm.DB {
		return db.Table("favorite_video AS fv").
			Joins("JOIN video AS v ON v.video_id = fv.video_id").
			Joins("JOIN favorites AS f ON f.favorites_id = fv.favorites_id").
			Where("fv.favorites_id = ? AND fv.deleted_at IS NULL AND v.deleted_at IS NULL", favoritesId).
			Where("f.user_id = ? OR f.is_private = ?", userId, 1)
	}
	var total int64
	if err := base().Count(&total).Error; err != nil {
		return nil, 0, err
	}
	rows := make([]*favoriteVideoRow, 0)
	err = base().Select("v.*, fv.created_at AS collected_at").
		Order("fv.created_at DESC, fv.id DESC").
		Offset(int(int64(pageNum-1) * int64(pageSize))).Limit(int(pageSize)).Find(&rows).Error
	if err != nil {
		return nil, 0, err
	}
	items := make([]*biz.FavoriteVideo, len(rows))
	for i, row := range rows {
		items[i] = &biz.FavoriteVideo{Video: &row.Video, CollectedAt: row.CollectedAt}
	}
	return items, total, nil
}

// GetUserFavoriteID 联查收藏夹所有者读取当前用户的视频收藏归属，不依赖尚未新增的 user_id 列。
// 无有效关系返回 0；发现多个所属收藏夹时返回冲突，避免静默选择并隐藏历史不一致。
func (v *videoInfoRepo) GetUserFavoriteID(ctx context.Context, userId, videoId int64) (int64, error) {
	db := getQuery(ctx).FavoriteVideo.WithContext(ctx).UnderlyingDB().Session(&gorm.Session{NewDB: true, Context: ctx})
	var ids []int64
	err := db.Table("favorite_video AS fv").Select("fv.favorites_id").
		Joins("JOIN favorites AS f ON f.favorites_id = fv.favorites_id").
		Where("f.user_id = ? AND fv.video_id = ? AND fv.deleted_at IS NULL", userId, videoId).
		Order("fv.id DESC").Limit(2).Pluck("fv.favorites_id", &ids).Error
	if err != nil {
		return 0, err
	}
	if len(ids) > 1 {
		return 0, kerrors.Conflict("FAVORITE_RELATION_CONFLICT", "该视频存在多个收藏归属，需要先核对收藏记录")
	}
	if len(ids) == 0 {
		return 0, nil
	}
	return ids[0], nil
}
