package captcha_test

import (
	"testing"
	"vw_gateway/internal/pkg/captcha"
)

// TestVerificationCodeFormat 仅检查安全生成器的六位格式，不发送邮件或依赖 SMTP 配置。
func TestVerificationCodeFormat(t *testing.T) {
	email := &captcha.Email{}
	for i := 0; i < 100; i++ {
		code, err := email.CreateVerificationCode()
		if err != nil || len(code) != 6 {
			t.Fatalf("生成失败: %q %v", code, err)
		}
		for _, digit := range code {
			if digit < '0' || digit > '9' {
				t.Fatal("生成非数字验证码")
			}
		}
	}
}
