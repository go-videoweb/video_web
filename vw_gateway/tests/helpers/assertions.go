package helpers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

// AssertStatus 断言 HTTP 状态码与预期一致。
// 输入：t 为测试上下文；response 为待检查响应；want 为期望状态码；message 为失败提示。
// 输出：断言通过时返回；响应为空或状态码不匹配时调用 t.Fatalf 终止当前测试。
// 示例：helpers.AssertStatus(t, resp, http.StatusCreated, "创建视频")。
func AssertStatus(t *testing.T, response *Response, want int, message string) {
	t.Helper()
	if response == nil {
		t.Fatalf("%s: 响应为空", message)
	}
	if response.StatusCode != want {
		t.Fatalf("%s: HTTP 状态码=%d，期望=%d，body=%q", message, response.StatusCode, want, response.Text())
	}
}

// AssertHTTP2xx 断言响应为 HTTP 2xx。
// 输入：t 为测试上下文；response 为待检查响应；message 为失败提示。
// 输出：响应状态处于 2xx 时返回；响应为空或状态不成功时调用 t.Fatalf。
// 示例：helpers.AssertHTTP2xx(t, resp, "获取视频详情")。
func AssertHTTP2xx(t *testing.T, response *Response, message string) {
	t.Helper()
	if response == nil || !response.IsSuccess() {
		if response == nil {
			t.Fatalf("%s: 响应为空", message)
		}
		t.Fatalf("%s: HTTP 状态码=%d，body=%q", message, response.StatusCode, response.Text())
	}
}

// AssertJSONEqual 以 JSON 结构比较两个值。
// 输入：t 为测试上下文；got 和 want 为可 JSON 编码的实际值与期望值；message 为失败提示。
// 输出：两个值的 JSON 结构等价时返回；编码失败或结构不一致时调用 t.Fatalf。
// 示例：helpers.AssertJSONEqual(t, map[string]any{"ok": true}, map[string]any{"ok": true}, "响应结构")。
func AssertJSONEqual(t *testing.T, got, want any, message string) {
	t.Helper()
	gotJSON, err := normalizeJSON(got)
	if err != nil {
		t.Fatalf("%s: actual JSON 无法解析: %v", message, err)
	}
	wantJSON, err := normalizeJSON(want)
	if err != nil {
		t.Fatalf("%s: expected JSON 无法解析: %v", message, err)
	}
	if !reflect.DeepEqual(gotJSON, wantJSON) {
		t.Fatalf("%s: JSON 不一致，actual=%v，expected=%v", message, gotJSON, wantJSON)
	}
}

// normalizeJSON 将任意可编码值解码为使用 json.Number 的 JSON 结构。
// 输入：value 为可被 json.Marshal 编码的值。
// 输出：返回统一的 JSON 结构；编码或解码失败时返回错误。
// 示例：normalized, err := normalizeJSON(map[string]int{"id": 1})。
func normalizeJSON(value any) (any, error) {
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("编码 JSON 失败: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var normalized any
	if err := decoder.Decode(&normalized); err != nil {
		return nil, err
	}
	return normalized, nil
}
