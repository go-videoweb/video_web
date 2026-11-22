package errdef

import (
	kerr "github.com/go-kratos/kratos/v2/errors"
	"vw_gateway/internal/pkg/ecode"
)

// ErrRequestContextMissing 表示缺少请求上下文。
var ErrRequestContextMissing = kerr.New(ecode.RequestContextMissing, "REQUEST_CONTEXT_MISSING", "缺少请求上下文")

// ErrInvalidUserID 表示用户 ID 无效。
var ErrInvalidUserID = kerr.New(ecode.InvalidUserID, "INVALID_USER_ID", "用户 ID 无效")

// ErrInvalidIdentity 表示请先登录。
var ErrInvalidIdentity = kerr.New(ecode.InvalidIdentity, "INVALID_IDENTITY", "请先登录")

// ErrInvalidContext 表示缺少 HTTP 请求。
var ErrInvalidContext = kerr.New(ecode.InvalidContext, "INVALID_CONTEXT", "缺少 HTTP 请求")

// ErrInvalidRequestContext 表示服务器内部错误。
var ErrInvalidRequestContext = kerr.New(ecode.InvalidRequestContext, "invalid context", "服务器内部错误")

// ErrUnauthorized 表示请先登录。
var ErrUnauthorized = kerr.New(ecode.Unauthorized, "UNAUTHORIZED", "请先登录")

// ErrUserIDMissing 表示请先登录。
var ErrUserIDMissing = kerr.New(ecode.UserIDMissing, "USER_ID_MISSING", "请先登录")

// ErrInvalidUserIDUnauthorized 表示请先登录。
var ErrInvalidUserIDUnauthorized = kerr.New(ecode.InvalidUserIDUnauthorized, "INVALID_USER_ID", "请先登录")
