package videoinfo_test

import (
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	videov1 "vw_gateway/api/v1/video/videoinfo"
	videov2 "vw_video/api/v1/videoinfo"
)

// TestVideoReadFieldCompatibility 验证视频摘要保留旧字段编号，新增字段及详情时间沿用 Timestamp 契约。
func TestVideoReadFieldCompatibility(t *testing.T) {
	for _, message := range []proto.Message{&videov1.VideoSummary{}, &videov2.VideoSummary{}} {
		fields := message.ProtoReflect().Descriptor().Fields()
		for name, number := range map[protoreflect.Name]protoreflect.FieldNumber{
			"video_id": 1, "cnt_barrages": 2, "cnt_viewed": 3, "title": 4, "duration": 5, "publisher_name": 6, "cover_path": 7,
			"description": 8, "video_path": 9, "created_time": 10,
		} {
			field := fields.ByName(name)
			if field == nil || field.Number() != number {
				t.Fatalf("field %s was renumbered", name)
			}
		}
		if fields.ByName("created_time").Message().FullName() != "google.protobuf.Timestamp" {
			t.Fatal("summary time must be a Timestamp")
		}
		legacy := protowire.AppendTag(nil, 4, protowire.BytesType)
		legacy = protowire.AppendString(legacy, "old title")
		legacy = protowire.AppendTag(legacy, 7, protowire.BytesType)
		legacy = protowire.AppendString(legacy, "old cover")
		if err := proto.Unmarshal(legacy, message); err != nil {
			t.Fatal(err)
		}
		if message.ProtoReflect().Get(fields.ByName("title")).String() != "old title" || message.ProtoReflect().Get(fields.ByName("cover_path")).String() != "old cover" {
			t.Fatal("old summary no longer decodes")
		}
	}
	gateway := (&videov1.VideoMetaInfo{}).ProtoReflect().Descriptor().Fields()
	if gateway.ByName("created_time").Number() != 10 || gateway.ByName("created_time").Message().FullName() != "google.protobuf.Timestamp" {
		t.Fatal("existing detail timestamp contract changed")
	}
	video := (&videov2.VideoMetaInfo{}).ProtoReflect().Descriptor().Fields()
	if video.ByName("created_time").Number() != 14 || video.ByName("records").Number() != 10 || video.ByName("duration").Number() != 11 || video.ByName("size").Number() != 13 {
		t.Fatal("video detail field numbers changed")
	}
}
