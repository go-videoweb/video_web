package userbiz

import (
	"context"
	"github.com/go-kratos/kratos/v2/log"
	followv1 "vw_gateway/api/v1/user/follow"
)

// FollowRepo 定义用户域 RPC 转发，不在网关组合关注、粉丝和计数业务。
type FollowRepo interface {
	FollowOtherUser(context.Context, int64, int64, int64) (*followv1.FollowUserResp, error)
	UnfollowOtherUser(context.Context, int64, int64) error
	GetFolloweeInfo(context.Context, int64, int64, int32, int32) (*followv1.GetFolloweeInfoResp, error)
	CreateFollowList(context.Context, int64, string) (*followv1.CreateFollowListResp, error)
	DeleteFollowLists(context.Context, int64, []int64) (*followv1.FollowListMutationResp, error)
	GetFollowLists(context.Context, int64) (*followv1.GetFollowListsResp, error)
	ModifyFollowList(context.Context, int64, int64, string) (*followv1.FollowListMutationResp, error)
	SetDefaultFollowList(context.Context, int64, int64) (*followv1.FollowListMutationResp, error)
	GetFollowRelation(context.Context, int64, int64) (*followv1.GetFollowRelationResp, error)
	MoveFollowee(context.Context, int64, int64, int64) (*followv1.FollowListMutationResp, error)
}

// FollowUsecase 只向用户服务转发经过身份确认的关注请求。
type FollowUsecase struct{ repo FollowRepo }

// NewFollowUsecase 构造关注转发用例。
func NewFollowUsecase(repo FollowRepo, _ log.Logger) *FollowUsecase {
	return &FollowUsecase{repo: repo}
}

// FollowOtherUser 转发明确的关注意图及真实列表 ID。
func (u *FollowUsecase) FollowOtherUser(ctx context.Context, userID, targetID, listID int64) (*followv1.FollowUserResp, error) {
	return u.repo.FollowOtherUser(ctx, userID, targetID, listID)
}

// UnfollowOtherUser 转发取消关注意图。
func (u *FollowUsecase) UnfollowOtherUser(ctx context.Context, userID, targetID int64) error {
	return u.repo.UnfollowOtherUser(ctx, userID, targetID)
}

// GetFolloweeInfo 转发本人分组或全部关注分页查询。
func (u *FollowUsecase) GetFolloweeInfo(ctx context.Context, userID, listID int64, page, size int32) (*followv1.GetFolloweeInfoResp, error) {
	return u.repo.GetFolloweeInfo(ctx, userID, listID, page, size)
}

// CreateFollowList 转发普通列表创建。
func (u *FollowUsecase) CreateFollowList(ctx context.Context, userID int64, name string) (*followv1.CreateFollowListResp, error) {
	return u.repo.CreateFollowList(ctx, userID, name)
}

// DeleteFollowLists 转发原子批量删除，不在网关拆成多个 RPC。
func (u *FollowUsecase) DeleteFollowLists(ctx context.Context, userID int64, ids []int64) (*followv1.FollowListMutationResp, error) {
	return u.repo.DeleteFollowLists(ctx, userID, ids)
}

// GetFollowLists 转发本人全部列表查询。
func (u *FollowUsecase) GetFollowLists(ctx context.Context, userID int64) (*followv1.GetFollowListsResp, error) {
	return u.repo.GetFollowLists(ctx, userID)
}

// ModifyFollowList 转发列表名称修改。
func (u *FollowUsecase) ModifyFollowList(ctx context.Context, userID, listID int64, name string) (*followv1.FollowListMutationResp, error) {
	return u.repo.ModifyFollowList(ctx, userID, listID, name)
}

// SetDefaultFollowList 转发默认切换。
func (u *FollowUsecase) SetDefaultFollowList(ctx context.Context, userID, listID int64) (*followv1.FollowListMutationResp, error) {
	return u.repo.SetDefaultFollowList(ctx, userID, listID)
}

// GetFollowRelation 转发当前用户对目标的关系读取。
func (u *FollowUsecase) GetFollowRelation(ctx context.Context, userID, targetID int64) (*followv1.GetFollowRelationResp, error) {
	return u.repo.GetFollowRelation(ctx, userID, targetID)
}

// MoveFollowee 转发关注分组移动。
func (u *FollowUsecase) MoveFollowee(ctx context.Context, userID, targetID, listID int64) (*followv1.FollowListMutationResp, error) {
	return u.repo.MoveFollowee(ctx, userID, targetID, listID)
}
