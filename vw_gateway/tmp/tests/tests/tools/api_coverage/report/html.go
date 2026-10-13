package report

import (
	"bytes"
	"fmt"
	"html/template"
	"os"
	"path/filepath"
	"strings"
	"time"

	"yssim-go/tests/tools/api_coverage/coverage"
)

// htmlMetric 是 HTML 图表中使用的一条指标。
// Width 使用 template.CSS，避免 html/template 在 style 上下文中把合法百分比值转义为不可用内容。
type htmlMetric struct {
	Name  string       // 指标名称。
	Value int          // 指标数值。
	Width template.CSS // 相对最大值计算得到的条形图宽度，例如 72.04%。
}

// htmlReportView 是 HTML 模板使用的视图模型。
// 它只保存展示层所需的衍生数据，避免模板直接承担百分比和排序计算。
type htmlReportView struct {
	GeneratedAt           string
	Report                coverage.CoverageReport
	CoverageWidth         template.CSS
	SummaryMetrics        []htmlMetric
	RouteClassMetrics     []htmlMetric
	NonCurrentRoutes      []coverage.Route
	HasWarnings           bool
	HasErrors             bool
	HasNonCurrentMarks    bool
	HasNonCurrentRoutes   bool
	HasUnknownMarks       bool
	HasDuplicatePrimaries bool
	HasDeprecatedRoutes   bool
	HasMissingRoutes      bool
	HasCoveredRoutes      bool
}

// WriteHTML 将覆盖率报告写入静态 HTML 文件。
// HTML 报告不依赖外部 CDN、字体或脚本，便于在 TeamCity artifact/report tab 中稳定展示。
func WriteHTML(outputPath string, report coverage.CoverageReport) error {
	outputPath = strings.TrimSpace(outputPath)
	if outputPath == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("create html report directory: %w", err)
	}

	view := buildHTMLReportView(report)
	tmpl, err := template.New("api-coverage").Parse(apiCoverageHTMLTemplate)
	if err != nil {
		return fmt.Errorf("parse html report template: %w", err)
	}

	var buffer bytes.Buffer
	if err := tmpl.Execute(&buffer, view); err != nil {
		return fmt.Errorf("render html report: %w", err)
	}
	if err := os.WriteFile(outputPath, buffer.Bytes(), 0o644); err != nil {
		return fmt.Errorf("write html report %s: %w", outputPath, err)
	}
	return nil
}

// buildHTMLReportView 根据覆盖率报告生成 HTML 展示模型。
func buildHTMLReportView(report coverage.CoverageReport) htmlReportView {
	nonCurrentRoutes := buildNonCurrentRoutes(report.Routes)
	return htmlReportView{
		GeneratedAt:           time.Now().Format("2006-01-02 15:04:05"),
		Report:                report,
		CoverageWidth:         percentWidth(report.Summary.CoveragePercent),
		SummaryMetrics:        buildSummaryMetrics(report.Summary),
		RouteClassMetrics:     buildRouteClassMetrics(report.RouteClassification),
		NonCurrentRoutes:      nonCurrentRoutes,
		HasWarnings:           len(report.Warnings) > 0,
		HasErrors:             len(report.Errors) > 0,
		HasNonCurrentMarks:    len(report.NonCurrentPrimaryMarks) > 0,
		HasNonCurrentRoutes:   len(nonCurrentRoutes) > 0,
		HasUnknownMarks:       len(report.UnknownPrimaryMarks) > 0,
		HasDuplicatePrimaries: len(report.DuplicatePrimaryTests) > 0,
		HasDeprecatedRoutes:   len(report.DeprecatedRoutes) > 0,
		HasMissingRoutes:      len(report.MissingCurrentRoutes) > 0,
		HasCoveredRoutes:      len(report.CoveredCurrentRoutes) > 0,
	}
}

// buildNonCurrentRoutes 提取 router 中仍然存在、但不属于当前接口统计口径的旧接口。
// 该列表用于 HTML 展示接口演进现状，不影响覆盖率计算。
func buildNonCurrentRoutes(routes []coverage.Route) []coverage.Route {
	result := make([]coverage.Route, 0)
	for _, route := range routes {
		if route.Classification == coverage.ClassVersionedSuperseded || route.Classification == coverage.ClassLegacyShadowed {
			result = append(result, route)
		}
	}
	return result
}

