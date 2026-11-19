package errdef

import (
	kerr "github.com/go-kratos/kratos/v2/errors"
	"vw_gateway/internal/pkg/ecode"
)

// ErrAvatarRequired 表示请提供 avatar 文件。
var ErrAvatarRequired = kerr.New(ecode.AvatarRequired, "AVATAR_REQUIRED", "请提供 avatar 文件")

// ErrInvalidAvatar 表示头像不能为空。
var ErrInvalidAvatar = kerr.New(ecode.InvalidAvatar, "INVALID_AVATAR", "头像不能为空")

// ErrAvatarTooLarge 表示头像不能超过 5 MiB。
var ErrAvatarTooLarge = kerr.New(ecode.AvatarTooLarge, "AVATAR_TOO_LARGE", "头像不能超过 5 MiB")

// ErrInvalidAvatarResponse 表示用户服务返回的头像无效。
var ErrInvalidAvatarResponse = kerr.New(ecode.InvalidAvatarResponse, "INVALID_AVATAR_RESPONSE", "用户服务返回的头像无效")
