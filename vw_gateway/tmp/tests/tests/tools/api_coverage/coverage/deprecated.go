package coverage

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"
)

// LoadDeprecatedRouteBlacklist 读取废弃接口黑名单 JSON，并转换为覆盖统计使用的 map。
// path 为空或文件不存在时返回空黑名单，便于没有废弃接口的项目/分支继续使用同一个工具。
func LoadDeprecatedRouteBlacklist(path, repoRoot string) (map[RouteKey]string, error) {
	blacklist := make(map[RouteKey]string)
	path = strings.TrimSpace(path)
	if path == "" {
		return blacklist, nil
	}

	configPath := ResolveConfigPath(path, repoRoot)
	content, err := os.ReadFile(configPath)
	if errors.Is(err, os.ErrNotExist) {
		return blacklist, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read deprecated route blacklist %s: %w", configPath, err)
	}

	var entries []DeprecatedRouteEntry
	if err := json.Unmarshal(content, &entries); err != nil {
		return nil, fmt.Errorf("parse deprecated route blacklist %s: %w", configPath, err)
	}

	// 由于黑名单文件是人工编写，可能存在错误，因此还需要对黑名单配置项进行检测。
	// 如果检测出错误，则直接返回错误，避免覆盖率结果被错误配置污染。
	for index, entry := range entries {
		key, reason, err := normalizeDeprecatedRouteEntry(entry, index)
		if err != nil {
			return nil, fmt.Errorf("parse deprecated route blacklist %s: %w", configPath, err)
		}
		if _, exists := blacklist[key]; exists {
			return nil, fmt.Errorf("parse deprecated route blacklist %s: duplicate route %s %s", configPath, key.Method, key.Path)
		}
		blacklist[key] = reason
	}
	return blacklist, nil
}

// normalizeDeprecatedRouteEntry 校验并规范化黑名单配置项。
// index 用于报错时指出 JSON 数组中哪一项配置有问题。
func normalizeDeprecatedRouteEntry(entry DeprecatedRouteEntry, index int) (RouteKey, string, error) {
	method := strings.ToUpper(strings.TrimSpace(entry.Method))
	if method == "" {
		return RouteKey{}, "", fmt.Errorf("entry %d: method is required", index)
	}
	if strings.ContainsAny(method, " \t\r\n") {
		return RouteKey{}, "", fmt.Errorf("entry %d: method %q contains whitespace", index, entry.Method)
	}

	path := NormalizePath(entry.Path)
	if path == "" || !strings.HasPrefix(path, "/") {
		return RouteKey{}, "", fmt.Errorf("entry %d: path must start with /, got %q", index, entry.Path)
	}

	reason := strings.TrimSpace(entry.Reason)
	if reason == "" {
		reason = "未填写废弃原因"
	}
	return RouteKey{Method: method, Path: path}, reason, nil
}

// deprecatedRouteReason 查询路由是否在废弃接口黑名单中。
func deprecatedRouteReason(key RouteKey, deprecatedRoutes map[RouteKey]string) (string, bool) {
	reason, ok := deprecatedRoutes[key]
	return reason, ok
}

// buildDeprecatedReports 输出黑名单排除接口的明细，用于审查“跳过统计”的接口是否仍应保留。
func buildDeprecatedReports(routes []Route, deprecatedRoutes map[RouteKey]string) []DeprecatedRouteReport {
	reports := make([]DeprecatedRouteReport, 0)
	for _, route := range routes {
		reason, ok := deprecatedRouteReason(route.Key, deprecatedRoutes)
		if !ok || route.Classification != ClassDeprecated {
			continue
		}
		reports = append(reports, DeprecatedRouteReport{
			Method: route.Method,
			Path:   route.Path,
			Reason: reason,
		})
	}
	sortDeprecatedReports(reports)
	return reports
}

// buildDeprecatedBlacklistWarnings 检查黑名单中是否存在 router 扫描不到的路由。
// 这种情况通常表示接口已删除或黑名单配置写错了，需要在报告中提醒维护者清理。
func buildDeprecatedBlacklistWarnings(deprecatedRoutes map[RouteKey]string, routeByKey map[RouteKey]Route) []string {
	var warnings []string
	for _, key := range sortedRouteKeysFromReasonMap(deprecatedRoutes) {
		if _, exists := routeByKey[key]; !exists {
			warnings = append(warnings, fmt.Sprintf("废弃接口黑名单中存在 router 声明中找不到的路由：%s %s", key.Method, key.Path))
		}
	}
	return warnings
}

// sortDeprecatedReports 按 path、method 稳定排序废弃接口报告。
func sortDeprecatedReports(reports []DeprecatedRouteReport) {
	sort.Slice(reports, func(i, j int) bool {
		return routeLess(
			RouteKey{Method: reports[i].Method, Path: reports[i].Path},
			RouteKey{Method: reports[j].Method, Path: reports[j].Path},
		)
	})
}
