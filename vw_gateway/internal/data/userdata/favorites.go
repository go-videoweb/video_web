package userdata

import (
	"context"
	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"vw_gateway/internal/biz/userbiz"
	"vw_gateway/internal/domain"
	favorv1 "vw_user/api/v1/favorites"
)

type favoritesRepo struct {
	data   *Data
	logger *log.Helper
}

func NewFavoritesRepo(data *Data, favoritesClient favorv1.FavoriteClient, logger log.Logger) userbiz.FavoritesRepo {
	return &favoritesRepo{
		data:   data,
		logger: log.NewHelper(logger),
	}

}

// CreateFavorites 转发创建请求，仅在下游返回有效 ID 时报告成功，不自动重试写操作。
func (f *favoritesRepo) CreateFavorites(ctx context.Context, userId int64, favoritesName string, isPrivate int32, description string) (int64, error) {
	resp, err := f.data.favoritesClient.CreateFavorites(ctx, &favorv1.CreateFavoritesReq{
		UserId:        userId,
		FavoritesName: favoritesName,
		IsPrivate:     isPrivate,
		Description:   description,
	})
	if err != nil {
		return 0, err
	}
	if resp.GetFavoriteId() <= 0 {
		return 0, kerrors.InternalServer("FAVORITE_ID_MISSING", "用户服务未返回有效的收藏夹 ID，请先核对创建结果，勿重复提交")
	}
	return resp.FavoriteId, nil
}

// GetFavoritesList 查询指定用户的收藏夹，并转换为网关领域模型；空结果返回空切片。
func (f *favoritesRepo) GetFavoritesList(ctx context.Context, userId int64) ([]*domain.FavoriteDetail, error) {
	resp, err := f.data.favoritesClient.GetFavoritesList(ctx, &favorv1.GetFavoritesListReq{UserId: userId})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, kerrors.InternalServer("FAVORITES_LIST_MISSING", "用户服务未返回收藏夹列表响应")
	}
	list := make([]*domain.FavoriteDetail, len(resp.FavoriteList))
	for i, favorite := range resp.FavoriteList {
		list[i] = &domain.FavoriteDetail{
			FavoriteId: favorite.GetFavoriteId(), FavoriteName: favorite.GetFavoriteName(),
			Description: favorite.GetDescription(), IsPrivate: favorite.GetIsPrivate(),
		}
	}
	return list, nil
}

// DeleteFavorites 将鉴权操作人传给用户服务，禁止只凭收藏夹 ID 执行删除。
func (f *favoritesRepo) DeleteFavorites(ctx context.Context, userId, favoritesId int64) error {
	_, err := f.data.favoritesClient.DeleteFavorites(ctx, &favorv1.DeleteFavoritesReq{
		FavoritesId: favoritesId,
		UserId:      userId,
	})
	return err
}

// ModifyFavorites 传递目标 ID、可信操作人和可选属性，不修改收藏夹所有者。
func (f *favoritesRepo) ModifyFavorites(ctx context.Context, userId, favoritesId int64, favoritesName *string, isPrivate *int32, description *string) error {
	_, err := f.data.favoritesClient.ModifyFavorites(ctx, &favorv1.ModifyFavoritesReq{
		FavoritesId:   favoritesId,
		UserId:        userId,
		FavoritesName: favoritesName,
		IsPrivate:     isPrivate,
		Description:   description,
	})
	return err
}
