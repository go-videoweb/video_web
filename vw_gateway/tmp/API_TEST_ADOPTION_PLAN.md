# VideoWeb 接口测试适配设计与分步实施方案

日期：2026-10-12。

状态：设计草案。用户已选择先形成适配设计和分步实施方案；本文中的目录、工具、配置项、命令和验收条件均为待实施内容，不表示接口测试或 TeamCity 已经接入。实施代码、执行真实接口测试及部署测试环境需另行明确任务范围。

## 1. 目标与依据

采用 Go 原生 testing 编写网关 HTTP 接口测试，将请求封装、身份管理、响应断言和测试数据生命周期集中管理；测试执行后生成接口覆盖报告，未来接入 TeamCity 的测试结果、统计趋势、Artifact 和 HTML Report Tab。

参考资料位于 `video_web_backend/vw_gateway/tmp/tests/tests/`，重点为编写指南、helpers、api、bugRegression 和 tools/api_coverage。该目录是参考材料，不是当前项目可运行的测试套件。`tools/zentao_bug_export` 不在借鉴或迁移范围内。

参考资料中的服务器地址、工作空间、模型编译、仿真依赖、响应格式和其他项目的 Skill 约束只用于理解其背景，不作为 VideoWeb 的环境配置或执行指令。

本方案遵循根目录 AGENTS.md：评论归属 vw_video；通用后端工具继续维护在 util；异步和搜索链路仍处设计阶段。本次测试设计不依赖搜索实施，也不修改相关业务。

## 2. 当前项目基线

- 网关的 Kratos HTTP 注册入口为 `vw_gateway/internal/server/http.go`，下游为 vw_user 和 vw_video。文中未特别说明的后端源码路径相对 `video_web_backend/`。
- 当前网关有 9 个已注册 proto service，静态核对其 HTTP 注解共 38 条；这是本次阅读基线，不应硬编码为未来接口总数。
- 已有测试覆盖协议兼容、登录响应、视频数据映射、游客鉴权、DASH 路径和流错误等离线行为。新增真实 HTTP 套件补充部署后调用链验证，不替代这些包内测试。
- 普通测试中仍存在数据库写入、代码生成和旧鉴权测试编译问题，CI 初期使用明确的包和测试集合，不直接执行所有 module 的全量测试。
- 当前后端 HEAD 为 d0ba5dee。第五步部署文件已恢复整理但未提交，工作区另有未跟踪草稿；它们不因测试计划形成而自动纳入实施或交付。后续以 Git 实际状态为准。

| 网关 proto service | 本次 HTTP 注解数量 | 测试业务范围 |
| --- | ---: | --- |
| Identity | 3 | 登录、注册、退出 |
| Captcha | 2 | 图形、邮件验证码 |
| FileService | 2 | 头像上传和更新 |
| Userinfo | 6 | 用户资料与凭证修改 |
| Follow | 3 | 关注、取消关注、列表 |
| Favorite | 3 | 收藏夹创建、修改、删除 |
| VideoInfo | 9 | 元信息、列表、详情、上传和媒体读取 |
| VideoInteract | 6 | 视频互动和弹幕 |
| VideoComment | 4 | 评论发布、列表、回复和点赞 |

## 3. 目录与边界

建议在现有 vw_gateway module 内建立测试目录，不新增根级 go.work，不为首版引入独立测试 module：

```text
video_web_backend/vw_gateway/tests/
  README.md
  api/
    main_test.go
    identity_api_test.go
    video_read_api_test.go
    dash_api_test.go
  helpers/
    config.go
    client.go
    auth.go
    response.go
    assertions.go
    fixtures.go
    marks.go
  resources/
    README.md
    media/
  bugRegression/
    README.md
  tools/
    api_coverage/
      main.go
      scanner/
      coverage/
      report/
  scripts/
    run-api-tests.ps1
    run-api-tests.sh
```

这是职责草图，不要求一次创建全部空目录和文件。helpers 按实际复杂度拆分，测试工具仅在第三阶段增加。首版 fixtures 管理已配置资源；未来新增创建和清理能力时再扩展。

