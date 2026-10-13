package helpers

import "testing"

// TestAssertJSONEqual 验证 JSON 结构比较不受字段顺序影响。
func TestAssertJSONEqual(t *testing.T) {
	AssertJSONEqual(t, map[string]any{"a": 1, "b": []any{"x"}}, map[string]any{"b": []any{"x"}, "a": 1}, "same JSON")
}
