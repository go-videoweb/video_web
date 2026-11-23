package coverage

import "fmt"

// BuildReport 执行所有统计步骤，并生成供 stdout、JSON、TeamCity 共用的报告对象。
// routes 来自 router 扫描，executedMarks 来自 go test -json 中的 API_MARK。
func BuildReport(repoRoot string, jsonInputCount int, routes []Route, executedMarks []Mark, deprecatedRoutes map[RouteKey]string) CoverageReport {
	classifyRoutes(routes, deprecatedRoutes)
	SortRoutes(routes)

	routeByKey := make(map[RouteKey]Route, len(routes))
	currentRoutes := make(map[RouteKey]bool)
	classCounts := map[string]int{
		ClassVersionedCurrent:    0,
		ClassVersionlessCurrent:  0,
		ClassVersionedSuperseded: 0,
		ClassLegacyShadowed:      0,
		ClassInternalTest:        0,
		ClassDeprecated:          0,
	}
	for _, route := range routes {
		routeByKey[route.Key] = route
		classCounts[route.Classification]++
		if route.Classification == ClassVersionedCurrent || route.Classification == ClassVersionlessCurrent {
			currentRoutes[route.Key] = true
		}
	}

	// 从测试结果文件提取的 API_MARK 中获取已执行主测接口，再与当前路由集合做交集。
	executedPrimary := keysByRole(executedMarks, RolePrimary)
	covered := intersectKeys(executedPrimary, currentRoutes)
	missing := subtractKeys(currentRoutes, covered)
	unknown := subtractKeys(executedPrimary, routeKeySet(routeByKey))
	nonCurrentRouteByKey := make(map[RouteKey]Route)
	for key := range executedPrimary {
		route, exists := routeByKey[key]
		if exists && !currentRoutes[key] {
			nonCurrentRouteByKey[key] = route
		}
	}

	duplicates, unboundCount := findDuplicatePrimaryTests(executedMarks)
	coverageSource := "未提供 API_MARK 输入（未传入 --json）"
	if jsonInputCount > 0 {
		coverageSource = "从 go test -json 输出中解析到的 API_MARK"
	}

	coveragePercent := 100.0
	if len(currentRoutes) > 0 {
		coveragePercent = float64(len(covered)) / float64(len(currentRoutes)) * 100
	}

	warnings := buildWarnings(jsonInputCount, len(unknown), len(nonCurrentRouteByKey), unboundCount, buildDeprecatedBlacklistWarnings(deprecatedRoutes, routeByKey))
	errorsList := buildErrors(len(duplicates))

	summary := Summary{
		CurrentRoutes:          len(currentRoutes),
		CoveredRoutes:          len(covered),
		MissingRoutes:          len(missing),
		DeprecatedRoutes:       classCounts[ClassDeprecated],
		CoveragePercent:        round2(coveragePercent),
		UnknownPrimaryMarks:    len(unknown),
		NonCurrentPrimaryMarks: len(nonCurrentRouteByKey),
		DuplicatePrimaryRoutes: len(duplicates),
		UnboundPrimaryMarks:    unboundCount,
		WarningCount:           len(warnings),
		ErrorCount:             len(errorsList),
	}

	return CoverageReport{
		RepoRoot:               repoRoot,
		CoverageSource:         coverageSource,
		Summary:                summary,
		RouteClassification:    classCounts,
		Warnings:               warnings,
		Errors:                 errorsList,
		CoveredCurrentRoutes:   buildCoveredRouteReports(covered, executedMarks),
		MissingCurrentRoutes:   sortedKeys(missing),
		UnknownPrimaryMarks:    sortedKeys(unknown),
		NonCurrentPrimaryMarks: buildNonCurrentReports(nonCurrentRouteByKey),
		DuplicatePrimaryTests:  buildDuplicateReports(duplicates),
		DeprecatedRoutes:       buildDeprecatedReports(routes, deprecatedRoutes),
		Routes:                 routes,
	}
}

// buildWarnings 根据统计结果生成警告文本。
// 警告不会让脚本返回非 0，但会写入 stdout 和 JSON，便于后续人工处理。
func buildWarnings(jsonCount, unknownCount, nonCurrentCount, unboundPrimaryMarkCount int, extraWarnings []string) []string {
	var warnings []string
	if jsonCount == 0 {
		warnings = append(warnings, "未提供 --json 文件，本次覆盖统计使用空的 API_MARK 集合")
	}
	if unknownCount > 0 {
		warnings = append(warnings, fmt.Sprintf("存在 router 声明中找不到的主测接口标记：%d 个", unknownCount))
	}
	if nonCurrentCount > 0 {
		warnings = append(warnings, fmt.Sprintf("存在仍在测试旧接口的主测标记：%d 个", nonCurrentCount))
	}
	if unboundPrimaryMarkCount > 0 {
		warnings = append(warnings, fmt.Sprintf("存在无法关联到 go test Test 字段的主测接口标记：%d 个", unboundPrimaryMarkCount))
	}
	warnings = append(warnings, extraWarnings...)
	return warnings
}

