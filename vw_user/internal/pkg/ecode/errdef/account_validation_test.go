package errdef

import (
	"errors"
	"testing"
	"util/helper"
)

// TestAccountValidationError 验证普通校验错误在服务边界转换为稳定业务错误，同时保留原始原因。
func TestAccountValidationError(t *testing.T) {
	for _, tc := range []struct {
		input  error
		reason string
		code   int32
	}{
		{helper.ErrInvalidEmailLength, "INVALID_EMAIL", ErrInvalidEmail.Code},
		{helper.ErrInvalidEmailASCII, "INVALID_EMAIL", ErrInvalidEmail.Code},
		{helper.ErrInvalidEmailFormat, "INVALID_EMAIL", ErrInvalidEmail.Code},
		{helper.ErrInvalidPassword, "INVALID_PASSWORD", ErrInvalidPassword.Code},
		{helper.ErrInvalidCodePurpose, "INVALID_CODE_PURPOSE", ErrInvalidCodePurpose.Code},
	} {
		t.Run(tc.input.Error(), func(t *testing.T) {
			got := AccountValidationError(tc.input)
			if !errors.Is(got, tc.input) {
				t.Fatal("转换丢失原始校验错误")
			}
			business, ok := got.(interface {
				GetCode() int32
				GetReason() string
				GetMessage() string
			})
			if !ok || business.GetCode() != tc.code || business.GetCode() <= 599 || business.GetReason() != tc.reason || business.GetMessage() != tc.input.Error() {
				t.Fatalf("业务错误契约变化：%v", got)
			}
		})
	}
	other := errors.New("其他底层错误")
	if AccountValidationError(other) != other || AccountValidationError(nil) != nil {
		t.Fatal("不属于公共校验的错误被改写")
	}
}

// TestWithMessageCopiesError 验证场景提示和 metadata 的修改不会污染公共定义或其他请求。
func TestWithMessageCopiesError(t *testing.T) {
	base := ErrInvalidEmail.WithMetadata(map[string]string{"field": "email"})
	first := WithMessage(base, "第一个请求")
	second := WithMessage(base, "第二个请求")
	first.Metadata["field"] = "changed"
	if base.Message != ErrInvalidEmail.Message || second.Message != "第二个请求" {
		t.Fatal("场景提示改写了公共错误或其他请求")
	}
	if base.Metadata["field"] != "email" || second.Metadata["field"] != "email" {
		t.Fatal("请求共享了可变 metadata")
	}
	if first.Code != base.Code || first.Reason != base.Reason {
		t.Fatal("场景提示改变了错误身份")
	}
}
