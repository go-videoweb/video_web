package errdef

import (
	kerr "github.com/go-kratos/kratos/v2/errors"
	"vw_user/internal/pkg/ecode"
)

// ErrParseMultipartForm 表示请求参数解析失败。
var ErrParseMultipartForm = kerr.New(ecode.ParseMultipartForm, "ParseMultipartForm ERROR", "请求参数解析失败")

// ErrBindRequest 表示请求参数解析失败。
var ErrBindRequest = kerr.New(ecode.BindRequest, "BIND", "请求参数解析失败")

// ErrRequestCodec 表示请求参数解析失败。
var ErrRequestCodec = kerr.New(ecode.RequestCodec, "CODEC", "请求参数解析失败")
