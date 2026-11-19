package errdef

import (
	kerr "github.com/go-kratos/kratos/v2/errors"
	"vw_video/internal/pkg/ecode"
)

// ErrFavoriteVersionConflict 表示视频版本已变化，请重试。
var ErrFavoriteVersionConflict = kerr.New(ecode.FavoriteVersionConflict, "FAVORITE_VERSION_CONFLICT", "视频版本已变化，请重试")

// ErrInvalidFavorite 表示收藏需要有效身份和明确操作，取消必须提供当前收藏记录 ID。
var ErrInvalidFavorite = kerr.New(ecode.InvalidFavorite, "INVALID_FAVORITE", "收藏需要有效身份和明确操作，取消必须提供当前收藏记录 ID")

// ErrFavoriteNotFound 表示收藏夹不存在。
var ErrFavoriteNotFound = kerr.New(ecode.FavoriteNotFound, "FAVORITE_NOT_FOUND", "收藏夹不存在")

// ErrFavoriteRelationConflict 表示收藏关系异常，请核对后重试。
var ErrFavoriteRelationConflict = kerr.New(ecode.FavoriteRelationConflict, "FAVORITE_RELATION_CONFLICT", "收藏关系异常，请核对后重试")

// ErrVideoAlreadyFavorited 表示视频已在其他收藏夹中，请先取消原收藏。
var ErrVideoAlreadyFavorited = kerr.New(ecode.VideoAlreadyFavorited, "VIDEO_ALREADY_FAVORITED", "视频已在其他收藏夹中，请先取消原收藏")

// ErrFavoriteRecordChanged 表示收藏记录已变化，请刷新后重试。
var ErrFavoriteRecordChanged = kerr.New(ecode.FavoriteRecordChanged, "FAVORITE_RECORD_CHANGED", "收藏记录已变化，请刷新后重试")

// ErrInvalidFavoritePage 表示需要有效身份、收藏夹 ID 和分页参数。
var ErrInvalidFavoritePage = kerr.New(ecode.InvalidFavoritePage, "INVALID_FAVORITE_PAGE", "需要有效身份、收藏夹 ID 和分页参数")

// ErrFavoriteServiceUnavailable 表示收藏夹服务不可用。
var ErrFavoriteServiceUnavailable = kerr.New(ecode.FavoriteServiceUnavailable, "FAVORITE_SERVICE_UNAVAILABLE", "收藏夹服务不可用")

// ErrInvalidFavoriteMetadata 表示收藏夹元信息异常。
var ErrInvalidFavoriteMetadata = kerr.New(ecode.InvalidFavoriteMetadata, "INVALID_FAVORITE_METADATA", "收藏夹元信息异常")

// ErrFavoriteCountConflict 表示视频收藏计数异常。
var ErrFavoriteCountConflict = kerr.New(ecode.FavoriteCountConflict, "FAVORITE_COUNT_CONFLICT", "视频收藏计数异常")
