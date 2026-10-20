package favorv1_test

import (
	"testing"

	annotations "google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	favorv1 "vw_gateway/api/v1/user/favorites"
	userfavorv1 "vw_user/api/v1/favorites"
)

// TestFavoriteCreateWireCompatibility 验证两端创建响应保留原编号，旧响应可解码且缺失 ID 为零。
func TestFavoriteCreateWireCompatibility(t *testing.T) {
	for _, resp := range []proto.Message{&favorv1.CreateFavoritesResp{}, &userfavorv1.CreateFavoritesResp{}} {
		fields := resp.ProtoReflect().Descriptor().Fields()
		for name, number := range map[protoreflect.Name]protoreflect.FieldNumber{"status_code": 1, "msg": 2, "favorite_id": 3} {
			field := fields.ByName(name)
			if field == nil || field.Number() != number {
				t.Fatalf("响应字段 %s 编号发生变化", name)
			}
		}
		legacy := protowire.AppendTag(nil, 1, protowire.VarintType)
		legacy = protowire.AppendVarint(legacy, 200)
		legacy = protowire.AppendTag(legacy, 2, protowire.BytesType)
		legacy = protowire.AppendString(legacy, "success")
		if err := proto.Unmarshal(legacy, resp); err != nil {
			t.Fatal(err)
		}
		message := resp.ProtoReflect()
		if message.Get(fields.ByName("status_code")).Int() != 200 || message.Get(fields.ByName("msg")).String() != "success" || message.Get(fields.ByName("favorite_id")).Int() != 0 {
			t.Fatal("旧创建响应不兼容")
		}
	}
}

// TestFavoritesListContract 验证列表 RPC 使用正确消息及 GET 注解，保留用户新增字段编号。
func TestFavoritesListContract(t *testing.T) {
	for _, file := range []protoreflect.FileDescriptor{favorv1.File_v1_user_favorites_favorite_proto, userfavorv1.File_v1_favorites_user_favirites_proto} {
		method := file.Services().ByName("Favorite").Methods().ByName("GetFavoritesList")
		if method == nil || method.Input().Name() != "GetFavoritesListReq" || method.Output().Name() != "GetFavoritesListResp" {
			t.Fatal("列表 RPC 绑定错误")
		}
		if file == favorv1.File_v1_user_favorites_favorite_proto {
			rule := proto.GetExtension(method.Options(), annotations.E_Http).(*annotations.HttpRule)
			if rule.GetGet() != "/api/v1/favorites/list" || rule.GetDelete() != "" {
				t.Fatal("列表必须使用 GET")
			}
		}
		detail := file.Messages().ByName("FavoriteDetail").Fields()
		for name, number := range map[protoreflect.Name]protoreflect.FieldNumber{"favorite_id": 1, "favorite_name": 2, "description": 3, "is_private": 4} {
			if field := detail.ByName(name); field == nil || field.Number() != number {
				t.Fatalf("列表字段 %s 编号发生变化", name)
			}
		}
	}
}
