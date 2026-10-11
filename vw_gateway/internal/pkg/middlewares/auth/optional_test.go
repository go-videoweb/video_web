package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/go-kratos/kratos/v2/transport"
	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
)

type optionalHeader map[string]string

func (h optionalHeader) Get(k string) string      { return h[k] }
func (h optionalHeader) Set(k, v string)          { h[k] = v }
func (h optionalHeader) Add(k, v string)          { h[k] = v }
func (h optionalHeader) Values(k string) []string { return []string{h[k]} }
func (h optionalHeader) Keys() []string {
	var keys []string
	for k := range h {
		keys = append(keys, k)
	}
	return keys
}

type optionalTransport struct {
	transport.Transporter
	headers optionalHeader
}

func (tr optionalTransport) RequestHeader() transport.Header { return tr.headers }
func (tr optionalTransport) Operation() string               { return "detail" }

type optionalCache struct {
	redis.Cmdable
	value    string
	err      error
	calls    int
	key      string
	sets     int
	setErr   error
	setKey   string
	setValue string
	setTTL   time.Duration
}

func (c *optionalCache) Get(ctx context.Context, key string) *redis.StringCmd {
	c.calls++
	c.key = key
	cmd := redis.NewStringCmd(ctx, "get", key)
	cmd.SetVal(c.value)
	cmd.SetErr(c.err)
	return cmd
}

func (c *optionalCache) Set(ctx context.Context, key string, value interface{}, expiration time.Duration) *redis.StatusCmd {
	c.sets++
	c.setKey, c.setValue, c.setTTL = key, value.(string), expiration
	cmd := redis.NewStatusCmd(ctx, "set", key, value)
	cmd.SetErr(c.setErr)
	return cmd
}

// TestJwtAuthAnonymousPolicy 验证允许匿名的接口仅在有效会话下保留身份，凭证缺失或失效时按游客访问且不刷新 token。
// 缺少服务端传输上下文或缓存故障时应返回错误，不执行业务；已有上下文身份不能绕过校验。
func TestJwtAuthAnonymousPolicy(t *testing.T) {
	const secret = "optional-auth-test-secret"
	authorizer := &JWTAuth{Secret: secret, AccessExpireTime: time.Hour, RefreshExpireTime: 24 * time.Hour}
	access, refresh, err := authorizer.CreateToken(7, "viewer", false)
	if err != nil {
		t.Fatal(err)
	}
	sign := func(id int64, issuer string, expiry *jwt.NumericDate, method jwt.SigningMethod, key string) string {
		token := jwt.NewWithClaims(method, &AccessTokenClaims{UserID: id, RegisteredClaims: jwt.RegisteredClaims{Issuer: issuer, ExpiresAt: expiry}})
		value, err := token.SignedString([]byte(key))
		if err != nil {
			t.Fatal(err)
		}
		return value
	}
	future := jwt.NewNumericDate(time.Now().Add(time.Hour))
	past := jwt.NewNumericDate(time.Now().Add(-time.Hour))
	_, expiredRefresh, err := (&JWTAuth{Secret: secret, AccessExpireTime: time.Hour, RefreshExpireTime: -time.Hour}).CreateToken(7, "viewer", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, access, refresh, cached string
		cacheErr                      error
		wantID                        int64
		wantGets                      int
		wantErr                       bool
		noTransport                   bool
	}{
		{name: "anonymous"},
		{name: "missing transport", noTransport: true, wantErr: true},
		{name: "active session", access: "Bearer " + access, refresh: "Bearer " + refresh, cached: access, wantID: 7, wantGets: 1},
		{name: "case insensitive bearer", access: "bearer " + access, refresh: "BEARER " + refresh, cached: access, wantID: 7, wantGets: 1},
		{name: "missing refresh", access: "Bearer " + access},
		{name: "malformed access", access: "Bearer", refresh: "Bearer " + refresh},
		{name: "forged signature", access: "Bearer " + sign(7, TokenIssuer, future, jwt.SigningMethodHS256, "wrong"), refresh: "Bearer " + refresh},
		{name: "wrong algorithm", access: "Bearer " + sign(7, TokenIssuer, future, jwt.SigningMethodHS384, secret), refresh: "Bearer " + refresh},
		{name: "wrong issuer", access: "Bearer " + sign(7, "other", future, jwt.SigningMethodHS256, secret), refresh: "Bearer " + refresh},
		{name: "missing expiration", access: "Bearer " + sign(7, TokenIssuer, nil, jwt.SigningMethodHS256, secret), refresh: "Bearer " + refresh},
		{name: "expired access", access: "Bearer " + sign(7, TokenIssuer, past, jwt.SigningMethodHS256, secret), refresh: "Bearer " + refresh},
		{name: "expired refresh", access: "Bearer " + access, refresh: "Bearer " + expiredRefresh},
		{name: "forged refresh", access: "Bearer " + access, refresh: "Bearer " + sign(7, TokenIssuer, future, jwt.SigningMethodHS256, "wrong")},
		{name: "malformed refresh", access: "Bearer " + access, refresh: "Bearer invalid"},
		{name: "zero ID", access: "Bearer " + sign(0, TokenIssuer, future, jwt.SigningMethodHS256, secret), refresh: "Bearer " + refresh},
		{name: "negative ID", access: "Bearer " + sign(-1, TokenIssuer, future, jwt.SigningMethodHS256, secret), refresh: "Bearer " + refresh},
		{name: "logged out", access: "Bearer " + access, refresh: "Bearer " + refresh, cacheErr: redis.Nil, wantGets: 1},
		{name: "replaced session", access: "Bearer " + access, refresh: "Bearer " + refresh, cached: "another-token", wantGets: 1},
		{name: "cache failure", access: "Bearer " + access, refresh: "Bearer " + refresh, cacheErr: errors.New("cache unavailable"), wantGets: 1, wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cache := &optionalCache{value: tc.cached, err: tc.cacheErr}
			ctx := context.WithValue(context.Background(), "user_id", int64(999))
			if !tc.noTransport {
				ctx = transport.NewServerContext(ctx, optionalTransport{headers: optionalHeader{AccessTokenHeader: tc.access, RefreshTokenHeader: tc.refresh}})
			}
			calls := 0
			req := &struct{}{}
			resp, err := JwtAuth(secret, time.Hour, cache, func(_ context.Context, operation string) bool { return operation == "detail" })(func(ctx context.Context, got interface{}) (interface{}, error) {
				calls++
				if got != req || ctx.Value("user_id") != tc.wantID {
					t.Fatalf("unexpected viewer or request: %v", ctx.Value("user_id"))
				}
				return "detail", nil
			})(ctx, req)
			if (err != nil) != tc.wantErr {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.wantErr {
				if calls != 0 || resp != nil {
					t.Fatal("store failure reached handler")
				}
			} else if calls != 1 || resp != "detail" {
				t.Fatal("public handler not called exactly once")
			}
			if cache.calls != tc.wantGets {
				t.Fatalf("unexpected session lookup count: %d", cache.calls)
			}
			if cache.sets != 0 {
				t.Fatal("anonymous policy refreshed a token")
			}
			if cache.calls > 0 && cache.key != "7" {
				t.Fatalf("incorrect session key: %s", cache.key)
			}
		})
	}
}