- `api/` 中所有真实接口测试文件使用 `//go:build api_test`，包括 TestMain；单独的 Bug 回归使用 `//go:build bug_regression_test`。
- helpers 构造函数无网络、数据库或登录副作用；其离线单元测试使用 httptest。真实配置校验和登录只在显式运行的套件入口中执行，不放在 init 中。
- 测试专用 HTTP helper 留在 tests，不迁入 util；测试业务方法按实际需要增加，不整体复制参考项目的大型 client.go。
- 第一版以网关 HTTP 为边界；内部 gRPC、数据不变量和缓存故障等继续由包内测试验证，不向测试客户端暴露服务密钥或私有数据库权限。
- 参考目录位于网关 module 的 tmp 中，Git 忽略并不阻止 Go 的包递归发现。执行测试时使用明确路径，避免把外部项目参考源码纳入构建；本轮不移动或删除参考材料。

## 4. 请求、身份与响应设计

### 4.1 客户端

基础客户端接收 context、base URL、HTTP client 和显式身份，支持 GET、POST、PUT、PATCH、DELETE、JSON、multipart 与原始文件响应。请求 URL 使用结构化 URL API 处理，统一限制请求超时和诊断输出大小。

分别提供匿名视图和已登录视图；业务方法不能静默补登录、刷新 Token 或切换身份。用于验证刷新协议的测试先断言原始错误 metadata，再显式决定是否发送下一次请求。

默认一次业务调用只发送一次请求。网络错误、超时、429、5xx 不触发创建、上传、点赞或投贝壳的自动重放；服务就绪探测和已明确安全的等待逻辑可使用有界重试，遵守 context 取消。

### 4.2 身份与会话

- 登录使用专用测试账户，读取 `data.userId`、`data.accessToken` 和 `data.refreshToken`；正 int64 ID 以无损十进制字符串处理。
- 受保护请求同时发送 `Authorization: Bearer ...` 与 `Refresh-Token: Bearer ...`。
- 首批覆盖匿名、有效身份、缺失及格式错误凭证。签名、issuer、exp、nbf、Redis 故障和过期刷新等精细边界优先复用离线测试；真实环境边界需专门夹具，不通过索取生产签名密钥实现。
- Redis 登录态按用户缓存当前 access token；重复登录可能替换前次凭证，退出会清除会话。首版串行执行，退出、凭证修改及并发构建使用独立账户或环境互斥。
- URL 中 user_id 不作为已验证身份。详情和媒体用例覆盖该参数不能冒充用户或改变发布者存储定位的契约；真实 HTTP 不能观察的个人记录隔离仍由包内测试保证。

### 4.3 断言

不沿用参考项目统一的 `{status, msg, data}` 和 `status == 0` 假设：

| 响应类别 | 断言方式 |
| --- | --- |
| 登录等业务响应 | HTTP 状态和对应 proto JSON 字段、业务状态及数据 |
| 视频详情与列表 | 各自的结构、ID 字符串、时间、分类和分页契约 |
| Kratos 错误 | HTTP 状态与响应体 code、reason、message、metadata 分别核对 |
| MPD、封面和分片 | HTTP 状态、媒体类型、非空内容与文件格式 |

当前 ErrorEncoder 会将大于 600 的业务 code 通过 HTTP 200 输出，不能将 HTTP 200 一律视作业务成功；也不为统一测试而修改接口错误协议。reason 可能被错误包装扩展，优先断言稳定业务 code，再按具体契约检查 reason。

优先使用有类型的测试 DTO 或 protojson；涉及 JSON 的表示契约时，额外检查原始 JSON，避免宽松解码接受数字 ID 后掩盖字符串契约回归。通用动态 JSON 使用 json.Number，不经 float64 处理中转 ID。失败诊断保留方法、路由、HTTP 状态和经过脱敏且截断的响应，不输出密码、双 Token 或刷新 metadata 内的凭证。

## 5. 环境与数据生命周期

拟定配置项如下，均需由环境或 TeamCity 参数显式提供，不内置参考项目地址：

| 拟定配置项 | 用途 |
| --- | --- |
| API_TEST_BASE_URL | 已部署网关的 HTTP 地址 |
| API_TEST_USERNAME / API_TEST_PASSWORD | 专用测试账户 |
| API_TEST_VIDEO_ID / API_TEST_PUBLISHER_ID | 已准备的媒体夹具及实际发布者 |
| API_TEST_CLASS | 列表查询的有效分类 |
| API_TEST_TIMEOUT | 单请求超时，单位和默认值在实现时明确 |
| API_TEST_RUN_ID | 当前测试运行标识，用于报告和未来资源命名 |

