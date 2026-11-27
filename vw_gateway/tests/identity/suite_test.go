package identity

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	kerr "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/redis/go-redis/v9"
	"vw_gateway/internal/biz/userbiz"
	"vw_gateway/internal/conf"
	"vw_gateway/internal/domain"
	"vw_gateway/internal/pkg/middlewares/auth"
	"vw_gateway/internal/server"
	user "vw_gateway/internal/service/user_service"
	"vw_gateway/tests/helpers"
)

// identityFixture 替代用户 RPC 仓储和 Redis；HTTP 路由、JWT、错误及 JSON 编解码使用实际实现。
// 注册错误序列仅验证场景请求和断言接线，不替代用户服务的业务校验或数据库测试。
type identityFixture struct {
	userbiz.FavoritesRepo
	mu             sync.Mutex
	sessions       map[string]string
	registerErrors []int32
	registerCalls  int
	registered     []userbiz.RegisterInfo
	cacheError     bool
}

// Login 模拟凭证校验，固定返回可识别精度丢失的大 ID。
func (f *identityFixture) Login(_ context.Context, username, password string) (*userbiz.LoginResponse, error) {
	if username != "fixture-user" {
		return nil, kerr.New(userNotFound, "USER_NOT_FOUND", "")
	}
	if password != "fixture-password" {
		return nil, kerr.New(passwordError, "PASSWORD_ERROR", "")
	}
	return &userbiz.LoginResponse{UserId: 9007199254740993, Username: username}, nil
}

// Register 记录转发参数，按预设错误序列响应，不执行实际注册校验。
func (f *identityFixture) Register(_ context.Context, info *userbiz.RegisterInfo) (int64, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.registered = append(f.registered, *info)
	index := f.registerCalls
	f.registerCalls++
	if index < len(f.registerErrors) && f.registerErrors[index] != 0 {
		code := f.registerErrors[index]
		reason := "FIXTURE_ERROR"
		if code == 4102 {
			reason = "EMAIL_IN_USE"
		}
		return 0, false, kerr.New(int(code), reason, "")
	}
	return 9007199254740995, false, nil
}

// CacheAccessToken 在内存保存会话，并允许模拟注册后的缓存失败。
func (f *identityFixture) CacheAccessToken(_ context.Context, userID, token string, _ time.Duration) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.cacheError {
		return errors.New("fixture cache failure")
	}
	f.sessions[userID] = token
	return nil
}

// Logout 模拟幂等删除登录态，不调用实际用户服务。
func (f *identityFixture) Logout(_ context.Context, userID string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	delete(f.sessions, userID)
	return nil
}

// GetFavoritesList 为身份回读返回空列表，不测试收藏业务。
func (f *identityFixture) GetFavoritesList(context.Context, int64) ([]*domain.FavoriteDetail, error) {
	return []*domain.FavoriteDetail{}, nil
}

// DialHook 阻止遗漏的替身操作意外连接 Redis。
func (*identityFixture) DialHook(redis.DialHook) redis.DialHook {
	return func(context.Context, string, string) (net.Conn, error) {
		return nil, errors.New("禁止连接真实 Redis")
	}
}

// ProcessHook 只允许从内存读取会话，其他缓存操作直接报错。
func (f *identityFixture) ProcessHook(redis.ProcessHook) redis.ProcessHook {
	return func(_ context.Context, cmd redis.Cmder) error {
		get, ok := cmd.(*redis.StringCmd)
		if !ok || cmd.Name() != "get" {
			return errors.New("非预期缓存操作")
		}
		f.mu.Lock()
		defer f.mu.Unlock()
		value, exists := f.sessions[get.Args()[1].(string)]
		if !exists {
			get.SetErr(redis.Nil)
			return redis.Nil
		}
		get.SetVal(value)
		return nil
	}
}

// ProcessPipelineHook 拒绝未约定的批处理操作。
func (*identityFixture) ProcessPipelineHook(redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return func(context.Context, []redis.Cmder) error { return errors.New("非预期缓存管道") }
}

