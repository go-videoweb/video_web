# Codex Handoff: API 覆盖率工具与 TeamCity HTML 报告

## 当前任务目标

继续维护 `ModelingAndSimulation-tc-ci-test` 工作树中的 API 接口测试与接口覆盖率检测工具。当前重点已经从“补接口测试”转到“让 TeamCity 更好地展示 API 覆盖率检测结果”，包括 JSON/HTML 报告、TeamCity 自定义指标、Report Tab、以及相关工作文档整理。

## 必须遵守的约束

- 必须在 `tc-ci-test` 分支工作；开始前先执行 `git branch --show-current`。
- 默认只修改 `tests` 或 `.codex/skills` 相关内容；不要改业务代码，除非用户明确要求。
- 不要暂存或提交任何文件，尤其不要暂存 Modelica 相关文件。
- API 测试必须带 `-tags api_test`；普通单元测试不带该 tag。
- 新增或修改测试函数后，必须至少运行对应 `TestXxx`；大范围改动再跑全量。
- API 主测接口标记规则：主测接口调用使用 `testClient.Primary().Xxx()`、`PrimaryGet()` 或 `PrimaryPost()`；准备/清理/轮询等辅助接口不能标记为主测接口。
- 新增函数/方法必须写注释；复杂逻辑内部也要写简短说明。
- 通用测试 helper 应集中到 `tests/helpers`，不要在测试文件中散落重复工具函数。

## 已完成工作

### API 覆盖率检测工具

目录版工具位于 `tests/tools/api_coverage/`，旧单文件 `tests/tools/api_coverage.go` 保留为备份，不删除。

新目录结构：

- `tests/tools/api_coverage/main.go`：命令入口，只负责编排流程。
- `tests/tools/api_coverage/config/config.go`：参数解析，包含 `--json`、`--output-json`、`--output-html`、`--limit`、`--deprecated-routes`、`--teamcity`。
- `tests/tools/api_coverage/coverage/`：核心类型、路由分类、废弃黑名单、覆盖率计算。
- `tests/tools/api_coverage/scanner/`：扫描 `router/*.go` 路由、解析 `go test -json` 中的 `API_MARK`。
- `tests/tools/api_coverage/report/`：文本报告、JSON 报告、HTML 报告、TeamCity service message。

工具能力：

- 静态扫描 `router/*.go`，按 `HTTP Method + Path` 统计后端路由。
- 解析 `go test -json` 输出中的 `API_MARK role=primary method=... path=...`。
- 判断当前接口、被高版本替代接口、无版本旧接口、内部测试接口、废弃黑名单接口。
- 统计已覆盖/未覆盖当前接口、仍在测试旧接口、router 中找不到的主测标记、重复主测接口。
- 检测到“同一接口被多个顶层 Test 函数主测”时作为错误返回非 0。
- 输出控制台文本、JSON 和 HTML 报告。
- TeamCity 下输出 `buildStatisticValue`，用于折线图趋势。

### HTML 报告

新增 `tests/tools/api_coverage/report/html.go`：

- 通过 `--output-html` 生成静态 HTML。
- 不依赖 CDN、远程字体或外部 JS，适合内网 TeamCity。
- 使用同一份 `coverage.CoverageReport` 渲染，避免 JSON/控制台/HTML 口径不一致。
- 报告内容包括覆盖率摘要、覆盖进度条、核心指标、路由分类、检查结果、未覆盖接口、仍在测试旧接口、非当前路由清单、router 中找不到的主测标记、重复主测接口、废弃黑名单接口、已覆盖接口与 `TestXxx` 对应关系。
- 数据量大的章节使用原生 `details/summary` 折叠；问题类章节默认展开，大清单默认折叠。
- 报告中的 warning/error 文本已中文化。

### TeamCity 集成

推荐覆盖率检测 Build Step：

```bash
if [ ! -f "$API_JSON" ]; then
  echo "API 测试 JSON 不存在，跳过接口覆盖率检测: $API_JSON"
  exit 0
fi

COVERAGE_DIR="tmp/api-coverage-report"
COVERAGE_JSON="$COVERAGE_DIR/api_coverage_report.json"
COVERAGE_HTML="$COVERAGE_DIR/index.html"
mkdir -p "$COVERAGE_DIR"

go run ./tests/tools/api_coverage \
  --json "$API_JSON" \
  --output-json "$COVERAGE_JSON" \
  --output-html "$COVERAGE_HTML" \
  --limit 120 \
  --teamcity auto
```

注意：目录版必须写 `go run ./tests/tools/api_coverage`，不能写 `go run tests/tools/api_coverage`，否则 TeamCity Agent 上会报 `package tests/tools/api_coverage is not in std`。

Report Tab 配置：

- Artifact path / Start page：`api-coverage/index.html`
- Tab name：如 `API Coverage` 或 `API 接口测试覆盖率报告`
- Report 类型：Build-level

安全策略问题已明确：TeamCity 启用 Artifact Domain Isolation 后，自定义 HTML Report Tab 必须配置 Artifacts URL。`Server URL` 不能是 `http://localhost:8111`，因为它是给浏览器客户端使用的外部访问地址。当前建议：

- `Server URL`: `http://192.168.121.124:8111`
- `Artifacts URL`: 使用另一个 hostname，例如 `http://tc-artifacts.local:8111`
- 客户端 hosts 或内网 DNS：`192.168.121.124 tc-artifacts.local`

## 已执行验证及结果

使用本地已有全量 API 测试 JSON `tmp/ApiTestFull_20260804_112454.jsonl` 验证目录版工具。

