package videoinfo_test

import (
	"testing"

	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
	videov1 "vw_video/api/v1/videoinfo"
)

// TestUploadVideoInfoResponseCompatibility 验证视频 ID 的序列化及新旧 Empty 响应的协议兼容行为。
func TestUploadVideoInfoResponseCompatibility(t *testing.T) {
	want := &videov1.UploadVideoInfoResp{VideoId: 9007199254740993}
	field := want.ProtoReflect().Descriptor().Fields().ByName("video_id")
	if field == nil || field.Number() != 1 {
		t.Fatal("video_id must use response field 1")
	}
	encoded, err := proto.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	var got videov1.UploadVideoInfoResp
	if err := proto.Unmarshal(encoded, &got); err != nil || got.VideoId != want.VideoId {
		t.Fatal("response ID did not survive protobuf serialization")
	}
	// 旧客户端可忽略新增字段，新客户端必须能够识别旧 Empty 响应缺失的视频 ID。
	var oldClient emptypb.Empty
	if err := proto.Unmarshal(encoded, &oldClient); err != nil {
		t.Fatal("new response cannot be decoded by an old Empty client")
	}
	oldResponse, err := proto.Marshal(&emptypb.Empty{})
	if err != nil {
		t.Fatal(err)
	}
	got.Reset()
	if err := proto.Unmarshal(oldResponse, &got); err != nil || got.VideoId != 0 {
		t.Fatal("old service response must decode to a missing ID")
	}
}
