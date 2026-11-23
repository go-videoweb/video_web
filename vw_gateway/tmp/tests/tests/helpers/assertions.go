package helpers

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

// AssertResponse 断言响应
type AssertResponse struct {
	t *testing.T
}

// NewAssertResponse 创建响应断言
func NewAssertResponse(t *testing.T) *AssertResponse {
	return &AssertResponse{t: t}
}

// IsSuccess 断言响应成功
func (a *AssertResponse) IsSuccess(resp *Response, message string) bool {
	if resp == nil {
		a.t.Errorf("%s: response is nil", message)
		return false
	}

	if resp.Status != 0 {
		a.t.Errorf("%s: expected code 0 got %d. Message: %s. Error: %s", message, resp.Status, resp.Msg, resp.Err)
		return false
	}

	return true
}

// IsError 断言响应错误
func (a *AssertResponse) IsError(resp *Response, expectedCode int, message string) bool {
	if resp == nil {
		a.t.Errorf("%s: response is nil", message)
		return false
	}

	if resp.Status == expectedCode {
		return true
	}

	a.t.Errorf("%s: expected code %d, got %d. Message: %s; Error: %s", message, expectedCode, resp.Status, resp.Msg, resp.Err)
	return false
}

// HasField 断言响应数据包含指定字段
func (a *AssertResponse) HasField(resp *Response, fieldName string, message string) bool {
	if resp == nil || resp.Data == nil {
		a.t.Errorf("%s: response or data is nil", message)
		return false
	}

	dataMap, ok := resp.Data.(map[string]interface{})
	if !ok {
		a.t.Errorf("%s: response data is not a map", message)
		return false
	}

	if _, exists := dataMap[fieldName]; !exists {
		a.t.Errorf("%s: field '%s' not found in response data", message, fieldName)
		return false
	}

	return true
}

// HasFields 断言响应数据包含多个指定字段
func (a *AssertResponse) HasFields(resp *Response, fieldNames []string, message string) bool {
	if resp == nil || resp.Data == nil {
		a.t.Errorf("%s: response or data is nil", message)
		return false
	}

	dataMap, ok := resp.Data.(map[string]interface{})
	if !ok {
		a.t.Errorf("%s: response data is not a map", message)
		return false
	}

	for _, fieldName := range fieldNames {
		if _, exists := dataMap[fieldName]; !exists {
			a.t.Errorf("%s: field '%s' not found in response data", message, fieldName)
			return false
		}
	}

	return true
}

// FieldEquals 断言字段值相等
func (a *AssertResponse) FieldEquals(resp *Response, fieldName string, expectedValue interface{}, message string) bool {
	if resp == nil || resp.Data == nil {
		a.t.Errorf("%s: response or data is nil", message)
		return false
	}

	dataMap, ok := resp.Data.(map[string]interface{})
	if !ok {
		a.t.Errorf("%s: response data is not a map", message)
		return false
	}

	actualValue, exists := dataMap[fieldName]
	if !exists {
		a.t.Errorf("%s: field '%s' not found in response data", message, fieldName)
		return false
	}

	if actualValue != expectedValue {
		a.t.Errorf("%s: field '%s' expected %v, got %v", message, fieldName, expectedValue, actualValue)
		return false
	}

	return true
}

// DataIsNotEmpty 断言响应数据不为空
func (a *AssertResponse) DataIsNotEmpty(resp *Response, message string) bool {
	if resp == nil {
		a.t.Errorf("%s: response is nil", message)
		return false
	}

	if resp.Data == nil {
		a.t.Errorf("%s: response data is nil", message)
		return false
	}

	// 检查是否为空 map 或空数组
	dataJSON, _ := json.Marshal(resp.Data)
	if string(dataJSON) == "{}" || string(dataJSON) == "[]" || string(dataJSON) == "null" {
		a.t.Errorf("%s: response data is empty", message)
		return false
	}

	return true
}

