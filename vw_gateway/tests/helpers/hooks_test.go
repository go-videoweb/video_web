package helpers

import (
	"context"
	"strings"
	"testing"
)

// TestHooks_OrderAndError 验证钩子按注册顺序执行，并保留错误上下文。
func TestHooks_OrderAndError(t *testing.T) {
	var order []string
	hooks := Hooks{}
	hooks.AddBefore(func(context.Context, *HookContext) error { order = append(order, "before-1"); return nil })
	hooks.AddBefore(func(context.Context, *HookContext) error { order = append(order, "before-2"); return nil })
	hooks.AddAfter(func(context.Context, *HookContext) error { order = append(order, "after-1"); return nil })
	if err := hooks.Before(context.Background(), &HookContext{}); err != nil {
		t.Fatal(err)
	}
	if err := hooks.After(context.Background(), &HookContext{}); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(order, ","); got != "before-1,before-2,after-1" {
		t.Fatalf("order = %q", got)
	}
}
