package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	kerr "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/transport"
	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
	"vw_gateway/internal/pkg/ecode/errdef"
	"vw_gateway/internal/pkg/ecode/errdef/uerr"
)

// TestJwtAuthRequiredPolicy 验证强制鉴权拒绝缺失、失效及被替换的凭证，只有有效会话进入业务。
// 刷新成功时应先更新缓存再通过错误携带新 token，不执行业务；缓存故障和业务错误不被吞掉或重试。
func TestJwtAuthRequiredPolicy(t *testing.T) {
	const secret = "required-auth-test-secret"
	authorizer := &JWTAuth{Secret: secret, AccessExpireTime: time.Hour, RefreshExpireTime: 24 * time.Hour}
	access, refresh, err := authorizer.CreateToken(7, "viewer", true)
	if err != nil {
		t.Fatal(err)
	}
	expired, _, err := (&JWTAuth{Secret: secret, AccessExpireTime: -time.Hour, RefreshExpireTime: 24 * time.Hour}).CreateToken(7, "viewer", true)
	if err != nil {
		t.Fatal(err)
	}
	_, expiredRefresh, err := (&JWTAuth{Secret: secret, AccessExpireTime: time.Hour, RefreshExpireTime: -time.Hour}).CreateToken(7, "viewer", true)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, access, refresh, cached string
		getErr, setErr, handlerErr    error
		wantErr                       error
		wantGets, wantSets, wantCalls int
		noTransport                   bool
	}{
		{name: "missing credentials", wantErr: uerr.ErrTokenMissing},
		{name: "missing transport", noTransport: true, wantErr: uerr.ErrTokenMissing},
		{name: "missing refresh", access: access, wantErr: uerr.ErrTokenMissing},
		{name: "malformed access", access: "invalid", refresh: refresh, wantErr: uerr.ErrParseTokenFailed},
		{name: "valid session", access: access, refresh: refresh, cached: access, wantGets: 1, wantCalls: 1},
		{name: "expired refresh with valid access", access: access, refresh: expiredRefresh, cached: access, wantErr: uerr.ErrRefreshTokenExpired},
		{name: "logged out", access: access, refresh: refresh, getErr: redis.Nil, wantErr: uerr.ErrUserLoggedOut, wantGets: 1},
		{name: "replaced session", access: access, refresh: refresh, cached: "other", wantErr: uerr.ErrUserLoggedOut, wantGets: 1},
		{name: "cache failure", access: access, refresh: refresh, getErr: errors.New("cache unavailable"), wantErr: errdef.INTERNAL_ERROR, wantGets: 1},
		{name: "refresh access", access: expired, refresh: refresh, cached: expired, wantErr: uerr.ErrAccessTokenExpired, wantGets: 1, wantSets: 1},
		{name: "refresh cache failure", access: expired, refresh: refresh, cached: expired, setErr: errors.New("write failed"), wantErr: errdef.INTERNAL_ERROR, wantGets: 1, wantSets: 1},
		{name: "expired and replaced", access: expired, refresh: refresh, cached: "other", wantErr: uerr.ErrUserLoggedOut, wantGets: 1},
		{name: "business credential error is not retried", access: access, refresh: refresh, cached: access, handlerErr: uerr.ErrTokenMissing, wantErr: uerr.ErrTokenMissing, wantGets: 1, wantCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cache := &optionalCache{value: tc.cached, err: tc.getErr, setErr: tc.setErr}
			ctx := context.Background()
			if !tc.noTransport {
				ctx = transport.NewServerContext(ctx, optionalTransport{headers: optionalHeader{AccessTokenHeader: "Bearer " + tc.access, RefreshTokenHeader: "Bearer " + tc.refresh}})
			}
			calls := 0
			resp, err := JwtAuth(secret, time.Hour, cache, nil)(func(ctx context.Context, req interface{}) (interface{}, error) {
				calls++
				if ctx.Value("user_id") != int64(7) {
					t.Fatalf("incorrect identity: %v", ctx.Value("user_id"))
				}
				if tc.handlerErr != nil {
					return nil, tc.handlerErr
				}
				return "result", nil
			})(ctx, nil)
			if tc.wantErr != nil {
				if kerr.Code(err) != kerr.Code(tc.wantErr) || resp != nil {
					t.Fatalf("unexpected result: %v, %v", resp, err)
				}
			} else if err != nil || resp != "result" {
				t.Fatalf("valid session rejected: %v", err)
			}
			if calls != tc.wantCalls || cache.calls != tc.wantGets || cache.sets != tc.wantSets {
				t.Fatalf("unexpected calls: handler=%d get=%d set=%d", calls, cache.calls, cache.sets)
			}
			if tc.name == "refresh access" {
				updated := kerr.FromError(err).Metadata["access_token"]
				if updated == "" || updated != cache.setValue || cache.setKey != "7" || cache.setTTL <= 0 || cache.setTTL > 24*time.Hour {
					t.Fatal("refresh protocol or TTL changed")
				}
				claims := new(AccessTokenClaims)
				_, parseErr := jwt.ParseWithClaims(updated, claims, func(*jwt.Token) (interface{}, error) { return []byte(secret), nil })
				if parseErr != nil || claims.UserID != 7 || claims.Username != "viewer" || !claims.IsAdmin || !claims.ExpiresAt.After(time.Now()) {
					t.Fatal("refreshed claims changed")
				}
			}
			if tc.name == "refresh cache failure" && kerr.FromError(err).Metadata["access_token"] != "" {
				t.Fatal("failed refresh exposed an uncached token")
			}
		})
	}
}

