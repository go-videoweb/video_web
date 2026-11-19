package errdef

import (
	kerr "github.com/go-kratos/kratos/v2/errors"
	"vw_user/internal/pkg/ecode"
)

func init() {
	ErrFollowOtherUserFailed = kerr.New(ecode.FOLLOW_FollowOtherUserFailed, "关注用户失败", "关注用户失败，请稍后再试")
	ErrUnfollowOtherUserFailed = kerr.New(ecode.FOLLOW_UnfollowOtherUserFailed, "取消关注用户失败", "取消关注用户失败，请稍后再试")
}

var (
	ErrFollowOtherUserFailed   *kerr.Error
	ErrUnfollowOtherUserFailed *kerr.Error
)

// ErrInvalidFollowListName 表示关注列表名称需为 1 至 20 个字符。
var ErrInvalidFollowListName = kerr.New(ecode.InvalidFollowListName, "INVALID_FOLLOW_LIST_NAME", "关注列表名称需为 1 至 20 个字符")

// ErrInvalidFollowList 表示关注列表归属异常。
var ErrInvalidFollowList = kerr.New(ecode.InvalidFollowList, "INVALID_FOLLOW_LIST", "关注列表归属异常")

// ErrDefaultFollowListConflict 表示默认关注列表必须恰好一个，请先核对数据。
var ErrDefaultFollowListConflict = kerr.New(ecode.DefaultFollowListConflict, "DEFAULT_FOLLOW_LIST_CONFLICT", "默认关注列表必须恰好一个，请先核对数据")

// ErrFollowListNotFound 表示关注列表不存在。
var ErrFollowListNotFound = kerr.New(ecode.FollowListNotFound, "FOLLOW_LIST_NOT_FOUND", "关注列表不存在")

// ErrFollowListNameExists 表示关注列表名称已存在。
var ErrFollowListNameExists = kerr.New(ecode.FollowListNameExists, "FOLLOW_LIST_NAME_EXISTS", "关注列表名称已存在")

// ErrInvalidFollowListID 表示关注列表 ID 必须为正数。
var ErrInvalidFollowListID = kerr.New(ecode.InvalidFollowListID, "INVALID_FOLLOW_LIST", "关注列表 ID 必须为正数")

// ErrInvalidFollowListIDs 表示一次需删除 1 至 100 个关注列表。
var ErrInvalidFollowListIDs = kerr.New(ecode.InvalidFollowListIDs, "INVALID_FOLLOW_LIST_IDS", "一次需删除 1 至 100 个关注列表")

// ErrDefaultFollowListDelete 表示默认关注列表不可删除。
var ErrDefaultFollowListDelete = kerr.New(ecode.DefaultFollowListDelete, "DEFAULT_FOLLOW_LIST_DELETE", "默认关注列表不可删除")

// ErrInvalidFollowTarget 表示不能关注自己，目标用户和分组 ID 必须有效。
var ErrInvalidFollowTarget = kerr.New(ecode.InvalidFollowTarget, "INVALID_FOLLOW_TARGET", "不能关注自己，目标用户和分组 ID 必须有效")

// ErrFollowGroupConflict 表示已在其他分组关注，请使用分组移动接口。
var ErrFollowGroupConflict = kerr.New(ecode.FollowGroupConflict, "FOLLOW_GROUP_CONFLICT", "已在其他分组关注，请使用分组移动接口")

// ErrFollowNotFound 表示尚未关注该用户。
var ErrFollowNotFound = kerr.New(ecode.FollowNotFound, "FOLLOW_NOT_FOUND", "尚未关注该用户")

// ErrInvalidFollowPage 表示分组和分页参数不可为负数。
var ErrInvalidFollowPage = kerr.New(ecode.InvalidFollowPage, "INVALID_FOLLOW_PAGE", "分组和分页参数不可为负数")

// ErrFollowConflict 表示关注列表名称或关系已存在。
var ErrFollowConflict = kerr.New(ecode.FollowConflict, "FOLLOW_CONFLICT", "关注列表名称或关系已存在")

// ErrFollowWriteConflict 表示关注数据已变化，请刷新后重试。
var ErrFollowWriteConflict = kerr.New(ecode.FollowWriteConflict, "FOLLOW_WRITE_CONFLICT", "关注数据已变化，请刷新后重试")

// ErrFansCountOverflow 表示粉丝数量超过当前 RPC int32 范围。
var ErrFansCountOverflow = kerr.New(ecode.FansCountOverflow, "FANS_COUNT_OVERFLOW", "粉丝数量超过当前 RPC int32 范围")
