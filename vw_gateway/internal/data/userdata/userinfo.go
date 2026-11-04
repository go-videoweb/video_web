package userdata

import (
	"context"
	"github.com/go-kratos/kratos/v2/log"
	"vw_gateway/internal/biz/userbiz"
	"vw_gateway/internal/domain"
	infov1 "vw_user/api/v1/userinfo"
)

type userinfoRepo struct {
	logger *log.Helper
	data   *Data
}

func NewUserInfoRepo(data *Data, logger log.Logger) userbiz.UserinfoRepo {
	return &userinfoRepo{
		logger: log.NewHelper(logger),
		data:   data,
	}
}

func (repo *userinfoRepo) ForgetPassword(ctx context.Context, userId int64, email, inputCode, newPassword string) error {
	_, err := repo.data.userInfoClient.ForgetPassword(ctx, &infov1.ForgetPasswordReq{
		UserId:      userId,
		Email:       email,
		InputCode:   inputCode,
		NewPassword: newPassword,
	})
	if err != nil {
		return err
	}
	return nil
}

func (repo *userinfoRepo) ModifyEmail(ctx context.Context, userId int64, email string, inputCode string) (newEmail string, err error) {
	resp, err := repo.data.userInfoClient.ModifyEmail(ctx, &infov1.ModifyEmailReq{
		UserId:    userId,
		Email:     email,
		InputCode: inputCode,
	})
	if err != nil {
		return "", err
	}
	return resp.Email, nil
}

func (repo *userinfoRepo) ModifyPassword(ctx context.Context, userId int64, oldPasswd, newPasswd string) error {
	_, err := repo.data.userInfoClient.ModifyPassword(ctx, &infov1.ModifyPasswordReq{
		UserId:      userId,
		OldPassword: oldPasswd,
		NewPassword: newPasswd,
	})
	if err != nil {
		return err
	}
	return nil
}

func (repo *userinfoRepo) ModifyUserSignature(ctx context.Context, userId int64, signature string) (NewSignature string, err error) {
	resp, err := repo.data.userInfoClient.ModifyUserSignature(ctx, &infov1.ModifySignatureReq{
		UserId:    userId,
		Signature: signature,
	})
	if err != nil {
		return "", err
	}
	return resp.NewSignature, nil
}

func (repo *userinfoRepo) Userinfo(ctx context.Context, userId int64) (UserInfo *userbiz.UserInfo, err error) {
	resp, err := repo.data.userInfoClient.GetUserinfo(ctx, &infov1.UserinfoReq{
		UserId: userId,
	})
	if err != nil {
		return nil, err
	}
	userinfo := &userbiz.UserInfo{
		Username:   resp.UserInfo.UserName,
		Email:      resp.UserInfo.Email,
		Signature:  resp.UserInfo.Signature,
		Shells:     resp.UserInfo.Shells,
		CntFans:    resp.UserInfo.CntFans,
		CntFollows: resp.UserInfo.CntFollows,
		CntVideos:  resp.UserInfo.CntVideos,
		AvatarPath: domain.AvatarURL(userId),
		Gender:     resp.UserInfo.Gender,
		Birthday:   resp.UserInfo.Birthday.AsTime().Format("2006-01-02"),
	}
	return userinfo, nil
}

func (repo *userinfoRepo) ModifyUsername(ctx context.Context, userId int64, username string) (newUsername string, err error) {
	resp, err := repo.data.userInfoClient.ModifyUsername(ctx, &infov1.ModifyUsernameReq{
		UserId:      userId,
		NewUsername: username,
	})
	if err != nil {
		return "", err
	}
	return resp.NewUsername, nil
}
