package errdef

import (
	kerr "github.com/go-kratos/kratos/v2/errors"
	"vw_video/internal/pkg/ecode"
)

// ErrInvalidInteractionIdentity 表示用户和视频 ID 必须为正数。
var ErrInvalidInteractionIdentity = kerr.New(ecode.InvalidInteractionIdentity, "INVALID_INTERACTION_IDENTITY", "用户和视频 ID 必须为正数")

// ErrInteractionNotSupported 表示暂未支持评论或弹幕点赞。
var ErrInteractionNotSupported = kerr.New(ecode.InteractionNotSupported, "INTERACTION_NOT_SUPPORTED", "暂未支持评论或弹幕点赞")

// ErrInvalidInteractionStatus 表示互动标记只能为 -1、0 或 1。
var ErrInvalidInteractionStatus = kerr.New(ecode.InvalidInteractionStatus, "INVALID_INTERACTION_STATUS", "互动标记只能为 -1、0 或 1")

// ErrInvalidShellsCount 表示已投贝壳数量必须在 0 到 2 之间。
var ErrInvalidShellsCount = kerr.New(ecode.InvalidShellsCount, "INVALID_SHELLS_COUNT", "已投贝壳数量必须在 0 到 2 之间")

// ErrVideoChanged 表示视频已变化。
var ErrVideoChanged = kerr.New(ecode.VideoChanged, "VIDEO_CHANGED", "视频已变化")

// ErrInvalidVideoVersion 表示视频版本缺失。
var ErrInvalidVideoVersion = kerr.New(ecode.InvalidVideoVersion, "INVALID_VIDEO_VERSION", "视频版本缺失")

// ErrInteractionStateConflict 表示互动状态已变化，请重新查询后操作。
var ErrInteractionStateConflict = kerr.New(ecode.InteractionStateConflict, "INTERACTION_STATE_CONFLICT", "互动状态已变化，请重新查询后操作")
