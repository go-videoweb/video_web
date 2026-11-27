// Package identity 提供可对远程网关及本地 HTTP 夹具复用的身份认证场景。
package identity

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"vw_gateway/internal/pkg/ecode"
	"vw_gateway/tests/helpers"
)

// 下游错误码是 HTTP 契约快照，来源 vw_user/internal/pkg/ecode/errors.go。
// 不跨 Go internal 边界导入用户实现，契约变化须连同测试方案一起审核。
const (
	userNotFound     = 4005
	passwordError    = 4006
	userExists       = 4007
	passwordMismatch = 4009
	signatureTooLong = 4010
	invalidEmail     = 4109
	invalidPassword  = 4110
	invalidEmailCode = 4114
)

// Registration 描述一次性注册夹具；代码不自动发信，也不删除注册账号。
// Enabled 只控制成功注册场景，负向校验始终执行；启用后须提供未注册用户名、邮箱及 register 用途验证码。
type Registration struct {
	Enabled  bool
	Username string
	Email    string
	Password string
	Code     string
}

// Suite 保存专用测试账户与客户端；执行时串行，不自动重试业务请求。
type Suite struct {
	Client       *helpers.APIClient
	Username     string
	Password     string
	Timeout      time.Duration
	Registration Registration
}

type session struct {
	UserID  string `json:"userId"`
	Access  string `json:"accessToken"`
	Refresh string `json:"refreshToken"`
}

// request 为每次请求设置截止时间，主测请求收到响应后才记录覆盖标记。
// 错误不打印 URL、凭证或原始 Body；辅助登录、会话回读与清理不记录主测。
func (s Suite) request(t *testing.T, client *helpers.APIClient, method, path string, body any, operation string) *helpers.Response {
	t.Helper()
	timeout := s.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	response, err := client.DoJSON(ctx, method, path, body)
	if err != nil {
		t.Fatal("身份请求失败；请检查网络、超时及测试环境，未自动重试")
	}
	if operation != "" {
		template := path
		if operation == "Logout" {
			template = "/api/v1/user/{user_id}/logout"
		}
		var mark strings.Builder
		err := helpers.EmitAPIMarker(&mark, helpers.APIMarker{
			Method:    method,
			Path:      template,
			Operation: "/gateway.api.v1.user.id.Identity/" + operation,
			TestName:  t.Name(),
		})
		if err != nil {
			t.Fatal("生成接口覆盖标记失败")
		}
		t.Log(strings.TrimSpace(mark.String()))
	}
	return response
}

// readSession 严格检查大 ID 的字符串表示及双 Token，不将 Token 写入失败日志。
func readSession(t *testing.T, response *helpers.Response) session {
	t.Helper()
	helpers.AssertAPIJSONSuccess(t, response)
	var result struct {
		StatusCode int     `json:"statusCode"`
		Data       session `json:"data"`
	}
	if err := response.DecodeJSON(&result); err != nil {
		t.Fatal("登录/注册响应协议错误")
	}
	id, err := strconv.ParseInt(result.Data.UserID, 10, 64)
	if err != nil || id <= 0 || result.StatusCode != 200 || result.Data.Access == "" || result.Data.Refresh == "" {
		t.Fatal("身份响应缺少正字符串 ID、业务状态或双 Token")
	}
	return result.Data
}

func (s Suite) login(t *testing.T, primary bool) session {
	t.Helper()
	op := ""
	if primary {
		op = "Login"
	}
	return readSession(t, s.request(t, s.Client.WithTokens("", ""), http.MethodPost, "/api/v1/user/login", map[string]string{"username": s.Username, "password": s.Password}, op))
}

// checkSession 通过已有的强制鉴权接口验证会话与身份，不仅检查 Token 非空。
func (s Suite) checkSession(t *testing.T, value session, errorCode int32) {
	t.Helper()
	client := s.Client.WithTokens(value.Access, value.Refresh)
	response := s.request(t, client, http.MethodGet, "/api/v1/favorites/list", nil, "")
	if errorCode != 0 {
		helpers.AssertAPIError(t, response, errorCode, "")
		return
	}
	helpers.AssertAPIJSONSuccess(t, response)
	var result struct {
		UserID string `json:"userId"`
		List   []any  `json:"favoriteList"`
	}
	if err := response.DecodeJSON(&result); err != nil || result.UserID != value.UserID || result.List == nil {
		t.Fatal("双 Token 会话不可用、身份不一致或收藏夹列表协议错误")
	}
}

