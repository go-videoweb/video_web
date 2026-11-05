package biz

import (
	"bytes"
	"context"
	"errors"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
	filev1 "vw_user/api/v1/userfile"
)

const maxAvatarBytes = 5 << 20
const maxAvatarDimension = 4096

// FileRepo 维护用户头像路径，SQL 统一从 getQuery 获取连接。
type FileRepo interface {
	// GetAvatarPath 查找有效用户当前路径，不存在用户返回 404。
	GetAvatarPath(ctx context.Context, userID int64) (string, error)
	// UpdateAvatarPath 仅在旧路径仍一致时切换到新文件，避免覆盖其他请求的头像。
	UpdateAvatarPath(ctx context.Context, userID int64, previous, next string) error
}

// FileUsecase 负责头像读取和替换，不生成用户 ID。
type FileUsecase struct {
	log         *log.Helper // 文件清理失败等非致命错误的日志。
	repo        FileRepo    // 用户及头像路径仓储。
	resourceDir string      // 服务端资源根目录，不接受客户端指定。
}

// NewFileUsecase 装配头像仓储和本地资源目录。
func NewFileUsecase(logger log.Logger, repo FileRepo) *FileUsecase {
	return &FileUsecase{log: log.NewHelper(logger), repo: repo, resourceDir: resourcePath}
}

// GetAvatar 按用户 ID 读取图片；空路径或文件丢失时回退默认头像，不存在的用户返回 404。
func (u *FileUsecase) GetAvatar(ctx context.Context, userID int64) (*filev1.AvatarResp, error) {
	if userID <= 0 {
		return nil, kerrors.BadRequest("INVALID_USER_ID", "用户 ID 无效")
	}
	path, err := u.repo.GetAvatarPath(ctx, userID)
	if err != nil {
		return nil, err
	}
	defaultPath := filepath.Join(u.resourceDir, "default", defaultAvatarName)
	if path == "" {
		path = defaultPath
	}
	data, err := u.readAvatar(userID, path)
	if errors.Is(err, os.ErrNotExist) {
		data, err = u.readAvatar(userID, defaultPath)
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil, kerrors.NotFound("AVATAR_NOT_FOUND", "头像及默认头像文件不存在")
	}
	if err != nil {
		return nil, err
	}
	contentType, _, err := validateAvatar(data)
	if err != nil {
		return nil, kerrors.InternalServer("INVALID_STORED_AVATAR", "头像文件损坏或格式不支持")
	}
	return &filev1.AvatarResp{Content: data, ContentType: contentType}, nil
}

// UpdateAvatar 要求首帧为可信用户元信息，后续仅含文件块；空流、空图片及重复元信息均拒绝。
// 新文件验证、写入并关闭后才更新数据库，失败仅清理本次文件，不覆盖默认或旧头像。
func (u *FileUsecase) UpdateAvatar(stream grpc.ClientStreamingServer[filev1.UpdateAvatarReq, emptypb.Empty]) error {
	first, err := stream.Recv()
	if err == io.EOF {
		return kerrors.BadRequest("INVALID_AVATAR_STREAM", "缺少头像元信息")
	}
	if err != nil {
		return err
	}
	metadata := first.GetMetaData()
	if metadata == nil || metadata.UserId <= 0 {
		return kerrors.BadRequest("INVALID_AVATAR_STREAM", "首帧必须为有效用户元信息")
	}
	ctx, userID := stream.Context(), metadata.UserId
	previous, err := u.repo.GetAvatarPath(ctx, userID)
	if err != nil {
		return err
	}
	var content bytes.Buffer
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		block, ok := req.GetData().(*filev1.UpdateAvatarReq_FileContent)
		if !ok {
			return kerrors.BadRequest("INVALID_AVATAR_STREAM", "元信息只允许出现在首帧")
		}
		if len(block.FileContent) > maxAvatarBytes-content.Len() {
			return kerrors.New(413, "AVATAR_TOO_LARGE", "头像不能超过 5 MiB")
		}
		content.Write(block.FileContent)
	}
	_, extension, err := validateAvatar(content.Bytes())
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	dir, err := u.avatarDirectory(userID)
	if err != nil {
		return err
	}
	// 唯一文件名不覆盖旧文件；数据库路径切换后，新文件才成为当前头像。
	file, err := os.CreateTemp(dir, "avatar-*"+extension)
	if err != nil {
		return err
	}
	path := file.Name()
	committed := false
	defer func() {
		_ = file.Close()
		if !committed {
			_ = os.Remove(path)
		}
	}()
	if _, err := file.Write(content.Bytes()); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := u.repo.UpdateAvatarPath(ctx, userID, previous, path); err != nil {
		return err
	}
	committed = true
	// 共享默认头像不可删除；只清理属于本用户、已验证范围的旧头像。
	if previous != "" {
		old, err := u.safeAvatarPath(userID, previous)
		defaultReal, _ := filepath.EvalSymlinks(filepath.Join(u.resourceDir, "default", defaultAvatarName))
		if err == nil && old != defaultReal && old != path {
			if err := os.Remove(old); err != nil {
				u.log.Warnf("清理旧头像失败: %v", err)
			}
		}
	}
	return stream.SendAndClose(&emptypb.Empty{})
}

