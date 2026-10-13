package helpers

import (
	"net/http"
	"testing"
)

// TestTokenStore_ApplyToRequest 验证会话令牌优先于匿名令牌。
func TestTokenStore_ApplyToRequest(t *testing.T) {
	store := TokenStore{}
	store.SetToken(AnonymousToken, "anonymous-token")
	store.SetToken(SessionToken, "session-token")
	req, err := http.NewRequest(http.MethodGet, "http://example.test", nil)
	if err != nil {
		t.Fatal(err)
	}
	store.ApplyToRequest(req)
	if got := req.Header.Get("Authorization"); got != "Bearer session-token" {
		t.Fatalf("Authorization = %q", got)
	}
	store.ClearToken(SessionToken)
	store.ApplyToRequest(req)
	if got := req.Header.Get("Authorization"); got != "Bearer anonymous-token" {
		t.Fatalf("fallback Authorization = %q", got)
	}
}