// buildSummaryMetrics 生成摘要条形图数据。
func buildSummaryMetrics(summary coverage.Summary) []htmlMetric {
	items := []htmlMetric{
		{Name: "当前接口", Value: summary.CurrentRoutes},
		{Name: "已覆盖接口", Value: summary.CoveredRoutes},
		{Name: "未覆盖接口", Value: summary.MissingRoutes},
		{Name: "废弃接口", Value: summary.DeprecatedRoutes},
		{Name: "警告", Value: summary.WarningCount},
		{Name: "错误", Value: summary.ErrorCount},
	}
	fillMetricWidths(items)
	return items
}

// buildRouteClassMetrics 生成路由分类条形图数据。
func buildRouteClassMetrics(classCounts map[string]int) []htmlMetric {
	items := make([]htmlMetric, 0, len(coverage.ClassOrder()))
	for _, className := range coverage.ClassOrder() {
		items = append(items, htmlMetric{
			Name:  routeClassDisplayName(className),
			Value: classCounts[className],
		})
	}
	fillMetricWidths(items)
	return items
}

// fillMetricWidths 按最大值填充条形图宽度。
func fillMetricWidths(items []htmlMetric) {
	maxValue := 0
	for _, item := range items {
		if item.Value > maxValue {
			maxValue = item.Value
		}
	}
	for index := range items {
		items[index].Width = ratioWidth(items[index].Value, maxValue)
	}
}

// routeClassDisplayName 将内部分类名转换为 HTML 报告中的中文名称。
func routeClassDisplayName(className string) string {
	switch className {
	case coverage.ClassVersionedCurrent:
		return "带版本当前接口"
	case coverage.ClassVersionlessCurrent:
		return "无版本当前接口"
	case coverage.ClassVersionedSuperseded:
		return "低版本接口"
	case coverage.ClassLegacyShadowed:
		return "已有新版替代的旧接口"
	case coverage.ClassInternalTest:
		return "内部测试接口"
	case coverage.ClassDeprecated:
		return "废弃黑名单接口"
	default:
		return className
	}
}

// ratioWidth 根据 value/max 生成 CSS 百分比宽度。
func ratioWidth(value, maxValue int) template.CSS {
	if maxValue <= 0 || value <= 0 {
		return template.CSS("0%")
	}
	return percentWidth(float64(value) / float64(maxValue) * 100)
}

// percentWidth 把百分比数值裁剪到 0 到 100 之间，并格式化为 CSS 百分比。
func percentWidth(value float64) template.CSS {
	if value < 0 {
		value = 0
	}
	if value > 100 {
		value = 100
	}
	return template.CSS(fmt.Sprintf("%.2f%%", value))
}

