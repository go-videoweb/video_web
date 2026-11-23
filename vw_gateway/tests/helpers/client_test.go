package helpers

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestAPIClient_DoJSON 验证 JSON 请求、鉴权头和响应保留行为。
func TestAPIClient_DoJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/items" {
			t.Fatalf("request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer session-token" {
			t.Fatalf("Authorization = %q", got)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Fatalf("Content-Type = %q", got)
		}
		var payload map[string]string
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("decode request = %v", err)
		}
		if payload["name"] != "demo" {
			t.Fatalf("payload = %#v", payload)
		}
		w.Header().Set("X-Request-ID", "req-1")
		w.WriteHeader(http.StatusCreated)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer server.Close()
	client := NewAPIClient(TestConfig{BaseURL: server.URL})
	client.SetToken(SessionToken, "session-token")
	response, err := client.DoJSON(context.Background(), http.MethodPost, "/v1/items", map[string]string{"name": "demo"})
	if err != nil {
		t.Fatalf("DoJSON() error = %v", err)
	}
	AssertStatus(t, response, http.StatusCreated, "create item")
	if response.HeaderValue("X-Request-ID") != "req-1" {
		t.Fatalf("request id = %q", response.HeaderValue("X-Request-ID"))
	}
}

// TestAPIClient_DoMultipart 验证 multipart 文本字段和文件字段均可发送。
func TestAPIClient_DoMultipart(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1024); err != nil {
			t.Fatalf("ParseMultipartForm() error = %v", err)
		}
		if r.FormValue("name") != "demo" {
			t.Fatalf("name = %q", r.FormValue("name"))
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			t.Fatalf("FormFile() error = %v", err)
		}
		defer file.Close()
		if header.Filename != "demo.txt" {
			t.Fatalf("filename = %q", header.Filename)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()
	client := NewAPIClient(TestConfig{BaseURL: server.URL})
	if _, err := client.DoMultipart(context.Background(), http.MethodPost, "/upload", "file", "demo.txt", "text/plain", []byte("hello"), map[string]string{"name": "demo"}); err != nil {
		t.Fatalf("DoMultipart() error = %v", err)
	}
}

// TestAPIClient_ResponseLimit 验证响应体大小限制生效。
func TestAPIClient_ResponseLimit(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "0123456789") }))
	defer server.Close()
	client := NewAPIClient(TestConfig{BaseURL: server.URL, ResponseSize: 4})
	if _, err := client.Do(context.Background(), http.MethodGet, "/", nil, ""); err == nil {
		t.Fatal("expected response size error")
	}
}