// buildErrors 根据统计结果生成硬错误文本。
// 目前只有“同一接口被多个顶层 Test 函数主测”会让脚本返回非 0。
func buildErrors(duplicatePrimaryRouteCount int) []string {
	if duplicatePrimaryRouteCount == 0 {
		return nil
	}
	return []string{
		fmt.Sprintf("存在被多个顶层 Test 函数重复标记为主测接口的路由：%d 个", duplicatePrimaryRouteCount),
	}
}

// buildNonCurrentReports 把旧接口标记明细转换为 JSON 报告结构。
func buildNonCurrentReports(routeByKey map[RouteKey]Route) []NonCurrentMarkReport {
	keys := sortedRouteKeysFromRouteMap(routeByKey)
	reports := make([]NonCurrentMarkReport, 0, len(keys))
	for _, key := range keys {
		route := routeByKey[key]
		reports = append(reports, NonCurrentMarkReport{
			Method:         key.Method,
			Path:           key.Path,
			Classification: route.Classification,
			LatestPath:     route.LatestPath,
		})
	}
	return reports
}

// buildCoveredRouteReports 生成“接口 -> 主测 Test 函数”的覆盖明细。
// 正常情况下一个接口只会对应一个顶层 Test 函数；如果测试违反该约束，重复明细由 duplicate_primary_tests 报告。
func buildCoveredRouteReports(covered map[RouteKey]bool, marks []Mark) []CoveredRouteReport {
	testsByRoute := make(map[RouteKey]map[string]bool)
	for _, mark := range marks {
		if mark.Role != RolePrimary || !covered[mark.Key] {
			continue
		}

		testName := topLevelTestName(mark.Test)
		if testName == "" {
			continue
		}
		if testsByRoute[mark.Key] == nil {
			testsByRoute[mark.Key] = make(map[string]bool)
		}
		testsByRoute[mark.Key][testName] = true
	}

	keys := sortedRouteKeysFromTestMap(testsByRoute)
	reports := make([]CoveredRouteReport, 0, len(keys))
	for _, key := range keys {
		reports = append(reports, CoveredRouteReport{
			Method: key.Method,
			Path:   key.Path,
			Test:   firstSortedString(testsByRoute[key]),
		})
	}
	return reports
}

// buildDuplicateReports 把重复主测接口集合转换为 JSON 报告结构。
func buildDuplicateReports(duplicates map[RouteKey]map[string]bool) []DuplicatePrimaryReport {
	keys := sortedRouteKeysFromTestMap(duplicates)
	reports := make([]DuplicatePrimaryReport, 0, len(keys))
	for _, key := range keys {
		reports = append(reports, DuplicatePrimaryReport{
			Method: key.Method,
			Path:   key.Path,
			Tests:  sortedStringSet(duplicates[key]),
		})
	}
	return reports
}

// keysByRole 过滤指定角色的 Mark 并转换为 RouteKey 集合。
func keysByRole(marks []Mark, role string) map[RouteKey]bool {
	result := make(map[RouteKey]bool)
	for _, mark := range marks {
		if mark.Role == role {
			result[mark.Key] = true
		}
	}
	return result
}

// findDuplicatePrimaryTests 检测是否有多个顶层测试函数标记了同一个主测接口。
// 返回值 duplicates 映射 RouteKey -> Test 函数集合；unboundCount 表示缺少 Test 字段的 primary 标记数量。
func findDuplicatePrimaryTests(marks []Mark) (map[RouteKey]map[string]bool, int) {
	testsByRoute := make(map[RouteKey]map[string]bool)
	unboundCount := 0
	for _, mark := range marks {
		if mark.Role != RolePrimary {
			continue
		}
		testName := topLevelTestName(mark.Test)
		if testName == "" {
			unboundCount++
			continue
		}
		if testsByRoute[mark.Key] == nil {
			testsByRoute[mark.Key] = make(map[string]bool)
		}
		testsByRoute[mark.Key][testName] = true
	}

	duplicates := make(map[RouteKey]map[string]bool)
	for key, tests := range testsByRoute {
		if len(tests) > 1 {
			duplicates[key] = tests
		}
	}
	return duplicates, unboundCount
}
