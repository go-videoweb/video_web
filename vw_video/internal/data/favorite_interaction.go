package data

import (
	"context"
	"errors"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	drivermysql "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	favorv1 "vw_user/api/v1/favorites"
	"vw_video/internal/biz"
	"vw_video/internal/data/dal/model"
)

// getFavoriteMetadata 仅向用户服务读取元信息，不在视频仓储读取用户域表。
// 输入：收藏夹 ID；输出：字段完整的元信息或原 RPC 错误，不回退共享数据库。
func (d *Data) getFavoriteMetadata(ctx context.Context, favoriteID int64) (*biz.FavoriteMetadata, error) {
	if d == nil || d.favoriteClient == nil {
		return nil, kerrors.ServiceUnavailable("FAVORITE_SERVICE_UNAVAILABLE", "收藏夹服务不可用")
	}
	row, err := d.favoriteClient.GetFavoriteInfo(ctx, &favorv1.GetFavoriteInfoReq{FavoriteId: favoriteID})
	if err != nil {
		return nil, err
	}
	if row == nil || row.FavoriteId != favoriteID || row.UserId <= 0 {
		return nil, kerrors.InternalServer("INVALID_FAVORITE_METADATA", "收藏夹元信息异常")
	}
	return &biz.FavoriteMetadata{FavoriteID: row.FavoriteId, UserID: row.UserId, IsPrivate: row.IsPrivate}, nil
}

// GetFavoriteMetadata 提供收藏夹元信息，由视频业务层决定是否允许写入。
func (i *interactRepo) GetFavoriteMetadata(ctx context.Context, favoriteID int64) (*biz.FavoriteMetadata, error) {
	return i.data.getFavoriteMetadata(ctx, favoriteID)
}

// GetFavoriteRelation 普通读取用户视频关系，不添加显式行锁。
// 输出：无关系返回 nil；保留删除标记交给业务层判断，数据库故障不伪装为未收藏。
func (i *interactRepo) GetFavoriteRelation(ctx context.Context, userID, videoID int64) (*model.FavoriteVideo, error) {
	q := getQuery(ctx).FavoriteVideo
	row, err := q.WithContext(ctx).Unscoped().Where(q.UserID.Eq(userID), q.VideoID.Eq(videoID)).First()
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return row, err
}

// CreateFavoriteRelation 保存业务层生成的关系 ID，唯一键冲突明确失败，不忽略或移动关系。
func (i *interactRepo) CreateFavoriteRelation(ctx context.Context, relation *model.FavoriteVideo) error {
	if relation == nil || relation.ID <= 0 || relation.UserID <= 0 || relation.FavoritesID <= 0 || relation.VideoID <= 0 {
		return biz.ErrInvalidInteractionIdentity
	}
	result := getQuery(ctx).FavoriteVideo.WithContext(ctx).UnderlyingDB().Create(relation)
	err := result.Error
	var duplicate *drivermysql.MySQLError
	if errors.Is(err, gorm.ErrDuplicatedKey) || (errors.As(err, &duplicate) && duplicate.Number == 1062) {
		return kerrors.Conflict("FAVORITE_RELATION_CONFLICT", "收藏关系已变化，请刷新后重试")
	}
	if err == nil && result.RowsAffected != 1 {
		return kerrors.Conflict("FAVORITE_RELATION_CONFLICT", "收藏关系未成功创建")
	}
	return err
}

// DeleteFavoriteRelation 硬删除指定代收藏关系，四个 ID 和有效标记必须同时匹配。
// 输入：业务层核对后的关系；输出：零行或非单行删除均返回冲突，由本地事务回滚。
func (i *interactRepo) DeleteFavoriteRelation(ctx context.Context, relation *model.FavoriteVideo) error {
	if relation == nil || relation.ID <= 0 || relation.UserID <= 0 || relation.FavoritesID <= 0 || relation.VideoID <= 0 {
		return biz.ErrInvalidInteractionIdentity
	}
	q := getQuery(ctx).FavoriteVideo
	result, err := q.WithContext(ctx).Unscoped().Where(q.ID.Eq(relation.ID), q.UserID.Eq(relation.UserID), q.FavoritesID.Eq(relation.FavoritesID), q.VideoID.Eq(relation.VideoID), q.DeletedAt.IsNull()).Delete(&model.FavoriteVideo{})
	if err != nil {
		return err
	}
	if result.RowsAffected != 1 {
		return kerrors.Conflict("FAVORITE_RECORD_CHANGED", "收藏记录已变化，请刷新后重试")
	}
	return nil
}

// UpdateFavoriteCount 使用上下文事务调整视频收藏数与热度，保留版本筛选并检查影响行数。
// 输入：delta 只允许 1/-1；减少时要求计数为正，已知计数异常不作为版本冲突重试。
// 版本条件未匹配返回专用错误，由用例回滚后重试整个事务；其他 SQL 错误原样返回。
func (i *interactRepo) UpdateFavoriteCount(ctx context.Context, videoID, delta int64) error {
	if delta != 1 && delta != -1 {
		return kerrors.BadRequest("INVALID_FAVORITE", "收藏计数变更必须为 1 或 -1")
	}
	q := getQuery(ctx).Video
	do, row, err := addVideoModel(ctx, videoID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return kerrors.NotFound("VIDEO_NOT_FOUND", "视频不存在")
		}
		return err
	}
	if !row.Version.Valid {
		return kerrors.InternalServer("INVALID_VIDEO_VERSION", "视频版本缺失")
	}
	if row.CntFavorited < 0 || (delta < 0 && row.CntFavorited < 1) {
		return kerrors.Conflict("FAVORITE_COUNT_CONFLICT", "视频收藏计数异常")
	}
	if delta < 0 {
		do = do.Where(q.CntFavorited.Gte(1))
	}
	result, err := do.Where(q.VideoID.Eq(videoID)).Updates(map[string]interface{}{"cnt_favorited": row.CntFavorited + delta, "hot": row.Hot + delta*HotEachFavorite})
	if err != nil {
		return err
	}
	if result.RowsAffected == 0 {
		return biz.ErrFavoriteVersionConflict
	}
	if result.RowsAffected != 1 {
		return kerrors.Conflict("FAVORITE_COUNT_CONFLICT", "视频收藏计数更新行数异常")
	}
	return nil
}

// SetFavoriteStatus 在当前事务内局部 upsert 收藏状态，无记录时创建默认状态行。
// 冲突更新仅涉及收藏状态、软删除标记和更新时间，不读取或覆盖其他互动及观看时间。
func (i *interactRepo) SetFavoriteStatus(ctx context.Context, userID, videoID int64, favorite bool) error {
	if userID <= 0 || videoID <= 0 {
		return biz.ErrInvalidInteractionIdentity
	}
	value := int64(0)
	if favorite {
		value = 1
	}
	row := &model.UserVideoStatus{UserID: userID, VideoID: videoID, IsFavor: value}
	return interactionDB(ctx).Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "user_id"}, {Name: "video_id"}},
		DoUpdates: clause.AssignmentColumns([]string{"is_favor", "deleted_at", "updated_at"}),
	}).Create(row).Error
}
