package errdef

import (
	kerr "github.com/go-kratos/kratos/v2/errors"
	"vw_gateway/internal/pkg/ecode"
)

// ErrRegistrationSessionFailed 表示账号已创建，但登录态建立失败，请登录后继续。
var ErrRegistrationSessionFailed = kerr.New(ecode.RegistrationSessionFailed, "REGISTRATION_SESSION_FAILED", "账号已创建，但登录态建立失败，请登录后继续")
