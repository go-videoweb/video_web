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
var ErrInvalidPublicSiteOrigin = kerr.InternalServer("INVALID_PUBLIC_SITE_ORIGIN", "分享站点地址未配置或格式无效")
