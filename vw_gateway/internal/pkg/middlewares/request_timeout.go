package middlewares

import (
	"context"
	"time"

	"github.com/go-kratos/kratos/v2/middleware"
	"github.com/go-kratos/kratos/v2/transport"
	videoinfov1 "vw_gateway/api/v1/video/videoinfo"
)

// RequestTimeout 为原片转发保留 100 秒，其余接口使用传入的默认业务处理期限。
// 请求解析完毕后开始计时，客户端取消仍会传到下游。
func RequestTimeout(defaultTimeout time.Duration) middleware.Middleware {
	return func(next middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req any) (any, error) {
			timeout := defaultTimeout
			if tr, ok := transport.FromServerContext(ctx); ok && tr.Operation() == videoinfov1.OperationVideoInfoUploadVideoFile {
				timeout = 100 * time.Second
			}
			if timeout <= 0 {
				return next(ctx, req)
			}
			ctx, cancel := context.WithTimeout(ctx, timeout)
			defer cancel()
			return next(ctx, req)
		}
	}
}
