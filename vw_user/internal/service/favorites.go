package service

import (
	"context"
	"github.com/go-kratos/kratos/v2/log"
	favorv1 "vw_user/api/v1/favorites"
	"vw_user/internal/biz"
	"vw_user/internal/domain"
)

type FavoritesService struct {
	favorv1.UnimplementedFavoriteServer
	favorites *biz.FavoritesUsecase
	logger    *log.Helper
}

func NewFavoritesService(favorites *biz.FavoritesUsecase, logger log.Logger) *FavoritesService {
	return &FavoritesService{
		favorites: favorites,
		logger:    log.NewHelper(logger),
	}
}

// CreateFavorites 创建收藏夹，将业务层确认保存成功的 ID 返回给网关。
func (fs *FavoritesService) CreateFavorites(ctx context.Context, req *favorv1.CreateFavoritesReq) (*favorv1.CreateFavoritesResp, error) {
	favoriteId, err := fs.favorites.CreateFavorites(ctx, &domain.FavoritesInfo{
		UserId:        req.UserId,
		FavoritesName: &req.FavoritesName,
		IsPrivate:     &req.IsPrivate,
		Description:   &req.Description,
	})
	if err != nil {
		return nil, err
	}
	return &favorv1.CreateFavoritesResp{
		StatusCode: 200,
		Msg:        "创建收藏夹成功",
		FavoriteId: favoriteId,
	}, nil
}

// GetFavoritesList 查询网关传入的已验证用户，显式转换数据库的私密标记。
// 数据库 -1 为私密、1 为公开；无记录返回空数组，查询失败返回错误。
func (fs *FavoritesService) GetFavoritesList(ctx context.Context, req *favorv1.GetFavoritesListReq) (*favorv1.GetFavoritesListResp, error) {
	list, err := fs.favorites.GetFavoritesList(ctx, req.UserId)
	if err != nil {
		return nil, err
	}
	resp := &favorv1.GetFavoritesListResp{FavoriteList: make([]*favorv1.FavoriteDetail, len(list))}
	for i, favorite := range list {
		resp.FavoriteList[i] = &favorv1.FavoriteDetail{
			FavoriteId: favorite.FavoritesID, FavoriteName: favorite.FavoritesName,
			Description: favorite.Description, IsPrivate: favorite.IsPrivate == -1,
		}
	}
	return resp, nil
}

// GetFavoriteInfo 返回收藏夹 ID、所有者和布尔隐私标记，仅用于可信内部 RPC。
// 输入：收藏夹 ID；输出：元信息或业务错误，不增加外部 HTTP 入口。
func (fs *FavoritesService) GetFavoriteInfo(ctx context.Context, req *favorv1.GetFavoriteInfoReq) (*favorv1.GetFavoriteInfoResp, error) {
	favorite, err := fs.favorites.GetFavoriteInfo(ctx, req.GetFavoriteId())
	if err != nil {
		return nil, err
	}
	return &favorv1.GetFavoriteInfoResp{FavoriteId: favorite.FavoritesID, UserId: favorite.UserID, IsPrivate: favorite.IsPrivate == -1}, nil
}

// ModifyFavorites 将可信操作人和可选属性交给业务层执行归属校验。
func (fs *FavoritesService) ModifyFavorites(ctx context.Context, req *favorv1.ModifyFavoritesReq) (*favorv1.ModifyFavoritesResp, error) {
	favorites := &domain.FavoritesInfo{FavoritesId: req.FavoritesId, UserId: req.UserId}
	favorites.FavoritesName = req.FavoritesName
	favorites.IsPrivate = req.IsPrivate
	favorites.Description = req.Description

	err := fs.favorites.ModifyFavorites(ctx, favorites)
	if err != nil {
		return nil, err
	}
	return &favorv1.ModifyFavoritesResp{
		StatusCode: 200,
		Msg:        "修改收藏夹信息成功",
	}, nil
}

// DeleteFavorites 由业务层校验操作人归属，并维持非空收藏夹禁止删除的规则。
func (fs *FavoritesService) DeleteFavorites(ctx context.Context, req *favorv1.DeleteFavoritesReq) (*favorv1.DeleteFavoritesResp, error) {
	err := fs.favorites.DeleteFavorites(ctx, req.UserId, req.FavoritesId)
	if err != nil {
		return nil, err
	}
	return &favorv1.DeleteFavoritesResp{
		StatusCode: 200,
		Msg:        "删除收藏夹成功",
	}, nil
}
