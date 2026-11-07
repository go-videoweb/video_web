package userdata

import (
	"context"
	"testing"

	"github.com/go-kratos/kratos/v2/errors"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
	infov1 "vw_user/api/v1/userinfo"
)

// passwordResetClient 记录唯一一次 RPC 请求，模拟用户服务部分成功的业务错误。
type passwordResetClient struct {
	infov1.UserinfoClient                           // 其他 RPC 不参与本测试。
	request               *infov1.ForgetPasswordReq // 网关实际发送的三个字段。
	failure               error                     // 用户服务原始结果。
	calls                 int                       // 防止网关重复提交已经消费验证码的操作。
}

// ForgetPassword 不连接真实 gRPC 服务，原样返回预设结果。
func (c *passwordResetClient) ForgetPassword(_ context.Context, request *infov1.ForgetPasswordReq, _ ...grpc.CallOption) (*emptypb.Empty, error) {
	c.request, c.calls = request, c.calls+1
	return &emptypb.Empty{}, c.failure
}

// TestPasswordResetForwarding 验证网关无预读和重试，且保留用户服务的部分成功 metadata。
func TestPasswordResetForwarding(t *testing.T) {
	partial := errors.InternalServer("PASSWORD_SESSION_INVALIDATION_FAILED", "密码已更新").WithMetadata(map[string]string{"password_changed": "true", "session_revoked": "false"})
	for _, failure := range []error{nil, partial} {
		client := &passwordResetClient{failure: failure}
		repo := &userinfoRepo{data: &Data{userInfoClient: client}}
		err := repo.ForgetPassword(context.Background(), "user@example.com", "123456", "new-password")
		want := &infov1.ForgetPasswordReq{Email: "user@example.com", InputCode: "123456", NewPassword: "new-password"}
		if !proto.Equal(client.request, want) || client.calls != 1 || err != failure {
			t.Fatal("请求映射、调用次数或错误透传发生变化")
		}
		if failure != nil && errors.FromError(err).Metadata["password_changed"] != "true" {
			t.Fatal("部分成功标记丢失")
		}
	}
}
