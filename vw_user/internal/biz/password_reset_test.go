package biz

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"golang.org/x/crypto/bcrypt"
	"util/helper"
	"vw_user/internal/data/dal/model"
	"vw_user/internal/domain"
)

// accountMemoryRepo 只实现本轮账号操作，记录查询邮箱、更新对象和会话清理顺序。
type accountMemoryRepo struct {
	UserInfoRepo         // 不使用的其他用户功能保持原接口。
	email         string // 当前账号邮箱。
	hash          string // 当前保存的密码哈希。
	fail          string // 失败或冲突阶段。
	updatedID     int64  // 实际被更新的账号。
	invalidatedID int64  // 实际失效的会话所属账号。
	lookups       int    // 邮箱定位次数。
}

// CheckUsernameConflict 模拟用户名未占用，不涉及生成器全局查询对象。
func (r *accountMemoryRepo) CheckUsernameConflict(context.Context, string) (bool, error) {
	return true, nil
}

// CheckEmailConflict 模拟数据库预检查和历史软删除占用规则。
func (r *accountMemoryRepo) CheckEmailConflict(context.Context, string, int64) error {
	if r.fail == "email conflict" {
		return kerrors.Conflict("EMAIL_IN_USE", "邮箱已占用")
	}
	return nil
}

// GetUserByEmail 返回唯一邮箱对应的稳定内部 ID；异常时不提供可更新目标。
func (r *accountMemoryRepo) GetUserByEmail(_ context.Context, email string) (*model.User, error) {
	r.lookups++
	if email != r.email || r.fail == "missing" {
		return nil, kerrors.BadRequest("PASSWORD_RESET_UNAVAILABLE", "无法重置")
	}
	if r.fail == "lookup" {
		return nil, errors.New("lookup failed")
	}
	if r.fail == "multiple" {
		return nil, kerrors.Conflict("EMAIL_RELATION_CONFLICT", "邮箱归属异常")
	}
	return &model.User{UserID: 7, Email: email}, nil
}

// ResetPasswordByEmail 模拟再次匹配邮箱，冲突或保存失败时不修改旧密码。
func (r *accountMemoryRepo) ResetPasswordByEmail(_ context.Context, id int64, email, hash string) error {
	if email != r.email || r.fail == "changed email" {
		return kerrors.Conflict("PASSWORD_RESET_CONFLICT", "邮箱已改变")
	}
	if r.fail == "update" {
		return errors.New("save failed")
	}
	r.updatedID, r.hash = id, hash
	return nil
}

// InvalidateUserSession 模拟密码写入后清理会话，单独记录部分成功的失败场景。
func (r *accountMemoryRepo) InvalidateUserSession(_ context.Context, id int64) error {
	if r.updatedID != id {
		return errors.New("session cleared before password update")
	}
	if r.fail == "session" {
		return errors.New("redis unavailable")
	}
	r.invalidatedID = id
	return nil
}

// GetUserInfoByUserId 供原有登录后修改密码流程读取密码哈希。
func (r *accountMemoryRepo) GetUserInfoByUserId(context.Context, int64) (*domain.UserInfo, error) {
	return &domain.UserInfo{Password: r.hash}, nil
}

// UpdatePassword 保持登录后修改密码仓储签名，只记录当前测试账号。
func (r *accountMemoryRepo) UpdatePassword(_ context.Context, id int64, hash string) error {
	r.updatedID, r.hash = id, hash
	return nil
}

// UpdateEmail 记录已验证的新邮箱。
func (r *accountMemoryRepo) UpdateEmail(_ context.Context, id int64, email string) error {
	r.updatedID, r.email = id, email
	return nil
}

// accountCodeRepo 模拟一次性消费契约，真实 Lua 行为由 data 测试独立验证。
type accountCodeRepo struct {
	CaptchaRepo        // 不使用签发和删除操作。
	purpose     string // 最近验证用途。
	email       string // 最近验证邮箱。
	consumed    bool   // 同一码已经消费。
	fail        bool   // 模拟验证码不可用。
	stored      bool   // 是否已经通过签发参数校验并调用仓储。
	clientIP    string // 仓储收到的规范化来源 IP。
}

// SetCodeToCache 记录签发参数，供业务层校验回归使用；不连接 Redis。
func (r *accountCodeRepo) SetCodeToCache(_ context.Context, email, _, purpose, clientIP string, _ time.Duration) error {
	r.email, r.purpose, r.clientIP, r.stored = email, purpose, clientIP, true
	return nil
}

// ConsumeCode 验证用途与一次性规则，拒绝任意用途替换或重复使用。
func (r *accountCodeRepo) ConsumeCode(_ context.Context, email, purpose, code string) error {
	r.email, r.purpose = email, purpose
	if r.fail || r.consumed || code != "123456" {
		return invalidEmailCode()
	}
	r.consumed = true
	return nil
}

