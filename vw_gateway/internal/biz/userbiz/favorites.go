package userbiz

import (
	"context"
	"github.com/go-kratos/kratos/v2/log"
	"vw_gateway/internal/domain"
)

// FavoritesRepo 定义收藏夹写入和当前用户收藏夹列表的下游访问能力。
type FavoritesRepo interface {
	// CreateFavorites 仅在保存成功后返回收藏夹 ID。
	CreateFavorites(ctx context.Context, userId int64, favoritesName string, isPrivate int32, description string) (int64, error)
	// GetFavoritesList 返回指定用户的全部收藏夹，包含私密收藏夹。
	GetFavoritesList(ctx context.Context, userId int64) ([]*domain.FavoriteDetail, error)
	DeleteFavorites(ctx context.Context, favoritesId int64) error
	ModifyFavorites(ctx context.Context, userId int64, favoritesName *string, isPrivate *int32, description *string) error
}

type FavoritesUsecase struct {
	repo   FavoritesRepo
	logger *log.Helper
}

func NewFavoritesUsecase(repo FavoritesRepo, logger log.Logger) *FavoritesUsecase {
	return &FavoritesUsecase{
		repo:   repo,
		logger: log.NewHelper(logger),
	}
}

// CreateFavorites 为已验证的用户创建收藏夹，并返回下游保存的 ID。
func (uc *FavoritesUsecase) CreateFavorites(ctx context.Context, userId int64, favoritesName string, isPrivate int32, description string) (int64, error) {
	return uc.repo.CreateFavorites(ctx, userId, favoritesName, isPrivate, description)
}

// GetFavoritesList 获取已验证用户的全部收藏夹，下游错误原样返回。
func (uc *FavoritesUsecase) GetFavoritesList(ctx context.Context, userId int64) ([]*domain.FavoriteDetail, error) {
	return uc.repo.GetFavoritesList(ctx, userId)
}

func (uc *FavoritesUsecase) DeleteFavorites(ctx context.Context, favoritesId int64) error {
	return uc.repo.DeleteFavorites(ctx, favoritesId)
}

func (uc *FavoritesUsecase) ModifyFavorites(ctx context.Context, userId int64, favoritesName *string, isPrivate *int32, description *string) error {
	return uc.repo.ModifyFavorites(ctx, userId, favoritesName, isPrivate, description)
}
