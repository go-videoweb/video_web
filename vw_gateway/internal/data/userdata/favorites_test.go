package userdata

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"testing"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"google.golang.org/grpc"
	favorv1 "vw_gateway/api/v1/user/favorites"
	"vw_gateway/internal/biz/userbiz"
	"vw_gateway/internal/pkg/codecs"
	"vw_gateway/internal/pkg/ecode/errdef/uerr"
	user "vw_gateway/internal/service/user_service"
	userfavorv1 "vw_user/api/v1/favorites"
)

type favoriteClient struct {
	userfavorv1.FavoriteClient
	createResp  *userfavorv1.CreateFavoritesResp
	listResp    *userfavorv1.GetFavoritesListResp
	err         error
	createReq   *userfavorv1.CreateFavoritesReq
	listReq     *userfavorv1.GetFavoritesListReq
	createCalls int
	listCalls   int
}

func (c *favoriteClient) CreateFavorites(_ context.Context, req *userfavorv1.CreateFavoritesReq, _ ...grpc.CallOption) (*userfavorv1.CreateFavoritesResp, error) {
	c.createCalls++
	c.createReq = req
	return c.createResp, c.err
}

func (c *favoriteClient) GetFavoritesList(_ context.Context, req *userfavorv1.GetFavoritesListReq, _ ...grpc.CallOption) (*userfavorv1.GetFavoritesListResp, error) {
	c.listCalls++
	c.listReq = req
	return c.listResp, c.err
}

func favoriteService(client *favoriteClient) *user.FavoritesService {
	logger := log.NewStdLogger(io.Discard)
	repo := NewFavoritesRepo(&Data{favoritesClient: client}, client, logger)
	return user.NewFavoritesService(userbiz.NewFavoritesUsecase(repo, logger), logger)
}

// TestFavoriteCreateGatewayChain 验证完整网关调用链使用可信身份，返回已保存的大 ID，RPC 失败或旧响应不重试。
func TestFavoriteCreateGatewayChain(t *testing.T) {
	rpcErr := errors.New("RPC 失败")
	for _, tc := range []struct {
		name string
		resp *userfavorv1.CreateFavoritesResp
		err  error
	}{
		{name: "成功", resp: &userfavorv1.CreateFavoritesResp{FavoriteId: 9007199254740993}},
		{name: "旧服务响应", resp: &userfavorv1.CreateFavoritesResp{}},
		{name: "空响应"},
		{name: "负 ID", resp: &userfavorv1.CreateFavoritesResp{FavoriteId: -1}},
		{name: "RPC 失败", err: rpcErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &favoriteClient{createResp: tc.resp, err: tc.err}
			ctx := context.WithValue(context.Background(), "user_id", int64(7))
			resp, err := favoriteService(client).CreateFavorites(ctx, &favorv1.CreateFavoritesReq{
				UserId: 999, FavoritesName: "收藏夹", IsPrivate: -1, Description: "说明",
			})
			if client.createCalls != 1 || client.createReq.UserId != 7 || client.createReq.FavoritesName != "收藏夹" || client.createReq.IsPrivate != -1 || client.createReq.Description != "说明" {
				t.Fatal("创建身份、字段映射或调用次数错误")
			}
			if tc.err != nil {
				if !errors.Is(err, rpcErr) || resp != nil {
					t.Fatal("RPC 错误未原样返回")
				}
				return
			}
			if tc.resp.GetFavoriteId() <= 0 {
				if err == nil || resp != nil || kerrors.FromError(err).Reason != "FAVORITE_ID_MISSING" {
					t.Fatal("缺失 ID 应明确失败")
				}
				return
			}
			if err != nil || resp.FavoriteId != tc.resp.FavoriteId || resp.StatusCode != 200 {
				t.Fatalf("创建未返回保存的 ID：%v %v", resp, err)
			}
			writer := httptest.NewRecorder()
			req := httptest.NewRequest("POST", "/api/v1/favorites", nil)
			req.Header.Set("Accept", "application/json")
			if err := codecs.ResponseEncoder(writer, req, resp); err != nil {
				t.Fatal(err)
			}
			var body struct {
				FavoriteID string `json:"favoriteId"`
			}
			if err := json.Unmarshal(writer.Body.Bytes(), &body); err != nil || body.FavoriteID != "9007199254740993" {
				t.Fatal("JSON 收藏夹 ID 丢失精度")
			}
		})
	}
}

// TestFavoritesListGatewayChain 验证列表字段映射、当前用户隔离、空结果和 RPC 错误处理。
func TestFavoritesListGatewayChain(t *testing.T) {
	rpcErr := errors.New("查询 RPC 失败")
	for _, tc := range []struct {
		name string
		resp *userfavorv1.GetFavoritesListResp
		err  error
	}{
		{name: "完整列表", resp: &userfavorv1.GetFavoritesListResp{FavoriteList: []*userfavorv1.FavoriteDetail{
			{FavoriteId: 9007199254740993, FavoriteName: "私密", Description: "说明", IsPrivate: true},
			{FavoriteId: 8, FavoriteName: "公开", IsPrivate: false},
		}}},
		{name: "空列表", resp: &userfavorv1.GetFavoritesListResp{}},
		{name: "空响应"},
		{name: "RPC 失败", err: rpcErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &favoriteClient{listResp: tc.resp, err: tc.err}
			ctx := context.WithValue(context.Background(), "user_id", int64(7))
			resp, err := favoriteService(client).GetFavoritesList(ctx, &favorv1.GetFavoritesListReq{UserId: 999})
			if client.listCalls != 1 || client.listReq.UserId != 7 {
				t.Fatal("查询身份被参数伪造或发生重试")
			}
			if tc.err != nil || tc.resp == nil {
				if err == nil || resp != nil || (tc.err != nil && !errors.Is(err, rpcErr)) {
					t.Fatal("查询失败应返回错误")
				}
				return
			}
			if err != nil || resp.UserId != 7 || resp.FavoriteList == nil || len(resp.FavoriteList) != len(tc.resp.FavoriteList) {
				t.Fatalf("列表响应错误：%v %v", resp, err)
			}
			for i, favorite := range resp.FavoriteList {
				want := tc.resp.FavoriteList[i]
				if favorite.FavoriteId != want.FavoriteId || favorite.FavoriteName != want.FavoriteName || favorite.Description != want.Description || favorite.IsPrivate != want.IsPrivate {
					t.Fatal("列表字段映射错误")
				}
			}
		})
	}
}

// TestFavoritesRequireTrustedIdentity 验证直接调用服务时也不能以请求 user_id 绕过身份要求。
func TestFavoritesRequireTrustedIdentity(t *testing.T) {
	for _, identity := range []interface{}{nil, "7", int64(0), int64(-1)} {
		ctx := context.Background()
		if identity != nil {
			ctx = context.WithValue(ctx, "user_id", identity)
		}
		client := &favoriteClient{}
		svc := favoriteService(client)
		if resp, err := svc.CreateFavorites(ctx, &favorv1.CreateFavoritesReq{UserId: 7}); resp != nil || !errors.Is(err, uerr.ErrTokenInvalid) {
			t.Fatal("缺少可信身份仍可创建")
		}
		if resp, err := svc.GetFavoritesList(ctx, &favorv1.GetFavoritesListReq{UserId: 7}); resp != nil || !errors.Is(err, uerr.ErrTokenInvalid) {
			t.Fatal("缺少可信身份仍可查询")
		}
		if client.createCalls != 0 || client.listCalls != 0 {
			t.Fatal("非法身份触发下游调用")
		}
	}
}
