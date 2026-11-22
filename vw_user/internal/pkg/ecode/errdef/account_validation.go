package errdef

import (
	"errors"
	"util/helper"
	"vw_user/internal/pkg/ecode"

	kerr "github.com/go-kratos/kratos/v2/errors"
)

// ErrInvalidEmail 表示邮箱格式或长度不符合要求。
var ErrInvalidEmail = kerr.New(ecode.InvalidEmail, "INVALID_EMAIL", "邮箱格式不正确")

// ErrInvalidPassword 表示密码不符合长度或编码要求。
var ErrInvalidPassword = kerr.New(ecode.InvalidPassword, "INVALID_PASSWORD", "密码长度必须为6至72字节")

// ErrInvalidCodePurpose 表示验证码用途无效。
var ErrInvalidCodePurpose = kerr.New(ecode.InvalidCodePurpose, "INVALID_CODE_PURPOSE", "验证码用途不正确")

// AccountValidationError 将公共校验错误转换为本服务的业务错误，保留具体提示与错误原因。
// 其他错误原样向上传递。
func AccountValidationError(err error) error {
	switch {
	case errors.Is(err, helper.ErrInvalidEmailLength), errors.Is(err, helper.ErrInvalidEmailASCII), errors.Is(err, helper.ErrInvalidEmailFormat):
		return WithMessage(ErrInvalidEmail, err.Error()).WithCause(err)
	case errors.Is(err, helper.ErrInvalidPassword):
		return ErrInvalidPassword.WithCause(err)
	case errors.Is(err, helper.ErrInvalidCodePurpose):
		return ErrInvalidCodePurpose.WithCause(err)
	default:
		return err
	}
}
