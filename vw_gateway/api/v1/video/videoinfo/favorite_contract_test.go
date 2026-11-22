package videoinfo_test

import (
	"testing"

	annotations "google.golang.org/genproto/googleapis/api/annotations"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	videov1 "vw_gateway/api/v1/video/videoinfo"
	userfavorv1 "vw_user/api/v1/favorites"
	videov2 "vw_video/api/v1/videoinfo"
)

// TestFavoriteProtocolAlignment 验证个人状态和分页摘要两端字段一致，新增字段未复用旧编号。
func TestFavoriteProtocolAlignment(t *testing.T) {
	for _, message := range []proto.Message{&videov1.UserVideoStatus{}, &videov2.GetVideoInfoResp_UserVideoStatus{}} {
		fields := message.ProtoReflect().Descriptor().Fields()
		for name, number := range map[protoreflect.Name]protoreflect.FieldNumber{"is_upvoted": 1, "is_favorited": 2, "is_shared": 3, "is_thrown_shells": 4, "favorite_id": 5} {
			if field := fields.ByName(name); field == nil || field.Number() != number {
				t.Fatalf("状态字段不一致：%s", name)
			}
		}
	}
	for _, message := range []proto.Message{&videov1.GetFavoriteVideosResp{}, &videov2.GetFavoriteVideosResp{}} {
		fields := message.ProtoReflect().Descriptor().Fields()
		for name, number := range map[protoreflect.Name]protoreflect.FieldNumber{"videos": 1, "total": 2, "page_num": 3, "page_size": 4} {
			if field := fields.ByName(name); field == nil || field.Number() != number {
				t.Fatalf("分页字段不一致：%s", name)
			}
		}
		if fields.ByName("videos").Message().Fields().ByName("video").Message().Name() != "VideoSummary" {
			t.Fatal("分页未复用 VideoSummary")
		}
	}
	if (&userfavorv1.ModifyFavoritesReq{}).ProtoReflect().Descriptor().Fields().ByName("user_id").Number() != 5 || (&userfavorv1.DeleteFavoritesReq{}).ProtoReflect().Descriptor().Fields().ByName("user_id").Number() != 2 {
		t.Fatal("操作人字段编号错误")
	}
	method := videov1.File_v1_video_videoinfo_videoinfo_proto.Services().ByName("VideoInfo").Methods().ByName("GetFavoriteVideos")
	rule := proto.GetExtension(method.Options(), annotations.E_Http).(*annotations.HttpRule)
	if rule.GetGet() != "/api/v1/favorites/{favorites_id}/videos" {
		t.Fatal("分页 HTTP 路由错误")
	}
}