// fixtureSuite 构造本地真实 HTTP 服务；禁止实际连接 Redis 或调用未配置的下游。
func fixtureSuite(t *testing.T, fixture *identityFixture) Suite {
	t.Helper()
	fixture.sessions = make(map[string]string)
	logger := log.NewStdLogger(io.Discard)
	jwtConfig := &conf.JWT{Secret: "local-only-test-secret", AccessTokenExpiration: 1, RefreshTokenExpiration: 1}
	authorizer := auth.NewJWTAuth(jwtConfig)
	idService := user.NewUserIdentityService(userbiz.NewUserIdentityUsecase(fixture, authorizer, logger), logger)
	favorites := user.NewFavoritesService(userbiz.NewFavoritesUsecase(fixture, logger), logger)
	cache := redis.NewClusterClient(&redis.ClusterOptions{Addrs: []string{"127.0.0.1:1"}})
	cache.AddHook(fixture)
	t.Cleanup(func() { _ = cache.Close() })
	handler := server.NewHTTPServer(&conf.Server{Http: &conf.Server_HTTP{}}, jwtConfig, nil, nil, idService, nil, cache, nil, favorites, nil, nil, nil, logger)
	remote := httptest.NewServer(handler)
	t.Cleanup(remote.Close)
	return Suite{
		Client:   helpers.NewAPIClient(helpers.TestConfig{BaseURL: remote.URL}),
		Username: "fixture-user",
		Password: "fixture-password",
	}
}

// TestIdentityHTTPScenarios 验证同一套登录/退出场景可经真实 HTTP 路由与鉴权执行。
func TestIdentityHTTPScenarios(t *testing.T) {
	fixture := &identityFixture{}
	suite := fixtureSuite(t, fixture)
	t.Run("登录", suite.RunLogin)
	t.Run("退出", suite.RunLogout)
	if len(fixture.sessions) != 0 {
		t.Fatal("场景结束后残留登录态")
	}
}

// TestRegistrationHTTPScenarios 验证全部注册场景的协议接线及一次成功注册，不调用真实邮件或数据库。
func TestRegistrationHTTPScenarios(t *testing.T) {
	fixture := &identityFixture{registerErrors: []int32{invalidEmail, userExists, invalidPassword, invalidPassword, passwordMismatch, signatureTooLong, invalidEmailCode, 0, 4102}}
	suite := fixtureSuite(t, fixture)
	suite.Registration = Registration{
		Enabled:  true,
		Username: "new-fixture",
		Email:    "fixture@example.invalid",
		Password: "fixture-new-password",
		Code:     "123456",
	}
	suite.RunRegister(t)
	if fixture.registerCalls != 9 || len(fixture.sessions) != 0 {
		t.Fatal("注册请求次数或会话清理错误")
	}
	created := fixture.registered[7]
	if created.Username != suite.Registration.Username || created.InputCode != suite.Registration.Code || created.Birthday.Format("2006-01-02") != "2000-01-01" {
		t.Fatal("注册字段传输错误")
	}
	if fixture.registered[1].Username != suite.Username || fixture.registered[4].Password == fixture.registered[4].RepeatPassword {
		t.Fatal("负向夹具未正确传递")
	}
}

// TestRegistrationPartialFailure 验证真实注册业务在缓存失败时保留部分成功元数据且不返回 Token。
func TestRegistrationPartialFailure(t *testing.T) {
	fixture := &identityFixture{cacheError: true}
	suite := fixtureSuite(t, fixture)
	response := suite.request(t, suite.Client, "POST", "/api/v1/user/register", registerBody("new", "new@example.invalid", "password-123", "123456"), "")
	err := helpers.AssertAPIError(t, response, 3101, "REGISTRATION_SESSION_FAILED")
	if err.Metadata["user_created"] != "true" || fixture.registerCalls != 1 {
		t.Fatal("部分成功元数据或请求次数错误")
	}
	var body map[string]any
	if response.DecodeJSON(&body) != nil || body["data"] != nil {
		t.Fatal("失败响应不应携带登录数据")
	}
}

// TestSessionProtocol 验证 ID 字符串类型及显式注册开关，不以宽松转换掩盖协议错误。
func TestSessionProtocol(t *testing.T) {
	// 严格 JSON 字段绑定的错误由 DecodeJSON 返回，不通过故意使 t.Fatal 的子测试验证。
	for _, id := range []string{`9007199254740993`, `true`, `{}`} {
		var value session
		response := &helpers.Response{Body: []byte(`{"userId":` + id + `}`)}
		if response.DecodeJSON(&value) == nil {
			t.Fatal("接受了非字符串 ID")
		}
	}
	for _, flag := range []string{"yes", "true", "2"} {
		if _, err := ValidateRegistrationFlag(flag); err == nil {
			t.Fatal("非法注册开关未拒绝")
		}
	}
	for _, flag := range []string{"", "0", "1"} {
		if enabled, err := ValidateRegistrationFlag(flag); err != nil || enabled != (flag == "1") {
			t.Fatal("注册开关解析错误")
		}
	}
}
