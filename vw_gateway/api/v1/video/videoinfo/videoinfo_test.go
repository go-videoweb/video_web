package videoinfo_test

import (
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	videov1 "vw_gateway/api/v1/video/videoinfo"
)

// TestUploadVideoInfoResponseKeepsCommonResp 验证上传响应新增视频 ID 后仍兼容旧 CommonResp 字段。
func TestUploadVideoInfoResponseKeepsCommonResp(t *testing.T) {
	response := &videov1.UploadVideoInfoResp{
		Resp: &videov1.CommonResp{StatusCode: 200, Message: "success"}, VideoId: 9007199254740993,
	}
	fields := response.ProtoReflect().Descriptor().Fields()
	if fields.ByName("resp").Number() != 1 || fields.ByName("video_id").Number() != 2 {
		t.Fatal("existing resp field must remain 1 and video_id must use 2")
	}
	encoded, err := proto.Marshal(response)
	if err != nil {
		t.Fatal(err)
	}
	var decoded videov1.UploadVideoInfoResp
	if err := proto.Unmarshal(encoded, &decoded); err != nil || !proto.Equal(response, &decoded) {
		t.Fatal("response did not survive protobuf serialization")
	}
	common, err := proto.Marshal(response.Resp)
	if err != nil {
		t.Fatal(err)
	}
	legacy := protowire.AppendTag(nil, 1, protowire.BytesType)
	legacy = protowire.AppendBytes(legacy, common)
	decoded.Reset()
	if err := proto.Unmarshal(legacy, &decoded); err != nil || !proto.Equal(decoded.Resp, response.Resp) || decoded.VideoId != 0 {
		t.Fatal("old response is no longer compatible")
	}
}
