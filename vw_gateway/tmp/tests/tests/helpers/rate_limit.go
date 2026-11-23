package helpers

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"
)

const (
	defaultAPITestRateLimitRPS         = 35  // 普通接口测试每秒请求限制
	defaultAPITestRateLimitBurst       = 100 // 普通接口测试突发速率限制
	defaultAPITestStrictRateLimitRPS   = 3   // 严格接口请求速率限制. 严格接口为该接口定义使用了更严格的请求速率限制
	defaultAPITestStrictRateLimitBurst = 5   // 严格接口请求突发速率限制
	defaultAPITest429MaxAttempts       = 5
)

var apiRateLimiters = newAPIRateLimiterStore()

// strictRateLimitedPaths 记录服务端额外挂载 UserRateLimiter(5, 10) 的接口路径。
var strictRateLimitedPaths = map[string]struct{}{
	"/file/upload/package":                 {},
	"/file/upload/icon":                    {},
	"/file/upload/var":                     {},
	"/file/package/resources/upload":       {},
	"/calibration/data/upload":             {},
	"/pipenet/info/upload":                 {},
	"/pipenet/info/copy":                   {},
	"/api/v2/resource/file/upload":         {},
	"/api/v2/resource/file/resources/copy": {},
	"/user/background/upload":              {},
}

// apiRateLimiterStore 保存测试客户端侧按 baseURL、username 和路径类型分组的 limiter。
type apiRateLimiterStore struct {
	mu sync.Mutex
	m  map[string]*rate.Limiter
}

// newAPIRateLimiterStore 创建测试客户端侧 limiter 存储。
func newAPIRateLimiterStore() *apiRateLimiterStore {
	return &apiRateLimiterStore{m: make(map[string]*rate.Limiter)}
}

// get 返回指定 key 对应的 limiter，不存在时按当前配置创建。
func (s *apiRateLimiterStore) get(key string, limit rate.Limit, burst int) *rate.Limiter {
	s.mu.Lock()
	defer s.mu.Unlock()

	limiter, ok := s.m[key]
	if !ok {
		limiter = rate.NewLimiter(limit, burst)
		s.m[key] = limiter
	}
	return limiter
}

// doRequestWithRetry 执行 JSON 请求，并对 HTTP 429 限流响应做退避重试。
func (c *APIClient) doRequestWithRetry(reqFactory func() (*http.Request, error)) (*Response, error) {
	resp, err := c.doRawRequestWithRetry(reqFactory)
	if err != nil {
		return nil, err
	}
	return parseJSONResponse(resp)
}

// doRawRequestWithRetry 执行原始 HTTP 请求，并对 HTTP 429 限流响应做退避重试。
func (c *APIClient) doRawRequestWithRetry(reqFactory func() (*http.Request, error)) (*http.Response, error) {
	maxAttempts := envInt("API_TEST_429_MAX_ATTEMPTS", defaultAPITest429MaxAttempts)

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		req, err := reqFactory()
		if err != nil {
			return nil, err
		}

		resp, err := c.sendRawRequest(req)
		if err != nil {
			lastErr = err
			if attempt < maxAttempts {
				time.Sleep(rateLimitBackoff(nil, attempt))
			}
			continue
		}

		if resp.StatusCode != http.StatusTooManyRequests {
			c.markPrimaryRequest(req)
			return resp, nil
		}

		body := readAndCloseRateLimitedBody(resp)
		lastErr = fmt.Errorf("http 429 too many requests")
		if attempt < maxAttempts {
			delay := rateLimitBackoff(resp, attempt)
			fmt.Printf("API_RATE_LIMIT 遇到限流，正在重试 method=%s path=%s attempt=%d/%d wait=%s body=%q\n",
				req.Method, requestPath(req), attempt, maxAttempts, delay, body)
			time.Sleep(delay)
			continue
		}

		fmt.Printf("API_RATE_LIMIT 限流重试耗尽 method=%s path=%s attempts=%d body=%q\n",
			req.Method, requestPath(req), maxAttempts, body)
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("request failed after retries")
	}
	return nil, lastErr
}

// sendRawRequest 等待测试侧限速后发送一次 HTTP 请求，不负责重试。
func (c *APIClient) sendRawRequest(req *http.Request) (*http.Response, error) {
	if err := c.waitAPIRateLimit(req); err != nil {
		return nil, err
	}
	return c.client.Do(req)
}

