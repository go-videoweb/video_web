package user_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/golang-jwt/jwt/v5"
	idv1 "vw_gateway/api/v1/user/identity"
	"vw_gateway/internal/biz/userbiz"
	"vw_gateway/internal/pkg/codecs"
	"vw_gateway/internal/pkg/middlewares/auth"
	user "vw_gateway/internal/service/user_service"
)

type loginRepo struct {
	response     *userbiz.LoginResponse
	loginErr     error
	cacheErr     error
	username     string
	password     string
	cacheCalls   int
	cachedUserID string
	cachedToken  string
	cachedTTL    time.Duration
}

func (r *loginRepo) Login(_ context.Context, username, password string) (*userbiz.LoginResponse, error) {
	r.username, r.password = username, password
	return r.response, r.loginErr
}

func (r *loginRepo) CacheAccessToken(_ context.Context, userID, token string, ttl time.Duration) error {
	r.cacheCalls++
	r.cachedUserID, r.cachedToken, r.cachedTTL = userID, token, ttl
	return r.cacheErr
}

func (*loginRepo) Register(context.Context, *userbiz.RegisterInfo) (int64, bool, error) {
	panic("unexpected Register call")
}

func (*loginRepo) Logout(context.Context, string) error {
	panic("unexpected Logout call")
}

func newLoginUsecase(repo *loginRepo) (*userbiz.UserIdentityUsecase, *auth.JWTAuth, log.Logger) {
	logger := log.NewStdLogger(io.Discard)
	jwtAuth := &auth.JWTAuth{
		Secret: "login-test-only-secret", AccessExpireTime: time.Hour, RefreshExpireTime: 24 * time.Hour,
	}
	return userbiz.NewUserIdentityUsecase(repo, jwtAuth, logger), jwtAuth, logger
}

// TestIdentityServiceLogin 验证登录响应身份、双 token 与缓存一致，大用户 ID 经 JSON 编码不丢精度。
// 登录或缓存失败时应返回原错误，不返回成功响应。
func TestIdentityServiceLogin(t *testing.T) {
	loginErr, cacheErr := errors.New("login failed"), errors.New("cache failed")
	for _, tc := range []struct {
		name       string
		loginErr   error
		cacheErr   error
		wantErr    error
		cacheCalls int
	}{
		{name: "success", cacheCalls: 1},
		{name: "login error", loginErr: loginErr, wantErr: loginErr},
		{name: "cache error", cacheErr: cacheErr, wantErr: cacheErr, cacheCalls: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			identity := &userbiz.LoginResponse{UserId: 9007199254740993, Username: "canonical-user", IsAdmin: true}
			repo := &loginRepo{response: identity, loginErr: tc.loginErr, cacheErr: tc.cacheErr}
			uc, jwtAuth, logger := newLoginUsecase(repo)
			service := user.NewUserIdentityService(uc, logger)
			resp, err := service.Login(context.Background(), &idv1.LoginRequest{Username: "input-user", Password: "test-password"})
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("unexpected login error: %v", err)
			}
			if repo.username != "input-user" || repo.password != "test-password" || repo.cacheCalls != tc.cacheCalls {
				t.Fatal("login arguments or token caching behavior changed")
			}
			if tc.wantErr != nil {
				if resp != nil {
					t.Fatal("failed login returned a success response")
				}
				return
			}
			if resp == nil || resp.Data == nil || resp.StatusCode != 200 || resp.Msg != "login successfully" {
				t.Fatal("missing successful login response")
			}
			data := resp.Data
			if data.UserId != identity.UserId || data.AccessToken == "" || data.RefreshToken == "" {
				t.Fatal("login response must include authenticated user ID and both tokens")
			}
			if repo.cachedUserID != strconv.FormatInt(identity.UserId, 10) || repo.cachedToken != data.AccessToken || repo.cachedTTL != jwtAuth.RefreshExpireTime {
				t.Fatal("cached access token does not match the login response")
			}
			claims := &auth.AccessTokenClaims{}
			key := func(*jwt.Token) (interface{}, error) { return []byte(jwtAuth.Secret), nil }
			access, err := jwt.ParseWithClaims(data.AccessToken, claims, key, jwt.WithValidMethods([]string{"HS256"}))
			if err != nil || !access.Valid || claims.UserID != data.UserId || claims.Username != identity.Username || claims.IsAdmin != identity.IsAdmin {
				t.Fatal("response user ID does not match the authenticated access-token claims")
			}
			refresh, err := jwt.ParseWithClaims(data.RefreshToken, &auth.RefreshTokenClaims{}, key, jwt.WithValidMethods([]string{"HS256"}))
			if err != nil || !refresh.Valid {
				t.Fatal("invalid refresh token")
			}

			request := httptest.NewRequest("POST", "/api/v1/user/login", nil)
			request.Header.Set("Accept", "application/json")
			writer := httptest.NewRecorder()
			if err := codecs.ResponseEncoder(writer, request, resp); err != nil {
				t.Fatal(err)
			}
			var body struct {
				Data struct {
					UserID       string `json:"userId"`
					AccessToken  string `json:"accessToken"`
					RefreshToken string `json:"refreshToken"`
				} `json:"data"`
			}
			if err := json.Unmarshal(writer.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if body.Data.UserID != strconv.FormatInt(identity.UserId, 10) || body.Data.AccessToken != data.AccessToken || body.Data.RefreshToken != data.RefreshToken {
				t.Fatal("HTTP JSON response lost identity or token data")
			}
		})
	}
}

// TestLoginUsecaseErrorReturnsNoIdentityOrTokens 验证登录或缓存失败时不返回身份和 token。
func TestLoginUsecaseErrorReturnsNoIdentityOrTokens(t *testing.T) {
	wantErr := errors.New("test failure")
	for _, tc := range []struct {
		name     string
		loginErr error
		cacheErr error
	}{
		{name: "login error", loginErr: wantErr},
		{name: "cache error", cacheErr: wantErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &loginRepo{response: &userbiz.LoginResponse{UserId: 42}, loginErr: tc.loginErr, cacheErr: tc.cacheErr}
			uc, _, _ := newLoginUsecase(repo)
			userID, accessToken, refreshToken, err := uc.Login(context.Background(), "user", "password")
			if !errors.Is(err, wantErr) || userID != 0 || accessToken != "" || refreshToken != "" {
				t.Fatal("failed login must return only the original error")
			}
		})
	}
}
