package userdata

import (
	"context"
	"io"
	"mime/multipart"
	api "vw_gateway/api/v1/user/userfile"
	"vw_gateway/internal/biz/userbiz"
	rpc "vw_user/api/v1/userfile"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
)

const kb = 1024
const mb = 1024 * kb
const maxAvatarBytes = 5 * mb

// userFileRepo 通过 RPC 访问头像，不将图片落入网关资源目录。
type userFileRepo struct {
	data *Data       // 用户服务客户端集合。
	log  *log.Helper // 头像调用日志。
}
type streamInterface interface {
	CloseAndRecv() (*emptypb.Empty, error)
}

// NewUserFileRepo 装配头像 RPC 仓储。
func NewUserFileRepo(data *Data, logger log.Logger) userbiz.UserFileRepo {
	return &userFileRepo{data: data, log: log.NewHelper(logger)}
}

// GetAvatar 读取完整小图片并交给专用 HTTP 编码器，拒绝空结果及异常 MIME。
func (u *userFileRepo) GetAvatar(ctx context.Context, userID int64) (*api.AvatarResp, error) {
	// gRPC 默认只接收 4 MiB；头像调用单独放宽，并为 protobuf 字段预留 64 KiB。
	// 实际图片内容仍受下方 5 MiB 校验约束，其他 RPC 不受影响。
	response, err := u.data.fileClient.GetAvatar(ctx, &rpc.GetAvatarReq{UserId: userID},
		grpc.MaxCallRecvMsgSize(maxAvatarBytes+64*kb))
	if err != nil {
		return nil, err
	}
	if response == nil || len(response.Content) == 0 || len(response.Content) > maxAvatarBytes {
		return nil, kerrors.InternalServer("INVALID_AVATAR_RESPONSE", "用户服务返回的头像无效")
	}
	if response.ContentType != "image/jpeg" && response.ContentType != "image/png" {
		return nil, kerrors.InternalServer("INVALID_AVATAR_RESPONSE", "用户服务返回的头像类型无效")
	}
	return &api.AvatarResp{Content: response.Content, ContentType: response.ContentType}, nil
}

// UpdateAvatar 将可信身份作为首帧，后续发送文件块；EOF 同时返回的有效字节也必须发送。
func (u *userFileRepo) UpdateAvatar(ctx context.Context, userID int64, header *multipart.FileHeader) error {
	input, err := header.Open()
	if err != nil {
		return err
	}
	defer input.Close()
	rpcCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	stream, err := u.data.fileClient.UpdateAvatar(rpcCtx)
	if err != nil {
		return err
	}
	if err := stream.Send(&rpc.UpdateAvatarReq{Data: &rpc.UpdateAvatarReq_MetaData{
		MetaData: &rpc.UpdateAvatarReq_FileMetadata{UserId: userID},
	}}); err != nil {
		return avatarSendError(stream, err)
	}
	buffer := make([]byte, 32*kb)
	var total int
	for {
		n, readErr := input.Read(buffer)
		if n > 0 {
			total += n
			if total > maxAvatarBytes {
				return kerrors.New(413, "AVATAR_TOO_LARGE", "头像不能超过 5 MiB")
			}
			if err := stream.Send(&rpc.UpdateAvatarReq{Data: &rpc.UpdateAvatarReq_FileContent{FileContent: buffer[:n]}}); err != nil {
				return avatarSendError(stream, err)
			}
		}
		if readErr == io.EOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	_, err = stream.CloseAndRecv()
	return err
}

// avatarSendError 在服务端提前关闭流时读取业务错误，不只返回没有说明的 EOF。
func avatarSendError(stream streamInterface, sendErr error) error {
	if sendErr != io.EOF {
		return sendErr
	}
	_, err := stream.CloseAndRecv()
	if err != nil {
		return err
	}
	return sendErr
}
