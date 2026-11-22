package errdef

import kerr "github.com/go-kratos/kratos/v2/errors"

// WithMessage 返回带有场景提示的独立错误副本，防止并发请求改写公共错误。
// 错误码、reason 和已有 metadata 沿用公共定义。
func WithMessage(base *kerr.Error, message string) *kerr.Error {
	err := kerr.Clone(base)
	err.Message = message
	return err
}