未来需要写入套件时另设显式启用开关和对应 profile，不能把设置开关当作自动清理的替代品。CI 缺少所选 profile 必需的参数、服务不可达、没有执行预期用例或整套跳过时应失败，不静默回退本机环境。

首版使用预置账户、视频、分类和已生成 DASH 文件，不在 TestMain 自动注册、上传或调用 ffmpeg。夹具文档记录生成来源、必要字段、初始化分片及媒体分片位置，不提交真实凭证和大型媒体。

查询并不等于零副作用：登录可能增加经验并改写会话，视频详情会增加播放量或热度，已登录详情还可能写观看历史。首批也必须运行在允许这些变化的专用环境，断言避免依赖固定播放量。

后续创建资源使用 run ID 和唯一前缀，创建成功立即注册 t.Cleanup，准备中途失败也要处理已经创建的资源。清理采用独立且有界的 context，错误要进入结果或诊断；强制中断进程时不能保证清理执行，必须保留运行清单供环境重置追溯。

现有网关未提供用户、视频和评论删除接口。写入测试启用前应确定隔离环境重置机制；不假设存在清理 API，不为了测试新增公共删除接口，不由 HTTP helper 默认直连数据库删除业务数据。点赞等切换操作也不能无条件再调用一次当作清理，需结合原状态和专用夹具设计。

## 6. 接口清单与覆盖口径

### 6.1 清单来源

首选读取网关已生成 protobuf descriptor 中的 google.api.http 注解，包括未来出现的 additional_bindings，获得 method、path template 和 operation。范围限定为 http.go 实际注册的服务，不包含下游仅 gRPC 的方法。

可维护一份显式 descriptor registry，并以 Go AST 解析 http.go 的注册调用作离线一致性校验；新增注册服务而未加入 registry 时应失败。工具不连接业务服务，不执行 Wire、protoc 或 GORM Gen。OpenAPI 用于交叉核对，不作为唯一且可能过期的清单来源。

注册范围和 descriptor 的适配测试是第三阶段交付的一部分。方法与路径模板共同构成唯一键；同一路径的 GET 和 POST 分开统计。无匹配路由、重复声明、零路由扫描结果应作为工具异常处理。

### 6.2 主测标记

保留参考项目的 primary 思路：用例的目标接口计入主测，准备、登录辅助、清理和状态轮询不计入。一个接口指定一个常规 API 主测测试函数，其成功、参数错误、鉴权和资源不存在场景放入子测试；多接口流程或 Bug 回归可以独立存在，不强行套用常规套件的单接口归属约束。

请求 wrapper 绑定 operation 和路由模板，发往实际 URL 后经 t.Logf 记录主测方法、模板和结果；例如实际请求 `/api/v1/video/123/info` 归到 `GET /api/v1/video/{video_id}/info`。不能按任意数字替换来猜模板；DASH 的固定 mpd 路由与 segment_name 模板尤其需要区分。

按运行、Package、顶层 Test 和子测试关联 go test -json 的 run、output、pass、fail、skip 事件。事件格式解析失败或缺少结束事件必须标明不完整，不能容错吞掉后给出完整通过结果。

### 6.3 指标

- 执行覆盖率：本次获得主测 HTTP 响应的当前接口数 / 当前 HTTP 接口总数。网络错误仅记请求尝试，不计执行覆盖；401 或业务失败响应可计执行，但不代表接口行为正确。
- 测试结果：分别保留主测用例及其子测试的通过、失败、跳过和未完成状态。按预期验证错误返回的负向场景可以通过。
- 主测用例通过的接口数可作为独立指标，但不称为所有业务分支已经覆盖；单个接口只测了拒绝访问也不能证明其成功路径正确。
- 同时报告无用例接口、已规划但未执行接口、未知标记、重复主测归属、排除接口及原因。

smoke profile 的目标集合需明确，不能把只运行 smoke 的覆盖率当成全量回归质量下降。全量接口数持续可见，profile 内完成度另算；增量 run 的合并只允许同一源码版本、目标环境和运行批次的输入。

