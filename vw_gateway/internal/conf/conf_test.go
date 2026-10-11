package conf

import (
	"testing"

	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/reflect/protoreflect"
)

// TestServiceConfigFields 验证现用服务配置编号不变，退役评论服务的字段名和编号仍被保留。
func TestServiceConfigFields(t *testing.T) {
	descriptor := (&Service{}).ProtoReflect().Descriptor()
	fields := descriptor.Fields()
	if fields.Len() != 2 {
		t.Fatalf("expected only user and video service config, got %d fields", fields.Len())
	}
	for name, number := range map[string]int{"user": 1, "video": 2} {
		field := fields.ByNumber(protoreflect.FieldNumber(number))
		if field == nil || string(field.Name()) != name {
			t.Errorf("field %d must remain %s", number, name)
		}
	}
	if !descriptor.ReservedRanges().Has(3) || !descriptor.ReservedNames().Has("comment") {
		t.Fatal("retired comment field number and name must remain reserved")
	}
}

// TestServiceConfigIgnoresRetiredCommentEndpoint 验证旧评论配置被忽略时，用户和视频服务配置仍正常解析。
func TestServiceConfigIgnoresRetiredCommentEndpoint(t *testing.T) {
	input := []byte(`{"service":{"user":{"endpoint":"discovery:///vw_user"},"video":{"endpoint":"discovery:///vw_video"},"comment":{"endpoint":"discovery:///vw_comment"}}}`)
	var bootstrap Bootstrap
	// 与 Kratos config.Scan 一致，忽略已退役的 protobuf 配置字段。
	if err := (protojson.UnmarshalOptions{DiscardUnknown: true}).Unmarshal(input, &bootstrap); err != nil {
		t.Fatal(err)
	}
	if got := bootstrap.GetService().GetUser().GetEndpoint(); got != "discovery:///vw_user" {
		t.Errorf("unexpected user endpoint: %q", got)
	}
	if got := bootstrap.GetService().GetVideo().GetEndpoint(); got != "discovery:///vw_video" {
		t.Errorf("unexpected video endpoint: %q", got)
	}
}
