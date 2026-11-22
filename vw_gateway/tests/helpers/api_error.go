package helpers

import (
	"encoding/json"
	"fmt"
)

// APIError 是网关错误响应体；Code 为业务码，与 HTTP 状态独立。
// Metadata 保留刷新及部分成功信息，不能直接打印其中的凭证。
type APIError struct {
	Code     int32             `json:"code"`
	Reason   string            `json:"reason"`
	Message  string            `json:"message"`
	Metadata map[string]string `json:"metadata"`
}

// Error 仅输出错误标识，不输出可能含新 Token 的 metadata 或业务消息。
func (e *APIError) Error() string {
	return fmt.Sprintf("API 业务错误 code=%d", e.Code)
}

// BusinessError 识别完整的 Kratos 错误结构，不把 statusCode 或嵌套 resp 当作错误。
// 输出：正常业务 JSON 返回 nil；非 JSON、畸形错误结构返回解析错误，不打印原文。
func (r *Response) BusinessError() (*APIError, error) {
	var fields map[string]json.RawMessage
	if err := r.DecodeJSON(&fields); err != nil {
		return nil, err
	}
	if fields == nil {
		return nil, fmt.Errorf("业务响应必须是 JSON 对象")
	}
	if _, exists := fields["code"]; !exists {
		return nil, nil
	}
	if _, exists := fields["reason"]; !exists {
		return nil, fmt.Errorf("错误响应缺少 reason")
	}
	if _, exists := fields["message"]; !exists {
		return nil, fmt.Errorf("错误响应缺少 message")
	}
	var result APIError
	if err := r.DecodeJSON(&result); err != nil {
		return nil, err
	}
	if result.Code <= 0 {
		return nil, fmt.Errorf("业务错误码必须为正数")
	}
	return &result, nil
}
