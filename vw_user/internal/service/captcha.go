package service

import (
	"context"
	"github.com/go-kratos/kratos/v2/log"
	captv1 "vw_user/api/v1/captcha"
	"vw_user/internal/biz"
)

type CaptchaService struct {
	captv1.UnimplementedCaptchaServer
	logger *log.Helper
	cu     *biz.CaptchaUsecase
}

func NewCaptchaService(logger log.Logger, cu *biz.CaptchaUsecase) *CaptchaService {
	return &CaptchaService{
		logger: log.NewHelper(logger),
		cu:     cu,
	}
}

func (cs *CaptchaService) DeleteCodeFromCache(ctx context.Context, req *captv1.DeleteCodeFromCacheReq) (*captv1.DeleteCodeFromCacheResp, error) {
	err := cs.cu.DeleteCodeFromCache(ctx, req.Email, req.Purpose)
	if err != nil {
		return nil, err
	}
	return &captv1.DeleteCodeFromCacheResp{
		Code: 200,
		Msg:  "success",
	}, nil
}

func (cs *CaptchaService) SetCodeToCache(ctx context.Context, req *captv1.SetCodeToCacheReq) (*captv1.SetCodeToCacheResp, error) {
	err := cs.cu.SetCodeToCache(ctx, req.Email, req.CaptchaCode, req.Purpose, req.ClientIp, req.Expiration.AsDuration())

	if err != nil {
		return nil, err
	}
	return &captv1.SetCodeToCacheResp{
		Code: 200,
		Msg:  "success",
	}, nil
}
