package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

// Config 记录这次运行工具时传入了哪些参数。
type Config struct {
	// XMLPath 是禅道 XML 导出文件路径；示例："C:/Downloads/bugs.xml"。
	XMLPath string
	// HTMLPath 是禅道 HTML 导出文件路径，用于把附件的 fileID/download_url 一起读出来；示例："C:/Downloads/bugs.html"。
	HTMLPath string
	// Output 是 JSON 输出路径；"-" 表示直接打印到命令行窗口，示例："tests/bugRegression/BUG_1108/bug.json"。
	Output string
	// ResourceDir 是模型类附件下载目录；为空表示只记录附件文件名和下载链接，不下载 .mo/.zip/.fmu/.mat 等资源。
	ResourceDir string
	// CookieFile 是访问禅道附件下载链接时使用的 Cookie 文件路径；默认可由 ZENTAO_COOKIE_FILE 覆盖。
	CookieFile string
	// Cookie 是从 CookieFile 读取出的 Cookie 文本；不会直接从命令行参数读取，避免泄露登录信息。
	Cookie string
	// BugID 是可选的单 Bug 过滤条件；为空表示输出全部 Bug，示例："1108"。
	BugID string
	// Pretty 控制 JSON 是否换行缩进；true 更好读，false 文件更小。
	Pretty bool
}

// Parse 读取命令行参数，整理成工具后续要使用的配置。
//
// 作用：检查 -xml、-html、-out、-resource-dir、-cookie-file、-bug-id、-pretty 等参数，并从 Cookie 文件读取登录信息。
// 输入：命令行参数切片，例如 []string{"-xml", "bugs.xml", "-html", "bugs.html", "-resource-dir", "tmp/zentao", "-cookie-file", "tmp/zentao_cookie.txt"}。
// 输出：Config 和 error；成功时 Config.XMLPath 必有值，失败时 error 描述缺失参数或多余参数。
// 示例：Parse([]string{"-xml", "bugs.xml", "-out", "-"}) 返回直接打印到命令行窗口的配置。
func Parse(args []string) (Config, error) {
	fs := flag.NewFlagSet("zentao_bug_export", flag.ContinueOnError)
	fs.SetOutput(io.Discard)

	cfg := Config{Output: "-", Pretty: true, CookieFile: os.Getenv("ZENTAO_COOKIE_FILE")}
	if cfg.CookieFile == "" {
		cfg.CookieFile = "tmp/zentao_cookie.txt"
	}
	deprecatedCookie := ""
	fs.StringVar(&cfg.XMLPath, "xml", "", "禅道 XML 导出文件路径，作为主要读取文件")
	fs.StringVar(&cfg.HTMLPath, "html", "", "禅道 HTML 导出文件路径，用于补充附件 fileID/download_url")
	fs.StringVar(&cfg.Output, "out", "-", "输出 JSON 文件路径；使用 - 输出到 stdout")
	fs.StringVar(&cfg.ResourceDir, "resource-dir", "", "模型类附件下载目录；为空表示不下载附件")
	fs.StringVar(&cfg.CookieFile, "cookie-file", cfg.CookieFile, "禅道附件下载 Cookie 文件路径；默认 tmp/zentao_cookie.txt，可由 ZENTAO_COOKIE_FILE 覆盖")
	fs.StringVar(&cfg.CookieFile, "cookie_file", cfg.CookieFile, "同 -cookie-file；兼容下划线参数名")
	fs.StringVar(&deprecatedCookie, "cookie", "", "已弃用；请改用 -cookie-file")
	fs.StringVar(&cfg.BugID, "bug-id", "", "只输出指定 Bug 编号")
	fs.BoolVar(&cfg.Pretty, "pretty", true, "是否输出缩进后的 JSON")

	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	if flagProvided(args, "cookie") {
		return Config{}, errors.New("-cookie is deprecated; write the cookie to a local file and pass -cookie-file instead")
	}
	if cfg.XMLPath == "" {
		return Config{}, errors.New("missing required -xml")
	}
	if fs.NArg() > 0 {
		return Config{}, fmt.Errorf("unexpected arguments: %v", fs.Args())
	}
	cookie, err := readCookieFile(cfg.CookieFile, flagProvided(args, "cookie-file") || flagProvided(args, "cookie_file"))
	if err != nil {
		return Config{}, err
	}
	cfg.Cookie = cookie
	return cfg, nil
}

// readCookieFile 函数作用：从本地文件读取禅道 Cookie，并去除首尾空白。
// 入参：path 是 Cookie 文件路径；required 表示该路径由用户显式提供，文件不存在时应返回错误。
// 返回值：返回 Cookie 字符串和 error；文件不存在且非 required 时返回空 Cookie。
// 示例：readCookieFile("tmp/zentao_cookie.txt", false) 会读取形如 "zentaosid=...; lang=zh-cn" 的内容。
func readCookieFile(path string, required bool) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) && !required {
			return "", nil
		}
		return "", fmt.Errorf("read cookie file %q: %w", path, err)
	}
	return normalizeCookieFileContent(string(data)), nil
}

// normalizeCookieFileContent 函数作用：把 Cookie 文件内容整理成可直接使用的 Cookie 值。
// 入参：data 是 Cookie 文件原始内容，可以是 "zentaosid=..."，也可以是浏览器复制出的 "Cookie: zentaosid=..."。
// 返回值：去掉文件开头的隐藏字符、首尾空白和可选 "Cookie:" 前缀后的 Cookie 值；内容为空时返回空字符串。
// 示例：normalizeCookieFileContent("Cookie: zentaosid=abc; lang=zh-cn\n") 返回 "zentaosid=abc; lang=zh-cn"。
func normalizeCookieFileContent(data string) string {
	cookie := strings.TrimSpace(strings.TrimPrefix(data, "\ufeff"))
	if strings.HasPrefix(strings.ToLower(cookie), "cookie:") {
		cookie = strings.TrimSpace(cookie[len("Cookie:"):])
	}
	return cookie
}

// flagProvided 函数作用：判断命令行参数中是否写了某个选项。
// 入参：args 是命令行参数切片；name 是不带短横线的参数名，例如 "cookie-file"。
// 返回值：如果出现 -name 或 -name=value 返回 true，否则返回 false。
// 示例：flagProvided([]string{"-cookie-file", "tmp/cookie.txt"}, "cookie-file") 返回 true。
func flagProvided(args []string, name string) bool {
	prefix := "-" + name
	for _, arg := range args {
		if arg == prefix || strings.HasPrefix(arg, prefix+"=") {
			return true
		}
	}
	return false
}
