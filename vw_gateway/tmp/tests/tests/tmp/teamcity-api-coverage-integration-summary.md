# API 接口覆盖率检测与 TeamCity 集成成果总结

## 背景

项目已经在 TeamCity 中运行 API 自动化测试，原有流水线可以展示测试通过、失败和跳过情况，但很难直接回答接口覆盖层面的问题，例如哪些接口已经有测试覆盖、哪些接口仍未覆盖、覆盖率是否随补充测试持续提升，以及某一次构建中具体有哪些覆盖风险。

为了解决这些问题，现有 API 接口覆盖率检测工具已经接入 TeamCity。该集成不改变 API 测试本身的执行方式，而是在测试完成后基于 `go test -json` 输出生成覆盖率报告，并将结果同步展示到 TeamCity 的 Build Log、Build Statistics、Artifacts 和 HTML Report Tab 中。

## 集成目标

本次集成的目标是让 TeamCity 同时具备“趋势观察”和“单次构建详情查看”两类能力：

- 通过 Build Statistics 持续记录 API 覆盖率、当前接口数、已覆盖接口数、未覆盖接口数等指标。
- 通过 TeamCity 图表查看接口覆盖率随时间变化的趋势。
- 通过 Build Log 快速查看本次构建的覆盖率摘要。
- 通过 Artifact 归档覆盖率 JSON 和 HTML 报告，便于后续下载和追溯。
- 通过 HTML Report Tab 在单次 Build 页面中直接查看完整、可读的覆盖率报告。

## TeamCity 集成方式

### 1. API 测试步骤生成 JSON 输出

API 自动化测试步骤继续使用 `go test -tags api_test -json` 执行，并将测试过程输出保存为 JSON 文件。该 JSON 文件是覆盖率检测步骤的输入来源。

覆盖率检测依赖的是测试执行过程中输出的主测接口标记，因此只要 API 测试步骤生成了 JSON 文件，即使部分测试失败，后续覆盖率检测仍然可以继续运行并给出本次构建的覆盖情况。

### 2. 覆盖率检测步骤生成报告

API 测试步骤之后增加覆盖率检测 Build Step。该步骤读取 API 测试 JSON 文件，生成控制台摘要、结构化 JSON 报告和 HTML 报告。

推荐脚本如下：

```bash
if [ ! -f "$API_JSON" ]; then
  echo "API 测试 JSON 不存在，跳过接口覆盖率检测: $API_JSON"
  exit 0
fi

COVERAGE_DIR="tmp/api-coverage-report"
COVERAGE_JSON="$COVERAGE_DIR/api_coverage_report.json"
COVERAGE_HTML="$COVERAGE_DIR/index.html"

rm -rf "$COVERAGE_DIR"
mkdir -p "$COVERAGE_DIR"

go run ./tests/tools/api_coverage \
  --json "$API_JSON" \
  --output-json "$COVERAGE_JSON" \
  --output-html "$COVERAGE_HTML" \
  --limit 120 \
  --teamcity auto
```

需要注意，目录版工具必须使用 `go run ./tests/tools/api_coverage`。如果写成 `go run tests/tools/api_coverage`，TeamCity Agent 上可能会将其当成标准库路径处理，导致 `package tests/tools/api_coverage is not in std`。

### 3. 输出 TeamCity Build Statistics

覆盖率检测工具会在 TeamCity 环境中输出 service message，将关键指标写入 Build Statistics。当前主要指标包括：

- `apiCoveragePercent`：API 接口覆盖率。
- `apiCurrentRoutes`：当前接口数量。
- `apiCoveredRoutes`：已覆盖接口数量。
- `apiMissingRoutes`：未覆盖接口数量。
- `apiDeprecatedRoutes`：废弃接口数量。
- `apiUnknownPrimaryMarks`：router 中找不到的主测接口标记数量。
- `apiNonCurrentPrimaryMarks`：仍在测试旧接口或非当前接口的数量。
- `apiDuplicatePrimaryRoutes`：重复主测接口数量。

这些指标可以在 TeamCity 中配置为统计图表，用于观察覆盖率是否持续提升、未覆盖接口数量是否持续下降，以及接口总量变化对覆盖率的影响。

为了避免旧数据覆盖新数据，同一个构建中应只保留一个指标上报来源。覆盖率检测步骤已经会直接上报 TeamCity 统计值，因此不建议再额外保留一个独立的“覆盖指标报告”步骤重复读取旧 JSON 并再次上报同名 statistic key。

### 4. 发布 JSON 和 HTML Artifact

覆盖率检测步骤会将生成的报告目录发布为 TeamCity Artifact。推荐将 JSON 和 HTML 放在同一个目录中，例如：

```text
tmp/api-coverage-report/api_coverage_report.json
tmp/api-coverage-report/index.html
```

发布到 TeamCity 后，对应 Artifact 路径建议统一为：

```text
api-coverage/index.html
```

JSON 报告适合归档、排查和后续自动化处理；HTML 报告适合人工阅读和在 Build 结果页中展示。

### 5. 配置 HTML Report Tab

TeamCity 中可以新增一个 Build-level Report Tab，用于直接展示覆盖率 HTML 报告。

推荐配置如下：

```text
Report 类型: Build-level
Tab name: API Coverage
Start page: api-coverage/index.html
```

这里的 Start page 指的是 TeamCity Artifact 中的路径，而不是 Agent 工作目录中的本地路径。因此应填写 `api-coverage/index.html`，而不是 `tmp/api-coverage-report/index.html`。

## TeamCity 安全配置要点

