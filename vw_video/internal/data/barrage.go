package data

import (
	"context"
	"errors"
	"time"
	api "vw_video/api/v1/interact"
	"vw_video/internal/biz"
	"vw_video/internal/data/dal/model"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	"gorm.io/gorm"
)

// GetBarragePage 按播放秒数及 ID 正序读取区间，多读一条判断后续批次。
func (i *interactRepo) GetBarragePage(ctx context.Context, q *biz.BarrageQuery) ([]*model.Barrage, error) {
	if err := requireVideo(ctx, q.VideoID); err != nil {
		return nil, err
	}
	b := getQuery(ctx).Barrage
	db := b.WithContext(ctx).Where(
		b.VideoID.Eq(q.VideoID),
		b.PublishTime.Gte(int64(q.Start)),
		b.PublishTime.Lt(int64(q.End)),
	)
	if c := q.After; c != nil {
		// 下一批满足：播放秒数更大，或秒数相同但 ID 更大。使用严格比较排除已返回末条。
		// publish_time 表示视频播放位置，不是弹幕发布的实际时钟时间；相同秒数可能有多条弹幕。
		laterPosition := b.WithContext(ctx).Where(b.PublishTime.Gt(int64(c.Position))) // 在视频当前时间之后发送的弹幕
		samePositionAfterID := b.WithContext(ctx).Where(                               // 比当前弹幕晚发送, 且在视频同一秒发送的弹幕
			b.PublishTime.Eq(int64(c.Position)),
			b.BarrageID.Gt(c.ID),
		)
		// 同秒弹幕按 ID 继续读取，OR 分支整体受视频和播放区间约束。
		db = db.Where(laterPosition.Or(samePositionAfterID))
	}
	return db.Order(b.PublishTime.Asc(), b.BarrageID.Asc()).Limit(biz.BarragePageSize + 1).Find()
}

// GetBarrageLikes 批量读取当前批次的有效点赞，空列表不访问状态表。
func (i *interactRepo) GetBarrageLikes(ctx context.Context, userID, videoID int64, ids []int64) (map[int64]bool, error) {
	result := make(map[int64]bool, len(ids))
	if len(ids) == 0 {
		return result, nil
	}
	q := getQuery(ctx).UserBarrageLike
	rows, err := q.WithContext(ctx).Where(q.UserID.Eq(userID), q.VideoID.Eq(videoID), q.BarrageID.In(ids...)).Find()
	for _, row := range rows {
		result[row.BarrageID] = true
	}
	return result, err
}

// SetBarrageLike 在调用方本地事务内维护关系与原子计数，取消仅删除本人目标记录。
func (i *interactRepo) SetBarrageLike(ctx context.Context, userID, videoID, barrageID int64, liked bool) (*api.SetBarrageLikeResp, error) {
	if err := requireVideo(ctx, videoID); err != nil {
		return nil, err
	}
	b := getQuery(ctx).Barrage
	row, err := b.WithContext(ctx).Where(b.VideoID.Eq(videoID), b.BarrageID.Eq(barrageID)).First()
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, kerrors.NotFound("BARRAGE_NOT_FOUND", "弹幕不存在")
	}
	if err != nil {
		return nil, err
	}
	q := getQuery(ctx).UserBarrageLike
	changed := false
	if liked {
		err = q.WithContext(ctx).Create(&model.UserBarrageLike{UserID: userID, VideoID: videoID, BarrageID: barrageID, CreateAt: time.Now()})
		if err != nil && !isDuplicateLike(err) {
			return nil, err
		}
		changed = err == nil
	} else {
		result, err := q.WithContext(ctx).Where(q.UserID.Eq(userID), q.VideoID.Eq(videoID), q.BarrageID.Eq(barrageID)).Delete()
		if err != nil {
			return nil, err
		}
		if result.RowsAffected > 1 {
			return nil, kerrors.Conflict("BARRAGE_LIKE_CONFLICT", "点赞关系异常")
		}
		changed = result.RowsAffected == 1
	}
	if changed {
		delta := int64(1)
		if !liked {
			delta = -1
		}
		db := b.WithContext(ctx).UnderlyingDB().Model(&model.Barrage{}).Where("video_id = ? AND barrage_id = ?", videoID, barrageID)
		if !liked {
			db = db.Where("likes > 0")
		}
		result := db.UpdateColumn("likes", gorm.Expr("likes + ?", delta))
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected != 1 {
			return nil, kerrors.Conflict("BARRAGE_LIKE_CONFLICT", "弹幕已变化或点赞计数异常")
		}
		row, err = b.WithContext(ctx).Where(b.VideoID.Eq(videoID), b.BarrageID.Eq(barrageID)).First()
		if err != nil {
			return nil, err
		}
	}
	return &api.SetBarrageLikeResp{IsUpvoted: liked, UpvoteCount: row.Likes}, nil
}