首版不照搬参考项目的最高版本路径自动替代规则。VideoWeb 当前以实际注册接口为准；已移除的旧 DASH 路由属于兼容性拒绝用例，不加入当前接口分母。未来确需排除接口时显式记录原因，不通过隐藏扫描结果提高覆盖率。

## 7. 首批场景

| 接口或主题 | 第一批检查点 | 前置与边界 |
| --- | --- | --- |
| Login | 正常登录返回正 ID 和双 Token；错误密码返回稳定错误 | 专用账户，凭证错误不自动重试 |
| GetVideoInfo | 匿名成功、有效身份成功、错误凭证按游客、参数不能冒充身份 | 已有视频；不断言固定计数 |
| GetVideoList | 无凭证拒绝；有效会话按有效分类分页并返回新增字段 | 固定分类与视频夹具，保留现有 IN 筛选 |
| GetMpd | 匿名获取，媒体类型正确，XML 可解析 | 使用真实 MPD；结构化解析 BaseURL、初始化及媒体模板 |
| GetSegments | 按 MPD 规则选择实际存在的初始化/媒体分片并请求；旧路径字段拒绝 | 按 MPD 模板或时间线解析，不手拼假定文件名 |
| GetVideoCover | 匿名获取、媒体类型与文件格式相符 | 已有封面，检查内容而非仅状态码 |
| DownloadVideo | 无凭证拒绝；有效会话下载内容符合夹具 | 控制文件体积；原文件仍要求登录 |

路径穿越和非法分片名加入负向场景；注意 URL 编码可能先被 HTTP 路由拒绝，分别说明路由拒绝与业务校验的预期。符号链接越界、流取消、临时文件清理等已有离线测试继续保留，不用黑盒 HTTP 成功代替内部不变量验证。

第二批在数据重置方案确定后加入元信息 ID、封面和视频上传、收藏夹、关注、评论及互动。邮件验证码、注册、密码或邮箱变更、投贝壳和故障恢复归为有额外资源需求的场景，按依赖单独启用，不在首批隐式执行。

## 8. TeamCity 设计

```text
已准备的测试环境及版本记录
  -> 校验配置、账户与夹具
  -> go test -json 执行选定 profile
  -> 解析本轮结果并生成覆盖 JSON / HTML
  -> 发布 Artifact、统计值与 Report Tab
```

测试命令和流水线脚本必须使用同一个入口。以下只是未来命令示例，当前目录尚未建立；工作目录为 `video_web_backend/vw_gateway/`：

```text
go test -mod=readonly -tags api_test -json -count=1 -timeout 10m ./tests/api/...
```

timeout 为初始设计值，按实际媒体耗时校准。局部调试使用 -run，但报告标注为局部运行；不伪装成完整回归。首版不启用 t.Parallel；不同构建共享账户或数据时需环境锁或独立环境。

| 流水线步骤 | 行为与失败处理 |
| --- | --- |
| 配置及前置检查 | 验证显式 URL、必要参数和夹具；记录测试源码 revision、目标部署 revision、run ID 和 profile。缺少版本接口时由部署步骤传入版本证明，不虚构健康接口 |
| 接口测试 | 开启 Golang build feature；JSONL 同时写 stdout 和本轮报告目录；密码使用 TeamCity 密码参数，日志另做脱敏 |
| 覆盖与归档 | 普通测试失败后仍执行；读取本轮 JSONL，发布完整或明确标为不完整的报告；缺日志或零测试时不能 exit 0 冒充成功 |
| 最终状态 | 测试非零退出码、前置失败或报告工具失败均保留；后续成功不得覆盖前序失败 |

平台脚本应立即保存 go test 退出码，再执行报告与清理。Bash 需要正确处理管道中的测试退出码；PowerShell 需要显式保存原生命令退出码，并保持日志为 UTF-8 JSONL。stdout 中保留原始 Go JSON 事件供 TeamCity 实时解析，不用 JUnit 和原生解析重复上报同一批测试。

每个构建使用独立报告目录，归档原始 JSONL、coverage.json、index.html 及不含秘密的运行 manifest；不要复用上一次构建日志，也不要混合不同版本的报告。统计指标只有一个上报来源。Report Tab 例如指向 Artifact 的 `api-coverage/index.html`，不是 Agent 的本地路径。

