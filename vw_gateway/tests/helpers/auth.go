package helpers

import (
	"net/http"
	"strings"
)

// TokenKind 标识请求使用的鉴权令牌类型。
type TokenKind string

const (
	// AnonymousToken 表示匿名访问令牌。
	AnonymousToken TokenKind = "anonymous"
	// SessionToken 表示登录会话令牌。
	SessionToken TokenKind = "session"
)

// TokenStore 保存匿名令牌和会话令牌，客户端不会自动登录或刷新令牌。
type TokenStore struct {
	anonymous string
	session   string
}

// SetToken 设置指定类型的令牌。
// 输入：kind 指定匿名令牌或会话令牌；token 为令牌文本，首尾空白会被移除。
// 输出：无返回值；未知 kind 不修改已有令牌。
// 示例：store.SetToken(helpers.SessionToken, "access-token")。
func (s *TokenStore) SetToken(kind TokenKind, token string) {
	token = strings.TrimSpace(token)
	switch kind {
	case AnonymousToken:
		s.anonymous = token
	case SessionToken:
		s.session = token
	}
}

// ClearToken 清除指定类型的令牌。
// 输入：kind 指定需要清除的匿名令牌或会话令牌。
// 输出：无返回值；未知 kind 不修改已有令牌。
// 示例：store.ClearToken(helpers.SessionToken)。
func (s *TokenStore) ClearToken(kind TokenKind) { s.SetToken(kind, "") }

// Token 返回指定类型的令牌。
// 输入：kind 指定需要读取的令牌类型。
// 输出：返回对应令牌；未知 kind 或未设置令牌时返回空字符串。
// 示例：token := store.Token(helpers.AnonymousToken)。
func (s TokenStore) Token(kind TokenKind) string {
	switch kind {
	case AnonymousToken:
		return s.anonymous
	case SessionToken:
		return s.session
	default:
		return ""
	}
}

// ApplyToRequest 将会话令牌优先写入请求；没有会话令牌时回退到匿名令牌。
// 输入：req 为待添加 Authorization 头的 HTTP 请求，不能为 nil。
// 输出：无返回值；有可用令牌时写入 Bearer 认证头，没有令牌时保持请求头不变。
// 示例：store.ApplyToRequest(req)。
func (s TokenStore) ApplyToRequest(req *http.Request) {
	token := s.session
	if token == "" {
		token = s.anonymous
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
}
