package user

import (
	"context"
	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	followv1 "vw_gateway/api/v1/user/follow"
	"vw_gateway/internal/biz/userbiz"
)

// FollowService 只确认可信操作人和协议参数，业务判断全部留在用户服务。
type FollowService struct {
	followv1.UnimplementedFollowServer
	followUsecase *userbiz.FollowUsecase
}

// NewFollowService 构造关注 HTTP 服务。
func NewFollowService(_ log.Logger, usecase *userbiz.FollowUsecase) *FollowService {
	return &FollowService{followUsecase: usecase}
}

// followActor 读取鉴权身份，不信任 URL、查询参数或请求体中的操作人 ID。
func followActor(ctx context.Context) (int64, error) {
	id, ok := ctx.Value(userIdKey).(int64)
	if !ok || id <= 0 {
		return 0, kerrors.Unauthorized("UNAUTHORIZED", "请先登录")
	}
	return id, nil
}

// FollowUser 转发关注意图，保留旧字段名但将 follower_user_id 明确作为目标用户。
func (s *FollowService) FollowUser(ctx context.Context, req *followv1.FollowUserReq) (*followv1.FollowUserResp, error) {
	id, err := followActor(ctx)
	if err != nil {
		return nil, err
	}
	return s.followUsecase.FollowOtherUser(ctx, id, req.FollowerUserId, req.FollowListId)
}

// UnfollowUser 只允许当前登录用户取消自己的关注。
func (s *FollowService) UnfollowUser(ctx context.Context, req *followv1.UnfollowUserReq) (*followv1.UnfollowUserResp, error) {
	id, err := followActor(ctx)
	if err != nil {
		return nil, err
	}
	if err := s.followUsecase.UnfollowOtherUser(ctx, id, req.FollowedUserId); err != nil {
		return nil, err
	}
	return &followv1.UnfollowUserResp{StatusCode: 200, Message: "取消关注成功"}, nil
}

// GetFolloweeInfo 查询本人分组或全部关注，旧路径 user_id 不作为身份来源。
func (s *FollowService) GetFolloweeInfo(ctx context.Context, req *followv1.GetFolloweeInfoReq) (*followv1.GetFolloweeInfoResp, error) {
	id, err := followActor(ctx)
	if err != nil {
		return nil, err
	}
	return s.followUsecase.GetFolloweeInfo(ctx, id, req.FollowListId, req.PageNum, req.PageSize)
}

// CreateFollowList 为当前登录用户创建普通列表。
func (s *FollowService) CreateFollowList(ctx context.Context, req *followv1.CreateFollowListReq) (*followv1.CreateFollowListResp, error) {
	id, err := followActor(ctx)
	if err != nil {
		return nil, err
	}
	return s.followUsecase.CreateFollowList(ctx, id, req.Name)
}

// DeleteFollowLists 转发批量删除，失败由用户服务回滚。
func (s *FollowService) DeleteFollowLists(ctx context.Context, req *followv1.DeleteFollowListsReq) (*followv1.FollowListMutationResp, error) {
	id, err := followActor(ctx)
	if err != nil {
		return nil, err
	}
	return s.followUsecase.DeleteFollowLists(ctx, id, req.IdList)
}

// GetFollowLists 只返回当前登录用户的列表。
func (s *FollowService) GetFollowLists(ctx context.Context, _ *followv1.GetFollowListsReq) (*followv1.GetFollowListsResp, error) {
	id, err := followActor(ctx)
	if err != nil {
		return nil, err
	}
	return s.followUsecase.GetFollowLists(ctx, id)
}

// ModifyFollowList 修改当前用户的列表名称。
func (s *FollowService) ModifyFollowList(ctx context.Context, req *followv1.ModifyFollowListReq) (*followv1.FollowListMutationResp, error) {
	id, err := followActor(ctx)
	if err != nil {
		return nil, err
	}
	return s.followUsecase.ModifyFollowList(ctx, id, req.FollowListId, req.Name)
}

// SetDefaultFollowList 请求用户服务原子切换默认分组。
func (s *FollowService) SetDefaultFollowList(ctx context.Context, req *followv1.SetDefaultFollowListReq) (*followv1.FollowListMutationResp, error) {
	id, err := followActor(ctx)
	if err != nil {
		return nil, err
	}
	return s.followUsecase.SetDefaultFollowList(ctx, id, req.FollowListId)
}

// GetFollowRelation 查询当前用户对目标的关注状态。
func (s *FollowService) GetFollowRelation(ctx context.Context, req *followv1.GetFollowRelationReq) (*followv1.GetFollowRelationResp, error) {
	id, err := followActor(ctx)
	if err != nil {
		return nil, err
	}
	return s.followUsecase.GetFollowRelation(ctx, id, req.TargetUserId)
}

// MoveFollowee 修改当前用户已有关注的分组。
func (s *FollowService) MoveFollowee(ctx context.Context, req *followv1.MoveFolloweeReq) (*followv1.FollowListMutationResp, error) {
	id, err := followActor(ctx)
	if err != nil {
		return nil, err
	}
	return s.followUsecase.MoveFollowee(ctx, id, req.TargetUserId, req.FollowListId)
}
