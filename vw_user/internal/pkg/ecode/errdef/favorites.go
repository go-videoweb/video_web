package errdef

import (
	kerr "github.com/go-kratos/kratos/v2/errors"
	"vw_user/internal/pkg/ecode"
)

func init() {
	ErrCreateFavoriteFailed = kerr.New(ecode.FAVORITES_CreateFavoritesFailed, "创建收藏夹失败", "创建收藏夹失败, 请稍后再试")
	ErrFavoritesNameConflict = kerr.New(ecode.FAVORITES_FavoritesNameConflict, "收藏夹名称已存在", "收藏夹名称已存在, 请重新输入")
	ErrDeleteFavoriteFailed = kerr.New(ecode.FAVORITES_DeleteFavoritesFailed, "删除收藏夹失败", "删除收藏夹失败, 请稍后再试")
	ErrModifyFavoriteFailed = kerr.New(ecode.FAVORITES_ModifyFavoritesFailed, "修改收藏夹失败", "修改收藏夹失败, 请稍后再试")
	ErrFavoritesNotEmpty = kerr.New(ecode.FAVORITES_FavoritesNotEmpty, "收藏夹不为空", "收藏夹不为空, 请先清空收藏夹再删除")
	ErrGetFavoritesListFailed = kerr.New(ecode.FAVORITES_GetFavoritesListFailed, "获取收藏夹列表失败", "获取收藏夹列表失败，请稍后再试")
}

var (
	// ErrGetFavoritesListFailed 表示收藏夹列表查询失败，不用于表示空列表。
	ErrGetFavoritesListFailed *kerr.Error
	ErrCreateFavoriteFailed   *kerr.Error
	ErrFavoritesNameConflict  *kerr.Error
	ErrModifyFavoriteFailed   *kerr.Error

	ErrDeleteFavoriteFailed *kerr.Error
	ErrFavoritesNotEmpty    *kerr.Error
)
