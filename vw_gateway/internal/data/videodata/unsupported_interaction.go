package videodata

import kerrors "github.com/go-kratos/kratos/v2/errors"

// interactionNotSupported 明确区分未启用的点赞功能与下游故障。
func interactionNotSupported() error {
	return kerrors.New(501, "INTERACTION_NOT_SUPPORTED", "暂未支持评论或弹幕点赞")
}