// validateAvatar 验证 JPEG/PNG 的实际内容及解码尺寸，不信任文件名、扩展名或 MIME 声明。
func validateAvatar(content []byte) (string, string, error) {
	invalid := kerrors.BadRequest("INVALID_AVATAR", "头像必须是完整的 JPEG 或 PNG 图片，尺寸不能超过 4096")
	if len(content) == 0 {
		return "", "", invalid
	}
	if len(content) > maxAvatarBytes {
		return "", "", kerrors.New(413, "AVATAR_TOO_LARGE", "头像不能超过 5 MiB")
	}
	config, format, err := image.DecodeConfig(bytes.NewReader(content))
	if err != nil {
		return "", "", invalid
	}
	if config.Width <= 0 || config.Height <= 0 || config.Width > maxAvatarDimension || config.Height > maxAvatarDimension {
		return "", "", invalid
	}
	if format != "jpeg" && format != "png" {
		return "", "", invalid
	}
	if _, _, err := image.Decode(bytes.NewReader(content)); err != nil {
		return "", "", invalid
	}
	if format == "jpeg" {
		return "image/jpeg", ".jpg", nil
	}
	return "image/png", ".png", nil
}

// avatarDirectory 创建用户头像目录；符号链接不得将它重定向到其他位置。
func (u *FileUsecase) avatarDirectory(userID int64) (string, error) {
	if err := os.MkdirAll(u.resourceDir, 0755); err != nil {
		return "", err
	}
	root, err := filepath.Abs(u.resourceDir)
	if err != nil {
		return "", err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	expected := filepath.Join(root, strconv.FormatInt(userID, 10))
	if err := os.MkdirAll(expected, 0755); err != nil {
		return "", err
	}
	actual, err := filepath.EvalSymlinks(expected)
	if err != nil {
		return "", err
	}
	if actual != expected {
		return "", kerrors.InternalServer("INVALID_AVATAR_DIRECTORY", "头像目录不能重定向到其他位置")
	}
	return actual, nil
}

// safeAvatarPath 解析符号链接后仅允许本用户目录内的头像或固定默认头像，不接受任意资源路径。
func (u *FileUsecase) safeAvatarPath(userID int64, path string) (string, error) {
	root, err := filepath.Abs(u.resourceDir)
	if err != nil {
		return "", err
	}
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", err
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	actual, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", err
	}
	if actual == filepath.Join(root, "default", defaultAvatarName) {
		return actual, nil
	}
	if filepath.Dir(actual) != filepath.Join(root, strconv.FormatInt(userID, 10)) || !strings.HasPrefix(filepath.Base(actual), "avatar") {
		return "", kerrors.InternalServer("INVALID_AVATAR_PATH", "头像存储路径无效")
	}
	return actual, nil
}

// readAvatar 只读取范围校验通过的普通文件，并限制实际读取量。
func (u *FileUsecase) readAvatar(userID int64, path string) ([]byte, error) {
	resolved, err := u.safeAvatarPath(userID, path)
	if err != nil {
		return nil, err
	}
	file, err := os.Open(resolved)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, kerrors.InternalServer("INVALID_AVATAR_FILE", "头像不是普通文件")
	}
	return io.ReadAll(io.LimitReader(file, maxAvatarBytes+1))
}
