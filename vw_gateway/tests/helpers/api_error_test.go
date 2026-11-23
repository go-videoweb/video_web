package helpers

import (
	"strings"
	"testing"
)

// TestBusinessError 验证 HTTP 200 错误不被当作成功，且不混淆接口自身的状态字段。
func TestBusinessError(t *testing.T) {
	r := &Response{StatusCode: 200, Body: []byte(`{"code":3101,"reason":"REGISTRATION_SESSION_FAILED","message":"账号已创建","metadata":{"user_created":"true","access_token":"secret"}}`)}
	e := AssertAPIError(t, r, 3101, "REGISTRATION_SESSION_FAILED")
	if e.Metadata["user_created"] != "true" || strings.Contains(e.Error(), "secret") {
		t.Fatal("metadata 丢失或凭证泄露")
	}
	for _, body := range []string{`{"statusCode":200,"data":{"userId":"9007199254740993"}}`, `{"resp":{"statusCode":200},"videoId":"123"}`} {
		AssertAPIJSONSuccess(t, &Response{StatusCode: 200, Body: []byte(body)})
	}
	for _, body := range []string{`{"code":3101}`, `{"code":"3101","reason":"X","message":"x"}`, `{"code":0,"reason":"X","message":"x"}`, `null`, `<html>proxy</html>`, `{} {}`} {
		if _, err := (&Response{Body: []byte(body)}).BusinessError(); err == nil {
			t.Errorf("非法结构被接受: %s", body)
		}
	}
}

// TestDiagnostic 验证嵌套凭证、消息中的凭证及非 JSON 正文不会写入诊断。
func TestDiagnostic(t *testing.T) {
	for _, body := range []string{`{"code":3101,"message":"secret","metadata":{"access_token":"secret"},"data":[{"password":"secret"}]}`, `secret-not-json`} {
		diagnostic := (&Response{StatusCode: 200, Body: []byte(body)}).Diagnostic()
		if strings.Contains(diagnostic, "secret") {
			t.Fatal("诊断泄露凭证")
		}
	}
	large := `{"data":[` + strings.Repeat(`12345,`, 1000) + `0]}`
	if len((&Response{Body: []byte(large)}).Diagnostic()) > 2200 {
		t.Fatal("诊断没有截断")
	}
}
