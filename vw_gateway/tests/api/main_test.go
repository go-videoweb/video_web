//go:build api_test

package api_test

import (
	"fmt"
	"os"
	"sync/atomic"
	"testing"

	"vw_gateway/tests/helpers"
)

var (
	apiClient        *helpers.APIClient
	businessCasesRun atomic.Int64
)

// TestMain 校验显式远程配置，并拒绝将零业务用例当作成功回归。
// 初始化只构造客户端，不登录、不创建数据；后续业务用例须使用 runBusinessCase。
func TestMain(m *testing.M) {
	cfg, err := helpers.LoadConfig()
	if err == nil {
		err = cfg.ValidateRemoteConfig()
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "API 测试配置无效，请检查显式地址、专用账号及超时配置")
		os.Exit(1)
	}
	apiClient = helpers.NewAPIClient(cfg)
	code := m.Run()
	if code == 0 && businessCasesRun.Load() == 0 {
		fmt.Fprintln(os.Stderr, "未执行任何业务场景：配置检查不等于接口回归")
		code = 1
	}
	os.Exit(code)
}

// runBusinessCase 运行独立业务子场景，仅统计未跳过的场景。
// 输入：t 为当前测试；fn 必须执行实际接口调用和断言，不用于配置探测或占位测试。
func runBusinessCase(t *testing.T, fn func(*testing.T)) {
	t.Helper()
	t.Run("业务场景", func(t *testing.T) {
		defer func() {
			if !t.Skipped() {
				businessCasesRun.Add(1)
			}
		}()
		fn(t)
	})
}

// TestAPITestEnvironment 只校验远程 API 测试配置，不发起真实业务请求。
// 具体接口用例按业务模块维护，并继续使用 api_test 构建标签。
func TestAPITestEnvironment(t *testing.T) {
	cfg, err := helpers.LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if err := cfg.ValidateRemoteConfig(); err != nil {
		t.Fatal(err)
	}
	t.Log("API 测试配置已加载；此检查不发起业务请求")
}
