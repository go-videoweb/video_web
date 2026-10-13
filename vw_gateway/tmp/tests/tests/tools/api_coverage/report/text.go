package report

import (
	"fmt"

	"yssim-go/tests/tools/api_coverage/coverage"
)

// PrintText 输出人类可读的覆盖率报告。
// 这个输出会进入 TeamCity build log，因此保留具体明细用于失败时直接定位。
func PrintText(report coverage.CoverageReport, limit int) {
	fmt.Println("API 接口覆盖率报告")
	fmt.Printf("仓库路径: %s\n", report.RepoRoot)
	fmt.Println("\n路由分类:")
	for _, name := range coverage.ClassOrder() {
		fmt.Printf("  %-22s %d\n", name, report.RouteClassification[name])
	}

	fmt.Printf("\n覆盖来源: %s\n", report.CoverageSource)
	fmt.Printf("当前接口: %d\n", report.Summary.CurrentRoutes)
	fmt.Printf("已覆盖:   %d\n", report.Summary.CoveredRoutes)
	fmt.Printf("未覆盖:   %d\n", report.Summary.MissingRoutes)
	fmt.Printf("已废弃:   %d\n", report.Summary.DeprecatedRoutes)
	fmt.Printf("覆盖率:   %.2f%%\n", report.Summary.CoveragePercent)

	fmt.Println("\n警告:")
	if len(report.Warnings) == 0 {
		fmt.Println("  无")
	} else {
		for _, warning := range report.Warnings {
			fmt.Printf("  - %s\n", warning)
		}
	}

	if len(report.Errors) > 0 {
		fmt.Println("\n错误:")
		for _, item := range report.Errors {
			fmt.Printf("  - %s\n", item)
		}
	}

	if len(report.NonCurrentPrimaryMarks) > 0 {
		fmt.Println("\n仍在测试旧接口:")
		for _, item := range report.NonCurrentPrimaryMarks {
			suffix := ""
			if item.LatestPath != "" {
				suffix = fmt.Sprintf(" -> 最新接口 %s", item.LatestPath)
			}
			fmt.Printf("  %s  [%s]%s\n", coverage.FormatRoute(coverage.RouteKey{Method: item.Method, Path: item.Path}), item.Classification, suffix)
		}
	}

	printCoveredRouteTests(report.CoveredCurrentRoutes, limit)
	printDuplicatePrimaryTests(report.DuplicatePrimaryTests)
	printDeprecatedRoutes(report.DeprecatedRoutes, limit)
	printRouteList("router 中找不到的主测标记", report.UnknownPrimaryMarks, limit)
	printRouteList("未覆盖当前接口", report.MissingCurrentRoutes, limit)
}

// printCoveredRouteTests 输出已覆盖接口与主测 Test 函数的对应关系。
func printCoveredRouteTests(routes []coverage.CoveredRouteReport, limit int) {
	fmt.Printf("\n已覆盖当前接口与 Test 函数 (%d):\n", len(routes))
	if len(routes) == 0 {
		fmt.Println("  无")
		return
	}

	shown := routes
	if limit > 0 && len(routes) > limit {
		shown = routes[:limit]
	}
	for _, item := range shown {
		fmt.Printf("  %s\n", coverage.FormatRoute(coverage.RouteKey{Method: item.Method, Path: item.Path}))
		fmt.Printf("    - %s\n", item.Test)
	}
	if limit > 0 && len(routes) > limit {
		fmt.Printf("  ... 还有 %d 条\n", len(routes)-limit)
	}
}

// printDuplicatePrimaryTests 输出同一接口被多个 Test 函数标记的明细。
func printDuplicatePrimaryTests(duplicates []coverage.DuplicatePrimaryReport) {
	fmt.Println("\n被多个 Test 函数重复标记的主测接口:")
	if len(duplicates) == 0 {
		fmt.Println("  无")
		return
	}
	for _, item := range duplicates {
		fmt.Printf("  %s\n", coverage.FormatRoute(coverage.RouteKey{Method: item.Method, Path: item.Path}))
		for _, testName := range item.Tests {
			fmt.Printf("    - %s\n", testName)
		}
	}
}

// printDeprecatedRoutes 输出被黑名单排除的接口，避免“被跳过”变成不可见信息。
func printDeprecatedRoutes(routes []coverage.DeprecatedRouteReport, limit int) {
	fmt.Printf("\n废弃黑名单接口 (%d):\n", len(routes))
	if len(routes) == 0 {
		fmt.Println("  无")
		return
	}

	shown := routes
	if limit > 0 && len(routes) > limit {
		shown = routes[:limit]
	}
	for _, route := range shown {
		fmt.Printf("  %s  # %s\n", coverage.FormatRoute(coverage.RouteKey{Method: route.Method, Path: route.Path}), route.Reason)
	}
	if limit > 0 && len(routes) > limit {
		fmt.Printf("  ... 还有 %d 条\n", len(routes)-limit)
	}
}

// printRouteList 输出一组路由列表，并按 limit 截断。
// limit <= 0 表示不截断。
func printRouteList(title string, keys []coverage.RouteKey, limit int) {
	fmt.Printf("\n%s (%d):\n", title, len(keys))
	if len(keys) == 0 {
		fmt.Println("  无")
		return
	}

	shown := keys
	if limit > 0 && len(keys) > limit {
		shown = keys[:limit]
	}
	for _, key := range shown {
		fmt.Printf("  %s\n", coverage.FormatRoute(key))
	}
	if limit > 0 && len(keys) > limit {
		fmt.Printf("  ... 还有 %d 条\n", len(keys)-limit)
	}
}
