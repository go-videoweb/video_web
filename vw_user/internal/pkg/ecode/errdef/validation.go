package errdef

import (
	kerr "github.com/go-kratos/kratos/v2/errors"
	"vw_user/internal/pkg/ecode"
)

// ErrInvalidUserID 表示用户 ID 必须为正数。
var ErrInvalidUserID = kerr.New(ecode.InvalidUserID, "INVALID_USER_ID", "用户 ID 必须为正数")

// ErrUserNotFoundByID 表示用户不存在。
var ErrUserNotFoundByID = kerr.New(ecode.UserNotFoundByID, "USER_NOT_FOUND", "用户不存在")
