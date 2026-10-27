package interact

import (
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// TestFavoriteUserIdentityContract 验证明确操作、用户与记录 ID 的协议编号及大 ID 无损传输。
func TestFavoriteUserIdentityContract(t *testing.T) {
	req := &SetVideoFavoriteReq{UserId: 9007199254740993, VideoId: 42, FavoriteId: 91, Favorite: -1, FavoriteRecordId: 9007199254740994}
	fields := req.ProtoReflect().Descriptor().Fields()
	for i, name := range []protoreflect.Name{"video_id", "favorite_id", "favorite", "user_id", "favorite_record_id"} {
		field := fields.ByName(name)
		if field == nil || field.Number() != protoreflect.FieldNumber(i+1) {
			t.Fatalf("field number changed: %s", name)
		}
	}
	data, err := proto.Marshal(req)
	if err != nil {
		t.Fatal(err)
	}
	var got SetVideoFavoriteReq
	if err := proto.Unmarshal(data, &got); err != nil || !proto.Equal(req, &got) {
		t.Fatalf("favorite payload mismatch: %v %v", &got, err)
	}
	methods := File_v1_interact_interact_proto.Services().ByName("VideoInteract").Methods()
	if methods.ByName("SetVideoFavorite") == nil || methods.ByName("VideoFavorite") != nil || methods.ByName("VideoFavoriteRevert") != nil {
		t.Fatal("收藏 Saga 入口未移除")
	}
}
