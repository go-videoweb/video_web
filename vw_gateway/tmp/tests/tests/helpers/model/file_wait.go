package model

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"yssim-go/app/DataBase"
	"yssim-go/config"
)

// mountedYssimGoRoot 是 147/TeamCity 环境中 yssim-go static 文件的固定挂载根目录。
const mountedYssimGoRoot = "/YSSIM/cloud/apps/yssim-go"

// FileExpectation 描述一个需要等待满足的文件系统状态。
type FileExpectation struct {
	// Path 是需要检查的文件或目录绝对路径。
	Path string
	// ShouldExist 表示 Path 是否应该存在；为 false 时只断言路径不存在，不检查文本内容。
	ShouldExist bool
	// Contains 是文件内容必须包含的文本片段列表，用于宽松匹配源码或配置内容。
	Contains []string
	// NotContains 是文件内容不能包含的文本片段列表，用于确认旧内容已被移除。
	NotContains []string
	// ContainsLines 是文件内容按行归一化后必须精确包含的行列表，适合校验 package.order。
	ContainsLines []string
	// NotContainsLines 是文件内容按行归一化后不能出现的行列表，适合校验 package.order 移除项。
	NotContainsLines []string
}

// FileWaitOptions 描述等待文件系统状态时使用的指数退避参数。
type FileWaitOptions struct {
	// Timeout 是等待所有文件状态满足的总时长上限；小于等于 0 时使用默认值。
	Timeout time.Duration
	// InitialDelay 是首次重试前的等待间隔；小于等于 0 时使用默认值。
	InitialDelay time.Duration
	// MaxDelay 是指数退避过程中单次等待间隔的上限；小于等于 0 时使用默认值。
	MaxDelay time.Duration
}

// ModelPackageRootPath 返回模型库 package.mo 所在目录在 /YSSIM 共享挂载中的路径。
func ModelPackageRootPath(packageID string) (string, error) {
	if strings.TrimSpace(packageID) == "" {
		return "", fmt.Errorf("packageID 不能为空")
	}

	var packageModel DataBase.YssimModels
	if err := config.DB.Where("id = ?", packageID).First(&packageModel).Error; err != nil {
		return "", fmt.Errorf("查询模型库 %s 文件路径失败: %w", packageID, err)
	}
	if strings.TrimSpace(packageModel.FilePath) == "" {
		return "", fmt.Errorf("模型库 %s 的 file_path 为空", packageID)
	}

	packagePath, err := LocalStaticPath(packageModel.FilePath)
	if err != nil {
		return "", err
	}
	return filepath.Dir(packagePath), nil
}

// LocalStaticPath 将服务端记录的 static 相对路径映射到 /YSSIM 共享挂载目录。
func LocalStaticPath(serverPath string) (string, error) {
	cleanPath := strings.TrimSpace(serverPath)
	if cleanPath == "" {
		return "", fmt.Errorf("serverPath 不能为空")
	}

	slashPath := filepath.ToSlash(filepath.Clean(cleanPath))
	mountedRoot := filepath.ToSlash(mountedYssimGoRoot)
	if slashPath == mountedRoot || strings.HasPrefix(slashPath, mountedRoot+"/") {
		return filepath.Clean(filepath.FromSlash(slashPath)), nil
	}
	if idx := strings.Index(slashPath, "/static/"); idx >= 0 {
		slashPath = slashPath[idx+1:]
	}

	if strings.HasPrefix(slashPath, "static/") || slashPath == "static" {
		return filepath.Join(filepath.FromSlash(mountedYssimGoRoot), filepath.FromSlash(slashPath)), nil
	}
	if filepath.IsAbs(cleanPath) {
		return filepath.Clean(cleanPath), nil
	}
	return filepath.Join(filepath.FromSlash(mountedYssimGoRoot), "static", filepath.FromSlash(slashPath)), nil
}

// FileExpectationSkipReason 判断当前环境是否应该跳过远端文件系统校验。
// 仅当 SKIP_REMOTE_FILE_ASSERT 为真值，且当前测试进程无法访问 /YSSIM 挂载时，才建议跳过。
func FileExpectationSkipReason() (string, bool) {
	if !truthyEnv("SKIP_REMOTE_FILE_ASSERT") {
		return "", false
	}
	if yssimMountAvailable() {
		return "", false
	}
	return "SKIP_REMOTE_FILE_ASSERT 已开启，且当前环境无法访问 /YSSIM 挂载，跳过远端文件内容校验", true
}

