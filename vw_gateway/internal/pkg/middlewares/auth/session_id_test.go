package auth

import (
	"github.com/golang-jwt/jwt/v5"
	"testing"
	"time"
)

// TestTokenSessionID 验证同一身份连续签发时凭据不同，避免改密清除后重新登录复活旧 token。
func TestTokenSessionID(t *testing.T) {
	issuer := &JWTAuth{Secret: "session-test", AccessExpireTime: time.Hour, RefreshExpireTime: 24 * time.Hour}
	seen := make(map[string]bool)
	for i := 0; i < 10; i++ {
		access, refresh, err := issuer.CreateToken(7, "user", false)
		if err != nil {
			t.Fatal(err)
		}
		if seen[access] || seen[refresh] {
			t.Fatal("签发了重复会话凭据")
		}
		seen[access], seen[refresh] = true, true
		accessClaims, refreshClaims := &AccessTokenClaims{}, &RefreshTokenClaims{}
		key := func(*jwt.Token) (interface{}, error) { return []byte(issuer.Secret), nil }
		if _, err := jwt.ParseWithClaims(access, accessClaims, key); err != nil {
			t.Fatal(err)
		}
		if _, err := jwt.ParseWithClaims(refresh, refreshClaims, key); err != nil {
			t.Fatal(err)
		}
		if accessClaims.ID == "" || accessClaims.ID != refreshClaims.ID || accessClaims.UserID != 7 {
			t.Fatal("会话标识缺失或配对错误")
		}
	}
}
