package userdata

import (
	"context"
	"io"
	"testing"

	"github.com/go-kratos/kratos/v2/log"
	"google.golang.org/grpc"
	favorv1 "vw_gateway/api/v1/user/favorites"
	"vw_gateway/internal/biz/userbiz"
	user "vw_gateway/internal/service/user_service"
	userfavorv1 "vw_user/api/v1/favorites"
)

type favoriteOwnerClient struct {
	userfavorv1.FavoriteClient
	modified *userfavorv1.ModifyFavoritesReq
	deleted  *userfavorv1.DeleteFavoritesReq
}

func (c *favoriteOwnerClient) ModifyFavorites(_ context.Context, req *userfavorv1.ModifyFavoritesReq, _ ...grpc.CallOption) (*userfavorv1.ModifyFavoritesResp, error) {
	c.modified = req
	return &userfavorv1.ModifyFavoritesResp{StatusCode: 200}, nil
}
func (c *favoriteOwnerClient) DeleteFavorites(_ context.Context, req *userfavorv1.DeleteFavoritesReq, _ ...grpc.CallOption) (*userfavorv1.DeleteFavoritesResp, error) {
	c.deleted = req
	return &userfavorv1.DeleteFavoritesResp{StatusCode: 200}, nil
}

// TestFavoritesMutationIdentity 验证修改删除调用链使用鉴权身份，目标 ID 和可选零值属性不丢失。
func TestFavoritesMutationIdentity(t *testing.T) {
	client := &favoriteOwnerClient{}
	logger := log.NewStdLogger(io.Discard)
	svc := user.NewFavoritesService(userbiz.NewFavoritesUsecase(NewFavoritesRepo(&Data{favoritesClient: client}, client, logger), logger), logger)
	ctx := context.WithValue(context.Background(), "user_id", int64(7))
	description := ""
	privacy := int32(-1)
	if _, err := svc.ModifyFavorites(ctx, &favorv1.ModifyFavoritesReq{FavoritesId: 9, Description: &description, IsPrivate: &privacy}); err != nil {
		t.Fatal(err)
	}
	if client.modified == nil || client.modified.UserId != 7 || client.modified.FavoritesId != 9 || client.modified.Description == nil || *client.modified.Description != "" || *client.modified.IsPrivate != -1 {
		t.Fatal("修改参数或操作人错误")
	}
	if _, err := svc.DeleteFavorites(ctx, &favorv1.DeleteFavoritesReq{FavoritesId: 9}); err != nil {
		t.Fatal(err)
	}
	if client.deleted == nil || client.deleted.UserId != 7 || client.deleted.FavoritesId != 9 {
		t.Fatal("删除未携带操作人")
	}
	client.modified, client.deleted = nil, nil
	if resp, err := svc.ModifyFavorites(context.Background(), &favorv1.ModifyFavoritesReq{FavoritesId: 9}); err == nil || resp != nil || client.modified != nil {
		t.Fatal("匿名修改被放行")
	}
	if resp, err := svc.DeleteFavorites(context.Background(), &favorv1.DeleteFavoritesReq{FavoritesId: 9}); err == nil || resp != nil || client.deleted != nil {
		t.Fatal("匿名删除被放行")
	}
}
