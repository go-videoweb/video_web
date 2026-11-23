package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestParseReadsCookieFile 验证 -cookie-file 会从本地文件读取 Cookie。
func TestParseReadsCookieFile(t *testing.T) {
	t.Parallel()

	cookiePath := filepath.Join(t.TempDir(), "zentao_cookie.txt")
	if err := os.WriteFile(cookiePath, []byte(" Cookie: zentaosid=abc; lang=zh-cn \n"), 0o600); err != nil {
		t.Fatalf("write cookie file: %v", err)
	}

	cfg, err := Parse([]string{"-xml", "bugs.xml", "-cookie-file", cookiePath})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Cookie != "zentaosid=abc; lang=zh-cn" {
		t.Fatalf("unexpected cookie: %q", cfg.Cookie)
	}
}

// TestParseReadsCookieFileAlias 验证 -cookie_file 下划线别名也会从本地文件读取 Cookie。
func TestParseReadsCookieFileAlias(t *testing.T) {
	t.Parallel()

	cookiePath := filepath.Join(t.TempDir(), "zentao_cookie.txt")
	if err := os.WriteFile(cookiePath, []byte("zentaosid=alias; lang=zh-cn"), 0o600); err != nil {
		t.Fatalf("write cookie file: %v", err)
	}

	cfg, err := Parse([]string{"-xml", "bugs.xml", "-cookie_file", cookiePath})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Cookie != "zentaosid=alias; lang=zh-cn" {
		t.Fatalf("unexpected cookie: %q", cfg.Cookie)
	}
}

// TestParseAllowsMissingDefaultCookieFile 验证默认 Cookie 文件不存在时不阻塞只解析场景。
func TestParseAllowsMissingDefaultCookieFile(t *testing.T) {
	t.Setenv("ZENTAO_COOKIE_FILE", filepath.Join(t.TempDir(), "missing_cookie.txt"))

	cfg, err := Parse([]string{"-xml", "bugs.xml"})
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if cfg.Cookie != "" {
		t.Fatalf("expected empty cookie, got %q", cfg.Cookie)
	}
}

// TestParseRejectsDeprecatedCookieFlag 验证不能再把 Cookie 直接写在 -cookie 参数里。
func TestParseRejectsDeprecatedCookieFlag(t *testing.T) {
	t.Parallel()

	cases := [][]string{
		{"-xml", "bugs.xml", "-cookie", "zentaosid=abc"},
		{"-xml", "bugs.xml", "-cookie="},
	}
	for _, args := range cases {
		_, err := Parse(args)
		if err == nil || !strings.Contains(err.Error(), "deprecated") {
			t.Fatalf("expected deprecated cookie error for %v, got %v", args, err)
		}
	}
}

// TestParseRequiresExplicitCookieFileToExist 验证显式指定的 Cookie 文件不存在时会报错。
func TestParseRequiresExplicitCookieFileToExist(t *testing.T) {
	t.Parallel()

	missingPath := filepath.Join(t.TempDir(), "missing_cookie.txt")
	_, err := Parse([]string{"-xml", "bugs.xml", "-cookie-file", missingPath})
	if err == nil || !strings.Contains(err.Error(), "read cookie file") {
		t.Fatalf("expected missing cookie file error, got %v", err)
	}
}
