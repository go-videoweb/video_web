package helpers

import (
	"fmt"
	"os"
	"testing"
)

// Suite 代表一组测试的生命周期，在 TestMain 中使用。
type Suite struct {
	// Setup 在所有测试执行前调用一次，返回 error 则整包以退出码 1 终止。
	Setup func() error
	// Teardown 在所有测试执行后调用一次，无论 Setup 是否成功都会执行。
	Teardown func()
}

// Run 包裹 m.Run()，在其前后分别执行 Setup / Teardown，并返回退出码。
// 用法：
//
//	func TestMain(m *testing.M) {
//	    suite := &helpers.Suite{
//	        Setup:    func() error { ... },
//	        Teardown: func() { ... },
//	    }
//	    os.Exit(suite.Run(m))
//	}
func (s *Suite) Run(m *testing.M) int {
	if s.Teardown != nil {
		defer s.Teardown()
	}

	if s.Setup != nil {
		if err := s.Setup(); err != nil {
			fmt.Fprintf(os.Stderr, "[Suite] Setup failed: %v\n", err)
			return 1
		}
	}

	return m.Run()
}

// RunSuite 是 Suite.Run 的快捷方式，直接传入函数而不需要构造 Suite。
func RunSuite(m *testing.M, setup func() error, teardown func()) int {
	s := &Suite{Setup: setup, Teardown: teardown}
	return s.Run(m)
}

// NoopSuite 返回一个无任何操作的 Suite，适合暂不需要套件级钩子的包直接使用。
func NoopSuite() *Suite {
	return &Suite{}
}

// suiteSetupDone 用于在 Suite.Setup 内部标记子步骤完成，方便打印进度。
func LogSetupStep(step string) {
	fmt.Printf("[Suite.Setup] %s\n", step)
}

// LogTeardownStep 打印 Teardown 进度。
func LogTeardownStep(step string) {
	fmt.Printf("[Suite.Teardown] %s\n", step)
}

// SkipIfNotReady 若环境变量 TEST_SPACE_ID / TEST_USERNAME 未设置则跳过整个测试。
// 可在 Suite.Setup 中调用，用于 CI 环境快速跳过需要真实后端的测试包。
func SkipIfNotReady(t *testing.T) {
	t.Helper()
	if os.Getenv("TEST_SPACE_ID") == "" || os.Getenv("TEST_USERNAME") == "" {
		t.Skip("TEST_SPACE_ID or TEST_USERNAME not set, skipping integration test")
	}
}
