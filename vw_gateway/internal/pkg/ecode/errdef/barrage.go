package errdef

import (
	kerr "github.com/go-kratos/kratos/v2/errors"
	"vw_gateway/internal/pkg/ecode"
)

// ErrInvalidBarrageLike 表示必须提供 liked。
var ErrInvalidBarrageLike = kerr.New(ecode.InvalidBarrageLike, "INVALID_BARRAGE_LIKE", "必须提供 liked")
