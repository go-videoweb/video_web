package biz

import (
	"context"
	"github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"net"
	"time"
	"util/helper"
)

type CaptchaRepo interface {
	// DeleteCodeFromCache delete code cache from Redis
	DeleteCodeFromCache(ctx context.Context, email, purpose string) error
	SetCodeToCache(ctx context.Context, email, code, purpose, clientIP string, expiration time.Duration) error
	// ConsumeCode 原子校验并消费验证码；错误输入累计次数，不删除仍可使用的正确验证码。
	ConsumeCode(ctx context.Context, email, purpose, inputCode string) error
}

type CaptchaUsecase struct {
	logger *log.Helper
	repo   CaptchaRepo
}

func NewCaptchaUsecase(logger log.Logger, repo CaptchaRepo) *CaptchaUsecase {
	return &CaptchaUsecase{
		logger: log.NewHelper(logger),
		repo:   repo,
	}
}

// DeleteCodeFromCache 只删除指定用途验证码，不重置发送冷却或小时预算。
func (cu *CaptchaUsecase) DeleteCodeFromCache(ctx context.Context, email, purpose string) error {
	email, err := helper.NormalizeEmail(email)
	if err != nil {
		return err
	}
	if err := helper.ValidateEmailCodePurpose(purpose); err != nil {
		return err
	}
	return cu.repo.DeleteCodeFromCache(ctx, email, purpose)
}

// SetCodeToCache 为可信网关保存验证码，同时执行邮箱与来源 IP 限额；有效期固定五分钟。
func (cu *CaptchaUsecase) SetCodeToCache(ctx context.Context, email, code, purpose, clientIP string, expiration time.Duration) error {
	email, err := helper.NormalizeEmail(email)
	if err != nil {
		return err
	}
	if err := helper.ValidateEmailCodePurpose(purpose); err != nil {
		return err
	}
	ip := net.ParseIP(clientIP)
	if ip == nil {
		return errors.BadRequest("INVALID_CLIENT_IP", "无法识别请求来源")
	}
	if expiration != 5*time.Minute {
		return errors.BadRequest("INVALID_CODE_EXPIRATION", "验证码有效期必须为五分钟")
	}
	if len(code) != 6 {
		return invalidEmailCode()
	}
	for _, digit := range code {
		if digit < '0' || digit > '9' {
			return invalidEmailCode()
		}
	}
	return cu.repo.SetCodeToCache(ctx, email, code, purpose, ip.String(), expiration)
}

// invalidEmailCode 统一报告未签发、过期、用途不符及错误验证码，不暴露具体缓存状态。
func invalidEmailCode() error {
	return errors.BadRequest("INVALID_EMAIL_CODE", "验证码无效或已失效，请重新获取")
}
