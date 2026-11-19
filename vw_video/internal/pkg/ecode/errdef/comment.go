package errdef

import (
	kerr "github.com/go-kratos/kratos/v2/errors"
	"vw_video/internal/pkg/ecode"
)

// ErrInvalidComment 表示评论参数无效。
var ErrInvalidComment = kerr.New(ecode.InvalidComment, "INVALID_COMMENT", "评论参数无效")

// ErrCommentCountOverflow 表示评论总数超出响应范围。
var ErrCommentCountOverflow = kerr.New(ecode.CommentCountOverflow, "COMMENT_COUNT_OVERFLOW", "评论总数超出响应范围")

// ErrInvalidRootComment 表示必须提供主评论 ID。
var ErrInvalidRootComment = kerr.New(ecode.InvalidRootComment, "INVALID_ROOT_COMMENT", "必须提供主评论 ID")

// ErrCommentNotFound 表示主评论不存在。
var ErrCommentNotFound = kerr.New(ecode.CommentNotFound, "COMMENT_NOT_FOUND", "主评论不存在")

// ErrInvalidCommentLike 表示必须提供身份、视频、评论及点赞目标状态。
var ErrInvalidCommentLike = kerr.New(ecode.InvalidCommentLike, "INVALID_COMMENT_LIKE", "必须提供身份、视频、评论及点赞目标状态")

// ErrInvalidCommentQuery 表示评论范围、排序或游标无效。
var ErrInvalidCommentQuery = kerr.New(ecode.InvalidCommentQuery, "INVALID_COMMENT_QUERY", "评论范围、排序或游标无效")

// ErrCommentRootChanged 表示主评论创建失败。
var ErrCommentRootChanged = kerr.New(ecode.CommentRootChanged, "COMMENT_ROOT_CHANGED", "主评论创建失败")

// ErrInvalidCommentRoot 表示评论楼层关系异常。
var ErrInvalidCommentRoot = kerr.New(ecode.InvalidCommentRoot, "INVALID_COMMENT_ROOT", "评论楼层关系异常")

// ErrCommentLikeConflict 表示点赞关系异常。
var ErrCommentLikeConflict = kerr.New(ecode.CommentLikeConflict, "COMMENT_LIKE_CONFLICT", "点赞关系异常")

// ErrCommentSagaRemoved 表示评论点赞已改为本地事务，不支持旧补偿调用。
var ErrCommentSagaRemoved = kerr.New(ecode.CommentSagaRemoved, "COMMENT_SAGA_REMOVED", "评论点赞已改为本地事务，不支持旧补偿调用")
