package main

import (
	"fmt"
	"os"

	"yssim-go/tests/tools/api_coverage/config"
	"yssim-go/tests/tools/api_coverage/coverage"
	"yssim-go/tests/tools/api_coverage/report"
	"yssim-go/tests/tools/api_coverage/scanner"
)

// main 是进程入口，只负责把错误写到 stderr 并转换为进程退出码。
func main() {
	exitCode, err := run(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	os.Exit(exitCode)
}

// run 串联参数解析、覆盖率计算、stdout 输出、JSON 落盘和 TeamCity 输出。
// 返回值：exitCode 为最终进程退出码；err 表示工具自身执行失败，例如文件读取或 JSON 写入失败。
func run(args []string) (int, error) {
	cfg, err := config.Parse(args)
	if err != nil {
		return 2, err
	}

	deprecatedRoutes, err := coverage.LoadDeprecatedRouteBlacklist(cfg.DeprecatedRoutes, cfg.RepoRoot)
	if err != nil {
		return 2, err
	}

	routes, err := scanner.ScanRouterRoutes(cfg.RepoRoot)
	if err != nil {
		return 2, err
	}

	jsonPaths, err := config.ResolveJSONPaths(cfg.JSONPaths)
	if err != nil {
		return 2, err
	}

	var executedMarks []coverage.Mark
	if len(jsonPaths) > 0 {
		executedMarks, err = scanner.ScanExecutedMarks(jsonPaths)
		if err != nil {
			return 2, err
		}
	}

	coverageReport := coverage.BuildReport(cfg.RepoRoot, len(jsonPaths), routes, executedMarks, deprecatedRoutes)
	report.PrintText(coverageReport, cfg.Limit)

	outputJSON := coverage.ResolveOutputPath(cfg.OutputJSON, cfg.RepoRoot)
	if err := report.WriteJSON(outputJSON, coverageReport); err != nil {
		return 2, err
	}
	fmt.Printf("\nJSON report: %s\n", outputJSON)

	outputHTML := ""
	if cfg.OutputHTML != "" {
		outputHTML = coverage.ResolveOutputPath(cfg.OutputHTML, cfg.RepoRoot)
		if err := report.WriteHTML(outputHTML, coverageReport); err != nil {
			return 2, err
		}
		fmt.Printf("HTML report: %s\n", outputHTML)
	}

	hasErrors := len(coverageReport.Errors) > 0
	if report.ShouldEmitTeamCity(cfg.TeamCity) {
		report.PrintTeamCityMessages(coverageReport.Summary, outputJSON, outputHTML, cfg.RepoRoot, hasErrors)
	}
	if hasErrors {
		return 1, nil
	}
	return 0, nil
}
