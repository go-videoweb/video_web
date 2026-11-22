package errdef

import (
	kerr "github.com/go-kratos/kratos/v2/errors"
	"vw_gateway/internal/pkg/ecode"
)

// ErrInvalidCommentLike 表示必须提供 liked。
var ErrInvalidCommentLike = kerr.New(ecode.InvalidCommentLike, "INVALID_COMMENT_LIKE", "必须提供 liked")
