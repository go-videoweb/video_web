package errdef

import (
	kerr "github.com/go-kratos/kratos/v2/errors"
	"vw_gateway/internal/pkg/ecode"
)

func init() {
	INTERNAL_ERROR = kerr.New(ecode.INTERNAL_ErrInternal, "internal error", "服务内部错误，请稍后再试")
}

var (
	INTERNAL_ERROR *kerr.Error
)

// ErrInvalidPublicSiteOrigin 表示公开站点地址缺失或格式无效。
var ErrInvalidPublicSiteOrigin = kerr.New(ecode.InvalidPublicSiteOrigin, "INVALID_PUBLIC_SITE_ORIGIN", "分享站点地址未配置或格式无效")

// ErrErrorEncodingFailed 表示响应错误编码失败。
var ErrErrorEncodingFailed = kerr.New(ecode.ErrorEncodingFailed, "ERROR_ENCODING_FAILED", "服务器内部错误")
