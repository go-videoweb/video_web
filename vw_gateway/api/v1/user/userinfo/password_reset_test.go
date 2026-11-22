package infov1

import (
	"google.golang.org/protobuf/reflect/protoreflect"
	"testing"
	codev1 "vw_gateway/api/v1/user/captcha"
	upstream "vw_user/api/v1/userinfo"
)

// TestPasswordResetContract 验证两端移除 user_id 且保留现存字段编号，邮件验证码不再返回秘密。
func TestPasswordResetContract(t *testing.T) {
	for _, descriptor := range []protoreflect.MessageDescriptor{(&ForgetPasswordReq{}).ProtoReflect().Descriptor(), (&upstream.ForgetPasswordReq{}).ProtoReflect().Descriptor()} {
		if descriptor.Fields().ByName("user_id") != nil || descriptor.Fields().ByNumber(1) != nil {
			t.Fatal("重置仍接受外部用户ID或复用了旧编号")
		}
		if descriptor.Fields().ByName("email").Number() != 2 {
			t.Fatal("邮箱字段编号改变")
		}
	}
	if (&codev1.GetCodeCaptchaResp{}).ProtoReflect().Descriptor().Fields().ByName("code") != nil {
		t.Fatal("邮件验证码仍暴露于响应协议")
	}
	if (&codev1.GetCodeCaptchaReq{}).ProtoReflect().Descriptor().Fields().ByName("purpose").Number() != 2 {
		t.Fatal("缺少验证码用途")
	}
}