TeamCity 启用 Artifact Domain Isolation 后，自定义 HTML Report Tab 需要正确配置 `Server URL` 和 `Artifacts URL`，否则可能出现“自定义报告被禁用”或“已阻止此内容”的提示。

配置原则如下：

- `Server URL` 必须是用户浏览器实际访问 TeamCity 的地址，不能使用 `localhost`。
- `Artifacts URL` 必须使用与 `Server URL` 不同的 hostname。
- 如果没有正式内网域名，可以临时通过 hosts 或内网 DNS 增加一个 Artifact 专用 hostname。

示例配置：

```text
Server URL:
http://192.168.121.124:8111

Artifacts URL:
http://tc-artifacts.local:8111
```

客户端 hosts 或内网 DNS 中需要配置：

```text
192.168.121.124 tc-artifacts.local
```

配置完成后，TeamCity 主页面通过 `Server URL` 加载，Artifact HTML 通过 `Artifacts URL` 加载，两者 hostname 不同，从而满足 TeamCity 的安全隔离要求。

## 当前展示效果

完成集成后，TeamCity 中可以从四个层面查看 API 覆盖率结果。

### Build Log 摘要

覆盖率检测步骤会在 Build Log 中输出本次构建的摘要信息，例如：

```text
API 自动化接口测试路由覆盖率检测
覆盖来源: 从 go test -json 输出中解析到的 API_MARK
当前接口: 175
已覆盖:   163
未覆盖:   12
已废弃:   16
覆盖率:   93.14%
```

这部分适合在流水线日志中快速判断本次构建的整体覆盖情况。

### Build Statistics 趋势图

TeamCity 会记录覆盖率检测工具输出的自定义指标。基于这些指标，可以配置趋势图查看 API 覆盖率、已覆盖接口数、未覆盖接口数等随构建变化的趋势。

这类图表适合回答以下问题：

- 接口覆盖率是否在持续提升；
- 新增接口后是否及时补充了测试；
- 未覆盖接口数量是否在下降；
- 接口总量变化是否影响了整体覆盖水平。

### Artifact 归档

每次构建都会归档覆盖率 JSON 和 HTML 报告。JSON 文件保留完整结构化数据，适合后续问题排查、数据对比或自动化处理。HTML 文件保留本次构建的人类可读报告，适合直接查看。

Artifact 归档后，即使 Build Log 很长或后续统计图发生变化，仍然可以回到某一次构建中查看当时的完整覆盖率结果。

### HTML Report Tab

HTML Report Tab 是单次构建覆盖率详情的主要查看入口。报告页面会展示：

- 覆盖率摘要和进度条；
- 当前接口、已覆盖接口、未覆盖接口、废弃接口等核心指标；
- 路由分类统计；
- 警告和错误检查结果；
- 未覆盖接口列表；
- 仍在测试旧接口的列表；
- router 中找不到的主测标记；
- 重复主测接口；
- 废弃黑名单接口；
- 已覆盖接口与 `TestXxx` 测试函数的对应关系。

问题类章节默认展开，便于优先处理风险；大清单类章节默认折叠，避免页面过长影响阅读。

## 集成价值

该集成让 API 自动化测试从“只看测试是否通过”升级为“同时看接口覆盖是否完整”。主要价值包括：

- 覆盖情况可视化：团队可以直接看到当前接口覆盖率和未覆盖接口列表。
- 趋势可追踪：TeamCity 可以长期记录覆盖率变化，便于衡量补测试工作的实际效果。
- 问题可定位：HTML 报告能直接指出未覆盖接口、旧接口测试、找不到 router 的标记和重复主测接口。
- 责任更清晰：已覆盖接口会关联到对应 `TestXxx` 测试函数，便于后续维护。
- 构建结果可归档：每次构建的 JSON 和 HTML 报告都会作为 Artifact 保留，方便回溯。
- 流水线更透明：API 测试失败时，只要 JSON 文件存在，仍然可以生成覆盖率信息，帮助判断失败构建中的接口覆盖状态。

## 使用注意事项

- 覆盖率检测步骤应读取当前构建刚生成的 API 测试 JSON 文件，避免读取旧报告或旧测试输出。
- 建议每次生成报告前清理 `tmp/api-coverage-report` 目录，避免残留文件影响判断。
- 同一个构建中只保留一个 TeamCity statistics 上报来源，避免旧数据覆盖新数据。
- 新增 API 测试时，主测接口调用需要正确标记；准备数据、清理数据、轮询状态等辅助接口不要标记为主测接口。
- 一个顶层 `TestXxx` 原则上只主测一个接口，避免重复主测导致覆盖责任不清。
- HTML Report Tab 的 Start page 使用 Artifact 路径，不使用 Agent 本地路径。
- TeamCity 的 `Server URL` 不应配置为 `localhost`，`Artifacts URL` 应使用不同 hostname。

## 总结

当前 API 接口覆盖率检测工具已经完成与 TeamCity 的主要集成：测试执行后自动生成覆盖率摘要、JSON 报告和 HTML 报告，并将关键指标写入 TeamCity Build Statistics。TeamCity 既可以通过趋势图观察覆盖率长期变化，也可以通过 HTML Report Tab 查看单次构建的详细覆盖情况。

这套集成使接口测试维护从人工查看测试文件和日志，转变为由流水线自动产出可视化、可归档、可追踪的覆盖率结果。后续继续补充接口测试时，可以直接通过 TeamCity 观察覆盖率提升效果，并通过 HTML 报告定位剩余未覆盖接口和测试标记问题。