首次接入先检查报告完整性、未知标记和预期 smoke 测试是否执行；覆盖百分比先记录趋势，阈值和新增接口要求在稳定全量基线后确定。TeamCity 具体版本、Agent 系统、触发策略和 HTML Artifact 隔离配置在第五阶段核对。

官方依据：

- [Golang build feature 与 go test -json](https://www.jetbrains.com/help/teamcity/golang.html)
- [Service Messages、统计值与 Artifact 发布](https://www.jetbrains.com/help/teamcity/service-messages.html)
- [HTML Report Tab](https://www.jetbrains.com/help/teamcity/including-third-party-reports-in-the-build-results.html)

## 9. 分步实施与验收

下面各阶段均未实施；阶段完成须以实际源码和验证结果为准，不因设计确认自动转为完成，也不自动创建提交。

| 阶段 | 交付物 | 验收标准 | 实施前依赖 |
| --- | --- | --- | --- |
| T1 最小骨架 | 配置、无副作用 HTTP client、身份视图、响应断言、套件入口及说明 | helpers 的 httptest 单测验证请求头、超时、响应分类、无自动重试和脱敏；普通测试不启动真实套件；显式套件缺配置失败 | 明确委托实现骨架 |
| T2 首批 HTTP 场景 | 第七节登录、详情、列表和媒体用例，小型资源与夹具说明 | 在指定测试环境可重复执行；JSONL 可保存；请求次数、鉴权和内容断言符合契约；报告验证范围 | 测试地址、专用账户、视频/分类夹具、运行授权 |
| T3 覆盖工具 | descriptor 清单、注册一致性检查、测试事件与标记解析、JSON/HTML | 离线测试覆盖参数化路由、不同 method、辅助请求不计入、负向场景、失败/跳过/中断、零路由、缺日志和未知标记；不连接基础设施 | T1 的标记契约，T2 的结果样例 |
| T4 写入与 Bug 回归 | 元信息、上传、评论、互动用例及问题回归目录 | 数据隔离可验证，中途失败可处理，重复执行不依赖旧残留；无清理 API 的数据由已确定方案重置 | 隔离和重置策略、测试资源、场景授权 |
| T5 TeamCity 接入 | 平台运行脚本、参数说明、步骤配置及报告入口 | 故意失败的用例导致失败构建但仍有报告；缺配置/缺日志不绿色；通过、失败、跳过、趋势和 Artifact 可追溯至同一 run/revision | Agent 与 TeamCity 信息、环境准备、接入授权 |

T3 可与 T2 的环境准备并行推进。T5 可先接稳定 smoke，T4 随后扩展；不是必须先补齐全部接口再接 CI。

各阶段只提交对应测试、工具、脚本和说明，不混入现有部署差异、异步骨架、ES 草稿或参考材料。测试实现遵循项目的中文注释约定，不手改生成文件，不为验证执行 make all 或 go generate。

## 10. 待确定事项与当前结果

| 事项 | 建议起点 | 影响阶段 |
| --- | --- | --- |
| 测试目标环境 | 指定可重复使用的测试环境，记录其部署版本；先不自动创建基础设施 | T2 |
| 专用账户与会话隔离 | smoke 使用专用账户；退出、改密码等另用账户；共享环境的构建串行 | T2 / T4 / T5 |
| 固定媒体夹具 | 一个分类明确且包含 MPD、分片和封面的短视频；记录发布者和合法大 ID | T2 |
| 数据重置机制 | 无删除 API 的写入用例优先使用可重置的独立环境，具体方式另定 | T4 |
| TeamCity Agent 与版本 | 确定 Linux 或 Windows、Go 工具链、网络可达性及报告功能 | T5 |
| 触发与门槛 | 先手动 smoke，稳定后按部署/提交触发；全量或重型用例另设计划 | T5 |

这些事项不阻碍阅读和形成设计，也不要求现在一次答完。开始相应实施阶段时再确定其必需输入。

本轮完成参考材料阅读、当前路由与响应契约的静态对照，以及本文设计整理。没有新增测试代码、运行接口测试、验证真实服务、启动基础设施、配置 TeamCity 或执行暂存/提交。
