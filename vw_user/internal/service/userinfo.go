package service

import (
	"context"
	"github.com/go-kratos/kratos/v2/log"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
	infov1 "vw_user/api/v1/userinfo"
	"vw_user/internal/biz"
)

type UserInfoService struct {
	infov1.UnimplementedUserinfoServer
	info   *biz.UserInfoUsecase
	logger *log.Helper
}

func NewUserInfoService(info *biz.UserInfoUsecase, logger log.Logger) *UserInfoService {
	return &UserInfoService{
		info:   info,
		logger: log.NewHelper(logger),
	}
}

func (info *UserInfoService) GetUserinfo(ctx context.Context, req *infov1.UserinfoReq) (*infov1.UserinfoResp, error) {
	user, err := info.info.GetUserinfo(ctx, req.UserId)
	if err != nil {
		return nil, err
	}
	return &infov1.UserinfoResp{
		UserInfo: &infov1.UserInfo{
			UserName:   user.Username,
			Email:      user.Email,
			Signature:  user.Signature,
			Shells:     user.Shells,
			CntFans:    user.CntFans,
			CntFollows: user.CntFollows,
			Gender:     user.Gender,
			//TODO: cntVideos 暂时为空，等待vw_video部分编写完成，其中会提供<根据用户id获取用户视频数量这一功能>
			//CntVideos:  int32(userbiz.CntVideos),
			AvatarPath: user.AvatarPath,
			Birthday:   timestamppb.New(user.Birthday),
		},
	}, nil
}

func (info *UserInfoService) ModifyEmail(ctx context.Context, req *infov1.ModifyEmailReq) (*infov1.ModifyEmailResp, error) {
	email, err := info.info.ModifyEmail(ctx, req.UserId, req.Email, req.InputCode)
	if err != nil {
		return nil, err
	}
	return &infov1.ModifyEmailResp{
		Email: email,
	}, nil
}

func (info *UserInfoService) ModifyPassword(ctx context.Context, req *infov1.ModifyPasswordReq) (*emptypb.Empty, error) {
	if err := info.info.ModifyPassword(ctx, req.UserId, req.OldPassword, req.NewPassword); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

func (info *UserInfoService) ModifyUserSignature(ctx context.Context, req *infov1.ModifySignatureReq) (*infov1.ModifySignatureResp, error) {
	signature, err := info.info.ModifyUserSignature(ctx, req.UserId, req.Signature)
	if err != nil {
		return nil, err
	}
	return &infov1.ModifySignatureResp{
		NewSignature: signature,
	}, nil

}

func (info *UserInfoService) ForgetPassword(ctx context.Context, req *infov1.ForgetPasswordReq) (*emptypb.Empty, error) {
	if err := info.info.ForgetPassword(ctx, req.Email, req.InputCode, req.NewPassword); err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}

func (info *UserInfoService) ModifyUsername(ctx context.Context, req *infov1.ModifyUsernameReq) (*infov1.ModifyUsernameResp, error) {
	newUsername, err := info.info.ModifyUsername(ctx, req.UserId, req.NewUsername)
	if err != nil {
		return nil, err
	}
	return &infov1.ModifyUsernameResp{
		NewUsername: newUsername,
	}, nil
}

func (info *UserInfoService) UpdateUserCntLikes(ctx context.Context, req *infov1.UpdateUserCntLikesReq) (*emptypb.Empty, error) {
	err := info.info.UpdateUserCntLikes(ctx, req.UserId, req.UpvoteFlag)
	if err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}
func (info *UserInfoService) UpdateUserCntLikesRevert(ctx context.Context, req *infov1.UpdateUserCntLikesReq) (*emptypb.Empty, error) {
	return info.UpdateUserCntLikes(ctx, req)
}

func (info *UserInfoService) UpdateUserShells(ctx context.Context, req *infov1.UpdateUserShellsReq) (*emptypb.Empty, error) {
	err := info.info.UpdateUserShells(ctx, req.UserId, req.PublisherId, req.Shells)
	if err != nil {
		return nil, err
	}
	return &emptypb.Empty{}, nil
}
func (info *UserInfoService) UpdateUserShellsRevert(ctx context.Context, req *infov1.UpdateUserShellsReq) (*emptypb.Empty, error) {
	req.Shells = -req.Shells
	return info.UpdateUserShells(ctx, req)
}