// WaitForFileExpectations 等待一组文件状态满足；等待间隔按指数退避增长。
// 指数退避主要用于容忍 static 挂载目录的短暂同步延迟，每轮都会重新读取磁盘状态。
// 适用于本地调试的情况，给测试者留出足够的时间同步文件
func WaitForFileExpectations(expectations []FileExpectation, options FileWaitOptions) error {
	if len(expectations) == 0 {
		return fmt.Errorf("文件状态期望不能为空")
	}

	timeout := options.Timeout
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	delay := options.InitialDelay
	if delay <= 0 {
		delay = 100 * time.Millisecond
	}
	maxDelay := options.MaxDelay
	if maxDelay <= 0 {
		maxDelay = time.Second
	}

	deadline := time.Now().Add(timeout)
	var lastErr error
	for {
		lastErr = checkFileExpectations(expectations)
		if lastErr == nil {
			return nil
		}
		remaining := time.Until(deadline)
		if remaining <= 0 {
			return fmt.Errorf("等待文件状态超时: %w", lastErr)
		}

		sleepDelay := delay
		if sleepDelay > remaining {
			sleepDelay = remaining
		}
		time.Sleep(sleepDelay)
		delay *= 2
		if delay > maxDelay {
			delay = maxDelay
		}
	}
}

// checkFileExpectations 逐项检查文件状态期望，返回遇到的第一个失败原因。
func checkFileExpectations(expectations []FileExpectation) error {
	for _, expectation := range expectations {
		if err := checkFileExpectation(expectation); err != nil {
			return err
		}
	}
	return nil
}

// checkFileExpectation 检查单个路径是否符合存在性和文本内容期望。
func checkFileExpectation(expectation FileExpectation) error {
	path := strings.TrimSpace(expectation.Path)
	if path == "" {
		return fmt.Errorf("文件状态期望 path 不能为空")
	}

	info, statErr := os.Stat(path)
	exists := statErr == nil
	if statErr != nil && !os.IsNotExist(statErr) {
		return fmt.Errorf("读取文件状态 %s 失败: %w", path, statErr)
	}
	if expectation.ShouldExist && !exists {
		return fmt.Errorf("期望路径存在但不存在: %s", path)
	}
	if !expectation.ShouldExist {
		if exists {
			return fmt.Errorf("期望路径不存在但仍存在: %s", path)
		}
		return nil
	}
	hasContentExpectation := len(expectation.Contains) > 0 ||
		len(expectation.NotContains) > 0 ||
		len(expectation.ContainsLines) > 0 ||
		len(expectation.NotContainsLines) > 0
	if info.IsDir() && hasContentExpectation {
		return fmt.Errorf("路径是目录，不能校验文本内容: %s", path)
	}
	if !hasContentExpectation {
		return nil
	}

	contentBytes, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("读取文件内容 %s 失败: %w", path, err)
	}
	content := string(contentBytes)
	for _, text := range expectation.Contains {
		if !strings.Contains(content, text) {
			return fmt.Errorf("文件 %s 未包含期望内容 %q", path, text)
		}
	}
	for _, text := range expectation.NotContains {
		if strings.Contains(content, text) {
			return fmt.Errorf("文件 %s 仍包含不期望内容 %q", path, text)
		}
	}
	lines := normalizedContentLines(content)
	for _, line := range expectation.ContainsLines {
		if !containsLine(lines, line) {
			return fmt.Errorf("文件 %s 未包含期望行 %q", path, line)
		}
	}
	for _, line := range expectation.NotContainsLines {
		if containsLine(lines, line) {
			return fmt.Errorf("文件 %s 仍包含不期望行 %q", path, line)
		}
	}
	return nil
}

// normalizedContentLines 将文本统一为去空白的非空行，屏蔽 Windows/Linux 换行差异。
func normalizedContentLines(content string) []string {
	content = strings.ReplaceAll(content, "\r\n", "\n")
	content = strings.ReplaceAll(content, "\r", "\n")
	parts := strings.Split(content, "\n")
	lines := make([]string, 0, len(parts))
	for _, part := range parts {
		line := strings.TrimSpace(part)
		if line != "" {
			lines = append(lines, line)
		}
	}
	return lines
}

// containsLine 判断归一化后的行集合中是否存在指定行。
func containsLine(lines []string, want string) bool {
	want = strings.TrimSpace(want)
	for _, line := range lines {
		if line == want {
			return true
		}
	}
	return false
}

// yssimMountAvailable 判断当前测试进程是否能访问 /YSSIM 挂载根。
func yssimMountAvailable() bool {
	if _, err := os.Stat(filepath.FromSlash("/YSSIM")); err == nil || (err != nil && !os.IsNotExist(err)) {
		return true
	}
	return false
}

// truthyEnv 判断环境变量是否为开启态，支持 1/true/yes/on。
func truthyEnv(key string) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(key))) {
	case "1", "true", "yes", "on":
		return true
	default:
		return false
	}
}
