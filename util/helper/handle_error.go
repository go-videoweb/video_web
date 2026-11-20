package helper

import (
	"context"
	"errors"
	kerr "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/middleware"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"strconv"
	"strings"
)

// RPCBusinessCodeKey 标识随 gRPC ErrorInfo 传递的原始业务码。
const RPCBusinessCodeKey = "business_code"

// BusinessErrorMiddleware 在 RPC 返回前携带自定义业务码，供网关恢复响应中的 code。
// 同时用于普通调用和流式调用，具体错误由各服务定义。
func BusinessErrorMiddleware() middleware.Middleware {
	return func(next middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req interface{}) (interface{}, error) {
			resp, err := next(ctx, req)
			return resp, WithRPCBusinessCode(err)
		}
	}
}

// WithRPCBusinessCode 将自定义 Kratos 数字码放入错误详情，供跨 gRPC 调用后恢复。
// 输入：err 为服务返回的错误。复制业务错误后再写入元数据，以免修改共享错误定义。
func WithRPCBusinessCode(err error) error {
	var businessErr *kerr.Error
	if !errors.As(err, &businessErr) || businessErr.Code <= 599 {
		return err
	}
	rpcErr := kerr.Clone(businessErr)
	rpcErr.Metadata[RPCBusinessCodeKey] = strconv.Itoa(int(businessErr.Code))
	return rpcErr
}

func HandleError(kerror *kerr.Error, additionalErrs ...error) *kerr.Error {
	var retErr = kerr.Clone(kerror)

	if additionalErrs == nil {
		return retErr
	}

	for _, additionalErr := range additionalErrs {
		var aErr = &kerr.Error{}
		ok := errors.As(additionalErr, &aErr)

		// If additionalErr is not a kratos error,this is a standard-library error, just take it as the reason of kratos error.
		if !ok {
			retErr.Reason = retErr.Reason + ": " + additionalErr.Error()
		} else {
			// The additionalErr is another kratos error, merge it with the original kratos error.
			// merge their metadata, append the additional error's message to the original error's message,
			// and append the additional error's reason to the original error's reason.
			for k, v := range aErr.Metadata {
				retErr.Metadata[k] = v
			}
			retErr.Message = retErr.Message + ": " + aErr.Message
			retErr.Reason = retErr.Reason + ": " + aErr.Reason
		}
	}
	return retErr
}

// HandleGrpcError handles a grpc error with additional errors.
func HandleGrpcError(c codes.Code, msg string, additionalErrs ...error) error {
	builder := strings.Builder{}
	builder.WriteString(msg)
	builder.WriteString(": ")
	for _, e := range additionalErrs {
		builder.WriteString(e.Error())
		builder.WriteString("\n")
	}
	errmsg := builder.String()
	return status.New(c, errmsg).Err()
}
