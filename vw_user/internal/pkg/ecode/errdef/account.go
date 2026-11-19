package errdef

import (
	kerr "github.com/go-kratos/kratos/v2/errors"
	"vw_user/internal/pkg/ecode"
)

// ErrPasswordSessionInvalidationFailed 表示密码已更新，但登录态清理失败，请使用新密码重新登录并联系管理员处理旧会话。
var ErrPasswordSessionInvalidationFailed = kerr.New(ecode.PasswordSessionInvalidationFailed, "PASSWORD_SESSION_INVALIDATION_FAILED", "密码已更新，但登录态清理失败，请使用新密码重新登录并联系管理员处理旧会话")

// ErrEmailInUse 表示该邮箱已被使用。
var ErrEmailInUse = kerr.New(ecode.EmailInUse, "EMAIL_IN_USE", "该邮箱已被使用")

// ErrPasswordResetUnavailable 表示无法完成密码重置，请核对邮箱并重新获取验证码。
var ErrPasswordResetUnavailable = kerr.New(ecode.PasswordResetUnavailable, "PASSWORD_RESET_UNAVAILABLE", "无法完成密码重置，请核对邮箱并重新获取验证码")

// ErrEmailRelationConflict 表示邮箱归属异常，请联系管理员。
var ErrEmailRelationConflict = kerr.New(ecode.EmailRelationConflict, "EMAIL_RELATION_CONFLICT", "邮箱归属异常，请联系管理员")

// ErrPasswordResetConflict 表示账号状态或邮箱已变更，请重新获取验证码。
var ErrPasswordResetConflict = kerr.New(ecode.PasswordResetConflict, "PASSWORD_RESET_CONFLICT", "账号状态或邮箱已变更，请重新获取验证码")

// ErrAccountConflict 表示邮箱或用户名已被使用。
var ErrAccountConflict = kerr.New(ecode.AccountConflict, "ACCOUNT_CONFLICT", "邮箱或用户名已被使用")

// ErrEmailUpdateConflict 表示账号状态已变更，请重试。
var ErrEmailUpdateConflict = kerr.New(ecode.EmailUpdateConflict, "EMAIL_UPDATE_CONFLICT", "账号状态已变更，请重试")

// ErrPasswordUpdateConflict 表示账号状态已变更，请重试。
var ErrPasswordUpdateConflict = kerr.New(ecode.PasswordUpdateConflict, "PASSWORD_UPDATE_CONFLICT", "账号状态已变更，请重试")
