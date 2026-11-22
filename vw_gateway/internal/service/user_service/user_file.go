package user

import (
	"context"
	"github.com/go-kratos/kratos/v2/log"
	"net/http"
	filev1 "vw_gateway/api/v1/user/userfile"
	"vw_gateway/internal/biz/userbiz"
	"vw_gateway/internal/domain"
)

// FileService 提供公开头像读取和受保护的本人头像更新。
type FileService struct {
	filev1.UnimplementedFileServiceServer                          // 标准 gRPC 未实现行为。
	userFile                              *userbiz.UserFileUsecase // 头像转发用例。
	log                                   *log.Helper              // 服务日志。
}

// NewUserFileService 装配头像接口，不直接访问用户目录。
func NewUserFileService(userFile *userbiz.UserFileUsecase, logger log.Logger) *FileService {
	return &FileService{userFile: userFile, log: log.NewHelper(logger)}
}

// GetAvatar 返回交给 HTTP 编码器的头像内容，不返回服务器路径。
func (s *FileService) GetAvatar(ctx context.Context, req *filev1.GetAvatarReq) (*filev1.AvatarResp, error) {
	return s.userFile.GetAvatar(ctx, req.UserId)
}

// UpdateAvatar 更新本人头像，并返回可直接访问的 API 相对 URL。
func (s *FileService) UpdateAvatar(ctx context.Context, _ *filev1.UpdateAvatarReq) (*filev1.UpdateAvatarResp, error) {
	userID, err := s.userFile.UpdateAvatar(ctx)
	if err != nil {
		return nil, err
	}
	return &filev1.UpdateAvatarResp{StatusCode: http.StatusOK, Message: "修改头像成功", AvatarUrl: domain.AvatarURL(userID)}, nil
}
