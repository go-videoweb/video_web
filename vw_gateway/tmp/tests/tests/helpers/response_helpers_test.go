package helpers

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestReadAndCloseRawBody(t *testing.T) {
	resp := &http.Response{Body: io.NopCloser(strings.NewReader("hello"))}
	body, err := ReadAndCloseRawBody(resp)
	if err != nil {
		t.Fatalf("ReadAndCloseRawBody() error = %v", err)
	}
	if string(body) != "hello" {
		t.Fatalf("ReadAndCloseRawBody() = %q, want %q", string(body), "hello")
	}
}

func TestUnmarshalResponse(t *testing.T) {
	resp, err := UnmarshalResponse([]byte(`{"data":{"id":"1"},"msg":"ok","status":0,"err":""}`))
	if err != nil {
		t.Fatalf("UnmarshalResponse() error = %v", err)
	}
	if resp.Status != 0 {
		t.Fatalf("UnmarshalResponse().Status = %d, want 0", resp.Status)
	}
	dataMap, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("UnmarshalResponse().Data type = %T, want map[string]any", resp.Data)
	}
	if dataMap["id"] != "1" {
		t.Fatalf("UnmarshalResponse().Data[id] = %v, want %q", dataMap["id"], "1")
	}
}

func TestNormalizeJSONValue(t *testing.T) {
	tests := []struct {
		name  string
		value any
		want  any
	}{
		{
			name: "结构体归一化为 map",
			value: struct {
				Name string `json:"name"`
				On   bool   `json:"on"`
			}{Name: "snap", On: true},
			want: map[string]any{"name": "snap", "on": true},
		},
		{
			name:  "RawMessage 保持 JSON 结构",
			value: json.RawMessage(`{"k":[1,2]}`),
			want:  map[string]any{"k": []any{float64(1), float64(2)}},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			got, err := NormalizeJSONValue(tt.value)
			if err != nil {
				t.Fatalf("NormalizeJSONValue() error = %v", err)
			}
			AssertJSONEquivalent(t, got, tt.want, tt.name)
		})
	}
}

func TestAssertJSONEquivalent(t *testing.T) {
	AssertJSONEquivalent(t,
		map[string]any{"ids": []any{1, 2}, "ok": true},
		map[string]any{"ok": true, "ids": []any{1, 2}},
		"等价 JSON 应比较成功",
	)
}
