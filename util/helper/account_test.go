package helper

import "testing"

// TestAccountInput 验证统一邮箱规则、用途隔离与 bcrypt 密码字节边界。
func TestAccountInput(t *testing.T) {
	got, err := NormalizeEmail(" User.Name+tag@Example.COM ")
	if err != nil || got != "user.name+tag@example.com" {
		t.Fatalf("规范化错误: %q %v", got, err)
	}
	for _, email := range []string{"", "invalid", "User <user@example.com>", "用户@example.com", "K@example.com", "a@b\r\nBcc: a@b"} {
		if _, err := NormalizeEmail(email); err == nil {
			t.Fatalf("接受无效邮箱 %q", email)
		}
	}
	for _, purpose := range []string{EmailCodeRegister, EmailCodeChange, EmailCodeReset} {
		if err := ValidateEmailCodePurpose(purpose); err != nil {
			t.Fatal(err)
		}
	}
	if ValidateEmailCodePurpose("") == nil || ValidateEmailCodePurpose("other") == nil {
		t.Fatal("接受未指定用途")
	}
	if ValidatePassword("123456") != nil || ValidatePassword(string(make([]byte, 72))) != nil {
		t.Fatal("错误拒绝长度边界")
	}
	if ValidatePassword("12345") == nil || ValidatePassword(string(make([]byte, 73))) == nil {
		t.Fatal("错误接受长度越界")
	}
}
