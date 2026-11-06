package user

import (
	"context"
	"io"
	"testing"

	"github.com/go-kratos/kratos/v2/log"
	infov1 "vw_gateway/api/v1/user/userinfo"
	"vw_gateway/internal/biz/userbiz"
)

// accountIdentityRepo 记录改密及换绑实际使用的操作人，不执行真实写入。
type accountIdentityRepo struct {
	userbiz.UserinfoRepo       // 其他资料接口不参与本次测试。
	userID               int64 // 实际转发的鉴权用户 ID。
	calls                int   // 实际调用次数。
}

// ModifyEmail 记录换绑身份，返回传入的新邮箱。
func (r *accountIdentityRepo) ModifyEmail(_ context.Context, id int64, email, _ string) (string, error) {
	r.userID, r.calls = id, r.calls+1
	return email, nil
}

// ModifyPassword 记录正常改密使用的身份。
func (r *accountIdentityRepo) ModifyPassword(_ context.Context, id int64, _, _ string) error {
	r.userID, r.calls = id, r.calls+1
	return nil
}

// TestAccountUpdateIdentity 验证换绑和正常改密只信任 context，不能用路径 ID 操作另一账号。
func TestAccountUpdateIdentity(t *testing.T) {
	logger := log.NewStdLogger(io.Discard)
	repo := &accountIdentityRepo{}
	service := NewUserinfoService(userbiz.NewUserinfoUsecase(repo, logger), logger)
	ctx := context.WithValue(context.Background(), "user_id", int64(7))
	if _, err := service.ModifyEmail(ctx, &infov1.ModifyEmailReq{UserId: 99, Email: "user@example.com", InputCode: "123456"}); err != nil || repo.userID != 7 {
		t.Fatal("换绑使用了伪造身份")
	}
	if _, err := service.ModifyPassword(ctx, &infov1.ModifyPasswordReq{UserId: 99, OldPassword: "old-password", NewPassword: "new-password"}); err != nil || repo.userID != 7 {
		t.Fatal("正常改密使用了伪造身份")
	}
	if _, err := service.ModifyEmail(context.Background(), &infov1.ModifyEmailReq{UserId: 99}); err == nil {
		t.Fatal("无鉴权身份仍可换绑")
	}
	if _, err := service.ModifyPassword(context.Background(), &infov1.ModifyPasswordReq{UserId: 99}); err == nil {
		t.Fatal("无鉴权身份仍可改密")
	}
	if repo.calls != 2 {
		t.Fatal("鉴权失败仍调用下游")
	}
}
