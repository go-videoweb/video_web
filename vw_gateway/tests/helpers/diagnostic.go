package helpers

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Diagnostic 返回可写入 CI 日志的有限诊断，不输出响应头或响应中的字符串值。
// 仅保留 JSON 字段结构、数字和布尔值；未知文本响应省略正文，防止凭证通过错误消息泄露。
func (r *Response) Diagnostic() string {
	if r == nil {
		return "响应为空"
	}
	var value any
	if err := r.DecodeJSON(&value); err != nil {
		return fmt.Sprintf("HTTP=%d，非 JSON 正文已省略（%d 字节）", r.StatusCode, len(r.Body))
	}
	data, err := json.Marshal(redactDiagnostic(value))
	if err != nil {
		return fmt.Sprintf("HTTP=%d，正文已省略", r.StatusCode)
	}
	const limit = 2048
	text := string(data)
	if len(text) > limit {
		text = strings.ToValidUTF8(text[:limit], "") + "...[截断]"
	}
	return fmt.Sprintf("HTTP=%d，body=%s", r.StatusCode, text)
}

// redactDiagnostic 递归省略字符串及 metadata；业务内容不因字段名称未知而泄露。
func redactDiagnostic(value any) any {
	switch v := value.(type) {
	case string:
		return "[已省略]"
	case map[string]any:
		result := make(map[string]any, len(v))
		for key, item := range v {
			if strings.EqualFold(key, "metadata") {
				result[key] = "[已省略]"
				continue
			}
			result[key] = redactDiagnostic(item)
		}
		return result
	case []any:
		result := make([]any, len(v))
		for i, item := range v {
			result[i] = redactDiagnostic(item)
		}
		return result
	default:
		return value
	}
}
