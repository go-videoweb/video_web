package helpers

import "testing"

// TestHooks 单个测试函数的前置/后置钩子。
type TestHooks struct {
	// Before 在测试逻辑执行前调用。若 Before 内调用 t.Fatal/t.FailNow，
	// After 仍会通过 defer 执行。
	Before func(t *testing.T)
	// After 在测试逻辑执行后始终调用（通过 defer 保证），适合清理临时数据。
	After func(t *testing.T)
}

// RunWithHooks 用钩子包裹测试逻辑 fn。
// 执行顺序：Before → fn → After（After 通过 defer 保证始终执行）。
//
// 用法：
//
//	func TestExperimentCreate(t *testing.T) {
//	    var createdIds []string
//	    helpers.RunWithHooks(t, helpers.TestHooks{
//	        Before: func(t *testing.T) { /* 前置准备 */ },
//	        After:  func(t *testing.T) {
//	            for _, id := range createdIds { testClient.ExperimentDelete(id) }
//	        },
//	    }, func(t *testing.T) {
//	        // 正式测试逻辑
//	    })
//	}
func RunWithHooks(t *testing.T, hooks TestHooks, fn func(t *testing.T)) {
	t.Helper()

	if hooks.After != nil {
		defer hooks.After(t)
	}

	if hooks.Before != nil {
		hooks.Before(t)
		// 若 Before 已标记失败，不继续执行测试逻辑，但 After 仍会通过 defer 运行。
		if t.Failed() {
			return
		}
	}

	fn(t)
}

// SubTestHooks 用于子测试（t.Run）的前置/后置钩子，每个子测试独立执行一次。
// 用法：
//
//	for _, tt := range tests {
//	    tt := tt
//	    t.Run(tt.name, helpers.SubTestHooks(
//	        func(t *testing.T) { /* before */ },
//	        func(t *testing.T) { /* after */ },
//	        func(t *testing.T) { /* test body */ },
//	    ))
//	}
func SubTestHooks(before, after func(t *testing.T), fn func(t *testing.T)) func(*testing.T) {
	return func(t *testing.T) {
		t.Helper()
		RunWithHooks(t, TestHooks{Before: before, After: after}, fn)
	}
}
