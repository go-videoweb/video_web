package helpers

import (
	"encoding/json"
	"net/http"
	"testing"
)

// TestResponse_DecodeJSONPreservesNumber 验证大整数不会被解码为 float64。
func TestResponse_DecodeJSONPreservesNumber(t *testing.T) {
	response := &Response{StatusCode: http.StatusOK, Body: []byte(`{"id":9007199254740993}`)}
	value, err := response.JSONMap()
	if err != nil {
		t.Fatalf("JSONMap() error = %v", err)
	}
	got, ok := value["id"].(json.Number)
	if !ok || got.String() != "9007199254740993" {
		t.Fatalf("id = %#v, want json.Number(9007199254740993)", value["id"])
	}
}

// TestResponse_HeaderValue 验证响应头可按名称读取。
func TestResponse_HeaderValue(t *testing.T) {
	header := make(http.Header)
	header.Set("X-Test", "value")
	response := &Response{Header: header}
	if response.HeaderValue("X-Test") != "value" {
		t.Fatalf("HeaderValue() = %q", response.HeaderValue("X-Test"))
	}
}
