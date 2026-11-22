package errdef

import (
	kerr "github.com/go-kratos/kratos/v2/errors"
	"vw_user/internal/pkg/ecode"
)

// ErrInvalidClientIP 表示无法识别请求来源。
var ErrInvalidClientIP = kerr.New(ecode.InvalidClientIP, "INVALID_CLIENT_IP", "无法识别请求来源")

// ErrInvalidCodeExpiration 表示验证码有效期必须为五分钟。
var ErrInvalidCodeExpiration = kerr.New(ecode.InvalidCodeExpiration, "INVALID_CODE_EXPIRATION", "验证码有效期必须为五分钟")

// ErrInvalidEmailCode 表示验证码无效或已失效，请重新获取。
var ErrInvalidEmailCode = kerr.New(ecode.InvalidEmailCode, "INVALID_EMAIL_CODE", "验证码无效或已失效，请重新获取")

// ErrEmailCodeRateLimited 表示验证码请求过于频繁，请稍后再试。
var ErrEmailCodeRateLimited = kerr.New(ecode.EmailCodeRateLimited, "EMAIL_CODE_RATE_LIMITED", "验证码请求过于频繁，请稍后再试")