// markPrimaryRequest 在主测接口请求完成后输出覆盖标记。
func (c *APIClient) markPrimaryRequest(req *http.Request) {
	if c.markPrimary && req != nil && req.URL != nil {
		MarkAPI(req.Method, req.URL.Path)
	}
}

// waitAPIRateLimit 在发请求前按服务端限流维度执行测试侧主动降速。
func (c *APIClient) waitAPIRateLimit(req *http.Request) error {
	if req == nil || req.URL == nil {
		return nil
	}

	limit, burst, limiterKind := apiRateLimitConfig(req.URL.Path)
	limiter := apiRateLimiters.get(apiRateLimiterKey(c, req, limiterKind), limit, burst)
	if err := limiter.Wait(context.Background()); err != nil {
		return fmt.Errorf("wait api test rate limiter: %w", err)
	}
	return nil
}

// apiRateLimitConfig 返回指定路径使用的测试侧限速配置。
func apiRateLimitConfig(path string) (rate.Limit, int, string) {
	if isStrictRateLimitedPath(path) {
		return envRateLimit("API_TEST_STRICT_RATE_LIMIT_RPS", defaultAPITestStrictRateLimitRPS),
			envInt("API_TEST_STRICT_RATE_LIMIT_BURST", defaultAPITestStrictRateLimitBurst),
			"strict"
	}
	return envRateLimit("API_TEST_RATE_LIMIT_RPS", defaultAPITestRateLimitRPS),
		envInt("API_TEST_RATE_LIMIT_BURST", defaultAPITestRateLimitBurst),
		"default"
}

// apiRateLimiterKey 生成与服务端按 username 分桶相匹配的客户端 limiter key。
func apiRateLimiterKey(c *APIClient, req *http.Request, limiterKind string) string {
	baseURL := c.baseURL
	if req != nil && req.URL != nil && req.URL.Host != "" {
		baseURL = req.URL.Scheme + "://" + req.URL.Host
	}

	username := "anonymous"
	if req != nil {
		if headerUsername := strings.TrimSpace(req.Header.Get("username")); headerUsername != "" {
			username = headerUsername
		}
	}
	return baseURL + "|" + username + "|" + limiterKind
}

// isStrictRateLimitedPath 判断路径是否命中服务端更严格的 5rps 限流路由。
func isStrictRateLimitedPath(path string) bool {
	_, ok := strictRateLimitedPaths[normalizeMarkedAPIPath(path)]
	return ok
}

// rateLimitBackoff 计算 HTTP 429 或临时请求错误后的退避等待时间。
func rateLimitBackoff(resp *http.Response, attempt int) time.Duration {
	if resp != nil {
		if retryAfter := strings.TrimSpace(resp.Header.Get("Retry-After")); retryAfter != "" {
			if seconds, err := strconv.Atoi(retryAfter); err == nil && seconds >= 0 {
				return time.Duration(seconds) * time.Second
			}
			if retryTime, err := http.ParseTime(retryAfter); err == nil {
				if delay := time.Until(retryTime); delay > 0 {
					return delay
				}
			}
		}
	}

	if attempt < 1 {
		attempt = 1
	}
	delay := time.Duration(1<<uint(attempt-1)) * 300 * time.Millisecond
	if delay > 5*time.Second {
		delay = 5 * time.Second
	}
	return delay
}

// readAndCloseRateLimitedBody 读取并关闭限流响应体，用于输出诊断日志。
func readAndCloseRateLimitedBody(resp *http.Response) string {
	if resp == nil || resp.Body == nil {
		return ""
	}
	defer func() { _ = resp.Body.Close() }()

	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	_, _ = io.Copy(io.Discard, resp.Body)
	return strings.TrimSpace(string(body))
}

// requestPath 安全获取请求路径。
func requestPath(req *http.Request) string {
	if req == nil || req.URL == nil {
		return ""
	}
	return req.URL.Path
}

// envRateLimit 读取 rate.Limit 类型的正数环境变量。
func envRateLimit(name string, fallback float64) rate.Limit {
	return rate.Limit(envFloat(name, fallback))
}

// envFloat 读取正数浮点环境变量，缺失或非法时返回默认值。
func envFloat(name string, fallback float64) float64 {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

// envInt 读取正整数环境变量，缺失或非法时返回默认值。
func envInt(name string, fallback int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return fallback
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
