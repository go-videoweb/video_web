package userdata

import (
	"context"
	"github.com/go-kratos/kratos/v2/log"
	followv1 "vw_gateway/api/v1/user/follow"
	"vw_gateway/internal/biz/userbiz"
	userfollow "vw_user/api/v1/follow"
)

type followRepo struct{ data *Data }

// NewFollowRepo 构造关注 RPC 转发仓储，不创建额外服务客户端。
func NewFollowRepo(data *Data, _ log.Logger) userbiz.FollowRepo { return &followRepo{data: data} }

// FollowOtherUser 仅调用用户服务一次，旧 follower_user_id 字段实际表示被关注目标。
func (r *followRepo) FollowOtherUser(ctx context.Context, userID, targetID, listID int64) (*followv1.FollowUserResp, error) {
	resp, err := r.data.followClient.FollowUser(ctx, &userfollow.FollowUserReq{UserId: userID, FollowerUserId: targetID, FollowListId: listID})
	if err != nil {
		return nil, err
	}
	return &followv1.FollowUserResp{StatusCode: 200, Message: resp.Message, IsFollowing: resp.IsFollowing, FollowListId: resp.FollowListId}, nil
}

// UnfollowOtherUser 将可信操作人和目标传给用户服务。
func (r *followRepo) UnfollowOtherUser(ctx context.Context, userID, targetID int64) error {
	_, err := r.data.followClient.UnfollowUser(ctx, &userfollow.UnfollowUserReq{UserId: userID, FolloweeUserId: targetID})
	return err
}

// GetFolloweeInfo 保留每个被关注用户 ID、分组归属、分页总数及用户服务顺序。
func (r *followRepo) GetFolloweeInfo(ctx context.Context, userID, listID int64, page, size int32) (*followv1.GetFolloweeInfoResp, error) {
	resp, err := r.data.followClient.GetFolloweesInfo(ctx, &userfollow.GetFolloweesInfoReq{UserId: userID, FollowListId: listID, PageNum: page, PageSize: size})
	if err != nil {
		return nil, err
	}
	rows := make([]*followv1.GetFolloweeInfoResp_UserSummary, len(resp.FolloweesInfo))
	for i, row := range resp.FolloweesInfo {
		rows[i] = &followv1.GetFolloweeInfoResp_UserSummary{UserId: row.UserId, FollowListId: row.FollowListId, Username: row.Username, Email: row.Email, Signature: row.Signature, Gender: row.Gender, AvatarPath: row.AvatarPath, Birthday: row.Birthday}
	}
	return &followv1.GetFolloweeInfoResp{StatusCode: 200, Message: resp.Message, FolloweeInfo: rows, Total: resp.Total, PageNum: resp.PageNum, PageSize: resp.PageSize}, nil
}

// CreateFollowList 返回用户服务实际保存的列表 ID。
func (r *followRepo) CreateFollowList(ctx context.Context, userID int64, name string) (*followv1.CreateFollowListResp, error) {
	resp, err := r.data.followClient.CreateFollowList(ctx, &userfollow.CreateFollowListReq{UserId: userID, Name: name})
	if err != nil {
		return nil, err
	}
	return &followv1.CreateFollowListResp{FollowListId: resp.FollowListId}, nil
}

// DeleteFollowLists 一次转发整个删除集合，由用户服务保证全有或全无。
func (r *followRepo) DeleteFollowLists(ctx context.Context, userID int64, ids []int64) (*followv1.FollowListMutationResp, error) {
	resp, err := r.data.followClient.DeleteFollowLists(ctx, &userfollow.DeleteFollowListsReq{UserId: userID, IdList: ids})
	if err != nil {
		return nil, err
	}
	return &followv1.FollowListMutationResp{Success: resp.Success}, nil
}

// GetFollowLists 返回本人所有列表，空数组保持为非 nil。
func (r *followRepo) GetFollowLists(ctx context.Context, userID int64) (*followv1.GetFollowListsResp, error) {
	resp, err := r.data.followClient.GetFollowLists(ctx, &userfollow.GetFollowListsReq{UserId: userID})
	if err != nil {
		return nil, err
	}
	lists := make([]*followv1.FollowListInfo, len(resp.Lists))
	for i, row := range resp.Lists {
		lists[i] = &followv1.FollowListInfo{FollowListId: row.FollowListId, UserId: row.UserId, Name: row.Name, IsDefault: row.IsDefault}
	}
	return &followv1.GetFollowListsResp{Lists: lists}, nil
}

// ModifyFollowList 转发名称，不修改默认标记。
func (r *followRepo) ModifyFollowList(ctx context.Context, userID, listID int64, name string) (*followv1.FollowListMutationResp, error) {
	resp, err := r.data.followClient.ModifyFollowList(ctx, &userfollow.ModifyFollowListReq{UserId: userID, FollowListId: listID, Name: name})
	if err != nil {
		return nil, err
	}
	return &followv1.FollowListMutationResp{Success: resp.Success}, nil
}

// SetDefaultFollowList 仅调用原子默认切换 RPC。
func (r *followRepo) SetDefaultFollowList(ctx context.Context, userID, listID int64) (*followv1.FollowListMutationResp, error) {
	resp, err := r.data.followClient.SetDefaultFollowList(ctx, &userfollow.SetDefaultFollowListReq{UserId: userID, FollowListId: listID})
	if err != nil {
		return nil, err
	}
	return &followv1.FollowListMutationResp{Success: resp.Success}, nil
}

// GetFollowRelation 读取确定的关注状态及所属列表。
func (r *followRepo) GetFollowRelation(ctx context.Context, userID, targetID int64) (*followv1.GetFollowRelationResp, error) {
	resp, err := r.data.followClient.GetFollowRelation(ctx, &userfollow.GetFollowRelationReq{UserId: userID, TargetUserId: targetID})
	if err != nil {
		return nil, err
	}
	return &followv1.GetFollowRelationResp{IsFollowing: resp.IsFollowing, FollowListId: resp.FollowListId}, nil
}

// MoveFollowee 转发分组移动，不取消再重新关注。
func (r *followRepo) MoveFollowee(ctx context.Context, userID, targetID, listID int64) (*followv1.FollowListMutationResp, error) {
	resp, err := r.data.followClient.MoveFollowee(ctx, &userfollow.MoveFolloweeReq{UserId: userID, TargetUserId: targetID, FollowListId: listID})
	if err != nil {
		return nil, err
	}
	return &followv1.FollowListMutationResp{Success: resp.Success}, nil
}