// uniqueSuffix 生成测试数据前缀；不在日志记录邮箱、验证码或密码。
func uniqueSuffix(t *testing.T) string {
	t.Helper()
	var data [8]byte
	if _, err := rand.Read(data[:]); err != nil {
		t.Fatal("生成隔离测试前缀失败")
	}
	return hex.EncodeToString(data[:])
}

// RunLogin 验证 U01 成功、错误凭证、空字段、会话替换及缺失单 Token。
// 输入：必须是可重复登录的专用账号；清理只注销本次登录，不恢复先前已替换的会话。
func (s Suite) RunLogin(t *testing.T) {
	t.Helper()
	current := s.login(t, true)
	t.Cleanup(func() { s.logout(t, current.UserID, false) })
	t.Run("有效双Token", func(t *testing.T) { s.checkSession(t, current, 0) })
	for _, tc := range []struct {
		name, username, password string
		code                     int32
	}{
		{"错误密码", s.Username, "wrong-" + uniqueSuffix(t), passwordError},
		{"不存在用户", "missing-" + uniqueSuffix(t), s.Password, userNotFound},
		// 现有实现不提前返回空字段专用码：空用户名走查询，空密码走密码比较。
		{"空用户名", "", s.Password, userNotFound},
		{"空密码", s.Username, "", passwordError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			resp := s.request(t, s.Client.WithTokens("", ""), http.MethodPost, "/api/v1/user/login", map[string]string{"username": tc.username, "password": tc.password}, "Login")
			helpers.AssertAPIError(t, resp, tc.code, "")
			s.checkSession(t, current, 0)
		})
	}
	t.Run("仅一个Token", func(t *testing.T) {
		accessOnly, refreshOnly := current, current
		accessOnly.Refresh, refreshOnly.Access = "", ""
		s.checkSession(t, accessOnly, ecode.AUTH_TokenMissing)
		s.checkSession(t, refreshOnly, ecode.AUTH_TokenMissing)
	})
	t.Run("重登替换旧会话", func(t *testing.T) {
		next := s.login(t, true)
		if next.UserID != current.UserID || next.Access == current.Access || next.Refresh == current.Refresh {
			t.Fatal("重新登录身份变化或凭据复用")
		}
		s.checkSession(t, current, ecode.IDENTITY_ErrUserLoggedOut)
		s.checkSession(t, next, 0)
		current = next
	})
	t.Run("错误JSON结构", func(t *testing.T) {
		response := s.request(t, s.Client.WithTokens("", ""), http.MethodPost, "/api/v1/user/login", "not-an-object", "Login")
		helpers.AssertAPIError(t, response, ecode.RequestCodec, "")
	})
}

// logout 只针对当前场景拥有的账户，不尝试注销另一个用户以适配公开退出缺口。
func (s Suite) logout(t *testing.T, userID string, primary bool) {
	t.Helper()
	op := ""
	if primary {
		op = "Logout"
	}
	response := s.request(t, s.Client.WithTokens("", ""), http.MethodPost, "/api/v1/user/"+userID+"/logout", nil, op)
	helpers.AssertAPIJSONSuccess(t, response)
	var body struct {
		StatusCode int `json:"statusCode"`
	}
	if err := response.DecodeJSON(&body); err != nil || body.StatusCode != 200 {
		t.Fatal("退出响应业务状态不正确")
	}
}

