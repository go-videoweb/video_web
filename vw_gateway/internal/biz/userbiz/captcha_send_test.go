package userbiz

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	khttp "github.com/go-kratos/kratos/v2/transport/http"
	"vw_gateway/internal/pkg/codecs"
)

// codeSendRepo 记录缓存动作，以验证缓存限流先于邮件发送，并且不会删除预算或旧数据。
type codeSendRepo struct {
	CaptchaRepo        // 测试不允许执行其他缓存操作。
	email       string // 规范化的邮箱。
	purpose     string // 明确验证码用途。
	ip          string // 实际连接来源 IP。
	stored      bool   // 本次验证码是否已通过限额检查并保存。
	fail        bool   // 模拟用户服务或 Redis 拒绝请求。
}

// SetCodeToCache 记录请求，不连接真实 RPC。
func (r *codeSendRepo) SetCodeToCache(_ context.Context, email, code, purpose, ip string) error {
	if r.fail {
		return errors.New("cache unavailable")
	}
	r.email, r.purpose, r.ip, r.stored = email, purpose, ip, true
	return nil
}

// testCodeSender 模拟 SMTP，检查验证码在发信前已保存，且错误时不向 HTTP 回传秘密。
type testCodeSender struct {
	repo  *codeSendRepo // 本次签发记录。
	calls int           // 发信次数。
	fail  bool          // 模拟邮件失败。
}

// CreateVerificationCode 提供可断言的测试验证码，不影响生产 crypto/rand 实现。
func (s *testCodeSender) CreateVerificationCode() (string, error) { return "123456", nil }

// SendCode 检查调用顺序，再模拟发送结果。
func (s *testCodeSender) SendCode(context.Context, string, string) error {
	if !s.repo.stored {
		return errors.New("send before storage")
	}
	s.calls++
	if s.fail {
		return errors.New("SMTP contains secret 123456")
	}
	return nil
}

// TestEmailCodeSendHTTP 验证来源 IP 不信任转发头、用途必填、缓存失败不发信及邮件错误不泄露验证码。
func TestEmailCodeSendHTTP(t *testing.T) {
	for _, scenario := range []string{"success", "cache failure", "mail failure", "missing purpose"} {
		t.Run(scenario, func(t *testing.T) {
			repo := &codeSendRepo{fail: scenario == "cache failure"}
			sender := &testCodeSender{repo: repo, fail: scenario == "mail failure"}
			uc := &CaptchaUsecase{captchaRepo: repo, email: sender}
			server := khttp.NewServer(khttp.ErrorEncoder(codecs.ErrorEncoder))
			server.Route("/").GET("/send", func(ctx khttp.Context) error {
				purpose := "reset_password"
				if scenario == "missing purpose" {
					purpose = ""
				}
				if err := uc.SendCodeCaptcha(ctx, " User@Example.COM ", purpose); err != nil {
					return err
				}
				return ctx.String(200, "sent")
			})
			request := httptest.NewRequest(http.MethodGet, "/send", nil)
			request.RemoteAddr = "203.0.113.5:9000"
			request.Header.Set("X-Forwarded-For", "8.8.8.8")
			writer := httptest.NewRecorder()
			server.ServeHTTP(writer, request)
			if scenario == "success" {
				if writer.Code != 200 || sender.calls != 1 || repo.email != "user@example.com" || repo.ip != "203.0.113.5" || repo.purpose != "reset_password" {
					t.Fatal("发送参数或顺序错误")
				}
			} else if writer.Code < 400 {
				t.Fatal("错误被误报成功")
			}
			if (scenario == "cache failure" || scenario == "missing purpose") && sender.calls != 0 {
				t.Fatal("验证或缓存失败仍发信")
			}
			if strings.Contains(writer.Body.String(), "123456") {
				t.Fatal("响应泄露验证码")
			}
		})
	}
}
