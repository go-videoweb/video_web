package codecs

import (
	stdhttp "net/http"
	"strconv"

	"github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/transport/http"
	"util/helper"
)

// ErrorEncoder 以 HTTP 200 返回 API 错误，客户端从响应体读取 code 和 reason。
// 下游 gRPC 携带原始业务码时恢复到 code，并保留其他错误元数据。
func ErrorEncoder(w stdhttp.ResponseWriter, r *stdhttp.Request, err error) {
	se := errors.FromError(err)
	if code, parseErr := strconv.ParseInt(se.Metadata[helper.RPCBusinessCodeKey], 10, 32); parseErr == nil && code > 599 {
		se = errors.Clone(se)
		se.Code = int32(code)
		delete(se.Metadata, helper.RPCBusinessCodeKey)
	}
	codec, _ := http.CodecForRequest(r, "Accept")
	contentType := ContentType(codec.Name())
	body, err := codec.Marshal(se)
	if err != nil {
		contentType = "application/json"
		body = []byte(`{"code":500,"reason":"ERROR_ENCODING_FAILED","message":"服务器内部错误"}`)
	}
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(stdhttp.StatusOK)
	_, _ = w.Write(body)
}
