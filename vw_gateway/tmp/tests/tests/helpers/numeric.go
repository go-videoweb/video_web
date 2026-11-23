package helpers

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
)

// NumberAsFloat64 将 JSON 数字或基础数值类型统一转换为 float64。
func NumberAsFloat64(value any) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case json.Number:
		f, err := v.Float64()
		return f, err == nil
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return f, err == nil
	default:
		return 0, false
	}
}

// FloatEqual 比较两个浮点数是否在指定误差范围内相等。
func FloatEqual(actual, expected, epsilon float64) bool {
	if epsilon <= 0 {
		epsilon = 1e-9
	}
	return math.Abs(actual-expected) < epsilon
}
