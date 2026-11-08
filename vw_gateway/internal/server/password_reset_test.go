package server

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"vw_gateway/internal/biz/userbiz"
	"vw_gateway/internal/conf"
	user "vw_gateway/internal/service/user_service"
)

// passwordResetHTTPRepo 记录匿名请求转发的三个业务字段，不允许额外 user_id 改变目标。
type passwordResetHTTPRepo struct {
	userbiz.UserinfoRepo        // 其余接口不在本测试中执行。
	email                string // 转发的邮箱。
	code                 string // 转发的验证码。
	password             string // 转发的新密码，仅保留于本地测试内存。
	calls                int    // 下游调用次数。
}

// ForgetPassword 模拟用户服务返回原始业务错误，供 HTTP 状态映射回归。
func (r *passwordResetHTTPRepo) ForgetPassword(_ context.Context, email, code, password string) error {
	r.email, r.code, r.password = email, code, password
	r.calls++
	if code == "wrong" {
		return errors.BadRequest("INVALID_EMAIL_CODE", "验证码无效")
	}
	return nil
}

// TestPasswordResetAnonymousHTTP 验证无 token 可找回、伪造 ID 不改变目标、旧路由删除且其他修改接口仍受保护。
func TestPasswordResetAnonymousHTTP(t *testing.T) {
	logger := log.NewStdLogger(io.Discard)
	repo := &passwordResetHTTPRepo{}
	info := user.NewUserinfoService(userbiz.NewUserinfoUsecase(repo, logger), logger)
	server := NewHTTPServer(&conf.Server{Http: &conf.Server_HTTP{}}, &conf.JWT{Secret: "reset-test"}, nil, nil, nil, nil, nil, info, nil, nil, nil, nil, logger)
	for _, code := range []string{"123456", "wrong"} {
		request := httptest.NewRequest(http.MethodPost, "/api/v1/userinfo/forgetPassword?user_id=99", strings.NewReader(`{"email":"user@example.com","inputCode":"`+code+`","newPassword":"new-password","userId":"888"}`))
		request.Header.Set("Content-Type", "application/json")
		writer := httptest.NewRecorder()
		server.ServeHTTP(writer, request)
		if code == "wrong" {
			assertHTTPErrorCode(t, writer, http.StatusBadRequest)
		}
		if writer.Code != http.StatusOK || repo.email != "user@example.com" || repo.code != code || repo.password != "new-password" {
			t.Fatalf("匿名转发失败: %d %s", writer.Code, writer.Body.String())
		}
	}
	if repo.calls != 2 {
		t.Fatal("重置请求被重复提交或未转发")
	}
	for _, path := range []string{"/api/v1/userinfo/99/modifyEmail", "/api/v1/userinfo/99/modifyPassword"} {
		writer := httptest.NewRecorder()
		request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(`{}`))
		request.Header.Set("Content-Type", "application/json")
		server.ServeHTTP(writer, request)
		assertMissingToken(t, writer)
	}
	writer := httptest.NewRecorder()
	server.ServeHTTP(writer, httptest.NewRequest(http.MethodPost, "/api/v1/userinfo/99/forgetPassword", nil))
	if writer.Code != 404 && writer.Code != 405 {
		t.Fatalf("旧路由仍存在: %d", writer.Code)
	}
}