已执行：

- `go test ./tests/tools/api_coverage/...`：通过。
- `go test ./tests/tools/...`：通过。
- `go run ./tests/tools/api_coverage --json tmp/ApiTestFull_20260804_112454.jsonl --output-json tmp/api-coverage-html-check/api_coverage_report.json --output-html tmp/api-coverage-html-check/index.html --limit 5 --teamcity always`：通过。
- `git diff --check -- tests/tools/api_coverage`：只有 CRLF 换行提示，无内容格式错误。

最近一次用旧 JSON 生成覆盖统计的结果：

- 当前接口：175
- 已覆盖：134
- 未覆盖：41
- 废弃接口：16
- 覆盖率：76.57%
- router 中找不到的主测接口标记：1 个，`GET /api/v2/simulation/log/get`
- 仍在测试旧接口：3 个
- 重复主测接口：0

示例 HTML 输出：`tmp/api-coverage-html-check/index.html`。

## 关键文件/函数

- `tests/helpers/client.go`
  - `MarkAPI`
  - `APIClient.Primary`
  - `PrimaryGet`
  - `PrimaryPost`
  - 这些是 `API_MARK` 的来源。
- `tests/tools/api_coverage/main.go`
  - `run`
- `tests/tools/api_coverage/config/config.go`
  - `Config`
  - `Parse`
  - `ResolveJSONPaths`
- `tests/tools/api_coverage/coverage/build.go`
  - `BuildReport`
  - `buildWarnings`
  - `buildErrors`
- `tests/tools/api_coverage/coverage/classify.go`
  - `classifyRoutes`
- `tests/tools/api_coverage/coverage/deprecated.go`
  - `LoadDeprecatedRouteBlacklist`
  - `buildDeprecatedBlacklistWarnings`
- `tests/tools/api_coverage/scanner/router.go`
  - `ScanRouterRoutes`
- `tests/tools/api_coverage/scanner/mark.go`
  - `ScanExecutedMarks`
- `tests/tools/api_coverage/report/html.go`
  - `WriteHTML`
  - `buildHTMLReportView`
- `tests/tools/api_coverage/report/teamcity.go`
  - `PrintTeamCityMessages`
  - `coverageArtifactPaths`
- `tests/api/api_deprecated_routes.json`
  - 废弃接口黑名单。

## 重要技术决策及原因

- 覆盖率统计只使用动态 `API_MARK`，不再静态统计测试中显式调用。原因：静态统计很难区分主测接口和准备/清理辅助接口。
- 一个顶层 `TestXxx` 原则上只主测一个接口。原因：便于统计和维护覆盖责任，重复主测会被工具报错。
- HTML 报告只做展示，不重新计算数据。原因：确保控制台、JSON、HTML 同口径。
- HTML 使用纯静态页面和原生折叠控件。原因：TeamCity artifact/report tab 环境更稳定，避免外部依赖。
- TeamCity 统计 key 保持英文，如 `apiCoveragePercent`。原因：TeamCity 图表配置依赖这些稳定 key；仅人读文本改中文。

## 已知问题

- `GET /api/v2/simulation/log/get` 的主测标记在 router 静态扫描中找不到。需要后续确认：路由是否不存在、注册方式未被扫描规则识别，还是测试标记路径过期。
- 仍有 3 个测试主测旧接口：
  - `POST /api/v2/simulation/result` -> 最新 `/api/v3/simulation/result`
  - `POST /api/v2/simulation/result/singular` -> 最新 `/api/v3/simulation/result/singular`
  - `GET /user/userspace/get` -> 最新 `/api/v2/user/userspace/get`
- TeamCity HTML Report Tab 是否可见依赖服务器 Global Settings 中的 `Server URL` / `Artifacts URL` 和客户端 DNS/hosts。若仍显示“已阻止此内容”，优先检查这两个 URL 和 artifact path。
- 当前工作区有不少既有未提交/未暂存变更，不能随意还原。开始新工作前务必 `git status --short`。

## 已完成的外部文档

位于 `C:\Users\MSI\Documents\Obsidian Vault\个人\工作成果`：

- `api 接口测试覆盖情况检测工具.md`
- `TeamCity 集成 HTML 覆盖率报告.md`

这些是用户的工作成果文档，位于仓库外；若继续更新，需要写入外部路径权限。

## 尚未完成的工作

- 若用户继续要求，需要进一步优化 HTML 报告，例如按业务模块分组、搜索/筛选、锚点导航。
- 需要决定是否把 TeamCity HTML 集成说明同步到项目内文档或 Skill 中。
- 需要跟进 TeamCity Report Tab 实际是否能在服务器配置修正后正常展示。
- 可能需要为目录版 `api_coverage` 工具补单元测试，特别是路由分类、黑名单、HTML 渲染和 TeamCity artifact path。
- 旧单文件 `tests/tools/api_coverage.go` 暂时保留为备份；后续是否删除由用户决定。

## 明确下一步

新会话开始后：

1. 确认分支为 `tc-ci-test`。
2. 读取 `.codex/skills/yslab-api-test/SKILL.md`。
3. 执行 `git status --short`，了解已有变更，不能还原用户改动。
4. 如果继续 TeamCity/HTML 方向，优先确认用户是否已经能看到 Report Tab。
5. 如果要验证工具，运行：
   - `go test ./tests/tools/...`
   - 用最新 `go test -json` 文件运行 `go run ./tests/tools/api_coverage --json ... --output-json ... --output-html ... --teamcity never/always`

