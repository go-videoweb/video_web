package helpers

import "testing"

// TestLoadConfig_Defaults 验证未设置可选变量时使用默认超时和响应体限制。
func TestLoadConfig_Defaults(t *testing.T) {
	t.Setenv("API_TEST_BASE_URL", "http://127.0.0.1:8001/")
	t.Setenv("API_TEST_USERNAME", "user")
	t.Setenv("API_TEST_PASSWORD", "pass")
	t.Setenv("API_TEST_TIMEOUT", "")
	t.Setenv("API_TEST_RESPONSE_SIZE", "")
	cfg, err := LoadConfig()
	if err != nil {
		t.Fatalf("LoadConfig() error = %v", err)
	}
	if cfg.BaseURL != "http://127.0.0.1:8001" {
		t.Fatalf("BaseURL = %q", cfg.BaseURL)
	}
	if cfg.Timeout != defaultTimeout || cfg.ResponseSize != defaultResponseSize {
		t.Fatalf("defaults = (%s, %d)", cfg.Timeout, cfg.ResponseSize)
	}
}

// TestValidateRemoteConfig 验证远程 API 入口所需配置的边界。
func TestValidateRemoteConfig(t *testing.T) {
	valid := TestConfig{BaseURL: "http://127.0.0.1:8001", Username: "user", Password: "pass"}
	if err := valid.ValidateRemoteConfig(); err != nil {
		t.Fatalf("valid config error = %v", err)
	}
	invalid := valid
	invalid.BaseURL = "127.0.0.1:8001"
	if err := invalid.ValidateRemoteConfig(); err == nil {
		t.Fatal("invalid URL should fail")
	}
}
