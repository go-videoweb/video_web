package userbiz

import (
	"context"
	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"strconv"
	"time"
	"vw_gateway/internal/pkg/middlewares/auth"
)

type RegisterInfo struct {
	Username       string
	Password       string
	RepeatPassword string
	Gender         int32
	InputCode      string
	Email          string
	Signature      string
	Birthday       time.Time
}
type UserSummaryInfo struct {
	UserID     int64
	Username   string
	Password   string
	Email      string
	Gender     int
	Signature  string
	AvatarPath string
	Birthday   time.Time
	IsAdmin    bool
}

type LoginResponse struct {
	UserId   int64
	Username string
	IsAdmin  bool
}

type UserIdentityRepo interface {
	CacheAccessToken(ctx context.Context, userId, accessToken string, expireTime time.Duration) error
	Register(ctx context.Context, registerInfo *RegisterInfo) (userID int64, isAdmin bool, err error)
	Login(ctx context.Context, username, password string) (*LoginResponse, error)
	Logout(ctx context.Context, userId string) error
}

type UserIdentityUsecase struct {
	identityRepo UserIdentityRepo
	jwt          *auth.JWTAuth
	logger       *log.Helper
}

func NewUserIdentityUsecase(repo UserIdentityRepo, jwt *auth.JWTAuth, logger log.Logger) *UserIdentityUsecase {
	return &UserIdentityUsecase{
		identityRepo: repo,
		jwt:          jwt,
		logger:       log.NewHelper(logger),
	}
}

// Register 在创建用户后签发并缓存登录态，使注册返回的双 token 可立即用于头像更新。
// 缓存失败不回滚已创建的账号，也不返回不可用 token；客户端应转到登录，不能盲目重复注册。
func (uc *UserIdentityUsecase) Register(ctx context.Context, registerInfo *RegisterInfo) (userID int64, accessToken, refreshToken string, err error) {
	userID, isAdmin, err := uc.identityRepo.Register(ctx, registerInfo)
	if err != nil {
		return 0, "", "", err
	}

	accessToken, refreshToken, err = uc.jwt.CreateToken(userID, registerInfo.Username, isAdmin)
	if err == nil {
		err = uc.identityRepo.CacheAccessToken(ctx, strconv.FormatInt(userID, 10), accessToken, uc.jwt.RefreshExpireTime)
	}
	if err != nil {
		uc.logger.Errorf("注册后建立登录态失败: %v", err)
		return 0, "", "", kerrors.InternalServer("REGISTRATION_SESSION_FAILED", "账号已创建，但登录态建立失败，请登录后继续").
			WithMetadata(map[string]string{"user_created": "true"})
	}
	return userID, accessToken, refreshToken, nil
}

func (uc *UserIdentityUsecase) Login(ctx context.Context, username, password string) (userID int64, accessToken, refreshToken string, err error) {
	resp, err := uc.identityRepo.Login(ctx, username, password)
	if err != nil {
		return 0, "", "", err
	}

	atoken, rtoken, err := uc.jwt.CreateToken(resp.UserId, resp.Username, resp.IsAdmin)
	if err != nil {
		return 0, "", "", err
	}

	err = uc.identityRepo.CacheAccessToken(ctx, strconv.FormatInt(resp.UserId, 10), atoken, uc.jwt.RefreshExpireTime)
	if err != nil {
		// TODO: 做异步处理
		return 0, "", "", err
	}

	return resp.UserId, atoken, rtoken, nil
}

func (uc *UserIdentityUsecase) Logout(ctx context.Context, userId string) error {
	return uc.identityRepo.Logout(ctx, userId)
}
