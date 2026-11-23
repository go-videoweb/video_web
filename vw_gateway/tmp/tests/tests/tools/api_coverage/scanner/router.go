package scanner

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"yssim-go/tests/tools/api_coverage/coverage"
)

// ScanRouterRoutes 从 router/*.go 中提取 Gin 路由。
// 返回的 Route 列表尚未完成新旧版本分类，分类由 coverage.BuildReport 统一处理。
func ScanRouterRoutes(repoRoot string) ([]coverage.Route, error) {
	routerDir := filepath.Join(repoRoot, "router")
	goFiles, err := filepath.Glob(filepath.Join(routerDir, "*.go"))
	if err != nil {
		return nil, fmt.Errorf("list router files: %w", err)
	}
	sortStrings(goFiles)

	// 解析出 routers/*.go 中的所有路由，将解析出的路由信息放入 routesByKey 中。
	routesByKey := make(map[coverage.RouteKey]coverage.Route)
	for _, goFile := range goFiles {
		if err := parseRouterFile(repoRoot, goFile, routesByKey); err != nil {
			return nil, err
		}
	}

	routes := make([]coverage.Route, 0, len(routesByKey))
	for _, route := range routesByKey {
		routes = append(routes, route)
	}
	return routes, nil
}

// parseRouterFile 扫描单个 router Go 文件。
// groups 保存 Gin 分组变量到完整路径前缀的映射，例如 ModelsV2 -> /api/v2/model。
func parseRouterFile(repoRoot, goFile string, routesByKey map[coverage.RouteKey]coverage.Route) error {
	// 第一步：打开 router 文件，并准备相对路径。报告中使用相对路径，便于在 CI 日志中定位源码。
	file, err := os.Open(goFile)
	if err != nil {
		return fmt.Errorf("open router file %s: %w", goFile, err)
	}
	defer file.Close()

	relFile, err := filepath.Rel(repoRoot, goFile)
	if err != nil {
		relFile = goFile
	}
	relFile = filepath.ToSlash(relFile)

	// 第二步：逐行扫描文件，同时维护 Gin group 变量到完整路径前缀的映射。
	// map key 是 group 变量名，例如 g、Models、ModelsV2；value 是该 group 对应的路径前缀。
	// 根对象 g 的路径前缀为空，后续 g.Group("/api/v2/model") 会登记为 /api/v2/model。
	groups := map[string]string{"g": ""}
	scanner := bufio.NewScanner(file)
	lineNo := 0
	for scanner.Scan() {
		lineNo++
		line := scanner.Text()
		stripped := strings.TrimSpace(line)
		if strings.HasPrefix(stripped, "//") { // 跳过单行注释
			continue
		}

		// 第三步：优先识别 group 定义。后续路由只记录在已知 group 上，避免误扫普通方法调用。
		if matches := groupRE.FindStringSubmatch(line); len(matches) == 4 {
			name, base, path := matches[1], matches[2], matches[3]
			groups[name] = coverage.JoinPaths(groups[base], path)
			continue
		}

		// 第四步：识别具体 HTTP 路由定义，并用 group 前缀拼出完整接口路径。
		matches := routeRE.FindStringSubmatch(line)
		if len(matches) != 4 {
			continue
		}
		group, method, path := matches[1], strings.ToUpper(matches[2]), matches[3]
		prefix, exists := groups[group]
		if !exists {
			continue
		}

		// 第五步：登记路由。RouteKey 去重后可避免同一路由声明被重复计入统计。
		fullPath := coverage.JoinPaths(prefix, path)
		version, tail := coverage.SplitVersion(fullPath)
		key := coverage.RouteKey{Method: method, Path: fullPath}
		if _, exists := routesByKey[key]; exists {
			continue
		}
		routesByKey[key] = coverage.Route{
			Key:            key,
			Method:         method,
			Path:           fullPath,
			File:           relFile,
			Line:           lineNo,
			Version:        version,
			Tail:           tail,
			Classification: coverage.ClassUnclassified,
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan router file %s: %w", goFile, err)
	}
	return nil
}
