package userbiz

import (
	"context"
	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	khttp "github.com/go-kratos/kratos/v2/transport/http"
	"mime/multipart"
	"net/http"
	filev1 "vw_gateway/api/v1/user/userfile"
)

const maxAvatarBytes = 5 << 20

// UserFileRepo 转发头像读取及更新，网关不操作用户服务的磁盘路径。
type UserFileRepo interface {
	// GetAvatar 读取公开用户的头像，响应只包含实际图片和 MIME。
	GetAvatar(ctx context.Context, userID int64) (*filev1.AvatarResp, error)
	// UpdateAvatar 将可信用户 ID 和 multipart 文件传给用户服务。
	UpdateAvatar(ctx context.Context, userID int64, file *multipart.FileHeader) error
}

// UserFileUsecase 校验上传参数和鉴权身份，不生成用户 ID。
type UserFileUsecase struct {
	repo UserFileRepo // 用户服务头像 RPC 适配器。
	log  *log.Helper  // 用例日志。
}

// NewUserFileUsecase 装配头像转发用例。
func NewUserFileUsecase(repo UserFileRepo, logger log.Logger) *UserFileUsecase {
	return &UserFileUsecase{repo: repo, log: log.NewHelper(logger)}
}

// GetAvatar 允许按公开用户 ID 读取头像，非法 ID 在调用下游前拒绝。
func (u *UserFileUsecase) GetAvatar(ctx context.Context, userID int64) (*filev1.AvatarResp, error) {
	if userID <= 0 {
		return nil, kerrors.BadRequest("INVALID_USER_ID", "用户 ID 无效")
	}
	return u.repo.GetAvatar(ctx, userID)
}

// UpdateAvatar 只使用鉴权身份，multipart 中的 user_id 不作为更新目标。
func (u *UserFileUsecase) UpdateAvatar(ctx context.Context) (int64, error) {
	userID, ok := ctx.Value("user_id").(int64)
	if !ok || userID <= 0 {
		return 0, kerrors.Unauthorized("INVALID_IDENTITY", "请先登录")
	}
	request, ok := khttp.RequestFromServerContext(ctx)
	if !ok {
		return 0, kerrors.InternalServer("INVALID_CONTEXT", "缺少 HTTP 请求")
	}
	file, header, err := request.FormFile("avatar")
	if err != nil {
		return 0, kerrors.BadRequest("AVATAR_REQUIRED", "请提供 avatar 文件")
	}
	_ = file.Close()
	if header.Size <= 0 {
		return 0, kerrors.BadRequest("INVALID_AVATAR", "头像不能为空")
	}
	if header.Size > maxAvatarBytes {
		return 0, kerrors.New(http.StatusRequestEntityTooLarge, "AVATAR_TOO_LARGE", "头像不能超过 5 MiB")
	}
	return userID, u.repo.UpdateAvatar(ctx, userID, header)
}
