package data

import (
	"context"
	"crypto/sha256"
	"fmt"
	"github.com/go-kratos/kratos/v2/log"
	"time"
	"vw_user/internal/biz"
	"vw_user/internal/pkg/ecode/errdef"
)

type captchaRepo struct {
	logger *log.Helper
	data   *Data
}

func NewCaptRepo(data *Data, logger log.Logger) biz.CaptchaRepo {
	return &captchaRepo{
		logger: log.NewHelper(logger),
		data:   data,
	}
}

func (r *captchaRepo) DeleteCodeFromCache(ctx context.Context, email, purpose string) error {
	_, err := r.data.redis.Del(ctx, emailCodeKey(email, purpose)).Result()
	return err
}

// SetCodeToCache 先限制来源 IP，再原子检查邮箱额度、替换指定用途验证码；不跨 Redis slot 执行脚本。
func (r *captchaRepo) SetCodeToCache(ctx context.Context, email, code, purpose, clientIP string, expiration time.Duration) error {
	ipKey := fmt.Sprintf("email-code-ip:%x", sha256.Sum256([]byte(clientIP)))
	allowed, err := reserveEmailCodeIP.Run(ctx, r.data.redis, []string{ipKey}).Int()
	if err != nil {
		return err
	}
	if allowed != 1 {
		return errdef.ErrEmailCodeRateLimited
	}
	allowed, err = storeEmailCode.Run(ctx, r.data.redis, []string{
		emailCodeKey(email, "cooldown"), emailCodeKey(email, "send-budget"), emailCodeKey(email, purpose),
	}, code, int64(expiration/time.Second)).Int()
	if err != nil {
		return err
	}
	if allowed != 1 {
		return errdef.ErrEmailCodeRateLimited
	}
	return nil
}

// ConsumeCode 校验并一次性消费正确验证码，错误尝试与小时预算不会因重发而清零。
func (r *captchaRepo) ConsumeCode(ctx context.Context, email, purpose, inputCode string) error {
	if len(inputCode) != 6 {
		return errdef.ErrInvalidEmailCode
	}
	result, err := consumeEmailCode.Run(ctx, r.data.redis, []string{
		emailCodeKey(email, purpose), emailCodeKey(email, "verify-budget"),
	}, inputCode).Int()
	if err != nil {
		return err
	}
	if result == -1 {
		return errdef.WithMessage(errdef.ErrEmailCodeRateLimited, "验证码验证次数过多，请稍后再试")
	}
	if result != 1 {
		return errdef.ErrInvalidEmailCode
	}
	return nil
}
