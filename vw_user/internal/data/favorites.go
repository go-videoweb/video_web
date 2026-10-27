package data

import (
	"context"
	stderr "errors"
	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"gorm.io/gorm"
	"vw_user/internal/biz"
	"vw_user/internal/data/dal/model"
)

type favoritesRepo struct {
	logger *log.Helper
	data   *Data
}

func NewFavoritesRepo(data *Data, logger log.Logger) biz.FavoritesRepo {
	return &favoritesRepo{
		logger: log.NewHelper(logger),
		data:   data,
	}
}

func (f *favoritesRepo) CheckFavoritesName(ctx context.Context, userId int64, favoritesName string) (bool, error) {
	favorites := getQuery(ctx).Favorite
	favoritesDo := favorites.WithContext(ctx)
	count, err := favoritesDo.Where(favorites.UserID.Eq(userId), favorites.FavoritesName.Eq(favoritesName)).Count()

	// if error is "record not found", set err to nil
	if err != nil && stderr.Is(err, gorm.ErrRecordNotFound) {
		err = nil
	}
	return count == 0, err
}

func (f *favoritesRepo) CreateFavorites(ctx context.Context, favorite *model.Favorite) error {
	favorites := getQuery(ctx).Favorite
	favoritesDo := favorites.WithContext(ctx)
	return favoritesDo.Create(favorite)
}

// GetFavoritesList 仅查询指定用户的收藏夹，包含公开和私密记录，并按 ID 升序稳定返回。
// 使用请求上下文和既有事务查询器；数据库错误原样返回。
func (f *favoritesRepo) GetFavoritesList(ctx context.Context, userId int64) ([]*model.Favorite, error) {
	favorites := getQuery(ctx).Favorite
	return favorites.WithContext(ctx).Where(favorites.UserID.Eq(userId)).Order(favorites.FavoritesID.Asc()).Find()
}

// DeleteFavorites 同时按操作人和收藏夹 ID 删除，避免仅按资源 ID 越权写入。
func (f *favoritesRepo) DeleteFavorites(ctx context.Context, userId, favoritesId int64) error {
	favorites := getQuery(ctx).Favorite
	favoritesDo := favorites.WithContext(ctx)
	result, err := favoritesDo.Where(favorites.FavoritesID.Eq(favoritesId), favorites.UserID.Eq(userId)).Delete(&model.Favorite{})
	if err != nil {
		return err
	}
	if result.RowsAffected == 0 {
		return kerrors.NotFound("FAVORITE_NOT_FOUND", "收藏夹不存在")
	}
	return nil
}

func (f *favoritesRepo) CheckIfFavoritesEmpty(ctx context.Context, favoritesId int64) (bool, error) {
	favoritesVideo := getQuery(ctx).FavoriteVideo
	favoritesVideoDo := favoritesVideo.WithContext(ctx)
	count, err := favoritesVideoDo.Where(favoritesVideo.FavoritesID.Eq(favoritesId)).Count()

	// if error is "record not found", set err to nil
	if err != nil && stderr.Is(err, gorm.ErrRecordNotFound) {
		err = nil
	}
	return count == 0, err
}

// UpdateFavorites 将用户 ID 只用于归属筛选，禁止通过属性更新改变收藏夹所有者。
func (f *favoritesRepo) UpdateFavorites(ctx context.Context, favorite map[string]any) error {
	favorites := getQuery(ctx).Favorite
	favoritesDo := favorites.WithContext(ctx)
	userId, userOK := favorite["user_id"].(int64)
	favoritesId, favoriteOK := favorite["favorites_id"].(int64)
	if !userOK || !favoriteOK || userId <= 0 || favoritesId <= 0 {
		return kerrors.BadRequest("INVALID_FAVORITE", "用户和收藏夹 ID 必须为正数")
	}
	updates := make(map[string]any)
	for _, field := range []string{"favorites_name", "is_private", "description"} {
		if value, ok := favorite[field]; ok {
			updates[field] = value
		}
	}
	if len(updates) == 0 {
		return nil
	}
	_, err := favoritesDo.Where(favorites.FavoritesID.Eq(favoritesId), favorites.UserID.Eq(userId)).Updates(updates)
	return err
}

func (f *favoritesRepo) GetFavoritesMetadata(ctx context.Context, favoritesId int64) (*model.Favorite, error) {
	favorites := getQuery(ctx).Favorite
	favoritesDo := favorites.WithContext(ctx)
	ret, err := favoritesDo.Where(favorites.FavoritesID.Eq(favoritesId)).First()
	if err != nil {
		return nil, err
	}
	return ret, nil
}
