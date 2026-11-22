package errdef

import (
	kerr "github.com/go-kratos/kratos/v2/errors"
	"vw_gateway/internal/pkg/ecode"
)

// ErrFavoriteIDMissing 表示用户服务未返回有效的收藏夹 ID，请先核对创建结果，勿重复提交。
var ErrFavoriteIDMissing = kerr.New(ecode.FavoriteIDMissing, "FAVORITE_ID_MISSING", "用户服务未返回有效的收藏夹 ID，请先核对创建结果，勿重复提交")

// ErrFavoritesListMissing 表示用户服务未返回收藏夹列表响应。
var ErrFavoritesListMissing = kerr.New(ecode.FavoritesListMissing, "FAVORITES_LIST_MISSING", "用户服务未返回收藏夹列表响应")

// ErrFavoriteVideosMissing 表示视频服务未返回收藏夹内容响应。
var ErrFavoriteVideosMissing = kerr.New(ecode.FavoriteVideosMissing, "FAVORITE_VIDEOS_MISSING", "视频服务未返回收藏夹内容响应")

// ErrFavoriteVideoInvalid 表示视频服务返回了缺少视频摘要的收藏记录。
var ErrFavoriteVideoInvalid = kerr.New(ecode.FavoriteVideoInvalid, "FAVORITE_VIDEO_INVALID", "视频服务返回了缺少视频摘要的收藏记录")