// DataIsArray 断言响应数据是数组
func (a *AssertResponse) DataIsArray(resp *Response, message string) bool {
	if resp == nil || resp.Data == nil {
		a.t.Errorf("%s: response or data is nil", message)
		return false
	}

	_, ok := resp.Data.([]interface{})
	if !ok {
		a.t.Errorf("%s: response data is not an array", message)
		return false
	}

	return true
}

// ArrayLength 断言数组长度
func (a *AssertResponse) ArrayLength(resp *Response, expectedLength int, message string) bool {
	if resp == nil || resp.Data == nil {
		a.t.Errorf("%s: response or data is nil", message)
		return false
	}
	arr, ok := resp.Data.([]interface{})
	if !ok {
		a.t.Errorf("%s: response data is not an array", message)
		return false
	}

	if len(arr) != expectedLength {
		a.t.Errorf("%s: expected array length %d, got %d", message, expectedLength, len(arr))
		return false
	}

	return true
}

// ArrayLengthGreaterThan 断言数组长度大于指定值
func (a *AssertResponse) ArrayLengthGreaterThan(resp *Response, minLength int, message string) bool {
	if resp == nil || resp.Data == nil {
		a.t.Errorf("%s: response or data is nil", message)
		return false
	}

	arr, ok := resp.Data.([]interface{})
	if !ok {
		a.t.Errorf("%s: response data is not an array", message)
		return false
	}

	if len(arr) <= minLength {
		a.t.Errorf("%s: expected array length > %d, got %d", message, minLength, len(arr))
		return false
	}

	return true
}

// MapStringFieldIn 断言 map 中指定字段为 string, 且值属于 allowed 中的内容
func (a *AssertResponse) MapStringFieldIn(data map[string]any, field string, allowed []string, message string) bool {
	actual, ok := data[field].(string)
	if !ok {
		a.t.Errorf("%s: %s is not string: %T", message, field, data[field])
		return false
	}
	for _, value := range allowed {
		if actual == value {
			return true
		}
	}
	a.t.Errorf("%s: %s=%q is not in %v", message, field, actual, allowed)
	return false
}

// ValueIsStructurallyEmpty 断言任意值在结构意义上为空。
// 判空规则：
//   - nil / 空 map / 空 slice / 空数组 / 空结构体 => 空
//   - map/slice/array/struct 内所有子项递归均为空 => 空
//   - 标量类型使用零值语义："" / 0 / false 视为空
func (a *AssertResponse) ValueIsStructurallyEmpty(value any, message string) bool {
	if IsStructurallyEmpty(value) {
		return true
	}
	a.t.Errorf("%s: expected value to be structurally empty, got %#v", message, value)
	return false
}

// IsStructurallyEmpty 判断任意值是否在结构意义上为空。
func IsStructurallyEmpty(value any) bool {
	return isStructurallyEmptyValue(reflect.ValueOf(value))
}

func isStructurallyEmptyValue(v reflect.Value) bool {
	if !v.IsValid() {
		return true
	}

	for v.Kind() == reflect.Interface || v.Kind() == reflect.Pointer {
		if v.IsNil() {
			return true
		}
		v = v.Elem()
	}

	switch v.Kind() {
	case reflect.Map:
		if v.IsNil() || v.Len() == 0 {
			return true
		}
		iter := v.MapRange()
		for iter.Next() {
			if !isStructurallyEmptyValue(iter.Value()) {
				return false
			}
		}
		return true
	case reflect.Slice, reflect.Array:
		if v.Len() == 0 {
			return true
		}
		for i := 0; i < v.Len(); i++ {
			if !isStructurallyEmptyValue(v.Index(i)) {
				return false
			}
		}
		return true
	case reflect.Struct:
		if v.NumField() == 0 {
			return true
		}
		for i := 0; i < v.NumField(); i++ {
			if !isStructurallyEmptyValue(v.Field(i)) {
				return false
			}
		}
		return true
	default:
		return v.IsZero()
	}
}

// PrintResponse 打印响应（用于调试）
func (a *AssertResponse) PrintResponse(resp *Response) {
	if resp == nil {
		fmt.Println("Response is nil")
		return
	}

	data, _ := json.MarshalIndent(resp, "", "  ")
	fmt.Println(string(data))
}
