package user

import (
	"context"
	"github.com/go-kratos/kratos/v2/log"
	favorv1 "vw_gateway/api/v1/user/favorites"
	"vw_gateway/internal/biz/userbiz"
	"vw_gateway/internal/pkg/ecode/errdef/uerr"
)

type FavoritesService struct {
	favorv1.UnimplementedFavoriteServer
	favorites *userbiz.FavoritesUsecase
	logger    *log.Helper
}

func NewFavoritesService(favorites *userbiz.FavoritesUsecase, logger log.Logger) *FavoritesService {
	return &FavoritesService{
		favorites: favorites,
		logger:    log.NewHelper(logger),
	}
}

// CreateFavorites 使用鉴权上下文中的用户 ID 创建收藏夹，忽略请求中的兼容身份字段。
// 保存失败时不返回 ID；成功响应保留状态和消息，并返回可立即使用的 favorite_id。
func (u *FavoritesService) CreateFavorites(ctx context.Context, req *favorv1.CreateFavoritesReq) (*favorv1.CreateFavoritesResp, error) {
	userId, ok := ctx.Value(userIdKey).(int64)
	if !ok || userId <= 0 {
		return nil, uerr.ErrTokenInvalid
	}
	favoriteId, err := u.favorites.CreateFavorites(ctx, userId, req.FavoritesName, req.IsPrivate, req.Description)
	if err != nil {
		return nil, err
	}
	return &favorv1.CreateFavoritesResp{
		StatusCode: 200,
		Msg:        "创建收藏夹成功",
		FavoriteId: favoriteId,
	}, nil
}

// GetFavoritesList 获取当前登录用户的全部收藏夹，不接受请求参数指定其他用户。
// 身份缺失或无效时拒绝查询；空列表仍返回当前用户 ID 和空数组。
func (u *FavoritesService) GetFavoritesList(ctx context.Context, req *favorv1.GetFavoritesListReq) (*favorv1.GetFavoritesListResp, error) {
	userId, ok := ctx.Value(userIdKey).(int64)
	if !ok || userId <= 0 {
		return nil, uerr.ErrTokenInvalid
	}
	list, err := u.favorites.GetFavoritesList(ctx, userId)
	if err != nil {
		return nil, err
	}
	resp := &favorv1.GetFavoritesListResp{
		UserId: userId, FavoriteList: make([]*favorv1.FavoriteDetail, len(list)),
	}
	for i, favorite := range list {
		resp.FavoriteList[i] = &favorv1.FavoriteDetail{
			FavoriteId: favorite.FavoriteId, FavoriteName: favorite.FavoriteName,
			Description: favorite.Description, IsPrivate: favorite.IsPrivate,
		}
	}
	return resp, nil
}

// DeleteFavorites 仅使用鉴权上下文确定操作人，不允许按任意收藏夹 ID 越权删除。
func (u *FavoritesService) DeleteFavorites(ctx context.Context, req *favorv1.DeleteFavoritesReq) (*favorv1.DeleteFavoritesResp, error) {
	userId, ok := ctx.Value(userIdKey).(int64)
	if !ok || userId <= 0 {
		return nil, uerr.ErrTokenInvalid
	}
	err := u.favorites.DeleteFavorites(ctx, userId, req.FavoritesId)
	if err != nil {
		return nil, err
	}
	return &favorv1.DeleteFavoritesResp{
		StatusCode: 200,
		Msg:        "删除收藏夹成功",
	}, nil
}

// ModifyFavorites 仅将当前登录身份作为操作人，属性修改不包含视频所属收藏夹切换。
func (u *FavoritesService) ModifyFavorites(ctx context.Context, req *favorv1.ModifyFavoritesReq) (*favorv1.ModifyFavoritesResp, error) {
	userId, ok := ctx.Value(userIdKey).(int64)
	if !ok || userId <= 0 {
		return nil, uerr.ErrTokenInvalid
	}
	err := u.favorites.ModifyFavorites(ctx, userId, req.FavoritesId, req.FavoritesName, req.IsPrivate, req.Description)
	if err != nil {
		return nil, err
	}
	return &favorv1.ModifyFavoritesResp{
		StatusCode: 200,
		Msg:        "修改收藏夹成功",
	}, nil
}
