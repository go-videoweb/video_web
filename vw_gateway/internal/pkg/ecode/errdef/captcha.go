package errdef

import (
	kerr "github.com/go-kratos/kratos/v2/errors"
	"vw_gateway/internal/pkg/ecode"
)

// ErrInvalidClientIP 表示无法识别请求来源。
var ErrInvalidClientIP = kerr.New(ecode.InvalidClientIP, "INVALID_CLIENT_IP", "无法识别请求来源")

// ErrEmailCodeFailed 表示验证码生成失败。
var ErrEmailCodeFailed = kerr.New(ecode.EmailCodeFailed, "EMAIL_CODE_FAILED", "验证码生成失败")

// ErrEmailSendFailed 表示验证码发送失败，请稍后重试。
var ErrEmailSendFailed = kerr.New(ecode.EmailSendFailed, "EMAIL_SEND_FAILED", "验证码发送失败，请稍后重试")

// ErrEmailCodeCacheFailed 表示验证码缓存或用户服务调用失败。
var ErrEmailCodeCacheFailed = kerr.New(ecode.EmailCodeCacheFailed, "EMAIL_CODE_CACHE_FAILED", "验证码保存失败，请稍后重试")
