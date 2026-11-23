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
	"path"
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
	return &APIClient{baseURL: strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/"), httpClient: &http.Client{Timeout: timeout}, responseSize: limit}
}

// SetToken 设置客户端的匿名或会话令牌。
// 输入：kind 指定令牌类型；token 为待保存的令牌文本。
// 输出：无返回值；后续请求会按 TokenStore 规则注入令牌。
// 示例：client.SetToken(helpers.SessionToken, accessToken)。
func (c *APIClient) SetToken(kind TokenKind, token string) { c.tokens.SetToken(kind, token) }

// ClearToken 清除客户端的匿名或会话令牌。
// 输入：kind 指定需要清除的令牌类型。
// 输出：无返回值；后续请求不再使用该类型令牌。
// 示例：client.ClearToken(helpers.SessionToken)。
func (c *APIClient) ClearToken(kind TokenKind) { c.tokens.ClearToken(kind) }

// Do 发送一次原始 HTTP 请求并读取受限大小的响应体。
// 输入：ctx 控制请求取消；method 和 requestPath 指定 HTTP 方法与路径；body 和 contentType 指定请求体及媒体类型。
// 输出：返回保留状态、响应头和响应体的 Response；URL、网络、读取或响应体超限时返回错误。
// 示例：resp, err := client.Do(ctx, http.MethodGet, "/api/v1/videos", nil, "")。
func (c *APIClient) Do(ctx context.Context, method, requestPath string, body io.Reader, contentType string) (*Response, error) {
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
func (c *APIClient) DoJSON(ctx context.Context, method, requestPath string, payload any) (*Response, error) {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("编码 JSON 请求体失败: %w", err)
		}
		body = bytes.NewReader(data)
	}
	return c.Do(ctx, method, requestPath, body, "application/json")
}

// DoMultipart 发送包含文本字段和文件字段的 multipart 请求。
// 输入：fieldName 和 fileName 指定文件字段；fileData 为文件内容；fields 为文本字段；contentType 预留给后续 MIME 扩展。
// 输出：返回 APIClient.Do 生成的 Response；multipart 编码或 HTTP 请求失败时返回错误。
// 示例：resp, err := client.DoMultipart(ctx, http.MethodPost, "/api/v1/files", "file", "demo.txt", "text/plain", data, fields)。
func (c *APIClient) DoMultipart(ctx context.Context, method, requestPath, fieldName, fileName, contentType string, fileData []byte, fields map[string]string) (*Response, error) {
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
	return c.Do(ctx, method, requestPath, &body, writer.FormDataContentType())
}

// resolveURL 将相对接口路径拼接到客户端 BaseURL，并保留绝对 URL 的直接调用能力。
// 输入：requestPath 为相对接口路径或完整绝对 URL；客户端 BaseURL 必须包含合法的 scheme 和 host。
// 输出：返回可发送的完整 URL；BaseURL 为空、非法或解析失败时返回错误。
// 示例：url, err := client.resolveURL("/api/v1/videos")。
func (c *APIClient) resolveURL(requestPath string) (string, error) {
	if c.baseURL == "" {
		return "", fmt.Errorf("API 测试客户端 BaseURL 未设置")
	}
	if parsed, err := url.Parse(requestPath); err == nil && parsed.IsAbs() {
		return parsed.String(), nil
	}
	base, err := url.Parse(c.baseURL)
	if err != nil || base.Scheme == "" || base.Host == "" {
		return "", fmt.Errorf("API 测试客户端 BaseURL 无效: %q", c.baseURL)
	}
	cleanPath := requestPath
	if !strings.HasPrefix(cleanPath, "/") {
		cleanPath = "/" + cleanPath
	}
	base.Path = path.Join(base.Path, cleanPath)
	if strings.HasSuffix(cleanPath, "/") {
		base.Path += "/"
	}
	return base.String(), nil
}
