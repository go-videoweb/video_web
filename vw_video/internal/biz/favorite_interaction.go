package biz

import (
	"context"
	"errors"

	"util/getid"
	"vw_video/internal/data/dal/model"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	"gorm.io/gorm"
)

const favoriteTransactionAttempts = 3

// ErrFavoriteVersionConflict 表示视频乐观更新未匹配，可在回滚后重试整个收藏事务。
var ErrFavoriteVersionConflict = kerrors.Conflict("FAVORITE_VERSION_CONFLICT", "视频版本已变化，请重试")

// FavoriteMetadata 是用户服务提供的收藏夹元信息，不代表调用方已获得访问权限。
type FavoriteMetadata struct {
	FavoriteID int64
	UserID     int64
	IsPrivate  bool
}

// FavoriteCommand 表达明确的收藏或取消意图，取消须提供当前关系 ID。
type FavoriteCommand struct {
	UserID, VideoID, FavoriteID, RecordID int64
	Favorite                              int32
}

// FavoriteResult 返回事务提交后的收藏状态与当前有效关系 ID；未收藏时两个 ID 均为零。
type FavoriteResult struct {
	IsFavorited          bool
	FavoriteID, RecordID int64
}

// SetVideoFavorite 校验归属后，以本地事务统一维护收藏关系、视频计数及用户收藏状态。
// 输入：身份由网关验证；收藏标记仅为 1/-1，新增不得携带记录 ID，取消须携带正记录 ID。
// 输出：只在提交成功后返回结果；重复收藏同夹或重复取消不重复调整计数，跨夹和过时记录拒绝。
// 用户 RPC 在事务外执行；关系为事实来源，状态副本只更新收藏字段，不覆盖其他互动或观看时间。
// 仅对事务回调中的视频版本冲突重试，最多三次；开启、提交及其他业务错误不重试。
func (u *InteractUsecase) SetVideoFavorite(ctx context.Context, cmd *FavoriteCommand) (*FavoriteResult, error) {
	if cmd == nil || cmd.UserID <= 0 || cmd.VideoID <= 0 || cmd.FavoriteID <= 0 || // id 非法
		(cmd.Favorite != 1 && cmd.Favorite != -1) || // cmd.Favorite == 1 / -1 表示是否收藏, 必须为这两个值之一
		(cmd.Favorite == 1 && cmd.RecordID != 0) || (cmd.Favorite == -1 && cmd.RecordID <= 0) { // 收藏时收藏记录 id 必须为 0, 取消收藏时必须有收藏记录 id
		return nil, kerrors.BadRequest("INVALID_FAVORITE", "收藏需要有效身份和明确操作，取消必须提供当前收藏记录 ID")
	}
	metadata, err := u.repo.GetFavoriteMetadata(ctx, cmd.FavoriteID)
	if err != nil {
		return nil, err
	}
	if metadata == nil || metadata.FavoriteID != cmd.FavoriteID || metadata.UserID != cmd.UserID {
		return nil, kerrors.NotFound("FAVORITE_NOT_FOUND", "收藏夹不存在")
	}
	for attempt := 0; attempt < favoriteTransactionAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		var result *FavoriteResult
		var operationErr error
		err = u.tx.WithTx(ctx, func(ctx context.Context) error {
			result, operationErr = u.applyVideoFavorite(ctx, cmd)
			return operationErr
		})
		if err == nil {
			return result, nil
		}
		// 只重试回调已失败并回滚的版本冲突，不能把提交阶段的错误当作可重试失败。
		if !errors.Is(operationErr, ErrFavoriteVersionConflict) || !errors.Is(err, ErrFavoriteVersionConflict) {
			return nil, err
		}
	}
	return nil, err
}

// applyVideoFavorite 执行单次收藏事务，调用方负责开启、提交、回滚与重试。
// 每次调用重新读取视频和关系；幂等请求不额外写状态，实际关系变更才维护计数及收藏副本。
func (u *InteractUsecase) applyVideoFavorite(ctx context.Context, cmd *FavoriteCommand) (*FavoriteResult, error) {
	if _, err := u.repo.GetPublisherIdByVideoId(ctx, cmd.VideoID); err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, kerrors.NotFound("VIDEO_NOT_FOUND", "视频不存在")
		}
		return nil, err
	}
	relation, err := u.repo.GetFavoriteRelation(ctx, cmd.UserID, cmd.VideoID)
	if err != nil {
		return nil, err
	}
	if relation != nil && (relation.DeletedAt.Valid || relation.ID <= 0 || relation.UserID != cmd.UserID || relation.VideoID != cmd.VideoID) {
		return nil, kerrors.Conflict("FAVORITE_RELATION_CONFLICT", "收藏关系异常，请核对后重试")
	}
	if relation != nil && relation.FavoritesID != cmd.FavoriteID {
		return nil, kerrors.Conflict("VIDEO_ALREADY_FAVORITED", "视频已在其他收藏夹中，请先取消原收藏")
	}
	result := &FavoriteResult{}
	if cmd.Favorite == 1 {
		if relation != nil {
			return &FavoriteResult{IsFavorited: true, FavoriteID: relation.FavoritesID, RecordID: relation.ID}, nil
		}
		relation = &model.FavoriteVideo{ID: getid.GetID(), UserID: cmd.UserID, FavoritesID: cmd.FavoriteID, VideoID: cmd.VideoID}
		if err := u.repo.CreateFavoriteRelation(ctx, relation); err != nil {
			return nil, err
		}
		if err := u.repo.UpdateFavoriteCount(ctx, cmd.VideoID, 1); err != nil {
			return nil, err
		}
		result.IsFavorited, result.FavoriteID, result.RecordID = true, relation.FavoritesID, relation.ID
	} else {
		if relation == nil {
			return result, nil
		}
		if relation.ID != cmd.RecordID {
			return nil, kerrors.Conflict("FAVORITE_RECORD_CHANGED", "收藏记录已变化，请刷新后重试")
		}
		if err := u.repo.DeleteFavoriteRelation(ctx, relation); err != nil {
			return nil, err
		}
		if err := u.repo.UpdateFavoriteCount(ctx, cmd.VideoID, -1); err != nil {
			return nil, err
		}
	}
	if err := u.repo.SetFavoriteStatus(ctx, cmd.UserID, cmd.VideoID, result.IsFavorited); err != nil {
		return nil, err
	} // 设置用户-视频状态
	return result, nil
}