// TestPasswordResetBiz 验证按规范化邮箱定位、密码哈希与旧会话失效，以及各阶段失败不误报成功。
func TestPasswordResetBiz(t *testing.T) {
	for _, fail := range []string{"", "code", "missing", "lookup", "multiple", "changed email", "update", "session"} {
		t.Run(fail, func(t *testing.T) {
			repo := &accountMemoryRepo{email: "user@example.com", hash: "old-hash", fail: fail}
			code := &accountCodeRepo{fail: fail == "code"}
			uc := NewUserInfoUsecase(repo, code, log.NewStdLogger(io.Discard))
			err := uc.ForgetPassword(context.Background(), " User@Example.COM ", "123456", "new-password")
			if fail == "" {
				if err != nil || repo.updatedID != 7 || repo.invalidatedID != 7 {
					t.Fatalf("重置失败: %v", err)
				}
				if bcrypt.CompareHashAndPassword([]byte(repo.hash), []byte("new-password")) != nil {
					t.Fatal("未保存有效新密码哈希")
				}
				if bcrypt.CompareHashAndPassword([]byte(repo.hash), []byte("old-password")) == nil {
					t.Fatal("旧密码仍有效")
				}
			} else if err == nil {
				t.Fatal("失败被误报成功")
			}
			if code.purpose != helper.EmailCodeReset || code.email != "user@example.com" {
				t.Fatal("用途或邮箱错误")
			}
			if fail == "code" && repo.lookups != 0 {
				t.Fatal("验证码未通过仍查询了账号")
			}
			if fail == "session" {
				failure := kerrors.FromError(err)
				if failure.Reason != "PASSWORD_SESSION_INVALIDATION_FAILED" || failure.Metadata["password_changed"] != "true" || repo.updatedID != 7 {
					t.Fatal("部分成功未明确报告")
				}
			} else if fail != "" && repo.hash != "old-hash" {
				t.Fatal("失败时修改了原密码")
			}
			if fail != "code" && uc.ForgetPassword(context.Background(), repo.email, "123456", "new-password") == nil {
				t.Fatal("已消费验证码可再次重置")
			}
		})
	}
}

// TestEmailCodeIssueValidation 验证签发入口拒绝错误用途、IP、有效期及格式，并统一 IP 表示。
func TestEmailCodeIssueValidation(t *testing.T) {
	for _, scenario := range []string{"valid", "purpose", "IP", "expiration", "code length", "code format"} {
		t.Run(scenario, func(t *testing.T) {
			repo := &accountCodeRepo{}
			uc := NewCaptchaUsecase(log.NewStdLogger(io.Discard), repo)
			purpose, ip, code, expiration := helper.EmailCodeReset, "::ffff:192.0.2.1", "123456", 5*time.Minute
			switch scenario {
			case "purpose":
				purpose = ""
			case "IP":
				ip = "untrusted"
			case "expiration":
				expiration = time.Hour
			case "code length":
				code = "123"
			case "code format":
				code = "abcdef"
			}
			err := uc.SetCodeToCache(context.Background(), " User@Example.COM ", code, purpose, ip, expiration)
			if scenario == "valid" {
				if err != nil || !repo.stored || repo.email != "user@example.com" || repo.clientIP != "192.0.2.1" {
					t.Fatalf("签发规范化失败: %v", err)
				}
			} else if err == nil || repo.stored {
				t.Fatal("无效参数已进入缓存")
			}
		})
	}
}

// TestAccountInputAndOtherFlows 验证密码规则在消费前执行、注册/换绑用途分离且未绕过冲突检查。
func TestAccountInputAndOtherFlows(t *testing.T) {
	logger := log.NewStdLogger(io.Discard)
	repo := &accountMemoryRepo{email: "user@example.com"}
	code := &accountCodeRepo{}
	uc := NewUserInfoUsecase(repo, code, logger)
	for _, password := range []string{"12345", strings.Repeat("a", 73)} {
		if uc.ForgetPassword(context.Background(), repo.email, "123456", password) == nil || code.consumed {
			t.Fatal("无效密码消费了验证码")
		}
	}
	repo.fail = "email conflict"
	if _, err := uc.ModifyEmail(context.Background(), 7, "other@example.com", "123456"); err == nil || code.consumed {
		t.Fatal("占用邮箱通过换绑")
	}
	repo.fail = ""
	if _, err := uc.ModifyEmail(context.Background(), 7, "Other@Example.COM", "123456"); err != nil || code.purpose != helper.EmailCodeChange || repo.email != "other@example.com" {
		t.Fatalf("换绑用途错误: %v", err)
	}
	code = &accountCodeRepo{}
	identity := NewUserIdentityUsecase(nil, repo, code, logger)
	user, err := identity.handleRegisterInfo(context.Background(), &RegisterInfo{Username: "new", Email: " New@Example.COM ", Password: "123456", RepeatPassword: "123456", InputCode: "123456", Birthday: time.Now()})
	if err != nil || user.Email != "new@example.com" || code.purpose != helper.EmailCodeRegister {
		t.Fatalf("注册用途错误: %v", err)
	}
	oldHash, err := bcrypt.GenerateFromPassword([]byte("old-password"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	repo.hash, repo.updatedID, repo.invalidatedID = string(oldHash), 0, 0
	if err := uc.ModifyPassword(context.Background(), 7, "wrong", "new-password"); err == nil || repo.updatedID != 0 {
		t.Fatal("旧密码错误仍修改密码")
	}
	if err := uc.ModifyPassword(context.Background(), 7, "old-password", "new-password"); err != nil || repo.invalidatedID != 7 {
		t.Fatalf("登录后改密失败: %v", err)
	}
}
