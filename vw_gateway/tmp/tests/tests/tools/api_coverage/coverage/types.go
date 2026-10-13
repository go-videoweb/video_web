package coverage

// RouteKey 是接口覆盖统计的唯一键。
// 同一路径不同 HTTP method 视为不同接口，例如 GET /x 与 POST /x 会分别统计。
type RouteKey struct {
	Method string `json:"method"` // HTTP 方法，例如 GET、POST。
	Path   string `json:"path"`   // 接口完整路径，例如 /api/v2/user/userspace/get。
}

// Route 表示从 router/*.go 静态扫描得到的一条 Gin 路由。
// Version 为 nil 表示该路由没有 /api/vN 前缀；Tail 是去掉版本前缀后的路径。
type Route struct {
	Key            RouteKey `json:"-"`                     // 内部使用的接口唯一键，不写入 JSON，避免与 method/path 重复。
	Method         string   `json:"method"`                // 从 Gin 路由调用中解析出的 HTTP 方法。
	Path           string   `json:"path"`                  // 拼接 Gin group 前缀后的完整路由路径。
	File           string   `json:"file"`                  // 该路由所在的 router/*.go 相对路径。
	Line           int      `json:"line"`                  // 该路由定义在源文件中的行号，用于定位 router 声明。
	Version        *int     `json:"version"`               // /api/vN 中的 N；无 /api/vN 前缀时为 nil。
	Tail           string   `json:"tail"`                  // 去掉 /api/vN 后的路径，用于判断不同版本是否属于同一接口族。
	Classification string   `json:"classification"`        // 路由分类，例如当前接口、旧接口或被高版本替代的接口。
	LatestPath     string   `json:"latest_path,omitempty"` // 旧接口或低版本接口对应的最新接口路径。
}

// Mark 表示测试运行时打印出的接口覆盖标记。
// Test 来自 go test -json 的 Test 字段，用于判断哪个顶层 Test 函数主测了该接口。
type Mark struct {
	Key  RouteKey // 测试运行时标记到的接口。
	Role string   // 接口标记角色；当前只把 primary 计入主测覆盖率。
	Test string   // go test -json 中的 Test 字段，用于追溯哪个 Test 函数标记了该接口。
}

// TestEvent 只定义 go test -json 中本工具关心的字段。
// 其余字段由 encoding/json 忽略，避免跟随 Go 版本扩展频繁调整结构体。
type TestEvent struct {
	Test   string `json:"Test"`   // Go 测试事件所属的测试函数或子测试名称。
	Output string `json:"Output"` // 该测试事件输出的文本，API_MARK 会从这里解析。
}

// Summary 是报告中最常被 TeamCity 和人工阅读使用的统计摘要。
type Summary struct {
	CurrentRoutes          int     `json:"current_routes"`            // 当前应统计的新接口总数。
	CoveredRoutes          int     `json:"covered_routes"`            // 已被 primary API_MARK 覆盖的当前接口数量。
	MissingRoutes          int     `json:"missing_routes"`            // 当前接口中尚未被 primary API_MARK 覆盖的数量。
	DeprecatedRoutes       int     `json:"deprecated_routes"`         // 被废弃接口黑名单排除、不参与覆盖统计的路由数量。
	CoveragePercent        float64 `json:"coverage_percent"`          // CoveredRoutes / CurrentRoutes * 100，保留两位小数。
	UnknownPrimaryMarks    int     `json:"unknown_primary_marks"`     // 测试标记了但 router 静态扫描不到的接口数量。
	NonCurrentPrimaryMarks int     `json:"non_current_primary_marks"` // 测试标记到旧接口或低版本接口的数量。
	DuplicatePrimaryRoutes int     `json:"duplicate_primary_routes"`  // 被多个顶层 Test 函数同时标记为主测接口的数量。
	UnboundPrimaryMarks    int     `json:"unbound_primary_marks"`     // 无法从 go test -json 事件中关联到 Test 字段的 primary 标记数量。
	WarningCount           int     `json:"warning_count"`             // 本次统计产生的警告数量。
	ErrorCount             int     `json:"error_count"`               // 本次统计产生的硬错误数量；大于 0 时脚本返回非 0。
}

