package helpers

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
)

// TestRequestOptions 验证查询参数、双 Token、原始认证头覆盖和条件媒体响应。
func TestRequestOptions(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/prefix/items" || r.URL.Query().Get("page_num") != "2" || len(r.URL.Query()["class"]) != 2 {
			t.Errorf("查询参数错误: %s", r.URL)
		}
		if r.Header.Get("Authorization") != "Bearer access" || r.Header.Get("Refresh-Token") != "broken" {
			t.Error("认证头错误")
		}
		if r.Header.Get("If-None-Match") != "\"etag\"" || r.Header.Get("Range") != "bytes=0-9" {
			t.Error("自定义头缺失")
		}
		w.WriteHeader(http.StatusNotModified)
	}))
	defer server.Close()
	base := NewAPIClient(TestConfig{BaseURL: server.URL + "/prefix"})
	session := base.WithTokens("access", "refresh")
	response, err := session.Do(context.Background(), http.MethodGet, "/items?page_num=1", nil, "", RequestOptions{
		Query:   url.Values{"page_num": {"2"}, "class": {"A", "B"}},
		Headers: http.Header{"Refresh-Token": {"broken"}, "If-None-Match": {"\"etag\""}, "Range": {"bytes=0-9"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	AssertStatus(t, response, http.StatusNotModified, "条件请求")
	if base.tokens.Token(AccessToken) != "" || session.tokens.Token(RefreshToken) != "refresh" {
		t.Fatal("请求污染客户端身份")
	}
}

// TestResolveURL 验证编码路径和查询值保持分离，跨源请求不能携带默认身份。
func TestResolveURL(t *testing.T) {
	client := NewAPIClient(TestConfig{BaseURL: "http://example.test/prefix"})
	for _, path := range []string{"/dash/%2e%2e%2fsecret.m4s", "/dash/../secret.m4s", "/items?cursor.video_id=9007199254740993&name=a%2Bb"} {
		got, err := client.resolveURL(path)
		if err != nil || got != "http://example.test/prefix"+path {
			t.Errorf("路径被改写: %q %v", got, err)
		}
	}
	for _, path := range []string{"http://other.test/items", "//other.test/items", "/items#fragment", "/%zz"} {
		if _, err := client.resolveURL(path); err == nil {
			t.Errorf("非法地址未拒绝: %s", path)
		}
	}
}

// TestRequestNoReplay 验证重定向和服务错误均不重放业务请求，已取消请求不发送。
func TestRequestNoReplay(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.URL.Path == "/redirect" {
			w.Header().Set("Location", "/target")
			w.WriteHeader(307)
			return
		}
		w.WriteHeader(503)
	}))
	defer server.Close()
	client := NewAPIClient(TestConfig{BaseURL: server.URL})
	for _, path := range []string{"/redirect", "/unavailable"} {
		if _, err := client.DoJSON(context.Background(), http.MethodPost, path, nil); err != nil {
			t.Fatal(err)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Do(ctx, http.MethodGet, "/cancelled", nil, ""); err == nil {
		t.Fatal("取消请求未失败")
	}
	if calls.Load() != 2 {
		t.Fatalf("业务请求重放或取消失效: %d", calls.Load())
	}
}
