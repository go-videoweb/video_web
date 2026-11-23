package coverage

import (
	"fmt"
	"math"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// NormalizePath 规范化接口路径，保证路径以 / 开头且不保留尾部多余 /。
func NormalizePath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	for strings.Contains(path, "//") {
		path = strings.ReplaceAll(path, "//", "/")
	}
	if len(path) > 1 {
		path = strings.TrimRight(path, "/")
	}
	return path
}

// JoinPaths 拼接 Gin group 前缀和路由路径。
func JoinPaths(prefix, suffix string) string {
	prefix = NormalizePath(prefix)
	suffix = NormalizePath(suffix)
	return NormalizePath(strings.TrimRight(prefix, "/") + "/" + strings.TrimLeft(suffix, "/"))
}

// SplitVersion 拆分 /api/vN 前缀。
// 返回 nil 表示该接口没有显式版本前缀。
func SplitVersion(path string) (*int, string) {
	path = NormalizePath(path)
	matches := versionRE.FindStringSubmatch(path)
	if len(matches) != 3 {
		return nil, path
	}
	version, err := strconv.Atoi(matches[1])
	if err != nil {
		return nil, path
	}
	return &version, NormalizePath(matches[2])
}

// FormatRoute 统一格式化 HTTP method + path，便于 stdout 报告列对齐。
func FormatRoute(key RouteKey) string {
	return fmtRoute(key.Method, key.Path)
}

// ResolveOutputPath 解析报告输出路径。
// 相对路径按仓库根目录解析，绝对路径直接清理后返回。
func ResolveOutputPath(path, repoRoot string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(repoRoot, path))
}

// ResolveConfigPath 解析配置文件路径。
// 黑名单等配置文件默认放在仓库内，因此相对路径按 repoRoot 解析。
func ResolveConfigPath(path, repoRoot string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(filepath.Join(repoRoot, path))
}

// ClassOrder 返回 stdout 报告中路由分类的固定展示顺序。
func ClassOrder() []string {
	return []string{
		ClassVersionedCurrent,
		ClassVersionlessCurrent,
		ClassVersionedSuperseded,
		ClassLegacyShadowed,
		ClassInternalTest,
		ClassDeprecated,
	}
}

// topLevelTestName 将子测试名归并到顶层 Test 函数。
func topLevelTestName(test string) string {
	test = strings.TrimSpace(test)
	if test == "" {
		return ""
	}
	if index := strings.Index(test, "/"); index >= 0 {
		return test[:index]
	}
	return test
}

// routeFamilyKey 用 method + 去版本 tail 识别同一接口族。
func routeFamilyKey(method, tail string) string {
	return strings.ToUpper(method) + " " + NormalizePath(tail)
}

// routeKeySet 把路由 map 转换为 RouteKey 集合。
func routeKeySet(routes map[RouteKey]Route) map[RouteKey]bool {
	result := make(map[RouteKey]bool, len(routes))
	for key := range routes {
		result[key] = true
	}
	return result
}

// intersectKeys 返回两个 RouteKey 集合的交集。
func intersectKeys(left, right map[RouteKey]bool) map[RouteKey]bool {
	result := make(map[RouteKey]bool)
	for key := range left {
		if right[key] {
			result[key] = true
		}
	}
	return result
}

// subtractKeys 返回 left - right。
func subtractKeys(left, right map[RouteKey]bool) map[RouteKey]bool {
	result := make(map[RouteKey]bool)
	for key := range left {
		if !right[key] {
			result[key] = true
		}
	}
	return result
}

// sortedKeys 返回排序后的 RouteKey 列表。
func sortedKeys(keys map[RouteKey]bool) []RouteKey {
	result := make([]RouteKey, 0, len(keys))
	for key := range keys {
		result = append(result, key)
	}
	return sortedRouteKeys(result)
}

// sortedRouteKeysFromTestMap 返回测试集合 map 的排序键列表。
func sortedRouteKeysFromTestMap(items map[RouteKey]map[string]bool) []RouteKey {
	keys := make([]RouteKey, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	return sortedRouteKeys(keys)
}

// sortedRouteKeysFromRouteMap 返回路由 map 的排序键列表。
func sortedRouteKeysFromRouteMap(items map[RouteKey]Route) []RouteKey {
	keys := make([]RouteKey, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	return sortedRouteKeys(keys)
}

// sortedRouteKeysFromReasonMap 返回原因 map 的排序键列表。
func sortedRouteKeysFromReasonMap(items map[RouteKey]string) []RouteKey {
	keys := make([]RouteKey, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	return sortedRouteKeys(keys)
}

// sortedRouteKeys 按 path、method 稳定排序。
func sortedRouteKeys(keys []RouteKey) []RouteKey {
	sort.Slice(keys, func(i, j int) bool {
		return routeLess(keys[i], keys[j])
	})
	return keys
}

// SortRoutes 按 path、method 稳定排序路由列表。
func SortRoutes(routes []Route) {
	sort.Slice(routes, func(i, j int) bool {
		return routeLess(routes[i].Key, routes[j].Key)
	})
}

// routeLess 比较两个 RouteKey，用于稳定输出。
func routeLess(left, right RouteKey) bool {
	if left.Path != right.Path {
		return left.Path < right.Path
	}
	return left.Method < right.Method
}

// sortedStringSet 返回字符串集合的排序结果。
func sortedStringSet(items map[string]bool) []string {
	result := make([]string, 0, len(items))
	for item := range items {
		result = append(result, item)
	}
	sort.Strings(result)
	return result
}

// firstSortedString 返回字符串集合排序后的第一个值。
// 覆盖统计要求一个接口只由一个顶层 Test 函数主测；这里排序后取第一个，仅作为报告展示的兜底。
func firstSortedString(items map[string]bool) string {
	values := sortedStringSet(items)
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

// SortedStringKeys 返回 map 的排序键列表，用于稳定输出 TeamCity 统计项。
func SortedStringKeys(items map[string]string) []string {
	keys := make([]string, 0, len(items))
	for key := range items {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// round2 将浮点数保留两位小数。
func round2(value float64) float64 {
	return math.Round(value*100) / 100
}

// fmtRoute 统一格式化一条路由。
func fmtRoute(method, path string) string {
	return fmt.Sprintf("%-6s %s", strings.ToUpper(method), NormalizePath(path))
}
