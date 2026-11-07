package user

import (
	"context"
	"github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	infov1 "vw_gateway/api/v1/user/userinfo"
	"vw_gateway/internal/biz/userbiz"
)

type InfoService struct {
	infov1.UnimplementedUserinfoServer
	info   *userbiz.UserinfoUsecase
	logger *log.Helper
}

func NewUserinfoService(info *userbiz.UserinfoUsecase, logger log.Logger) *InfoService {
	return &InfoService{
		info:   info,
		logger: log.NewHelper(logger),
	}
}
func (uis *InfoService) ForgetPassword(ctx context.Context, req *infov1.ForgetPasswordReq) (*infov1.ForgetPasswordResp, error) {
	err := uis.info.ForgetPassword(ctx, req.Email, req.InputCode, req.NewPassword)
	if err != nil {
		return nil, err
	}
	return &infov1.ForgetPasswordResp{
		Common: &infov1.CommonResp{
			Status:  200,
			Message: "用户重置密码成功",
		},
	}, nil
}

func (uis *InfoService) ModifyEmail(ctx context.Context, req *infov1.ModifyEmailReq) (*infov1.ModifyEmailResp, error) {
	// 换绑影响后续找回密码的归属，目标必须来自鉴权身份，不能相信路由中的 user_id。
	userID, ok := ctx.Value("user_id").(int64)
	if !ok || userID <= 0 {
		return nil, errors.Unauthorized("USER_ID_MISSING", "请先登录")
	}
	newEmail, err := uis.info.ModifyEmail(ctx, userID, req.Email, req.InputCode)
	if err != nil {
		return nil, err
	}
	return &infov1.ModifyEmailResp{
		Email: newEmail,
		Common: &infov1.CommonResp{
			Status:  200,
			Message: "用户修改邮箱成功",
		},
	}, nil
}

func (uis *InfoService) ModifyPassword(ctx context.Context, req *infov1.ModifyPasswordReq) (*infov1.ModifyPasswordResp, error) {
	userID, ok := ctx.Value("user_id").(int64)
	if !ok || userID <= 0 {
		return nil, errors.Unauthorized("USER_ID_MISSING", "请先登录")
	}
	err := uis.info.ModifyPassword(ctx, userID, req.OldPassword, req.NewPassword)
	if err != nil {
		return nil, err
	}
	return &infov1.ModifyPasswordResp{
		Common: &infov1.CommonResp{
			Status:  200,
			Message: "用户修改密码成功",
		},
	}, nil
}

func (uis *InfoService) ModifyUserSignature(ctx context.Context, req *infov1.ModifySignatureReq) (*infov1.ModifySignatureResp, error) {
	newSignature, err := uis.info.ModifyUserSignature(ctx, req.UserId, req.Signature)
	if err != nil {
		return nil, err
	}
	return &infov1.ModifySignatureResp{
		NewSignature: newSignature,
		Common: &infov1.CommonResp{
			Status:  200,
			Message: "用户修改签名成功",
		},
	}, nil
}

func (uis *InfoService) Userinfo(ctx context.Context, req *infov1.UserinfoReq) (*infov1.UserinfoResp, error) {
	resp, err := uis.info.Userinfo(ctx, req.UserId)
	if err != nil {
		return nil, err
	}
	return &infov1.UserinfoResp{
		UserInfo: &infov1.UserInfo{
			UserName:   resp.Username,
			Email:      resp.Email,
			Signature:  resp.Signature,
			Shells:     resp.Shells,
			CntFans:    resp.CntFans,
			CntFollows: resp.CntFollows,
			CntVideos:  resp.CntVideos,
			AvatarPath: resp.AvatarPath,
			Gender:     resp.Gender,
			Birthday:   resp.Birthday,
		},
		Common: &infov1.CommonResp{
			Status:  200,
			Message: "获取用户信息成功",
		},
	}, nil
}

func (uis *InfoService) ModifyUsername(ctx context.Context, req *infov1.ModifyUsernameReq) (*infov1.ModifyUsernameResp, error) {
	newUsername, err := uis.info.ModifyUsername(ctx, req.UserId, req.NewUsername)
	if err != nil {
		return nil, err
	}
	return &infov1.ModifyUsernameResp{
		NewUsername: newUsername,
		Common: &infov1.CommonResp{
			Status:  200,
			Message: "修改用户名成功",
		},
	}, nil
}
