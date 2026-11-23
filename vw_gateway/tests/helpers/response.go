package helpers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

// Response 保留一次 HTTP 请求的状态、响应头和原始响应体。
type Response struct {
	StatusCode int
	Status     string
	Header     http.Header
	Body       []byte
}

// IsSuccess 返回 HTTP 状态是否处于 2xx 范围。
// 输入：使用接收者中的 StatusCode 判断，nil 响应也属于失败。
// 输出：状态码为 200 至 299 时返回 true；响应为空或状态码不在 2xx 时返回 false。
// 示例：if !resp.IsSuccess() { t.Fatalf("请求失败") }。
func (r *Response) IsSuccess() bool { return r != nil && r.StatusCode >= 200 && r.StatusCode < 300 }

// Text 返回未经修改的响应体文本。
// 输入：使用接收者中的 Body 字节；nil 响应按空文本处理。
// 输出：返回 Body 转换后的字符串，不执行字符集转换或 JSON 解析。
// 示例：body := resp.Text()。
func (r *Response) Text() string {
	if r == nil {
		return ""
	}
	return string(r.Body)
}

// HeaderValue 返回指定响应头的值。
// 输入：name 为 HTTP 响应头名称，查找遵循 net/http 的大小写不敏感规则。
// 输出：返回响应头值；响应为空、响应头为空或名称不存在时返回空字符串。
// 示例：requestID := resp.HeaderValue("X-Request-ID")。
func (r *Response) HeaderValue(name string) string {
	if r == nil || r.Header == nil {
		return ""
	}
	return r.Header.Get(name)
}

// DecodeJSON 使用 UseNumber 解码 JSON，避免大整数先转换为 float64。
// 输入：dst 为可写入解码结果的指针；响应 Body 必须是有效 JSON。
// 输出：解码成功返回 nil；响应为空或 JSON 无效时返回错误。
// 示例：var payload map[string]any; err := resp.DecodeJSON(&payload)。
func (r *Response) DecodeJSON(dst any) error {
	if r == nil {
		return fmt.Errorf("响应为空")
	}
	decoder := json.NewDecoder(bytes.NewReader(r.Body))
	decoder.UseNumber()
	if err := decoder.Decode(dst); err != nil {
		return fmt.Errorf("解析 JSON 响应失败: %w", err)
	}
	return nil
}

// JSONMap 将 JSON 对象响应解码为保留数字精度的 map。
// 输入：使用接收者中的 Body，Body 必须是 JSON 对象。
// 输出：返回 map[string]any；响应为空、JSON 无效或顶层不是对象时返回错误。
// 示例：payload, err := resp.JSONMap()。
func (r *Response) JSONMap() (map[string]any, error) {
	var value map[string]any
	if err := r.DecodeJSON(&value); err != nil {
		return nil, err
	}
	return value, nil
}
