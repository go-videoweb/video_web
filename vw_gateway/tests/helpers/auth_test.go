package helpers

import (
	"net/http"
	"testing"
)

// TestTokenStore_ApplyToRequest 验证双 Token 独立发送，清空后不残留身份。
func TestTokenStore_ApplyToRequest(t *testing.T) {
	store := TokenStore{}
	store.SetToken(AccessToken, " access-token ")
	store.SetToken(RefreshToken, " refresh-token ")
	req, err := http.NewRequest(http.MethodGet, "http://example.test", nil)
	if err != nil {
		t.Fatal(err)
	}
	store.ApplyToRequest(req)
	if got := req.Header.Get("Authorization"); got != "Bearer access-token" {
		t.Fatalf("Authorization = %q", got)
	}
	if req.Header.Get("Refresh-Token") != "Bearer refresh-token" {
		t.Fatal("refresh Token 缺失")
	}
	store.ClearToken(AccessToken)
	store.ApplyToRequest(req)
	if got := req.Header.Get("Authorization"); got != "" {
		t.Fatalf("匿名请求残留 Authorization = %q", got)
	}
	if req.Header.Get("Refresh-Token") != "Bearer refresh-token" {
		t.Fatal("另一种 Token 被修改")
	}
	store.ClearToken(RefreshToken)
	store.ApplyToRequest(req)
	if req.Header.Get("Refresh-Token") != "" {
		t.Fatal("匿名请求残留 refresh Token")
	}
}
