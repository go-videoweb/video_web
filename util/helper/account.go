package helper

import (
	"net/mail"
	"strings"
	"unicode/utf8"

	"github.com/go-kratos/kratos/v2/errors"
)

const (
	EmailCodeRegister = "register"       // 注册时验证邮箱归属。
	EmailCodeChange   = "change_email"   // 登录后验证待换绑邮箱。
	EmailCodeReset    = "reset_password" // 匿名找回密码，不能使用其他用途验证码。
)

// NormalizeEmail 将邮箱去首尾空白并转为小写；仅接受不带显示名称的 ASCII 邮箱。
// 长度上限 100 字节，与当前 user.email 列一致；不删除点或加号后缀。
func NormalizeEmail(email string) (string, error) {
	email = strings.TrimSpace(email)
	if email == "" || len(email) > 100 {
		return "", errors.BadRequest("INVALID_EMAIL", "邮箱不能为空且不得超过100字节")
	}
	for _, c := range email {
		if c > 127 {
			return "", errors.BadRequest("INVALID_EMAIL", "邮箱必须使用ASCII字符")
		}
	}
	email = strings.ToLower(email)
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || address.Name != "" {
		return "", errors.BadRequest("INVALID_EMAIL", "邮箱格式不正确")
	}
	return email, nil
}

// ValidatePassword 复用现有至少 6 字节规则，并限制 bcrypt 支持的最大 72 字节。
// 不裁剪密码或修改大小写，避免前后端保存的密码含义不一致。
func ValidatePassword(password string) error {
	if !utf8.ValidString(password) || len(password) < 6 || len(password) > 72 {
		return errors.BadRequest("INVALID_PASSWORD", "密码长度必须为6至72字节")
	}
	return nil
}

// ValidateEmailCodePurpose 拒绝未明确用途的验证码，不自动回退到注册或重置密码。
func ValidateEmailCodePurpose(purpose string) error {
	switch purpose {
	case EmailCodeRegister, EmailCodeChange, EmailCodeReset:
		return nil
	default:
		return errors.BadRequest("INVALID_CODE_PURPOSE", "验证码用途不正确")
	}
}
