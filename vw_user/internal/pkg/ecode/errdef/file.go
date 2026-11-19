package errdef

import (
	kerr "github.com/go-kratos/kratos/v2/errors"
	"vw_user/internal/pkg/ecode"
)

func init() {
	ErrUploadAvatarFailed = kerr.New(ecode.FILE_UploadAvatarFailed, "上传文件失败", "服务器内部错误,请稍后再试")
	ErrUpdateAvatarFailed = kerr.New(ecode.FILE_UpdateAvatarFailed, "更新文件失败", "服务器内部错误,请稍后再试")
}

var (
	ErrUploadAvatarFailed *kerr.Error
	ErrUpdateAvatarFailed *kerr.Error
)

// ErrAvatarNotFound 表示头像及默认头像文件不存在。
var ErrAvatarNotFound = kerr.New(ecode.AvatarNotFound, "AVATAR_NOT_FOUND", "头像及默认头像文件不存在")

// ErrInvalidStoredAvatar 表示头像文件损坏或格式不支持。
var ErrInvalidStoredAvatar = kerr.New(ecode.InvalidStoredAvatar, "INVALID_STORED_AVATAR", "头像文件损坏或格式不支持")

// ErrInvalidAvatarStream 表示缺少头像元信息。
var ErrInvalidAvatarStream = kerr.New(ecode.InvalidAvatarStream, "INVALID_AVATAR_STREAM", "缺少头像元信息")

// ErrAvatarTooLarge 表示头像不能超过 5 MiB。
var ErrAvatarTooLarge = kerr.New(ecode.AvatarTooLarge, "AVATAR_TOO_LARGE", "头像不能超过 5 MiB")

// ErrInvalidAvatar 表示头像必须是完整的 JPEG 或 PNG 图片，尺寸不能超过 4096。
var ErrInvalidAvatar = kerr.New(ecode.InvalidAvatar, "INVALID_AVATAR", "头像必须是完整的 JPEG 或 PNG 图片，尺寸不能超过 4096")

// ErrInvalidAvatarDirectory 表示头像目录不能重定向到其他位置。
var ErrInvalidAvatarDirectory = kerr.New(ecode.InvalidAvatarDirectory, "INVALID_AVATAR_DIRECTORY", "头像目录不能重定向到其他位置")

// ErrInvalidAvatarPath 表示头像存储路径无效。
var ErrInvalidAvatarPath = kerr.New(ecode.InvalidAvatarPath, "INVALID_AVATAR_PATH", "头像存储路径无效")

// ErrInvalidAvatarFile 表示头像不是普通文件。
var ErrInvalidAvatarFile = kerr.New(ecode.InvalidAvatarFile, "INVALID_AVATAR_FILE", "头像不是普通文件")

// ErrAvatarChanged 表示用户或头像已变化，请重新查询后重试。
var ErrAvatarChanged = kerr.New(ecode.AvatarChanged, "AVATAR_CHANGED", "用户或头像已变化，请重新查询后重试")
