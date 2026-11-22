package helpers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strings"
)

// APIClient 是面向接口测试的轻量 HTTP 客户端。
// 它只负责构造和发送请求，不自动登录、刷新令牌、重试或执行业务清理。
type APIClient struct {
	baseURL      string
	httpClient   *http.Client
	responseSize int64
	tokens       TokenStore
}

// RequestOptions 描述单次请求的查询参数和自定义头，不修改客户端默认身份。
// Query 按键替换同名参数；Headers 在默认双 Token 之后应用，空值列表可删除请求头。
type RequestOptions struct {
	Query   url.Values
	Headers http.Header
}

// NewAPIClient 使用测试配置创建客户端。
// 输入：cfg 提供 BaseURL、请求超时和响应体大小限制；非正超时或限制使用默认值。
// 输出：返回可复用的 APIClient；该函数不访问网络，也不会自动登录或刷新令牌。
// 示例：client := helpers.NewAPIClient(helpers.TestConfig{BaseURL: server.URL})。
func NewAPIClient(cfg TestConfig) *APIClient {
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = defaultTimeout
	}
	limit := cfg.ResponseSize
	if limit <= 0 {
		limit = defaultResponseSize
	}
	return &APIClient{
		baseURL: strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"),
		httpClient: &http.Client{Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		}},
		responseSize: limit,
	}
}

// SetToken 设置客户端的 access 或 refresh Token。
// 输入：kind 指定令牌类型；token 为待保存的令牌文本。
// 输出：无返回值；后续请求会按 TokenStore 规则注入令牌。
// 示例：client.SetToken(helpers.AccessToken, accessToken)。
func (c *APIClient) SetToken(kind TokenKind, token string) { c.tokens.SetToken(kind, token) }

// ClearToken 清除客户端的一种 Token，不自动刷新另一种。
// 输入：kind 指定需要清除的令牌类型。
// 输出：无返回值；后续请求不再使用该类型令牌。
// 示例：client.ClearToken(helpers.AccessToken)。
func (c *APIClient) ClearToken(kind TokenKind) { c.tokens.ClearToken(kind) }

// WithTokens 返回独立身份视图，共享连接池但不改变原客户端凭证。
// 输入：两个空字符串表示匿名；不完整或非法 Token 可用于负向测试。
// 输出：新客户端不会登录、刷新或访问网络。
func (c *APIClient) WithTokens(access, refresh string) *APIClient {
	view := *c
	view.tokens = TokenStore{}
	view.SetToken(AccessToken, access)
	view.SetToken(RefreshToken, refresh)
	return &view
}

// Do 发送一次原始 HTTP 请求并读取受限大小的响应体。
// 输入：ctx 控制取消；请求体与媒体类型显式指定；options 最多一项，提供查询参数和自定义头。
// 输出：返回保留状态、响应头和响应体的 Response；URL、网络、读取或响应体超限时返回错误。
// 示例：resp, err := client.Do(ctx, http.MethodGet, "/api/v1/videos", nil, "")。
func (c *APIClient) Do(ctx context.Context, method, requestPath string, body io.Reader, contentType string, options ...RequestOptions) (*Response, error) {
	if len(options) > 1 {
		return nil, fmt.Errorf("请求配置最多传入一项")
	}
	requestURL, err := c.resolveURL(requestPath)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, requestURL, body)
	if err != nil {
		return nil, fmt.Errorf("创建 HTTP 请求失败: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	if contentType != "" {
		req.Header.Set("Content-Type", contentType)
	}
	c.tokens.ApplyToRequest(req)
	if len(options) == 1 {
		query := req.URL.Query()
		for key, values := range options[0].Query {
			query.Del(key)
			for _, value := range values {
				query.Add(key, value)
			}
		}
		req.URL.RawQuery = query.Encode()
		for key, values := range options[0].Headers {
			req.Header.Del(key)
			for _, value := range values {
				req.Header.Add(key, value)
			}
		}
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("发送 HTTP 请求失败: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, c.responseSize+1))
	if err != nil {
		return nil, fmt.Errorf("读取 HTTP 响应失败: %w", err)
	}
	if int64(len(data)) > c.responseSize {
		return nil, fmt.Errorf("HTTP 响应体超过限制 %d 字节", c.responseSize)
	}
	return &Response{StatusCode: resp.StatusCode, Status: resp.Status, Header: resp.Header.Clone(), Body: data}, nil
}

// DoJSON 将 payload 编码为 JSON 后发送请求。
// 输入：ctx、method 和 requestPath 指定请求；payload 为可 JSON 编码的请求对象，nil 表示无请求体。
// 输出：返回 APIClient.Do 生成的 Response；JSON 编码或 HTTP 请求失败时返回错误。
// 示例：resp, err := client.DoJSON(ctx, http.MethodPost, "/api/v1/videos", payload)。
func (c *APIClient) DoJSON(ctx context.Context, method, requestPath string, payload any, options ...RequestOptions) (*Response, error) {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("编码 JSON 请求体失败: %w", err)
		}
		body = bytes.NewReader(data)
	}
	return c.Do(ctx, method, requestPath, body, "application/json", options...)
}

