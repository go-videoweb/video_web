package interact

import (
	"strings"
	"testing"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// TestFavoriteContractOffline 验证新增身份字段不改变原协议编号，且保留 Saga 正向和补偿 RPC。
func TestFavoriteContractOffline(t *testing.T) {
	descriptor := (&VideoFavoriteReq{}).ProtoReflect().Descriptor()
	for name, number := range map[protoreflect.Name]protoreflect.FieldNumber{"video_id": 1, "favorite_id": 2, "favorite": 3, "user_id": 4} {
		field := descriptor.Fields().ByName(name)
		if field == nil || field.Number() != number {
			t.Fatalf("字段编号发生变化：%s", name)
		}
	}
	methods := File_v1_interact_interact_proto.Services().ByName("VideoInteract").Methods()
	if methods.ByName("VideoFavorite") == nil || methods.ByName("VideoFavoriteRevert") == nil || methods.ByName("SetVideoFavorite") != nil {
		t.Fatal("第一批不应移除 Saga RPC")
	}
	encoded, err := protojson.Marshal(&VideoFavoriteReq{UserId: 9007199254740993})
	if err != nil || !strings.Contains(string(encoded), `"userId":"9007199254740993"`) {
		t.Fatalf("大用户 ID 编码丢失精度：%s %v", encoded, err)
	}
}