const apiCoverageHTMLTemplate = `<!doctype html>
<html lang="zh-CN">
<head>
  <meta charset="utf-8">
  <meta name="viewport" content="width=device-width, initial-scale=1">
  <title>API 接口覆盖率报告</title>
  <style>
    :root {
      color-scheme: light;
      --bg: #f6f7f9;
      --panel: #ffffff;
      --text: #172033;
      --muted: #657185;
      --line: #dce1e8;
      --brand: #2563eb;
      --brand-soft: #dbeafe;
      --ok: #0f9f6e;
      --warn: #b45309;
      --bad: #dc2626;
      --shadow: 0 1px 2px rgba(15, 23, 42, 0.08);
    }
    * { box-sizing: border-box; }
    body {
      margin: 0;
      background: var(--bg);
      color: var(--text);
      font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", "Microsoft YaHei", sans-serif;
      font-size: 14px;
      line-height: 1.5;
    }
    main {
      max-width: 1280px;
      margin: 0 auto;
      padding: 24px;
    }
    header {
      display: flex;
      justify-content: space-between;
      gap: 16px;
      align-items: flex-end;
      margin-bottom: 20px;
    }
    h1, h2, h3 { margin: 0; line-height: 1.2; }
    h1 { font-size: 26px; }
    h2 { font-size: 18px; margin-bottom: 14px; }
    h3 { font-size: 15px; margin-bottom: 10px; }
    .meta { color: var(--muted); text-align: right; }
    .grid {
      display: grid;
      grid-template-columns: repeat(4, minmax(0, 1fr));
      gap: 12px;
      margin-bottom: 16px;
    }
    .card, section, details.panel {
      background: var(--panel);
      border: 1px solid var(--line);
      border-radius: 8px;
      box-shadow: var(--shadow);
    }
    .card { padding: 16px; }
    .label { color: var(--muted); font-size: 13px; }
    .value { font-size: 28px; font-weight: 700; margin-top: 6px; }
    .value.ok { color: var(--ok); }
    .value.warn { color: var(--warn); }
    .value.bad { color: var(--bad); }
    section { padding: 18px; margin-top: 16px; }
    details.panel { margin-top: 16px; }
    details.panel > summary {
      display: flex;
      justify-content: space-between;
      align-items: center;
      gap: 12px;
      padding: 16px 18px;
      cursor: pointer;
      list-style: none;
      user-select: none;
    }
    details.panel > summary::-webkit-details-marker { display: none; }
    .summary-title {
      display: flex;
      align-items: baseline;
      gap: 10px;
      min-width: 0;
    }
    .summary-title h2 { margin: 0; }
    .summary-count {
      color: var(--muted);
      font-size: 13px;
      white-space: nowrap;
    }
    .summary-toggle {
      border: 1px solid var(--line);
      border-radius: 6px;
      color: var(--muted);
      padding: 3px 9px;
      font-size: 12px;
      white-space: nowrap;
    }
    .summary-toggle::before { content: "展开"; }
    details[open] > summary .summary-toggle::before { content: "收起"; }
    .panel-body {
      padding: 0 18px 18px;
    }
    .two-col {
      display: grid;
      grid-template-columns: minmax(0, 1fr) minmax(0, 1fr);
      gap: 16px;
    }
    .progress {
      height: 16px;
      background: #e8edf4;
      border-radius: 999px;
      overflow: hidden;
      margin: 14px 0 6px;
    }
    .progress > div {
      height: 100%;
      background: linear-gradient(90deg, var(--ok), var(--brand));
    }
    .bar-row {
      display: grid;
      grid-template-columns: 170px minmax(0, 1fr) 64px;
      gap: 12px;
      align-items: center;
      margin: 10px 0;
    }
    .bar-label { color: var(--muted); overflow-wrap: anywhere; }
    .bar-track {
      height: 12px;
      background: #e8edf4;
      border-radius: 999px;
      overflow: hidden;
    }
    .bar-fill {
      height: 100%;
      background: var(--brand);
      border-radius: 999px;
    }
    .bar-value { text-align: right; font-variant-numeric: tabular-nums; }
    .notice {
      padding: 10px 12px;
      border-radius: 6px;
      border: 1px solid var(--line);
      background: #f8fafc;
      color: var(--muted);
    }
    .notice.warn { border-color: #f59e0b; background: #fffbeb; color: #92400e; }
    .notice.bad { border-color: #f87171; background: #fef2f2; color: #991b1b; }
    table {
      width: 100%;
      border-collapse: collapse;
      table-layout: fixed;
    }
    th, td {
      border-bottom: 1px solid var(--line);
      padding: 9px 10px;
      text-align: left;
      vertical-align: top;
    }
    th {
      color: var(--muted);
      font-weight: 600;
      background: #f8fafc;
    }
    td { overflow-wrap: anywhere; }
    .method { width: 90px; font-family: ui-monospace, SFMono-Regular, Consolas, monospace; }
    .path { font-family: ui-monospace, SFMono-Regular, Consolas, monospace; }
    .test { font-family: ui-monospace, SFMono-Regular, Consolas, monospace; }
    .line { width: 72px; font-variant-numeric: tabular-nums; }
    .tag {
      display: inline-block;
      padding: 2px 7px;
      border-radius: 999px;
      background: var(--brand-soft);
      color: #1d4ed8;
      font-size: 12px;
      white-space: nowrap;
    }
    .stack { display: grid; gap: 8px; }
    @media (max-width: 900px) {
      main { padding: 16px; }
      header { display: block; }
      .meta { text-align: left; margin-top: 8px; }
      .grid, .two-col { grid-template-columns: 1fr; }
      .bar-row { grid-template-columns: 1fr; gap: 6px; }
      .bar-value { text-align: left; }
    }
  </style>
</head>
<body>
<main>
  <header>
    <div>
      <h1>API 接口覆盖率报告</h1>
      <div class="label">覆盖来源：{{.Report.CoverageSource}}</div>
    </div>
    <div class="meta">
      <div>生成时间：{{.GeneratedAt}}</div>
      <div>{{.Report.RepoRoot}}</div>
    </div>
  </header>

  <div class="grid">
    <div class="card"><div class="label">覆盖率</div><div class="value ok">{{printf "%.2f" .Report.Summary.CoveragePercent}}%</div></div>
    <div class="card"><div class="label">当前接口</div><div class="value">{{.Report.Summary.CurrentRoutes}}</div></div>
    <div class="card"><div class="label">已覆盖接口</div><div class="value ok">{{.Report.Summary.CoveredRoutes}}</div></div>
    <div class="card"><div class="label">未覆盖接口</div><div class="value warn">{{.Report.Summary.MissingRoutes}}</div></div>
  </div>

  <section>
    <h2>覆盖概览</h2>
    <div class="progress"><div style="width: {{.CoverageWidth}}"></div></div>
    <div class="label">已覆盖 {{.Report.Summary.CoveredRoutes}} / {{.Report.Summary.CurrentRoutes}} 个当前接口</div>
  </section>

  <section class="two-col">
    <div>
      <h2>核心指标</h2>
      {{range .SummaryMetrics}}
      <div class="bar-row">
        <div class="bar-label">{{.Name}}</div>
        <div class="bar-track"><div class="bar-fill" style="width: {{.Width}}"></div></div>
        <div class="bar-value">{{.Value}}</div>
      </div>
      {{end}}
    </div>
    <div>
      <h2>路由分类</h2>
      {{range .RouteClassMetrics}}
      <div class="bar-row">
        <div class="bar-label">{{.Name}}</div>
        <div class="bar-track"><div class="bar-fill" style="width: {{.Width}}"></div></div>
        <div class="bar-value">{{.Value}}</div>
      </div>
      {{end}}
    </div>
  </section>

  <details class="panel" open>
    <summary>
      <div class="summary-title"><h2>检查结果</h2><span class="summary-count">{{.Report.Summary.WarningCount}} 个警告 / {{.Report.Summary.ErrorCount}} 个错误</span></div>
      <span class="summary-toggle"></span>
    </summary>
    <div class="panel-body stack">
      {{if .HasErrors}}{{range .Report.Errors}}<div class="notice bad">{{.}}</div>{{end}}{{else}}<div class="notice">没有硬错误。</div>{{end}}
      {{if .HasWarnings}}{{range .Report.Warnings}}<div class="notice warn">{{.}}</div>{{end}}{{else}}<div class="notice">没有警告。</div>{{end}}
    </div>
  </details>

  <details class="panel" open>
    <summary>
      <div class="summary-title"><h2>未覆盖当前接口</h2><span class="summary-count">{{len .Report.MissingCurrentRoutes}} 条</span></div>
      <span class="summary-toggle"></span>
    </summary>
    <div class="panel-body">
    {{if .HasMissingRoutes}}
    <table>
      <thead><tr><th class="method">Method</th><th>Path</th></tr></thead>
      <tbody>
      {{range .Report.MissingCurrentRoutes}}<tr><td class="method">{{.Method}}</td><td class="path">{{.Path}}</td></tr>{{end}}
      </tbody>
    </table>
    {{else}}<div class="notice">没有未覆盖当前接口。</div>{{end}}
    </div>
  </details>

  <details class="panel" open>
    <summary>
      <div class="summary-title"><h2>仍在测试旧接口</h2><span class="summary-count">{{len .Report.NonCurrentPrimaryMarks}} 条</span></div>
      <span class="summary-toggle"></span>
    </summary>
    <div class="panel-body">
    {{if .HasNonCurrentMarks}}
    <table>
      <thead><tr><th class="method">Method</th><th>Path</th><th>分类</th><th>最新接口</th></tr></thead>
      <tbody>
      {{range .Report.NonCurrentPrimaryMarks}}
      <tr><td class="method">{{.Method}}</td><td class="path">{{.Path}}</td><td><span class="tag">{{.Classification}}</span></td><td class="path">{{.LatestPath}}</td></tr>
      {{end}}
      </tbody>
    </table>
    {{else}}<div class="notice">没有测试函数仍在主测旧接口。</div>{{end}}
    </div>
  </details>

  <details class="panel">
    <summary>
      <div class="summary-title"><h2>非当前路由清单</h2><span class="summary-count">{{len .NonCurrentRoutes}} 条</span></div>
      <span class="summary-toggle"></span>
    </summary>
    <div class="panel-body">
    {{if .HasNonCurrentRoutes}}
    <table>
      <thead><tr><th class="method">Method</th><th>Path</th><th>分类</th><th>最新接口</th><th>文件</th><th class="line">行号</th></tr></thead>
      <tbody>
      {{range .NonCurrentRoutes}}
      <tr><td class="method">{{.Method}}</td><td class="path">{{.Path}}</td><td><span class="tag">{{.Classification}}</span></td><td class="path">{{.LatestPath}}</td><td>{{.File}}</td><td class="line">{{.Line}}</td></tr>
      {{end}}
      </tbody>
    </table>
    {{else}}<div class="notice">没有非当前路由。</div>{{end}}
    </div>
  </details>

  <details class="panel" open>
    <summary>
      <div class="summary-title"><h2>router 中找不到的主测标记</h2><span class="summary-count">{{len .Report.UnknownPrimaryMarks}} 条</span></div>
      <span class="summary-toggle"></span>
    </summary>
    <div class="panel-body">
    {{if .HasUnknownMarks}}
    <table>
      <thead><tr><th class="method">Method</th><th>Path</th></tr></thead>
      <tbody>
      {{range .Report.UnknownPrimaryMarks}}<tr><td class="method">{{.Method}}</td><td class="path">{{.Path}}</td></tr>{{end}}
      </tbody>
    </table>
    {{else}}<div class="notice">没有 router 中找不到的主测标记。</div>{{end}}
    </div>
  </details>

  <details class="panel" open>
    <summary>
      <div class="summary-title"><h2>重复主测接口</h2><span class="summary-count">{{len .Report.DuplicatePrimaryTests}} 条</span></div>
      <span class="summary-toggle"></span>
    </summary>
    <div class="panel-body">
    {{if .HasDuplicatePrimaries}}
    <table>
      <thead><tr><th class="method">Method</th><th>Path</th><th>测试函数</th></tr></thead>
      <tbody>
      {{range .Report.DuplicatePrimaryTests}}
      <tr><td class="method">{{.Method}}</td><td class="path">{{.Path}}</td><td class="test">{{range .Tests}}{{.}}<br>{{end}}</td></tr>
      {{end}}
      </tbody>
    </table>
    {{else}}<div class="notice">没有重复主测接口。</div>{{end}}
    </div>
  </details>

  <details class="panel">
    <summary>
      <div class="summary-title"><h2>废弃黑名单接口</h2><span class="summary-count">{{len .Report.DeprecatedRoutes}} 条</span></div>
      <span class="summary-toggle"></span>
    </summary>
    <div class="panel-body">
    {{if .HasDeprecatedRoutes}}
    <table>
      <thead><tr><th class="method">Method</th><th>Path</th><th>原因</th></tr></thead>
      <tbody>
      {{range .Report.DeprecatedRoutes}}<tr><td class="method">{{.Method}}</td><td class="path">{{.Path}}</td><td>{{.Reason}}</td></tr>{{end}}
      </tbody>
    </table>
    {{else}}<div class="notice">没有废弃黑名单接口。</div>{{end}}
    </div>
  </details>

  <details class="panel">
    <summary>
      <div class="summary-title"><h2>已覆盖当前接口</h2><span class="summary-count">{{len .Report.CoveredCurrentRoutes}} 条</span></div>
      <span class="summary-toggle"></span>
    </summary>
    <div class="panel-body">
    {{if .HasCoveredRoutes}}
    <table>
      <thead><tr><th class="method">Method</th><th>Path</th><th>Test 函数</th></tr></thead>
      <tbody>
      {{range .Report.CoveredCurrentRoutes}}<tr><td class="method">{{.Method}}</td><td class="path">{{.Path}}</td><td class="test">{{.Test}}</td></tr>{{end}}
      </tbody>
    </table>
    {{else}}<div class="notice">没有已覆盖当前接口。</div>{{end}}
    </div>
  </details>
</main>
</body>
</html>
`
