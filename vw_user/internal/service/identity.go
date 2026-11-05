package service

import (
	"context"
	"github.com/go-kratos/kratos/v2/log"
	"google.golang.org/protobuf/types/known/emptypb"
	"time"
	idv1 "vw_user/api/v1/identity"
	"vw_user/internal/biz"
)

type UserIdentityService struct {
	idv1.UnimplementedIdentityServer
	identity *biz.UserIdentityUsecase
	logger   *log.Helper
}

func NewUserIdentityService(identity *biz.UserIdentityUsecase, logger log.Logger) *UserIdentityService {
	return &UserIdentityService{
		identity: identity,
		logger:   log.NewHelper(logger),
	}
}

func (s *UserIdentityService) Register(ctx context.Context, req *idv1.RegisterReq) (*idv1.RegisterResp, error) {
	birthday, _ := time.Parse("2006-01-02", req.Birthday)
	userID, isAdmin, err := s.identity.Register(ctx, &biz.RegisterInfo{
		Username:       req.Username,
		Password:       req.Password,
		RepeatPassword: req.RepeatPassword,
		Gender:         req.Gender,
		InputCode:      req.InputCode,
		Email:          req.Email,
		Signature:      req.Signature,
		Birthday:       birthday,
	})
	if err != nil {
		return nil, err
	}
	return &idv1.RegisterResp{
		StatusCode: 200,
		Msg:        "userbiz: " + req.Username + " register successfully.",
		UserID:     userID,
		IsAdmin:    isAdmin,
	}, nil
}

func (s *UserIdentityService) Logout(ctx context.Context, req *idv1.LogoutReq) (*emptypb.Empty, error) {
	return nil, s.identity.Logout(ctx, req.UserId)
}

func (s *UserIdentityService) Login(ctx context.Context, req *idv1.LoginReq) (*idv1.LoginResp, error) {
	return s.identity.Login(ctx, req.Username, req.Password)
}

func (s *UserIdentityService) CacheAccessToken(ctx context.Context, req *idv1.CacheAccessTokenReq) (*emptypb.Empty, error) {
	return nil, s.identity.CacheAccessToken(ctx, req.UserId, req.AccessToken, req.Expiration.AsDuration())
}
