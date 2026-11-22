package userbiz

import (
	"context"
	"net"
	"util/helper"
	"vw_gateway/internal/pkg/captcha"
	"vw_gateway/internal/pkg/ecode/errdef"

	kerrors "github.com/go-kratos/kratos/v2/errors"
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
		return errdef.AccountValidationError(err)
	}
	if err := helper.ValidateEmailCodePurpose(purpose); err != nil {
		return errdef.AccountValidationError(err)
	}
	request, ok := khttp.RequestFromServerContext(ctx)
	if !ok {
		return errdef.ErrRequestContextMissing
	}
	clientIP, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil || net.ParseIP(clientIP) == nil {
		return errdef.ErrInvalidClientIP
	}
	code, err := cu.email.CreateVerificationCode()
	if err != nil {
		return errdef.ErrEmailCodeFailed
	}
	err = cu.captchaRepo.SetCodeToCache(ctx, email, code, purpose, net.ParseIP(clientIP).String())
	if err != nil {
		if kerrors.Reason(err) != "" {
			return err
		}
		return errdef.ErrEmailCodeCacheFailed.WithCause(err)
	}
	if err := cu.email.SendCode(ctx, email, code); err != nil {
		return errdef.ErrEmailSendFailed
	}
	return nil
}
