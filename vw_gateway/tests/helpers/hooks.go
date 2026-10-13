package helpers

import (
	"context"
	"fmt"
	"net/http"
)

// HookContext 保存一次 API 测试请求在生命周期钩子中可观察的信息。
type HookContext struct {
	Name     string
	Method   string
	Path     string
	Request  *http.Request
	Response *Response
	Metadata map[string]string
}

// Hook 在请求前或请求后执行；返回错误时由调用方决定是否终止测试。
type Hook func(context.Context, *HookContext) error

// Hooks 提供 API 请求前后的可组合生命周期回调。
type Hooks struct {
	before []Hook
	after  []Hook
}

// AddBefore 注册请求前钩子。
// 输入：hook 为请求执行前调用的生命周期函数，nil 钩子会在执行阶段被跳过。
// 输出：无返回值；钩子按注册顺序追加到 before 列表。
// 示例：hooks.AddBefore(func(ctx context.Context, value *helpers.HookContext) error { return nil })。
func (h *Hooks) AddBefore(hook Hook) { h.before = append(h.before, hook) }

// AddAfter 注册请求后钩子。
// 输入：hook 为请求执行后调用的生命周期函数，nil 钩子会在执行阶段被跳过。
// 输出：无返回值；钩子按注册顺序追加到 after 列表。
// 示例：hooks.AddAfter(func(ctx context.Context, value *helpers.HookContext) error { return nil })。
func (h *Hooks) AddAfter(hook Hook) { h.after = append(h.after, hook) }

// Before 按注册顺序执行请求前钩子。
// 输入：ctx 为钩子共享上下文；value 保存本次请求的名称、路径、请求和元数据。
// 输出：全部钩子成功时返回 nil；首个钩子失败时停止后续执行并返回包装后的错误。
// 示例：if err := hooks.Before(ctx, hookValue); err != nil { t.Fatal(err) }。
func (h *Hooks) Before(ctx context.Context, value *HookContext) error {
	for _, hook := range h.before {
		if hook == nil {
			continue
		}
		if err := hook(ctx, value); err != nil {
			return fmt.Errorf("执行 Before 钩子失败: %w", err)
		}
	}
	return nil
}

// After 按注册顺序执行请求后钩子。
// 输入：ctx 为钩子共享上下文；value 保存本次请求的响应和其他生命周期信息。
// 输出：全部钩子成功时返回 nil；首个钩子失败时停止后续执行并返回包装后的错误。
// 示例：if err := hooks.After(ctx, hookValue); err != nil { t.Log(err) }。
func (h *Hooks) After(ctx context.Context, value *HookContext) error {
	for _, hook := range h.after {
		if hook == nil {
			continue
		}
		if err := hook(ctx, value); err != nil {
			return fmt.Errorf("执行 After 钩子失败: %w", err)
		}
	}
	return nil
}
