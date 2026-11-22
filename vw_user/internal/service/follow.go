package service

import (
	"context"
	followv1 "vw_user/api/v1/follow"
	"vw_user/internal/biz"

	"github.com/go-kratos/kratos/v2/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/protobuf/types/known/timestamppb"
)

type FollowService struct {
	followv1.UnimplementedFollowServer
	followUsecase *biz.FollowUsecase
	log           *log.Helper
}

func NewFollowService(followUsecase *biz.FollowUsecase, logger log.Logger) *FollowService {
	return &FollowService{
		followUsecase: followUsecase,
		log:           log.NewHelper(logger),
	}
}

// FollowUser 由用户域统一执行关注、粉丝记录和计数写入，返回提交后的关系状态。
func (s *FollowService) FollowUser(ctx context.Context, req *followv1.FollowUserReq) (*followv1.FollowUserResp, error) {
	state, err := s.followUsecase.Follow(ctx, req.UserId, req.FollowerUserId, req.FollowListId)
	if err != nil {
		return nil, err
	}
	return &followv1.FollowUserResp{
		StatusCode:   int32(codes.OK),
		Message:      "关注成功",
		IsFollowing:  state.IsFollowing,
		FollowListId: state.FollowListID,
	}, err
}

// UnfollowUser 解除双向关系，无有效关注时幂等成功。
func (s *FollowService) UnfollowUser(ctx context.Context, req *followv1.UnfollowUserReq) (*followv1.UnfollowUserResp, error) {
	err := s.followUsecase.Unfollow(ctx, req.UserId, req.FolloweeUserId)
	if err != nil {
		return nil, err
	}
	return &followv1.UnfollowUserResp{
		StatusCode: int32(codes.OK),
		Message:    "取消关注成功",
	}, err
}

// GetFolloweesInfo 返回被关注用户 ID、分组及稳定分页结果，列表 ID 为零时查询全部关注。
func (s *FollowService) GetFolloweesInfo(ctx context.Context, req *followv1.GetFolloweesInfoReq) (*followv1.GetFolloweesInfoResp, error) {
	userInfo, err := s.followUsecase.GetFolloweesInfo(ctx, req.UserId, req.FollowListId, req.PageNum, req.PageSize)
	if err != nil {
		return nil, err
	}

	userSummaries := make([]*followv1.GetFolloweesInfoResp_UserSummary, len(userInfo.Users))
	for i, u := range userInfo.Users {
		userSummaries[i] = &followv1.GetFolloweesInfoResp_UserSummary{
			UserId:       u.Id,
			FollowListId: u.FollowListID,
			Username:     u.Username,
			Email:        u.Email,
			Signature:    u.Signature,
			AvatarPath:   u.AvatarPath,
			Gender:       u.Gender,
			Birthday:     timestamppb.New(u.Birthday),
		}
	}

	return &followv1.GetFolloweesInfoResp{
		StatusCode:    int32(codes.OK),
		Message:       "获取关注列表成功",
		FolloweesInfo: userSummaries,
		Total:         userInfo.Total, PageNum: userInfo.PageNum, PageSize: userInfo.PageSize,
	}, nil
}

// CreateFollowList 创建普通列表并返回已保存 ID。
func (s *FollowService) CreateFollowList(ctx context.Context, req *followv1.CreateFollowListReq) (*followv1.CreateFollowListResp, error) {
	id, err := s.followUsecase.CreateFollowList(ctx, req.UserId, req.Name)
	if err != nil {
		return nil, err
	}
	return &followv1.CreateFollowListResp{FollowListId: id}, nil
}

// DeleteFollowLists 全部删除成功后才返回 success，任一错误由本地事务回滚。
func (s *FollowService) DeleteFollowLists(ctx context.Context, req *followv1.DeleteFollowListsReq) (*followv1.FollowListMutationResp, error) {
	if err := s.followUsecase.DeleteFollowLists(ctx, req.UserId, req.IdList); err != nil {
		return nil, err
	}
	return &followv1.FollowListMutationResp{Success: true}, nil
}

// GetFollowLists 返回全部本人列表及默认标记。
func (s *FollowService) GetFollowLists(ctx context.Context, req *followv1.GetFollowListsReq) (*followv1.GetFollowListsResp, error) {
	rows, err := s.followUsecase.GetFollowLists(ctx, req.UserId)
	if err != nil {
		return nil, err
	}
	lists := make([]*followv1.FollowListInfo, len(rows))
	for i, row := range rows {
		lists[i] = &followv1.FollowListInfo{FollowListId: row.ListID, UserId: row.UserID, Name: row.ListName, IsDefault: row.IsDefault}
	}
	return &followv1.GetFollowListsResp{Lists: lists}, nil
}

// ModifyFollowList 只修改指定列表的名称。
func (s *FollowService) ModifyFollowList(ctx context.Context, req *followv1.ModifyFollowListReq) (*followv1.FollowListMutationResp, error) {
	if err := s.followUsecase.ModifyFollowList(ctx, req.UserId, req.FollowListId, req.Name); err != nil {
		return nil, err
	}
	return &followv1.FollowListMutationResp{Success: true}, nil
}

// SetDefaultFollowList 原子切换默认列表，不改变已有关注所属分组。
func (s *FollowService) SetDefaultFollowList(ctx context.Context, req *followv1.SetDefaultFollowListReq) (*followv1.FollowListMutationResp, error) {
	if err := s.followUsecase.SetDefaultFollowList(ctx, req.UserId, req.FollowListId); err != nil {
		return nil, err
	}
	return &followv1.FollowListMutationResp{Success: true}, nil
}

// GetFollowRelation 返回当前用户和目标用户的关注状态及列表归属。
func (s *FollowService) GetFollowRelation(ctx context.Context, req *followv1.GetFollowRelationReq) (*followv1.GetFollowRelationResp, error) {
	state, err := s.followUsecase.GetFollowRelation(ctx, req.UserId, req.TargetUserId)
	if err != nil {
		return nil, err
	}
	return &followv1.GetFollowRelationResp{IsFollowing: state.IsFollowing, FollowListId: state.FollowListID}, nil
}

// MoveFollowee 移动已存在的关注关系，不创建新关注。
func (s *FollowService) MoveFollowee(ctx context.Context, req *followv1.MoveFolloweeReq) (*followv1.FollowListMutationResp, error) {
	if err := s.followUsecase.MoveFollowee(ctx, req.UserId, req.TargetUserId, req.FollowListId); err != nil {
		return nil, err
	}
	return &followv1.FollowListMutationResp{Success: true}, nil
}
