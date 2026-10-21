package biz

import (
	"context"
	"math"
	"time"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	"vw_video/internal/data/dal/model"
)

// FavoriteVideo 将视频摘要来源与收藏时间分开，历史空收藏时间用 nil 表示。
type FavoriteVideo struct {
	Video       *model.Video
	CollectedAt *time.Time
}

// GetFavoriteVideos 校验身份和分页，返回收藏夹中的有效视频及总数。
// 页码和数量为 0 时默认 1 和 20；负数、每页超过 100 条或偏移溢出时拒绝请求。
// 本人可读取任意隐私级别，其他登录用户仅可读取公开收藏夹；无权与不存在均为 404。
func (uc *VideoInfoUsecase) GetFavoriteVideos(ctx context.Context, userId, favoritesId int64, pageNum, pageSize int32) ([]*FavoriteVideo, int64, int32, int32, error) {
	if userId <= 0 || favoritesId <= 0 || pageNum < 0 || pageSize < 0 || pageSize > 100 {
		return nil, 0, 0, 0, kerrors.BadRequest("INVALID_FAVORITE_PAGE", "需要有效身份、收藏夹 ID 和分页参数")
	}
	if pageNum == 0 {
		pageNum = 1
	}
	if pageSize == 0 {
		pageSize = 20
	}
	if int64(pageNum-1)*int64(pageSize) > math.MaxInt32 {
		return nil, 0, 0, 0, kerrors.BadRequest("INVALID_FAVORITE_PAGE", "分页偏移过大")
	}
	rows, total, err := uc.repo.GetFavoriteVideos(ctx, userId, favoritesId, pageNum, pageSize)
	if err != nil {
		return nil, 0, 0, 0, err
	}
	return rows, total, pageNum, pageSize, nil
}
