package errdef

import (
	kerr "github.com/go-kratos/kratos/v2/errors"
	"vw_video/internal/pkg/ecode"
)

// ErrInvalidBarrageQuery 表示弹幕时间范围或游标无效。
var ErrInvalidBarrageQuery = kerr.New(ecode.InvalidBarrageQuery, "INVALID_BARRAGE_QUERY", "弹幕时间范围或游标无效")

// ErrInvalidBarrageLike 表示必须提供身份、视频、弹幕及点赞目标状态。
var ErrInvalidBarrageLike = kerr.New(ecode.InvalidBarrageLike, "INVALID_BARRAGE_LIKE", "必须提供身份、视频、弹幕及点赞目标状态")

// ErrInvalidBarrage 表示弹幕内容、颜色或播放位置无效。
var ErrInvalidBarrage = kerr.New(ecode.InvalidBarrage, "INVALID_BARRAGE", "弹幕内容、颜色或播放位置无效")

// ErrBarrageNotFound 表示弹幕不存在。
var ErrBarrageNotFound = kerr.New(ecode.BarrageNotFound, "BARRAGE_NOT_FOUND", "弹幕不存在")

// ErrBarrageLikeConflict 表示点赞关系异常。
var ErrBarrageLikeConflict = kerr.New(ecode.BarrageLikeConflict, "BARRAGE_LIKE_CONFLICT", "点赞关系异常")
