package userdata

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"testing"

	"github.com/go-kratos/kratos/v2/log"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
	followv1 "vw_gateway/api/v1/user/follow"
	"vw_gateway/internal/biz/userbiz"
	user "vw_gateway/internal/service/user_service"
	userfollowv1 "vw_user/api/v1/follow"
)

type followSummaryClient struct {
	userfollowv1.FollowClient
	request context.Context
	rows    []*userfollowv1.GetFolloweesInfoResp_UserSummary
	err     error
	t       *testing.T
}

// GetFolloweesInfo 模拟用户 RPC，不连接真实服务，并校验分页及目标列表参数。
func (c *followSummaryClient) GetFolloweesInfo(ctx context.Context, req *userfollowv1.GetFolloweesInfoReq, _ ...grpc.CallOption) (*userfollowv1.GetFolloweesInfoResp, error) {
	if ctx != c.request || req.UserId != 7 || req.FollowListId != 13 || req.PageNum != 2 || req.PageSize != 20 {
		c.t.Fatal("网关修改了关注摘要查询参数")
	}
	if c.err != nil {
		return nil, c.err
	}
	return &userfollowv1.GetFolloweesInfoResp{FolloweesInfo: c.rows, Total: int64(len(c.rows)), PageNum: 2, PageSize: 20}, nil
}

// TestFollowSummaryUserID 验证下游摘要经网关领域模型和响应后仍携带用户 ID，且大 ID 以 JSON 字符串输出。
func TestFollowSummaryUserID(t *testing.T) {
	const userID = int64(9007199254740993)
	fault := errors.New("用户 RPC 失败")
	for _, mode := range []string{"正常", "空页", "故障"} {
		t.Run(mode, func(t *testing.T) {
			ctx := context.WithValue(context.Background(), "user_id", int64(7))
			client := &followSummaryClient{request: ctx, t: t}
			if mode == "正常" {
				client.rows = []*userfollowv1.GetFolloweesInfoResp_UserSummary{{UserId: userID, Username: "followee", FollowListId: 13}, {UserId: userID + 1, Username: "second", FollowListId: 14}}
			}
			if mode == "故障" {
				client.err = fault
			}
			logger := log.NewStdLogger(io.Discard)
			svc := user.NewFollowService(logger, userbiz.NewFollowUsecase(NewFollowRepo(&Data{followClient: client}, logger), logger))
			resp, err := svc.GetFolloweeInfo(ctx, &followv1.GetFolloweeInfoReq{UserId: 999, FollowListId: 13, PageNum: 2, PageSize: 20})
			if mode == "故障" {
				if !errors.Is(err, fault) || resp != nil {
					t.Fatalf("下游错误未透传：%v %v", resp, err)
				}
				return
			}
			if err != nil || resp == nil || resp.FolloweeInfo == nil {
				t.Fatalf("摘要响应异常：%v %v", resp, err)
			}
			if mode == "空页" {
				if len(resp.FolloweeInfo) != 0 {
					t.Fatal("空页返回了摘要")
				}
				return
			}
			if len(resp.FolloweeInfo) != 2 || resp.FolloweeInfo[0].UserId != userID || resp.FolloweeInfo[1].UserId != userID+1 || resp.FolloweeInfo[0].Username != "followee" || resp.FolloweeInfo[0].FollowListId != 13 || resp.Total != 2 || resp.PageNum != 2 || resp.PageSize != 20 {
				t.Fatalf("网关丢失用户身份或摘要顺序：%v", resp)
			}
			encoded, err := protojson.Marshal(resp)
			if err != nil {
				t.Fatal(err)
			}
			var decoded struct {
				FolloweeInfo []struct {
					UserID string `json:"userId"`
				} `json:"followeeInfo"`
			}
			if err := json.Unmarshal(encoded, &decoded); err != nil {
				t.Fatal(err)
			}
			if len(decoded.FolloweeInfo) != 2 || decoded.FolloweeInfo[0].UserID != "9007199254740993" || decoded.FolloweeInfo[1].UserID != "9007199254740994" {
				t.Fatalf("大 ID JSON 编码不正确：%s", encoded)
			}
		})
	}
	fields := (&followv1.GetFolloweeInfoResp_UserSummary{}).ProtoReflect().Descriptor().Fields()
	for name, number := range map[protoreflect.Name]protoreflect.FieldNumber{"username": 1, "avatar_path": 2, "gender": 3, "signature": 4, "email": 5, "birthday": 6, "user_id": 7, "follow_list_id": 8} {
		if field := fields.ByName(name); field == nil || field.Number() != number {
			t.Fatalf("网关摘要协议字段编号错误：%s", name)
		}
	}
}
