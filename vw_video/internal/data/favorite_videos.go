package data

import (
	"context"
	stderr "errors"
	"time"

	"vw_video/internal/biz"
	"vw_video/internal/data/dal/model"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	"gorm.io/gorm"
)

type favoriteVideoRow struct {
	VideoID     int64
	CollectedAt *time.Time
}

// GetFavoriteVideos 分步读取收藏夹权限、关系分页和视频摘要，保持收藏时间及 ID 降序。
// 计数和分页共用有效视频子查询，先排除已软删除视频，再计算偏移；摘要按本页 ID 批量读取。
// 不调用详情用例，不增加播放量或写入观看历史，也不修改现有收藏关系。
func (v *videoInfoRepo) GetFavoriteVideos(ctx context.Context, userId, favoritesId int64, pageNum, pageSize int32) ([]*biz.FavoriteVideo, int64, error) {
	db := getQuery(ctx).Video.WithContext(ctx).UnderlyingDB().Session(&gorm.Session{NewDB: true, Context: ctx})
	if err := requireFavoriteReadAccess(db, userId, favoritesId); err != nil {
		return nil, 0, err
	}
	base := func() *gorm.DB {
		videos := db.Table("video").Select("video_id").Where("deleted_at IS NULL")
		return db.Table("favorite_video").
			Where("favorites_id = ? AND deleted_at IS NULL", favoritesId).
			Where("video_id IN (?)", videos)
	}
	var total int64
	if err := base().Count(&total).Error; err != nil {
		return nil, 0, err
	}
	rows := make([]*favoriteVideoRow, 0)
	err := base().Select("video_id, created_at AS collected_at").
		Order("created_at DESC, id DESC").
		Offset(int(int64(pageNum-1) * int64(pageSize))).Limit(int(pageSize)).Find(&rows).Error
	if err != nil {
		return nil, 0, err
	}
	items := make([]*biz.FavoriteVideo, 0, len(rows))
	if len(rows) > 0 {
		ids := make([]int64, len(rows))
		for i, row := range rows {
			ids[i] = row.VideoID
		}
		var videos []*model.Video
		if err := db.Model(&model.Video{}).Where("video_id IN ?", ids).Find(&videos).Error; err != nil {
			return nil, 0, err
		}
		byID := make(map[int64]*model.Video, len(videos))
		for _, video := range videos {
			byID[video.VideoID] = video
		}
		// IN 查询不保证返回顺序；以关系分页为准组装，忽略读取期间已删除的视频。
		for _, row := range rows {
			if video := byID[row.VideoID]; video != nil {
				items = append(items, &biz.FavoriteVideo{Video: video, CollectedAt: row.CollectedAt})
			}
		}
	}
	// 分步查询期间隐私可能被修改，返回内容和总数前再次核对，失败时不暴露已读取数据。
	if err := requireFavoriteReadAccess(db, userId, favoritesId); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// requireFavoriteReadAccess 按唯一收藏夹 ID 读取元信息，在 Go 中核对本人或公开权限。
// 不存在与私密无权统一返回 404，数据库故障不伪装成无权或空内容。
func requireFavoriteReadAccess(db *gorm.DB, userId, favoritesId int64) error {
	var favorite struct {
		UserID    int64
		IsPrivate int64
	}
	err := db.Table("favorites").Select("user_id, is_private").Where("favorites_id = ?", favoritesId).Take(&favorite).Error
	if stderr.Is(err, gorm.ErrRecordNotFound) || (err == nil && favorite.UserID != userId && favorite.IsPrivate != 1) { // 收藏夹所属 userId 非本人, 且该收藏夹为私密, 也返回未找到
		return kerrors.NotFound("FAVORITE_NOT_FOUND", "收藏夹不存在")
	}
	return err
}

// GetUserFavoriteID 先读取本人的收藏夹 ID，再查询指定视频的收藏归属，不依赖尚未新增的 user_id 列。
// 无有效关系返回 0；发现多个所属收藏夹时返回冲突，避免静默选择并隐藏历史不一致。
func (v *videoInfoRepo) GetUserFavoriteID(ctx context.Context, userId, videoId int64) (int64, error) {
	db := getQuery(ctx).FavoriteVideo.WithContext(ctx).UnderlyingDB().Session(&gorm.Session{NewDB: true, Context: ctx})
	var favoriteIDs []int64
	if err := db.Table("favorites").Where("user_id = ?", userId).Pluck("favorites_id", &favoriteIDs).Error; err != nil {
		return 0, err
	}
	if len(favoriteIDs) == 0 {
		return 0, nil
	}
	var ids []int64
	err := db.Table("favorite_video").
		Where("favorites_id IN ? AND video_id = ? AND deleted_at IS NULL", favoriteIDs, videoId).
		Order("id DESC").Limit(2).Pluck("favorites_id", &ids).Error
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
