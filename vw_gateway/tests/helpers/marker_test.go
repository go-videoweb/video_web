package helpers

import (
	"bytes"
	"strings"
	"testing"
)

// TestEmitAPIMarker 验证标记使用显式模板和稳定字段。
func TestEmitAPIMarker(t *testing.T) {
	var output bytes.Buffer
	err := EmitAPIMarker(&output, APIMarker{Method: "get", Path: "/api/v1/users/{user_id}", Operation: "get_user", TestName: "TestUserGet"})
	if err != nil {
		t.Fatalf("EmitAPIMarker() error = %v", err)
	}
	if got := output.String(); got != "API_MARK method=GET path=/api/v1/users/{user_id} operation=get_user test=TestUserGet\n" {
		t.Fatalf("marker = %q", got)
	}
	if err := EmitAPIMarker(&strings.Builder{}, APIMarker{}); err == nil {
		t.Fatal("empty marker should fail")
	}
}
