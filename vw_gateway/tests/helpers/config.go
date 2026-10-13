package helpers

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// TestConfig 描述 API 测试运行所需的最小配置。
type TestConfig struct {
	BaseURL      string
	Username     string
	Password     string
	Timeout      time.Duration
	ResponseSize int64
}

const (
	defaultTimeout      = 30 * time.Second
	defaultResponseSize = 8 << 20
)

// LoadConfig 从 API_TEST_* 环境变量读取测试配置。
// 输入：读取 API_TEST_BASE_URL、API_TEST_USERNAME、API_TEST_PASSWORD、API_TEST_TIMEOUT 和 API_TEST_RESPONSE_SIZE。
// 输出：返回解析后的 TestConfig；环境变量格式非法时返回空配置和错误。
// 示例：cfg, err := helpers.LoadConfig()。
func LoadConfig() (TestConfig, error) {
	cfg := TestConfig{
		BaseURL:      strings.TrimRight(strings.TrimSpace(os.Getenv("API_TEST_BASE_URL")), "/"),
		Username:     strings.TrimSpace(os.Getenv("API_TEST_USERNAME")),
		Password:     os.Getenv("API_TEST_PASSWORD"),
		Timeout:      defaultTimeout,
		ResponseSize: defaultResponseSize,
	}
	if raw := strings.TrimSpace(os.Getenv("API_TEST_TIMEOUT")); raw != "" {
		duration, err := time.ParseDuration(raw)
		if err != nil || duration <= 0 {
			return TestConfig{}, fmt.Errorf("API_TEST_TIMEOUT 无效: %q", raw)
		}
		cfg.Timeout = duration
	}
	if raw := strings.TrimSpace(os.Getenv("API_TEST_RESPONSE_SIZE")); raw != "" {
		limit, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || limit <= 0 {
			return TestConfig{}, fmt.Errorf("API_TEST_RESPONSE_SIZE 无效: %q", raw)
		}
		cfg.ResponseSize = limit
	}
	return cfg, nil
}

// ValidateRemoteConfig 校验真实 API 测试必需的地址和账号。
// 输入：接收当前 TestConfig，要求 BaseURL 是带 scheme 和 host 的绝对 URL，且用户名和密码非空。
// 输出：配置有效时返回 nil；缺少地址、地址非法或账号不完整时返回错误。
// 示例：if err := cfg.ValidateRemoteConfig(); err != nil { t.Fatal(err) }。
func (c TestConfig) ValidateRemoteConfig() error {
	if c.BaseURL == "" {
		return fmt.Errorf("API_TEST_BASE_URL 未设置")
	}
	u, err := url.Parse(c.BaseURL)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return fmt.Errorf("API_TEST_BASE_URL 不是有效的绝对 URL: %q", c.BaseURL)
	}
	if c.Username == "" || c.Password == "" {
		return fmt.Errorf("API_TEST_USERNAME 和 API_TEST_PASSWORD 必须同时设置")
	}
	return nil
}
