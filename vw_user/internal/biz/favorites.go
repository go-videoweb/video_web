package biz

import (
	"context"
	stderr "errors"
	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"gorm.io/gorm"
	"util/dbutil/mysqlutil"
	"util/getid"
	"util/helper"
	"vw_user/internal/data/dal/model"
	"vw_user/internal/domain"
	"vw_user/internal/pkg/ecode/errdef"
)

// FavoritesRepo 定义收藏夹持久化和按用户查询的能力。
type FavoritesRepo interface {
	// CheckFavoritesName Check if the userbiz has already created a favorites with the same name
	// Return FALSE if the userbiz has already created a favorites with the same name, otherwise return TRUE
	CheckFavoritesName(ctx context.Context, userId int64, favoritesName string) (bool, error)
	CreateFavorites(ctx context.Context, favorite *model.Favorite) error
	// GetFavoritesList 按用户 ID 查询全部收藏夹，并按收藏夹 ID 稳定排序。
	GetFavoritesList(ctx context.Context, userId int64) ([]*model.Favorite, error)

	// DeleteFavorites Delete the specified favorites
	DeleteFavorites(ctx context.Context, userId, favoritesId int64) error
	// GetFavoritesMetadata 读取收藏夹归属，用于修改和删除前校验。
	GetFavoritesMetadata(ctx context.Context, favoritesId int64) (*model.Favorite, error)

	// CheckIfFavoritesEmpty check if the specified favorites is empty.
	// If the favorites is empty(Can be deleted), return TRUE, otherwise return FALSE.
	CheckIfFavoritesEmpty(ctx context.Context, favoritesId int64) (bool, error)

	// UpdateFavorites Update the specified favorites
	UpdateFavorites(ctx context.Context, favorite map[string]any) error
}

type FavoritesUsecase struct {
	repo   FavoritesRepo
	logger *log.Helper
	tx     mysqlutil.Transaction
}

func NewFavoritesUsecase(repo FavoritesRepo, tx mysqlutil.Transaction, logger log.Logger) *FavoritesUsecase {
	return &FavoritesUsecase{
		repo:   repo,
		tx:     tx,
		logger: log.NewHelper(logger),
	}
}

// CreateFavorites 检查同用户下的名称冲突，生成 ID 并保存，成功后才返回该 ID。
// 私密标记沿用 1=公开、-1=私密；省略时的 0 保持原数据库默认公开行为。
func (uc *FavoritesUsecase) CreateFavorites(ctx context.Context, info *domain.FavoritesInfo) (int64, error) {
	if info == nil || info.UserId <= 0 || info.FavoritesName == nil || info.IsPrivate == nil || info.Description == nil {
		return 0, kerrors.BadRequest("INVALID_FAVORITE", "创建收藏夹需要有效用户和完整的收藏夹信息")
	}
	if *info.IsPrivate != -1 && *info.IsPrivate != 0 && *info.IsPrivate != 1 {
		return 0, kerrors.BadRequest("INVALID_FAVORITE_PRIVACY", "收藏夹隐私标记必须为 1（公开）或 -1（私密）")
	}
	ok, err := uc.repo.CheckFavoritesName(ctx, info.UserId, *info.FavoritesName)
	if err != nil {
		return 0, helper.HandleError(errdef.ErrCreateFavoriteFailed, err)
	}
	if !ok {
		return 0, helper.HandleError(errdef.ErrCreateFavoriteFailed, errdef.ErrFavoritesNameConflict)
	}

	favorite := &model.Favorite{
		FavoritesID:   getid.GetID(),
		UserID:        info.UserId,
		FavoritesName: *info.FavoritesName,
		Description:   *info.Description,
		IsPrivate:     int64(*info.IsPrivate),
	}
	if favorite.IsPrivate == 0 {
		favorite.IsPrivate = 1
	}
	err = uc.repo.CreateFavorites(ctx, favorite)
	if err != nil {
		return 0, helper.HandleError(errdef.ErrCreateFavoriteFailed, err)
	}
	return favorite.FavoritesID, nil
}

// GetFavoritesList 返回指定用户的全部收藏夹；非法身份和查询失败均不伪装为空列表。
func (uc *FavoritesUsecase) GetFavoritesList(ctx context.Context, userId int64) ([]*model.Favorite, error) {
	if userId <= 0 {
		return nil, kerrors.BadRequest("INVALID_USER_ID", "用户 ID 必须为正数")
	}
	list, err := uc.repo.GetFavoritesList(ctx, userId)
	if err != nil {
		return nil, helper.HandleError(errdef.ErrGetFavoritesListFailed, err)
	}
	return list, nil
}

// DeleteFavorites 校验可信操作人归属后删除空收藏夹，不删除其下的视频关系。
// 非本人和不存在统一返回 404，保持既有非空收藏夹不可删除规则。
func (uc *FavoritesUsecase) DeleteFavorites(ctx context.Context, userId, favoritesId int64) error {
	if userId <= 0 || favoritesId <= 0 {
		return kerrors.BadRequest("INVALID_FAVORITE", "用户和收藏夹 ID 必须为正数")
	}
	err := uc.tx.WithTx(ctx, func(ctx context.Context) error {
		if err := uc.requireFavoriteOwner(ctx, userId, favoritesId); err != nil {
			return err
		}
		// 1. Delete the records of the specified favorites
		ok, err := uc.repo.CheckIfFavoritesEmpty(ctx, favoritesId)
		if err != nil {
			return helper.HandleError(errdef.ErrDeleteFavoriteFailed, err)
		}
		if !ok {
			return helper.HandleError(errdef.ErrDeleteFavoriteFailed, errdef.ErrFavoritesNotEmpty)
		}

		// 2. Delete the specified favorites itself
		err = uc.repo.DeleteFavorites(ctx, userId, favoritesId)
		if err != nil {
			return helper.HandleError(errdef.ErrDeleteFavoriteFailed, err)
		}
		return nil
	})
	return err
}

// ModifyFavorites 仅允许收藏夹所有者更新属性，保留可选字段的零值更新语义。
func (uc *FavoritesUsecase) ModifyFavorites(ctx context.Context, info *domain.FavoritesInfo) error {
	if info == nil || info.UserId <= 0 || info.FavoritesId <= 0 {
		return kerrors.BadRequest("INVALID_FAVORITE", "用户和收藏夹 ID 必须为正数")
	}
	if info.IsPrivate != nil && *info.IsPrivate != -1 && *info.IsPrivate != 1 {
		return kerrors.BadRequest("INVALID_FAVORITE_PRIVACY", "收藏夹隐私标记必须为 1（公开）或 -1（私密）")
	}
	if err := uc.requireFavoriteOwner(ctx, info.UserId, info.FavoritesId); err != nil {
		return err
	}
	err := uc.repo.UpdateFavorites(ctx, info.ToMap())
	if err != nil {
		return helper.HandleError(errdef.ErrModifyFavoriteFailed, err)
	}
	return nil
}

// requireFavoriteOwner 在写入前核对所有者；无权与不存在返回同一错误，不泄露收藏夹信息。
func (uc *FavoritesUsecase) requireFavoriteOwner(ctx context.Context, userId, favoritesId int64) error {
	favorite, err := uc.repo.GetFavoritesMetadata(ctx, favoritesId)
	if stderr.Is(err, gorm.ErrRecordNotFound) || (err == nil && (favorite == nil || favorite.UserID != userId)) {
		return kerrors.NotFound("FAVORITE_NOT_FOUND", "收藏夹不存在")
	}
	return err
}
