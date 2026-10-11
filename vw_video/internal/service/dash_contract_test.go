package service

import (
	"testing"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	video "vw_video/api/v1/videoinfo"
)

// TestDASHSegmentRPCMigration 验证旧路径字段保持编号和类型，但被明确拒绝；新 ID 和文件名使用新编号。
func TestDASHSegmentRPCMigration(t *testing.T) {
	legacy := protowire.AppendString(protowire.AppendTag(nil, 1, protowire.BytesType), "C:/private/file")
	var req video.GetVideoSegmentReq
	if err := proto.Unmarshal(legacy, &req); err != nil {
		t.Fatal(err)
	}
	if req.VideoSegmentPath != "C:/private/file" || req.VideoId != 0 {
		t.Fatal("old field reinterpreted")
	}
	svc := &VideoInfoService{}
	if err := svc.GetVideoSegments(&req, nil); status.Code(err) != codes.InvalidArgument {
		t.Fatalf("old request not rejected: %v", err)
	}
	for _, tc := range []struct {
		name string
		num  protoreflect.FieldNumber
		kind protoreflect.Kind
	}{
		{"video_segment_path", 1, protoreflect.StringKind}, {"video_id", 2, protoreflect.Int64Kind}, {"segment_name", 3, protoreflect.StringKind},
	} {
		f := req.ProtoReflect().Descriptor().Fields().ByName(protoreflect.Name(tc.name))
		if f == nil || f.Number() != tc.num || f.Kind() != tc.kind {
			t.Fatalf("invalid field contract: %s", tc.name)
		}
	}
}