// NonCurrentMarkReport 保存“测试标记了旧接口”的明细。
// LatestPath 用于直接提示应迁移到哪个当前接口。
type NonCurrentMarkReport struct {
	Method         string `json:"method"`                // 被测试标记到的旧接口 HTTP 方法。
	Path           string `json:"path"`                  // 被测试标记到的旧接口路径。
	Classification string `json:"classification"`        // 旧接口分类，例如 legacy_shadowed 或 versioned_superseded。
	LatestPath     string `json:"latest_path,omitempty"` // 该接口族当前应使用的最新路径。
}

// CoveredRouteReport 保存“当前接口由哪个测试函数覆盖”的明细。
// Test 只保留顶层 Test 函数名，例如 TestA/正常场景 会归并为 TestA。
type CoveredRouteReport struct {
	Method string `json:"method"` // 已覆盖接口的 HTTP 方法。
	Path   string `json:"path"`   // 已覆盖接口的路径。
	Test   string `json:"test"`   // 标记该接口为主测接口的顶层 Test 函数名。
}

// DuplicatePrimaryReport 保存“同一接口被多个顶层 Test 函数主测”的明细。
type DuplicatePrimaryReport struct {
	Method string   `json:"method"` // 重复主测接口的 HTTP 方法。
	Path   string   `json:"path"`   // 重复主测接口的路径。
	Tests  []string `json:"tests"`  // 标记了同一主测接口的顶层 Test 函数列表。
}

// DeprecatedRouteReport 保存被废弃接口黑名单排除的路由明细。
type DeprecatedRouteReport struct {
	Method string `json:"method"` // 被排除接口的 HTTP 方法。
	Path   string `json:"path"`   // 被排除接口的路径。
	Reason string `json:"reason"` // 排除原因，便于后续审查黑名单是否仍合理。
}

// DeprecatedRouteEntry 是废弃接口黑名单配置文件中的单条记录。
// 它与报告结构分开定义，避免未来报告字段扩展时影响配置文件格式。
type DeprecatedRouteEntry struct {
	Method string `json:"method"` // 黑名单接口的 HTTP 方法，例如 GET、POST。
	Path   string `json:"path"`   // 黑名单接口完整路径，例如 /api/v3/simulation/state/get。
	Reason string `json:"reason"` // 接口废弃或跳过覆盖统计的原因。
}

// CoverageReport 是 JSON 报告文件的顶层结构。
// stdout 文本报告和 TeamCity service message 也从同一份结构派生，避免三处统计逻辑不一致。
type CoverageReport struct {
	RepoRoot               string                   `json:"repo_root"`                 // 本次统计使用的仓库根目录。
	CoverageSource         string                   `json:"coverage_source"`           // 覆盖率来源，例如 go test -json 中的 API_MARK。
	Summary                Summary                  `json:"summary"`                   // 最常用的统计摘要，TeamCity 自定义统计值也来自这里。
	RouteClassification    map[string]int           `json:"route_classification"`      // 每种路由分类的数量。
	Warnings               []string                 `json:"warnings"`                  // 不会导致脚本失败、但需要关注的问题。
	Errors                 []string                 `json:"errors"`                    // 会导致脚本返回非 0 的硬错误。
	CoveredCurrentRoutes   []CoveredRouteReport     `json:"covered_current_routes"`    // 已覆盖接口与主测 Test 函数的对应关系。
	MissingCurrentRoutes   []RouteKey               `json:"missing_current_routes"`    // 当前接口中尚未覆盖的列表。
	UnknownPrimaryMarks    []RouteKey               `json:"unknown_primary_marks"`     // 测试标记了但 router 中找不到的接口列表。
	NonCurrentPrimaryMarks []NonCurrentMarkReport   `json:"non_current_primary_marks"` // 测试标记到旧接口或低版本接口的明细。
	DuplicatePrimaryTests  []DuplicatePrimaryReport `json:"duplicate_primary_tests"`   // 同一接口被多个顶层 Test 函数标记的明细。
	DeprecatedRoutes       []DeprecatedRouteReport  `json:"deprecated_routes"`         // 被黑名单排除、不参与覆盖统计的路由明细。
	Routes                 []Route                  `json:"routes"`                    // 从 router/*.go 静态扫描出的全部路由及其分类。
}