// DoMultipart 发送包含文本字段和文件字段的 multipart 请求。
// 输入：fieldName 和 fileName 指定文件字段；fileData 为文件内容；fields 为文本字段；contentType 预留给后续 MIME 扩展。
// 输出：返回 APIClient.Do 生成的 Response；multipart 编码或 HTTP 请求失败时返回错误。
// 示例：resp, err := client.DoMultipart(ctx, http.MethodPost, "/api/v1/files", "file", "demo.txt", "text/plain", data, fields)。
func (c *APIClient) DoMultipart(ctx context.Context, method, requestPath, fieldName, fileName, contentType string, fileData []byte, fields map[string]string, options ...RequestOptions) (*Response, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for key, value := range fields {
		if err := writer.WriteField(key, value); err != nil {
			return nil, fmt.Errorf("写入 multipart 字段 %q 失败: %w", key, err)
		}
	}
	part, err := writer.CreateFormFile(fieldName, fileName)
	if err != nil {
		return nil, fmt.Errorf("创建 multipart 文件字段失败: %w", err)
	}
	if _, err := part.Write(fileData); err != nil {
		return nil, fmt.Errorf("写入 multipart 文件失败: %w", err)
	}
	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("关闭 multipart 编码器失败: %w", err)
	}
	_ = contentType
	return c.Do(ctx, method, requestPath, &body, writer.FormDataContentType(), options...)
}

// resolveURL 分离路径与查询参数，保留编码路径供非法路径用例使用。
// 输入：支持相对路径或同源绝对 URL；BaseURL 可带反向代理前缀。
// 输出：不清理点段或解码斜杠；跨源、非 HTTP 地址和片段返回错误。
// 示例：url, err := client.resolveURL("/api/v1/videos")。
func (c *APIClient) resolveURL(requestPath string) (string, error) {
	if c.baseURL == "" {
		return "", fmt.Errorf("API 测试客户端 BaseURL 未设置")
	}
	base, err := url.Parse(c.baseURL)
	if err != nil || (base.Scheme != "http" && base.Scheme != "https") || base.Host == "" || base.User != nil || base.RawQuery != "" || base.Fragment != "" {
		return "", fmt.Errorf("API 测试客户端 BaseURL 无效: %q", c.baseURL)
	}
	relative, err := url.Parse(requestPath)
	if err != nil || relative.User != nil || relative.Fragment != "" {
		return "", fmt.Errorf("API 请求路径无效")
	}
	if relative.IsAbs() || relative.Host != "" {
		if relative.Scheme != base.Scheme || !strings.EqualFold(relative.Host, base.Host) {
			return "", fmt.Errorf("API 请求必须与 BaseURL 同源")
		}
		return relative.String(), nil
	}
	escaped := strings.TrimRight(base.EscapedPath(), "/") + "/" + strings.TrimLeft(relative.EscapedPath(), "/")
	base.Path, err = url.PathUnescape(escaped)
	if err != nil {
		return "", fmt.Errorf("API 请求路径编码无效: %w", err)
	}
	base.RawPath = escaped
	base.RawQuery = relative.RawQuery
	return base.String(), nil
}
