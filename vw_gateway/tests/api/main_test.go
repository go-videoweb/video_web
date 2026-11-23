//go:build api_test

package api_test

import (
	"testing"

	"vw_gateway/tests/helpers"
)

// TestAPITestEnvironment 只校验远程 API 测试配置，不发起真实业务请求。
// 具体接口用例在后续阶段按业务模块补充，并继续使用 api_test 构建标签。
func TestAPITestEnvironment(t *testing.T) {
	cfg, err := helpers.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.BaseURL == "" && cfg.Username == "" && cfg.Password == "" {
		t.Skip("未设置 API_TEST_*，跳过远程 API 测试入口")
	}
	if err := cfg.ValidateRemoteConfig(); err != nil {
		t.Fatal(err)
	}
	t.Logf("API 测试配置已加载: %s", cfg.BaseURL)
}
