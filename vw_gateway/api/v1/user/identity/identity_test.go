package idv1_test

import (
	"testing"

	"google.golang.org/protobuf/encoding/protowire"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/reflect/protoreflect"
	idv1 "vw_gateway/api/v1/user/identity"
)

// TestLoginDataWireCompatibility 验证新增用户 ID 不改变旧 token 字段编号和双向解码行为。
func TestLoginDataWireCompatibility(t *testing.T) {
	fields := (&idv1.LoginResp_Data{}).ProtoReflect().Descriptor().Fields()
	for name, number := range map[protoreflect.Name]protoreflect.FieldNumber{
		"access_token": 3, "refresh_token": 4, "user_id": 5,
	} {
		field := fields.ByName(name)
		if field == nil || field.Number() != number {
			t.Fatalf("field %s must retain number %d", name, number)
		}
	}

	// 独立构造旧协议字节，避免依赖当前生成描述符而漏掉字段编号回归。
	legacy := protowire.AppendTag(nil, 3, protowire.BytesType)
	legacy = protowire.AppendString(legacy, "access")
	legacy = protowire.AppendTag(legacy, 4, protowire.BytesType)
	legacy = protowire.AppendString(legacy, "refresh")
	var data idv1.LoginResp_Data
	if err := proto.Unmarshal(legacy, &data); err != nil {
		t.Fatal(err)
	}
	if data.AccessToken != "access" || data.RefreshToken != "refresh" || data.UserId != 0 {
		t.Fatal("old token fields no longer decode correctly")
	}

	data.UserId = 9007199254740993
	encoded, err := proto.Marshal(&data)
	if err != nil {
		t.Fatal(err)
	}
	tokens := map[protowire.Number]string{}
	for len(encoded) > 0 {
		number, kind, n := protowire.ConsumeTag(encoded)
		if n < 0 {
			t.Fatal(protowire.ParseError(n))
		}
		encoded = encoded[n:]
		if number == 3 || number == 4 {
			if kind != protowire.BytesType {
				t.Fatal("old token wire type changed")
			}
			value, consumed := protowire.ConsumeString(encoded)
			if consumed < 0 {
				t.Fatal(protowire.ParseError(consumed))
			}
			tokens[number] = value
		}
		consumed := protowire.ConsumeFieldValue(number, kind, encoded)
		if consumed < 0 {
			t.Fatal(protowire.ParseError(consumed))
		}
		encoded = encoded[consumed:]
	}
	if tokens[3] != "access" || tokens[4] != "refresh" {
		t.Fatal("legacy readers cannot read tokens from the new response")
	}
}
