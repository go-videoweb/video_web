package config

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"strings"
)

// Config 保存命令行参数解析后的运行配置。
type Config struct {
	RepoRoot         string   // 仓库根目录，用于扫描 router/*.go 和解析相对输出路径。
	JSONPaths        []string // 一个或多个 go test -json 输出文件。
	Limit            int      // stdout 中每个明细列表最多展示多少条；0 表示不截断。
	OutputJSON       string   // 覆盖率 JSON 报告输出路径。
	OutputHTML       string   // 覆盖率 HTML 报告输出路径；为空时不生成 HTML。
	DeprecatedRoutes string   // 废弃接口黑名单 JSON 文件路径。
	TeamCity         string   // 是否输出 TeamCity service messages，可选 auto、always、never。
}

// stringListFlag 支持同一个命令行参数传入多次。
// 例如：--json a.jsonl --json b.jsonl 会被收集为两个输入文件。
type stringListFlag []string

// String 返回 flag 包需要的展示值。
func (f *stringListFlag) String() string {
	return strings.Join(*f, ",")
}

// Set 追加一个命令行传入值。
func (f *stringListFlag) Set(value string) error {
	*f = append(*f, value)
	return nil
}

// Parse 解析命令行参数，并给出与旧版单文件工具保持一致的默认值。
// --repo-root 默认通过当前源码文件位置推导为 tests/tools/api_coverage/../..，因此从仓库根目录 go run 也无需显式传参。
func Parse(args []string) (Config, error) {
	var jsonPaths stringListFlag
	defaultRoot, err := defaultRepoRoot()
	if err != nil {
		return Config{}, err
	}

	fs := flag.NewFlagSet("api_coverage", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	repoRoot := fs.String("repo-root", defaultRoot, "repository root, default: this source file directory ../../../..")
	limit := fs.Int("limit", 80, "max routes to print per section; 0 means no limit")
	outputJSON := fs.String("output-json", "tmp/api_coverage_report.json", "coverage report JSON path, relative paths are under repo root")
	outputHTML := fs.String("output-html", "", "coverage report HTML path; empty means no HTML report")
	deprecatedRoutes := fs.String("deprecated-routes", "tests/api/api_deprecated_routes.json", "deprecated route blacklist JSON path, relative paths are under repo root")
	teamCity := fs.String("teamcity", "auto", "emit TeamCity service messages: auto, always, or never")
	fs.Var(&jsonPaths, "json", "go test -json output file; can be passed more than once")

	if err := fs.Parse(args); err != nil {
		return Config{}, err
	}
	if *teamCity != "auto" && *teamCity != "always" && *teamCity != "never" {
		return Config{}, fmt.Errorf("invalid --teamcity value %q, want auto, always, or never", *teamCity)
	}

	absRepoRoot, err := filepath.Abs(*repoRoot)
	if err != nil {
		return Config{}, fmt.Errorf("resolve repo root: %w", err)
	}
	return Config{
		RepoRoot:         filepath.Clean(absRepoRoot),
		JSONPaths:        jsonPaths,
		Limit:            *limit,
		OutputJSON:       *outputJSON,
		OutputHTML:       *outputHTML,
		DeprecatedRoutes: *deprecatedRoutes,
		TeamCity:         *teamCity,
	}, nil
}

// ResolveJSONPaths 将命令行传入的 go test -json 路径转换为绝对路径。
// 覆盖率报告只关心已解析后的输入文件，避免后续模块重复处理相对路径。
func ResolveJSONPaths(paths []string) ([]string, error) {
	jsonPaths := make([]string, 0, len(paths))
	for _, path := range paths {
		absPath, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("resolve json path %q: %w", path, err)
		}
		jsonPaths = append(jsonPaths, filepath.Clean(absPath))
	}
	return jsonPaths, nil
}

// defaultRepoRoot 使用 runtime.Caller 获取本 Go 源文件路径，再向上四级定位仓库根目录。
// 这样 `go run ./tests/tools/api_coverage` 和在 tests/tools/api_coverage 目录内直接运行都能得到同一默认根目录。
func defaultRepoRoot() (string, error) {
	_, sourceFile, _, ok := runtime.Caller(0)
	if !ok {
		return "", errors.New("cannot determine api_coverage config source path")
	}
	if !filepath.IsAbs(sourceFile) {
		abs, err := filepath.Abs(sourceFile)
		if err != nil {
			return "", fmt.Errorf("resolve source path: %w", err)
		}
		sourceFile = abs
	}
	return filepath.Clean(filepath.Join(filepath.Dir(sourceFile), "../../../..")), nil
}
