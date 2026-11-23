package report

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"yssim-go/tests/tools/api_coverage/coverage"
)

// PrintTeamCityMessages 输出 TeamCity 可识别的 service messages。
// stdout 中出现这些行后，TeamCity 会记录自定义统计值、发布 artifact，并在硬错误时标记 build problem。
func PrintTeamCityMessages(summary coverage.Summary, outputJSON, outputHTML, repoRoot string, hasErrors bool) {
	stats := map[string]string{
		"apiCoveragePercent":        fmt.Sprintf("%.2f", summary.CoveragePercent),
		"apiCurrentRoutes":          strconv.Itoa(summary.CurrentRoutes),
		"apiCoveredRoutes":          strconv.Itoa(summary.CoveredRoutes),
		"apiMissingRoutes":          strconv.Itoa(summary.MissingRoutes),
		"apiDeprecatedRoutes":       strconv.Itoa(summary.DeprecatedRoutes),
		"apiUnknownPrimaryMarks":    strconv.Itoa(summary.UnknownPrimaryMarks),
		"apiNonCurrentPrimaryMarks": strconv.Itoa(summary.NonCurrentPrimaryMarks),
		"apiDuplicatePrimaryRoutes": strconv.Itoa(summary.DuplicatePrimaryRoutes),
	}
	for _, key := range coverage.SortedStringKeys(stats) {
		fmt.Printf("##teamcity[buildStatisticValue key='%s' value='%s']\n", teamCityEscape(key), teamCityEscape(stats[key]))
	}

	for _, artifactPath := range coverageArtifactPaths(outputJSON, outputHTML) {
		artifactSpec := fmt.Sprintf("%s => api-coverage", teamCityArtifactPath(artifactPath, repoRoot))
		fmt.Printf("##teamcity[publishArtifacts '%s']\n", teamCityEscape(artifactSpec))
	}

	if hasErrors {
		fmt.Println("##teamcity[buildProblem identity='apiCoverageDuplicatePrimary' description='API 覆盖率检测失败：存在重复主测接口']")
	}
}

// coverageArtifactPaths 返回本次需要发布到 TeamCity 的 artifact 路径。
// 生成 HTML 时发布整个 HTML 报告目录，便于 TeamCity Report Tab 读取 index.html。
func coverageArtifactPaths(outputJSON, outputHTML string) []string {
	if outputHTML != "" {
		htmlDir := filepath.Clean(filepath.Dir(outputHTML))
		if filepath.Clean(filepath.Dir(outputJSON)) == htmlDir {
			return []string{htmlDir}
		}
		return []string{htmlDir, outputJSON}
	}
	return []string{outputJSON}
}

// teamCityArtifactPath 将本地路径转换为 TeamCity artifact service message 使用的路径。
func teamCityArtifactPath(path, repoRoot string) string {
	artifactPath := filepath.Clean(path)
	if rel, err := filepath.Rel(repoRoot, artifactPath); err == nil && !strings.HasPrefix(rel, "..") {
		artifactPath = filepath.ToSlash(rel)
	}
	return artifactPath
}

// ShouldEmitTeamCity 根据命令行选项判断是否输出 TeamCity service messages。
// auto 模式下只要 TEAMCITY_VERSION 环境变量存在，就认为当前运行在 TeamCity 中。
func ShouldEmitTeamCity(mode string) bool {
	switch mode {
	case "always":
		return true
	case "never":
		return false
	default:
		return os.Getenv("TEAMCITY_VERSION") != ""
	}
}

// teamCityEscape 转义 TeamCity service message 属性值。
// 该转义规则来自 TeamCity service message 规范，避免路径或描述中的特殊字符破坏解析。
func teamCityEscape(value string) string {
	replacer := strings.NewReplacer(
		"|", "||",
		"'", "|'",
		"\n", "|n",
		"\r", "|r",
		"[", "|[",
		"]", "|]",
	)
	return replacer.Replace(value)
}
