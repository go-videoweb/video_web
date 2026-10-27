package biz

import (
	interactv1 "vw_video/api/v1/interact"

	kerrors "github.com/go-kratos/kratos/v2/errors"
)

var (
	// ErrInvalidInteractionIdentity 表示请求缺少有效身份、视频 ID 或必需的状态信息。
	ErrInvalidInteractionIdentity = kerrors.BadRequest("INVALID_INTERACTION_IDENTITY", "用户和视频 ID 必须为正数")
	// ErrInteractionNotSupported 表示尚未确定数据模型的评论或弹幕点赞能力暂未启用。
	ErrInteractionNotSupported = kerrors.New(501, "INTERACTION_NOT_SUPPORTED", "暂未支持评论或弹幕点赞")
)

// StatusChange 记录单个互动字段的预期旧值和目标值，补偿时交换二者。
type StatusChange struct {
	Before int64
	After  int64
}

// VideoStatusPatch 仅包含本次操作涉及的字段，不覆盖收藏关系或观看时间。
type VideoStatusPatch struct {
	IsLike      *StatusChange
	IsShared    *StatusChange
	ShellsCount *StatusChange
}

// videoStatusPatch 验证 RPC 操作标记并构造局部变更，忽略兼容字段中的收藏副本。
// 输入：req 必须包含正用户/视频 ID 及状态信息；revert 表示交换本次操作的旧值和新值。
// 输出：未涉及的字段为 nil；身份、标记或贝壳数量非法时返回错误，不修改请求或持久化数据。
func videoStatusPatch(req *interactv1.SetUserVideoStatusReq, revert bool) (*VideoStatusPatch, error) {
	if req == nil || req.Statuses == nil || req.UserId <= 0 || req.VideoId <= 0 {
		return nil, ErrInvalidInteractionIdentity
	}
	s := req.Statuses
	statusSet := []int32{s.IsUpvoted, s.IsShared, s.IsFavorited, s.IsThrownShells}
	for _, flag := range statusSet {
		if flag < -1 || flag > 1 {
			return nil, kerrors.BadRequest("INVALID_INTERACTION_STATUS", "互动标记只能为 -1、0 或 1")
		}
	}
	patch := &VideoStatusPatch{
		IsLike:   statusFlagChange(req.OriginStatus, s.IsUpvoted, UpvoteStatus),
		IsShared: statusFlagChange(req.OriginStatus, s.IsShared, ShareStatus),
	}
	if s.IsThrownShells != 0 {
		if req.OriginShellsCount < 0 || req.OriginShellsCount > 2 || req.ShellsCount < 0 || req.ShellsCount > 2 {
			return nil, kerrors.BadRequest("INVALID_SHELLS_COUNT", "已投贝壳数量必须在 0 到 2 之间")
		}
		patch.ShellsCount = &StatusChange{Before: int64(req.OriginShellsCount), After: int64(req.ShellsCount)}
	}
	if revert {
		statusChangeSet := []*StatusChange{patch.IsLike, patch.IsShared, patch.ShellsCount}
		for _, value := range statusChangeSet {
			if value != nil {
				value.Before, value.After = value.After, value.Before
			}
		}
	}
	return patch, nil
}

// statusFlagChange 将一次布尔互动操作转换为数据库使用的 0/1 变更。
// 输入：originStatus 为原 RPC 位掩码；flag 须已验证为 -1、0 或 1；mask 为目标状态位。
// 输出：flag 为 0 时返回 nil，否则只提取目标位的旧值和操作要求的新值，不修改原位掩码。
func statusFlagChange(originStatus int64, flag int32, mask int64) *StatusChange {
	if flag == 0 {
		return nil
	}
	change := &StatusChange{}
	if originStatus&mask != 0 {
		change.Before = 1
	}
	if flag == 1 {
		change.After = 1
	}
	return change
}
