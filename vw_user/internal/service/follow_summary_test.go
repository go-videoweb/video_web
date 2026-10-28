package service

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	followv1 "vw_user/api/v1/follow"
	"vw_user/internal/biz"
	"vw_user/internal/data/dal/model"
	"vw_user/internal/domain"
)

type followSummaryRepo struct {
	biz.FollowRepo
	request context.Context
	rows    []*domain.UserSummary
	err     error
	t       *testing.T
}

// GetFolloweesPage 模拟关注摘要查询，验证查询参数和上下文完整透传。
func (r *followSummaryRepo) GetFolloweesPage(ctx context.Context, userID, listID int64, pageNum, pageSize int32) (*biz.FollowPage, error) {
	if ctx != r.request || userID != 7 || listID != 13 || pageNum != 2 || pageSize != 20 {
		r.t.Fatal("关注摘要查询参数发生变化")
	}
	return &biz.FollowPage{Users: r.rows, Total: int64(len(r.rows)), PageNum: pageNum, PageSize: pageSize}, r.err
}

// GetFollowLists 提供本人默认列表，使分页测试覆盖真实分组权限入口。
func (r *followSummaryRepo) GetFollowLists(context.Context, int64) ([]*model.FollowList, error) {
	return []*model.FollowList{{ListID: 13, UserID: 7, IsDefault: true}}, nil
}

// TestFollowSummaryUserID 验证模型、领域摘要、RPC 映射和 protobuf 编解码均保留被关注用户 ID。
func TestFollowSummaryUserID(t *testing.T) {
	const userID = int64(9007199254740993)
	fault := errors.New("摘要查询失败")
	for _, mode := range []string{"正常", "空页", "故障"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.Background()
			repo := &followSummaryRepo{request: ctx, t: t}
			if mode == "正常" {
				repo.rows = domain.NewUserSummaries(&model.User{UserID: userID, Username: "followee", Birthday: time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)})
				repo.rows[0].FollowListID = 13
			}
			if mode == "故障" {
				repo.err = fault
			}
			logger := log.NewStdLogger(io.Discard)
			svc := NewFollowService(biz.NewFollowUseCase(repo, nil, nil, logger), logger)
			resp, err := svc.GetFolloweesInfo(ctx, &followv1.GetFolloweesInfoReq{UserId: 7, FollowListId: 13, PageNum: 2, PageSize: 20})
			if mode == "故障" {
				if !errors.Is(err, fault) || resp != nil {
					t.Fatalf("查询故障未透传：%v %v", resp, err)
				}
				return
			}
			if err != nil || resp == nil || resp.FolloweesInfo == nil {
				t.Fatalf("摘要响应异常：%v %v", resp, err)
			}
			if mode == "空页" {
				if len(resp.FolloweesInfo) != 0 {
					t.Fatal("空页返回了摘要")
				}
				return
			}
			encoded, err := proto.Marshal(resp)
			if err != nil {
				t.Fatal(err)
			}
			decoded := new(followv1.GetFolloweesInfoResp)
			if err := proto.Unmarshal(encoded, decoded); err != nil {
				t.Fatal(err)
			}
			if len(decoded.FolloweesInfo) != 1 || decoded.FolloweesInfo[0].UserId != userID || decoded.FolloweesInfo[0].Username != "followee" || decoded.FolloweesInfo[0].FollowListId != 13 || decoded.Total != 1 || decoded.PageNum != 2 || decoded.PageSize != 20 {
				t.Fatalf("被关注用户身份丢失：%v", decoded)
			}
		})
	}
	fields := (&followv1.GetFolloweesInfoResp_UserSummary{}).ProtoReflect().Descriptor().Fields()
	for name, number := range map[protoreflect.Name]protoreflect.FieldNumber{"username": 1, "email": 2, "signature": 3, "avatar_path": 4, "gender": 5, "birthday": 6, "user_id": 7, "follow_list_id": 8} {
		if field := fields.ByName(name); field == nil || field.Number() != number {
			t.Fatalf("用户摘要协议字段编号错误：%s", name)
		}
	}
}
