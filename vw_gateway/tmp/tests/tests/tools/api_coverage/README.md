# API 接口覆盖率检测工具

`api_coverage` 用于在 API 接口测试运行后统计接口覆盖情况。它会静态扫描 `router/*.go` 中声明的接口，再读取 `go test -json` 输出中的 `API_MARK`，生成文本、JSON、可选 HTML 报告，并在 TeamCity 中输出 service messages。

## 统计口径

- 接口唯一键为 `HTTP Method + Path`。
- 同一路径不同 method 视为不同接口。
- 只有当前接口参与覆盖率计算；旧版本接口、被高版本替代接口、内部测试接口和废弃黑名单接口会单独分类。
- 覆盖来源是接口测试运行时输出的 `API_MARK`。
- 只有 `Primary()`、`PrimaryGet()`、`PrimaryPost()` 等主测标记会计入覆盖率。
- 同一接口被多个顶层 `Test*` 标记为主测接口会被视为硬错误，工具返回非 0。

## 典型流程

1. 运行 API 接口测试，并保存 `go test -json` 输出。
2. 运行 `api_coverage` 读取 JSONL 测试结果。
3. 查看 stdout 文本报告、JSON 报告和可选 HTML 报告。
4. 在 TeamCity 中发布报告 artifact，并记录覆盖率统计值。

示例：

```powershell
$result = "tmp/ApiTestFull.jsonl"
go test -tags api_test -json ./tests/api/... | Tee-Object -FilePath $result

go run ./tests/tools/api_coverage \
  -json $result \
  -output-json "tmp/api_coverage_report.json" \
  -output-html "tmp/api_coverage_html/index.html"
```

## 参数说明

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `-repo-root` | 自动推导仓库根目录 | 仓库根目录，用于扫描 router 和解析相对路径。 |
| `-json` | 空 | `go test -json` 输出文件；可传多次。 |
| `-limit` | `80` | stdout 中每个明细列表最多展示多少条；`0` 表示不截断。 |
| `-output-json` | `tmp/api_coverage_report.json` | 覆盖率 JSON 报告路径；相对路径基于仓库根目录。 |
| `-output-html` | 空 | 覆盖率 HTML 报告路径；为空时不生成 HTML。 |
| `-deprecated-routes` | `tests/api/api_deprecated_routes.json` | 废弃接口黑名单 JSON 路径。 |
| `-teamcity` | `auto` | 是否输出 TeamCity service messages：`auto`、`always`、`never`。 |

同一个 `-json` 可以传多次：

```bash
go run ./tests/tools/api_coverage \
  -json tmp/api_part1.jsonl \
  -json tmp/api_part2.jsonl
```

## 输出内容

### stdout 文本报告

工具会输出人类可读报告，包含：

- 路由分类数量
- 当前接口数、已覆盖数、未覆盖数、废弃接口数、覆盖率
- 警告和错误
- 仍在测试旧接口的主测标记
- 已覆盖接口与顶层 `Test*` 函数对应关系
- 重复主测接口
- 废弃黑名单接口
- router 中找不到的主测标记
- 未覆盖当前接口

### JSON 报告

默认写入：

```text
tmp/api_coverage_report.json
```

JSON 顶层结构为 `CoverageReport`，主要字段包括：

- `summary`：覆盖率摘要，TeamCity 指标也来自这里。
- `route_classification`：路由分类数量。
- `warnings`：不会导致失败、但需要关注的问题。
- `errors`：会导致工具返回非 0 的硬错误。
- `covered_current_routes`：当前接口与主测测试函数的对应关系。
- `missing_current_routes`：尚未覆盖的当前接口。
- `unknown_primary_marks`：测试标记了但 router 中找不到的接口。
- `non_current_primary_marks`：仍在测试旧接口或低版本接口的标记。
- `duplicate_primary_tests`：同一接口被多个顶层测试函数主测的明细。
- `deprecated_routes`：废弃黑名单接口。
- `routes`：静态扫描出的全部路由及分类。

