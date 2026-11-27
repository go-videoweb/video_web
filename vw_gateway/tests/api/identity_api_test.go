//go:build api_test

package api_test

import (
	"os"
	"testing"

	"vw_gateway/tests/helpers"
	"vw_gateway/tests/identity"
)

// identitySuite 使用现有入口配置构造场景，不自动登录或注册。
func identitySuite(t *testing.T) identity.Suite {
	t.Helper()
	cfg, err := helpers.LoadConfig()
	if err != nil {
		t.Fatal("读取身份测试配置失败")
	}
	return identity.Suite{
		Client:   apiClient,
		Username: cfg.Username,
		Password: cfg.Password,
		Timeout:  cfg.Timeout,
	}
}

// TestIdentityLogin 对远程网关验证 U01 及其会话副作用，不并行共享登录账号。
func TestIdentityLogin(t *testing.T) {
	runBusinessCase(t, identitySuite(t).RunLogin)
}

// TestIdentityRegister 对远程网关验证 U02，成功注册必须显式提供一次性邮件夹具。
func TestIdentityRegister(t *testing.T) {
	suite := identitySuite(t)
	enabled, err := identity.ValidateRegistrationFlag(os.Getenv("API_TEST_REGISTER"))
	if err != nil {
		t.Fatal(err)
	}
	suite.Registration = identity.Registration{
		Enabled:  enabled,
		Username: os.Getenv("API_TEST_REGISTER_USERNAME"),
		Email:    os.Getenv("API_TEST_REGISTER_EMAIL"),
		Password: os.Getenv("API_TEST_REGISTER_PASSWORD"),
		Code:     os.Getenv("API_TEST_REGISTER_CODE"),
	}
	runBusinessCase(t, suite.RunRegister)
}

// TestIdentityLogout 对远程网关验证 U03，仅注销本测试账号的会话。
func TestIdentityLogout(t *testing.T) {
	runBusinessCase(t, identitySuite(t).RunLogout)
}
