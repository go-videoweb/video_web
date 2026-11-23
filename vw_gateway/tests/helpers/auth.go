package helpers

import (
	"net/http"
	"strings"
)

// TokenKind 标识请求使用的鉴权令牌类型。
type TokenKind string

const (
	// AccessToken 对应 Authorization 请求头。
	AccessToken TokenKind = "access"
	// RefreshToken 对应 Refresh-Token 请求头。
	RefreshToken TokenKind = "refresh"
)

// TokenStore 分别保存双 Token；零值表示匿名，不自动补齐凭证或刷新。
type TokenStore struct {
	access  string
	refresh string
}

// SetToken 设置指定类型的令牌。
// 输入：kind 指定 access 或 refresh；token 的首尾空白会被移除。
// 输出：无返回值；未知 kind 不修改已有令牌。
// 示例：store.SetToken(helpers.AccessToken, "access-token")。
func (s *TokenStore) SetToken(kind TokenKind, token string) {
	token = strings.TrimSpace(token)
	switch kind {
	case AccessToken:
		s.access = token
	case RefreshToken:
		s.refresh = token
	}
}

// ClearToken 清除指定类型的令牌。
// 输入：kind 指定需要清除的 access 或 refresh。
// 输出：无返回值；未知 kind 不修改已有令牌。
// 示例：store.ClearToken(helpers.AccessToken)。
func (s *TokenStore) ClearToken(kind TokenKind) { s.SetToken(kind, "") }

// Token 返回指定类型的令牌。
// 输入：kind 指定需要读取的令牌类型。
// 输出：返回对应令牌；未知 kind 或未设置令牌时返回空字符串。
// 示例：token := store.Token(helpers.AccessToken)。
func (s TokenStore) Token(kind TokenKind) string {
	switch kind {
	case AccessToken:
		return s.access
	case RefreshToken:
		return s.refresh
	default:
		return ""
	}
}

// ApplyToRequest 分别设置两个认证头，不做匿名令牌回退。
// 输入：req 为请求；可单独设置一种凭证，以测试缺失或非法凭证。
// 输出：空 Token 删除对应请求头，避免请求对象复用时残留身份。
// 示例：store.ApplyToRequest(req)。
func (s TokenStore) ApplyToRequest(req *http.Request) {
	for header, token := range map[string]string{"Authorization": s.access, "Refresh-Token": s.refresh} {
		if token == "" {
			req.Header.Del(header)
		} else {
			req.Header.Set(header, "Bearer "+token)
		}
	}
}
