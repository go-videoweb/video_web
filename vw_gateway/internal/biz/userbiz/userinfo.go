package userbiz

import (
	"context"
	"github.com/go-kratos/kratos/v2/log"
)

type UserinfoRepo interface {
	ForgetPassword(ctx context.Context, email, inputCode, newPassword string) error
	ModifyEmail(ctx context.Context, userId int64, email string, inputCode string) (newEmail string, err error)
	ModifyPassword(ctx context.Context, userId int64, oldPasswd, newPasswd string) error
	ModifyUserSignature(ctx context.Context, userId int64, signature string) (NewSignature string, err error)
	Userinfo(ctx context.Context, userId int64) (UserInfo *UserInfo, err error)
	ModifyUsername(ctx context.Context, userId int64, username string) (newUsername string, err error)
}

type UserinfoUsecase struct {
	repo   UserinfoRepo
	logger *log.Helper
}

func NewUserinfoUsecase(repo UserinfoRepo, logger log.Logger) *UserinfoUsecase {
	return &UserinfoUsecase{
		repo:   repo,
		logger: log.NewHelper(logger),
	}
}

type UserInfo struct {
	Username   string
	Email      string
	Signature  string
	Shells     int32
	CntFans    int32
	CntFollows int32
	CntVideos  int32
	AvatarPath string
	Gender     int32
	Birthday   string
}

// ForgetPassword 直接将邮箱和验证材料交给用户服务，不在网关查询或拼接目标账号。
func (info *UserinfoUsecase) ForgetPassword(ctx context.Context, email, inputCode, newPassword string) error {
	return info.repo.ForgetPassword(ctx, email, inputCode, newPassword)
}

func (info *UserinfoUsecase) ModifyEmail(ctx context.Context, userId int64, email string, inputCode string) (newEmail string, err error) {
	return info.repo.ModifyEmail(ctx, userId, email, inputCode)
}

func (info *UserinfoUsecase) ModifyPassword(ctx context.Context, userId int64, oldPasswd, newPasswd string) error {
	return info.repo.ModifyPassword(ctx, userId, oldPasswd, newPasswd)
}

func (info *UserinfoUsecase) ModifyUserSignature(ctx context.Context, userId int64, signature string) (NewSignature string, err error) {
	return info.repo.ModifyUserSignature(ctx, userId, signature)
}

func (info *UserinfoUsecase) Userinfo(ctx context.Context, userId int64) (UserInfo *UserInfo, err error) {
	return info.repo.Userinfo(ctx, userId)
}

func (info *UserinfoUsecase) ModifyUsername(ctx context.Context, userId int64, username string) (newUsername string, err error) {
	return info.repo.ModifyUsername(ctx, userId, username)
}
