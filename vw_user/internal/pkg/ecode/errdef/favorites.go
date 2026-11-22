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

// ErrInvalidFavorite 表示创建收藏夹需要有效用户和完整的收藏夹信息。
var ErrInvalidFavorite = kerr.New(ecode.InvalidFavorite, "INVALID_FAVORITE", "创建收藏夹需要有效用户和完整的收藏夹信息")

// ErrInvalidFavoritePrivacy 表示收藏夹隐私标记必须为 1（公开）或 -1（私密）。
var ErrInvalidFavoritePrivacy = kerr.New(ecode.InvalidFavoritePrivacy, "INVALID_FAVORITE_PRIVACY", "收藏夹隐私标记必须为 1（公开）或 -1（私密）")

// ErrFavoriteNotFound 表示收藏夹不存在。
var ErrFavoriteNotFound = kerr.New(ecode.FavoriteNotFound, "FAVORITE_NOT_FOUND", "收藏夹不存在")

// ErrInvalidFavoriteMetadata 表示收藏夹元信息异常。
var ErrInvalidFavoriteMetadata = kerr.New(ecode.InvalidFavoriteMetadata, "INVALID_FAVORITE_METADATA", "收藏夹元信息异常")
