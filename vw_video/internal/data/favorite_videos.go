package data

import (
	"context"
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
	if err := v.data.requireFavoriteAccess(ctx, userId, favoritesId, false); err != nil {
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
	if err := v.data.requireFavoriteAccess(ctx, userId, favoritesId, false); err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// requireFavoriteAccess 通过用户 RPC 校验收藏夹权限，不访问用户域的数据表。
// 输入：用户及收藏夹 ID；write 为 true 时仅允许所有者，否则也允许读取公开收藏夹。
// 输出：无权与不存在统一 404；RPC 故障或元信息异常拒绝访问，不回退共享表查询。
// RPC 检查与视频数据库事务不构成跨服务原子快照。
func (d *Data) requireFavoriteAccess(ctx context.Context, userId, favoritesId int64, write bool) error {
	if userId <= 0 || favoritesId <= 0 {
		return kerrors.BadRequest("INVALID_FAVORITE", "用户和收藏夹 ID 必须为正数")
	}
	favorite, err := d.getFavoriteMetadata(ctx, favoritesId)
	if err != nil {
		return err
	}
	if favorite.UserID != userId && (write || favorite.IsPrivate) {
		return kerrors.NotFound("FAVORITE_NOT_FOUND", "收藏夹不存在")
	}
	return nil
}

// GetUserFavorite 按用户和视频读取有效关系的收藏夹和记录 ID，不读取用户域表。
// 无有效关系返回两个零；多条关系或异常 ID 返回冲突，不能静默选择。
func (v *videoInfoRepo) GetUserFavorite(ctx context.Context, userId, videoId int64) (int64, int64, error) {
	if userId <= 0 || videoId <= 0 {
		return 0, 0, kerrors.BadRequest("INVALID_FAVORITE", "用户和视频 ID 必须为正数")
	}
	db := getQuery(ctx).FavoriteVideo.WithContext(ctx).UnderlyingDB().Session(&gorm.Session{NewDB: true, Context: ctx})
	var rows []*model.FavoriteVideo
	err := db.Table("favorite_video").Unscoped().Select("id, favorites_id").
		Where("user_id = ? AND video_id = ? AND deleted_at IS NULL", userId, videoId).
		Order("id DESC").Limit(2).Find(&rows).Error
	if err != nil {
		return 0, 0, err
	}
	if len(rows) > 1 || (len(rows) == 1 && (rows[0].ID <= 0 || rows[0].FavoritesID <= 0)) {
		return 0, 0, kerrors.Conflict("FAVORITE_RELATION_CONFLICT", "收藏关系异常，请核对收藏记录")
	}
	if len(rows) == 0 {
		return 0, 0, nil
	}
	return rows[0].FavoritesID, rows[0].ID, nil
}
