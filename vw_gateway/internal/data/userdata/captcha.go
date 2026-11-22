package userdata

import (
	"context"
	"github.com/go-kratos/kratos/v2/log"
	"google.golang.org/protobuf/types/known/durationpb"
	"time"
	"vw_gateway/internal/biz/userbiz"
	"vw_gateway/internal/pkg/captcha"
	"vw_user/api/v1/captcha"
)

type captchaRepo struct {
	logger *log.Helper
	data   *Data
}

func NewCaptchaRepo(logger log.Logger, data *Data) userbiz.CaptchaRepo {
	return &captchaRepo{
		logger: log.NewHelper(logger),
		data:   data,
	}
}
func (c *captchaRepo) DeleteCodeFromCache(ctx context.Context, email, purpose string) error {
	_, err := c.data.captchaClient.DeleteCodeFromCache(ctx, &captv1.DeleteCodeFromCacheReq{
		Email:   email,
		Purpose: purpose,
	})
	return err
}

func (c *captchaRepo) SetCodeToCache(ctx context.Context, email, code, purpose, clientIP string) error {
	_, err := c.data.captchaClient.SetCodeToCache(ctx, &captv1.SetCodeToCacheReq{
		Email:       email,
		CaptchaCode: code,
		Purpose:     purpose,
		ClientIp:    clientIP,
		Expiration:  durationpb.New(time.Duration(captcha.ExpirationTime)),
	})
	return err
}
