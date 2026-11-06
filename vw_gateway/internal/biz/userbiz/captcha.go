package userbiz

import (
	"context"
	"net"
	"util/helper"
	"vw_gateway/internal/pkg/captcha"

	"github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	khttp "github.com/go-kratos/kratos/v2/transport/http"
)

type CaptchaRepo interface {
	// DeleteCodeFromCache delete code from Redis cache
	DeleteCodeFromCache(ctx context.Context, email, purpose string) error

	// SetCodeToCache set code to Redis cache
	SetCodeToCache(ctx context.Context, email, code, purpose, clientIP string) error
}

// emailCodeSender 保留现有邮件实现，并允许离线测试替换发信端，不连接 SMTP。
type emailCodeSender interface {
	CreateVerificationCode() (string, error)
	SendCode(context.Context, string, string) error
}

type CaptchaUsecase struct {
	logger      *log.Helper
	captchaRepo CaptchaRepo
	email       emailCodeSender
}

func NewCaptchaUsecase(
	logger log.Logger,
	repo CaptchaRepo,
	email *captcha.Email,
) *CaptchaUsecase {
	return &CaptchaUsecase{
		logger:      log.NewHelper(logger),
		captchaRepo: repo,
		email:       email,
	}
}

// SendCodeCaptcha 先申请用户服务中的发送额度并缓存验证码，再通过现有邮件模块发送。
// 只使用连接来源 IP；发信失败不退还额度、不恢复旧验证码，避免借失败绕过限流。
func (cu *CaptchaUsecase) SendCodeCaptcha(ctx context.Context, email, purpose string) error {
	email, err := helper.NormalizeEmail(email)
	if err != nil {
		return err
	}
	if err := helper.ValidateEmailCodePurpose(purpose); err != nil {
		return err
	}
	request, ok := khttp.RequestFromServerContext(ctx)
	if !ok {
		return errors.InternalServer("REQUEST_CONTEXT_MISSING", "缺少请求上下文")
	}
	clientIP, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil || net.ParseIP(clientIP) == nil {
		return errors.BadRequest("INVALID_CLIENT_IP", "无法识别请求来源")
	}
	code, err := cu.email.CreateVerificationCode()
	if err != nil {
		return errors.InternalServer("EMAIL_CODE_FAILED", "验证码生成失败")
	}
	err = cu.captchaRepo.SetCodeToCache(ctx, email, code, purpose, net.ParseIP(clientIP).String())
	if err != nil {
		return err
	}
	if err := cu.email.SendCode(ctx, email, code); err != nil {
		return errors.InternalServer("EMAIL_SEND_FAILED", "验证码发送失败，请稍后重试")
	}
	return nil
}