### HTML 报告

传入 `-output-html` 时生成静态 HTML 报告，例如：

```bash
go run ./tests/tools/api_coverage \
  -json tmp/api_test.jsonl \
  -output-html tmp/api_coverage_html/index.html
```

HTML 报告不依赖外部 CDN、字体或脚本，适合在 TeamCity artifact/report tab 中展示。

## TeamCity 集成

`-teamcity` 支持三种模式：

- `auto`：检测到 `TEAMCITY_VERSION` 环境变量时输出 TeamCity service messages。
- `always`：始终输出 TeamCity service messages。
- `never`：不输出 TeamCity service messages。

TeamCity service messages 包括：

- `buildStatisticValue`：记录覆盖率和数量指标。
- `publishArtifacts`：发布 JSON/HTML 报告 artifact。
- `buildProblem`：当存在重复主测接口等硬错误时标记构建问题。

当前输出的主要统计 key：

```text
apiCoveragePercent
apiCurrentRoutes
apiCoveredRoutes
apiMissingRoutes
apiDeprecatedRoutes
apiUnknownPrimaryMarks
apiNonCurrentPrimaryMarks
apiDuplicatePrimaryRoutes
```

TeamCity 示例：

```bash
go run ./tests/tools/api_coverage \
  -json tmp/api_test.jsonl \
  -output-json tmp/api_coverage_report.json \
  -output-html tmp/api_coverage_html/index.html \
  -teamcity always
```

## 退出码

| 退出码 | 含义 |
| --- | --- |
| `0` | 工具运行成功，未发现硬错误。 |
| `1` | 工具运行成功，但覆盖率报告中存在硬错误，例如重复主测接口。 |
| `2` | 参数解析、文件读取、router 扫描、JSON 解析或报告写入失败。 |

## 警告与错误

警告不会导致非 0 退出码，常见场景包括：

- 未传入 `-json`，覆盖统计使用空的 `API_MARK` 集合。
- 存在 router 中找不到的主测接口标记。
- 存在仍在测试旧接口的主测标记。
- 存在无法关联到 `go test -json` Test 字段的主测标记。
- 废弃接口黑名单中存在无效或过期配置。

硬错误会导致退出码为 `1`，当前主要场景是：

- 同一接口被多个顶层 `Test*` 函数重复标记为主测接口。

## 废弃接口黑名单

默认读取：

```text
tests/api/api_deprecated_routes.json
```

格式示例：

```json
[
  {
    "method": "GET",
    "path": "/api/v3/simulation/state/get",
    "reason": "已由 /user/userspace/change/poll 替代"
  }
]
```

黑名单接口会从当前覆盖率统计中排除，但会在报告中显示，避免“被跳过”的接口不可见。

## 维护注意事项

- 新增 API 测试时，主测接口必须使用 `Primary()`、`PrimaryGet()` 或 `PrimaryPost()` 标记。
- 准备数据、清理数据、辅助查询接口不要使用 `Primary()`，否则会污染覆盖统计。
- 一个顶层 `Test*` 原则上只标记一个主测接口。
- 如果覆盖报告显示 `unknown_primary_marks`，优先检查测试中标记的 method/path 是否与 router 声明一致。
- 如果覆盖报告显示 `non_current_primary_marks`，优先迁移测试到最新接口。
- 如果覆盖报告显示 `duplicate_primary_tests`，应拆分或调整测试，保证一个接口只有一个顶层主测测试函数。
- 修改 router 分类、废弃判断或 TeamCity 输出时，应同时检查 JSON、stdout、HTML 和 TeamCity service messages 是否保持一致。

## 本地验证

运行工具包测试：

```bash
go test ./tests/tools/api_coverage/...
```

只做静态覆盖扫描、不传测试 JSON：

```bash
go run ./tests/tools/api_coverage -teamcity never
```

这会生成报告，但会提示未提供 `-json`，因此覆盖来源为空的 `API_MARK` 集合。