// TestJwtAuthInvalidClaimsAndBusinessErrors 验证无效签名、算法及声明不会读取或更新登录态缓存。
// 强制鉴权应拒绝请求，允许匿名时应以游客身份执行业务一次，并原样返回业务错误。
func TestJwtAuthInvalidClaimsAndBusinessErrors(t *testing.T) {
	const secret = "claims-test-secret"
	_, refresh, err := (&JWTAuth{Secret: secret, AccessExpireTime: time.Hour, RefreshExpireTime: 24 * time.Hour}).CreateToken(7, "viewer", false)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		claims AccessTokenClaims
		method jwt.SigningMethod
		key    string
	}{
		{name: "expired with wrong issuer", claims: AccessTokenClaims{UserID: 7, RegisteredClaims: jwt.RegisteredClaims{Issuer: "wrong", ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour))}}, method: jwt.SigningMethodHS256, key: secret},
		{name: "expired but not active", claims: AccessTokenClaims{UserID: 7, RegisteredClaims: jwt.RegisteredClaims{Issuer: TokenIssuer, ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour)), NotBefore: jwt.NewNumericDate(time.Now().Add(time.Hour))}}, method: jwt.SigningMethodHS256, key: secret},
		{name: "expired forged signature", claims: AccessTokenClaims{UserID: 7, RegisteredClaims: jwt.RegisteredClaims{Issuer: TokenIssuer, ExpiresAt: jwt.NewNumericDate(time.Now().Add(-time.Hour))}}, method: jwt.SigningMethodHS256, key: "wrong"},
		{name: "wrong algorithm", claims: AccessTokenClaims{UserID: 7, RegisteredClaims: jwt.RegisteredClaims{Issuer: TokenIssuer, ExpiresAt: jwt.NewNumericDate(time.Now().Add(time.Hour))}}, method: jwt.SigningMethodHS384, key: secret},
		{name: "missing expiration", claims: AccessTokenClaims{UserID: 7, RegisteredClaims: jwt.RegisteredClaims{Issuer: TokenIssuer}}, method: jwt.SigningMethodHS256, key: secret},
	} {
		for _, anonymous := range []bool{false, true} {
			t.Run(tc.name+map[bool]string{false: "/required", true: "/anonymous"}[anonymous], func(t *testing.T) {
				access, err := jwt.NewWithClaims(tc.method, &tc.claims).SignedString([]byte(tc.key))
				if err != nil {
					t.Fatal(err)
				}
				cache := &optionalCache{value: access}
				ctx := transport.NewServerContext(context.Background(), optionalTransport{headers: optionalHeader{AccessTokenHeader: "Bearer " + access, RefreshTokenHeader: "Bearer " + refresh}})
				calls := 0
				_, err = JwtAuth(secret, time.Hour, cache, func(context.Context, string) bool { return anonymous })(func(ctx context.Context, req interface{}) (interface{}, error) {
					calls++
					if ctx.Value("user_id") != int64(0) {
						t.Fatal("invalid credentials supplied an identity")
					}
					return nil, uerr.ErrTokenMissing
				})(ctx, nil)
				if cache.calls != 0 || cache.sets != 0 {
					t.Fatal("invalid credentials reached session store")
				}
				if anonymous {
					if calls != 1 || err != uerr.ErrTokenMissing {
						t.Fatal("business error swallowed or handler retried")
					}
				} else if calls != 0 || kerr.Code(err) != kerr.Code(uerr.ErrParseTokenFailed) {
					t.Fatalf("invalid credentials accepted: %v", err)
				}
			})
		}
	}
}
