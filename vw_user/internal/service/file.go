package service

import (
	"context"
	"github.com/go-kratos/kratos/v2/log"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
	filev1 "vw_user/api/v1/userfile"
	"vw_user/internal/biz"
)

type FileService struct {
	filev1.UnimplementedFileServiceServer
	log         *log.Helper
	fileUsecase *biz.FileUsecase
}

func NewFileService(logger log.Logger, fileUsecase *biz.FileUsecase) *FileService {
	return &FileService{
		log:         log.NewHelper(logger),
		fileUsecase: fileUsecase,
	}
}

// GetAvatar 按用户 ID 返回实际图片内容，HTTP 展示策略由网关负责。
func (fs *FileService) GetAvatar(ctx context.Context, req *filev1.GetAvatarReq) (*filev1.AvatarResp, error) {
	return fs.fileUsecase.GetAvatar(ctx, req.UserId)
}

// UpdateAvatar 使用流内可信身份更新头像，保存失败不破坏旧头像。
func (fs *FileService) UpdateAvatar(stream grpc.ClientStreamingServer[filev1.UpdateAvatarReq, emptypb.Empty]) error {
	return fs.fileUsecase.UpdateAvatar(stream)
}
