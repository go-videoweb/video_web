package helpers

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"testing"
)

// ReadAndCloseRawBody 读取原始 HTTP 响应体并负责关闭 Body。
func ReadAndCloseRawBody(resp *http.Response) ([]byte, error) {
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return body, nil
}

// UnmarshalResponse 将原始响应体反序列化为统一 Response 结构。
func UnmarshalResponse(body []byte) (*Response, error) {
	var resp Response
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, fmt.Errorf("反序列化响应失败: %w, body=%q", err, string(body))
	}
	return &resp, nil
}

// ObjectData 读取统一响应中的 object 类型 data。
func ObjectData(resp *Response, message string) (map[string]any, error) {
	dataMap, ok := resp.Data.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s: data 不是 object: %T", message, resp.Data)
	}
	return dataMap, nil
}

// ArrayData 读取统一响应中的数组类型 data。
func ArrayData(resp *Response, message string) ([]any, error) {
	list, ok := resp.Data.([]any)
	if !ok {
		return nil, fmt.Errorf("%s: data 不是数组: %T", message, resp.Data)
	}
	return list, nil
}

// ObjectListData 读取统一响应中的 object 数组类型 data。
func ObjectListData(resp *Response, message string) ([]map[string]any, error) {
	dataList, ok := resp.Data.([]any)
	if !ok {
		return nil, fmt.Errorf("%s: data 不是数组: %T", message, resp.Data)
	}
	result := make([]map[string]any, 0, len(dataList))
	for i, item := range dataList {
		m, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("%s: data 第 %d 项不是 object: %T", message, i, item)
		}
		result = append(result, m)
	}
	return result, nil
}

// AssertJSONEquivalent 以 JSON 归一化后的结构比较两个值是否等价。
func AssertJSONEquivalent(t *testing.T, got, want any, message string) {
	t.Helper()

	normalizedGot, err := NormalizeJSONValue(got)
	if err != nil {
		t.Errorf("%s: actual JSON 归一化失败: %v", message, err)
		return
	}
	normalizedWant, err := NormalizeJSONValue(want)
	if err != nil {
		t.Errorf("%s: expected JSON 归一化失败: %v", message, err)
		return
	}
	if !reflect.DeepEqual(normalizedGot, normalizedWant) {
		gotBytes, _ := json.Marshal(normalizedGot)
		wantBytes, _ := json.Marshal(normalizedWant)
		t.Errorf("%s: JSON 不一致，actual=%s, expected=%s", message, string(gotBytes), string(wantBytes))
	}
}

// AssertSSEContentType 断言原始响应是 SSE 流。
func AssertSSEContentType(t *testing.T, resp *http.Response, message string) {
	t.Helper()

	contentType := resp.Header.Get("Content-Type")
	if !strings.Contains(contentType, "text/event-stream") {
		t.Fatalf("%s: 期望 Content-Type 包含 text/event-stream，实际=%q", message, contentType)
	}
}

// ReadSSEEvent 从 SSE 响应中读取一个完整事件。
func ReadSSEEvent(reader *bufio.Reader) (string, []byte, error) {
	var eventName string
	dataLines := make([]string, 0, 1)

	for {
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", nil, fmt.Errorf("读取 SSE 事件失败: %w", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			break
		}
		switch {
		case strings.HasPrefix(line, "event: "):
			eventName = strings.TrimSpace(strings.TrimPrefix(line, "event: "))
		case strings.HasPrefix(line, "data: "):
			dataLines = append(dataLines, strings.TrimPrefix(line, "data: "))
		}
	}

	if eventName == "" {
		return "", nil, fmt.Errorf("SSE 事件缺少 event 字段")
	}
	return eventName, []byte(strings.Join(dataLines, "\n")), nil
}

// NormalizeJSONValue 将任意 JSON 兼容值归一化为可直接做 DeepEqual 比较的结构。
func NormalizeJSONValue(value any) (any, error) {
	var raw []byte
	switch v := value.(type) {
	case []byte:
		raw = v
	case json.RawMessage:
		raw = v
	default:
		var err error
		raw, err = json.Marshal(v)
		if err != nil {
			return nil, fmt.Errorf("JSON 序列化失败: %w", err)
		}
	}

	var normalized any
	if err := json.Unmarshal(raw, &normalized); err != nil {
		return nil, fmt.Errorf("JSON 反序列化失败: %w, raw=%s", err, string(raw))
	}
	return normalized, nil
}