// RunLogout 验证 U03 清除自身登录态、重复退出不报错及重新登录可恢复。
// 当前退出接口公开；跨用户退出的安全目标未确认，不编写“允许越权”为成功用例。
func (s Suite) RunLogout(t *testing.T) {
	t.Helper()
	value := s.login(t, false)
	t.Cleanup(func() { s.logout(t, value.UserID, false) })
	s.checkSession(t, value, 0)
	s.logout(t, value.UserID, true)
	s.checkSession(t, value, ecode.IDENTITY_ErrUserLoggedOut)
	s.logout(t, value.UserID, true)
	value = s.login(t, false)
	s.checkSession(t, value, 0)
}

func registerBody(username, email, password, code string) map[string]any {
	return map[string]any{
		"username":       username,
		"email":          email,
		"password":       password,
		"repeatPassword": password,
		"birthday":       "2000-01-01",
		"gender":         1,
		"signature":      "api-test",
		"inputCode":      code,
	}
}

// RunRegister 验证 U02 无须发信的前置失败路径，成功注册仅在显式一次性夹具启用后执行。
// 输入：Registration 的验证码只发出一次成功注册请求，失败或响应不确定都不自动重试。
func (s Suite) RunRegister(t *testing.T) {
	t.Helper()
	for _, tc := range []struct {
		name   string
		code   int32
		change func(map[string]any)
	}{
		{"邮箱无效", invalidEmail, func(b map[string]any) { b["email"] = "invalid" }},
		{"用户名重复", userExists, func(b map[string]any) { b["username"] = s.Username }},
		{"密码少于六字节", invalidPassword, func(b map[string]any) { b["password"] = "short"; b["repeatPassword"] = "short" }},
		{"密码超过七十二字节", invalidPassword, func(b map[string]any) { b["password"] = strings.Repeat("a", 73) }},
		{"密码不一致", passwordMismatch, func(b map[string]any) { b["repeatPassword"] = "different-password" }},
		{"签名超过二十五字符", signatureTooLong, func(b map[string]any) { b["signature"] = strings.Repeat("测", 26) }},
		{"缺验证码", invalidEmailCode, func(b map[string]any) {}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			suffix := uniqueSuffix(t)
			body := registerBody("api-"+suffix, suffix+"@example.invalid", "Example-pass-123", "")
			tc.change(body)
			resp := s.request(t, s.Client.WithTokens("", ""), http.MethodPost, "/api/v1/user/register", body, "Register")
			helpers.AssertAPIError(t, resp, tc.code, "")
		})
	}
	t.Run("真实邮件码成功注册", func(t *testing.T) {
		fixture := s.Registration
		if !fixture.Enabled {
			t.Skip("未启用一次性注册夹具；成功注册未验证")
		}
		if fixture.Username == "" || fixture.Email == "" || fixture.Password == "" || fixture.Code == "" || fixture.Username == s.Username {
			t.Fatal("注册夹具不完整或复用了登录账号")
		}
		body := registerBody(fixture.Username, fixture.Email, fixture.Password, fixture.Code)
		response := s.request(t, s.Client.WithTokens("", ""), http.MethodPost, "/api/v1/user/register", body, "Register")
		if apiErr, err := response.BusinessError(); err == nil && apiErr != nil && apiErr.Code == ecode.RegistrationSessionFailed {
			t.Fatal("账号可能已创建但会话失败（user_created），请核对注册账号，不重试注册")
		}
		value := readSession(t, response)
		t.Logf("本轮注册用户 ID=%s；没有删除接口，需按隔离环境策略回收", value.UserID)
		t.Cleanup(func() { s.logout(t, value.UserID, false) })
		s.checkSession(t, value, 0)
		// 重新注册同一资料应优先识别已占用邮箱，不消耗已使用验证码再次创建。
		duplicate := s.request(t, s.Client.WithTokens("", ""), http.MethodPost, "/api/v1/user/register", body, "Register")
		helpers.AssertAPIError(t, duplicate, 4102, "EMAIL_IN_USE")
	})
}

// ValidateRegistrationFlag 只接受显式 0/1，避免拼错配置后静默跳过成功注册。
func ValidateRegistrationFlag(value string) (bool, error) {
	switch value {
	case "", "0":
		return false, nil
	case "1":
		return true, nil
	default:
		return false, fmt.Errorf("API_TEST_REGISTER 只能为 0 或 1")
	}
}
