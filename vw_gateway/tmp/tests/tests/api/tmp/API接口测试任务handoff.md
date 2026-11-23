# API 接口测试任务 Handoff

## 当前任务目标

在 `D:\code\projects\yslab\ModelingAndSimulation-tc-ci-test` 工作树上继续完善 API 自动化测试，重点补齐接口覆盖率工具统计出的未覆盖接口。当前大类按用户确认分为：

1. 仿真相关接口
2. 模型相关接口
3. 管网相关接口

当前正在处理仿真相关接口，最近卡在 `TestGetSimulationResultFileV2` 的验证阶段。用户因该测试长时间无响应而停止任务，现在需要新会话接着判断该测试是否应调整可观测性、超时策略或等待逻辑，再继续验证。

## 必须遵守的约束

- 必须在 `tc-ci-test` 分支工作。开始前执行 `git branch --show-current`，不是该分支就停止并提示用户。
- 默认工作树：`D:\code\projects\yslab\ModelingAndSimulation-tc-ci-test`。
- 只修改 `tests` 或 `.codex/skills` 相关内容，除非用户明确要求，不修改业务代码。
- 不要暂存、提交任何文件，尤其不要暂存 Modelica 相关文件。
- 接口测试使用 `-tags api_test`。
- API 测试默认请求 147 环境 yslab 服务，不要随意切本机服务，否则会出现模型、MAT、FMU、编译产物只存在一侧的问题。
- 请求头使用 `space-id`，不要再使用旧的 `space_id`。
- 主测接口必须通过 `testClient.Primary().Xxx()`、`PrimaryGet()` 或 `PrimaryPost()` 标记；准备数据、清理数据、加载模型、状态查询等辅助接口不要标记。
- 一个顶层 `TestXxx` 原则上只标记一个主测接口。
- 新增函数/方法必须写注释；复杂逻辑内部也要有简短注释。
- 公共 helper 放在 `tests/helpers`，不要在测试文件里随意写未导出辅助函数。需要跨文件复用时，提取为公共 helper。
- `tests/helpers` 公共函数不要接收 `testing.T`，应返回 `error`，由测试函数决定 `t.Fatal` 或继续。
- 上传文件测试使用 `tests/resources` 下的固定文件，不依赖个人路径。
- 仿真相关测试一律使用新版接口，不新增旧仿真接口测试。

## 需要优先读取的 Skill/参考

接口测试相关任务使用：

`D:\code\projects\yslab\ModelingAndSimulation-tc-ci-test\.codex\skills\yslab-api-test\SKILL.md`

按任务读取对应 reference：

- 运行测试：`references/environment-and-run.md`
- 编写/修改测试：`references/writing-conventions.md`
- 仿真、MAT/FMU、TeamCity、147 环境：`references/simulation-ci.md`
- 覆盖率统计：`references/reporting-and-coverage.md`

## 重要技术决策

### 仿真等待逻辑

- 新版仿真启动接口使用 `POST /api/v4/simulation/simulate-with-translate`。
- 仿真完成等待统一轮询 `GET /user/userspace/change/poll`。
- 不再使用 `/api/v3/simulation/state/get`。
- 等待时先看 `data.space_data.translate_status_data`，再看 `data.space_data.simulate_status_data`。
- 必要时用同一 poll 响应里的 `data.model_data.simulate_status_data.id` 匹配本次实验。
- 用户指出 `model_data` 可能残留上一次仿真信息，所以编译/仿真状态应优先看 `space_data`。
- MAT/FMU 根目录只使用 `/YSSIM/cloud/apps/yssim-go`，不要使用旧目录 `/YSSIM/cloud/yssim-go`。

### 接口覆盖统计

- 覆盖率基于 `go test -json` 输出中的 `API_MARK`。
- `API_MARK` 由 `tests/helpers/client.go` 里的 `Primary()`、`PrimaryGet()`、`PrimaryPost()` 触发。
- 辅助接口不能打标，否则会污染覆盖率。
- 路由覆盖工具使用 Go 版本：`D:\code\projects\yslab\ModelingAndSimulation-tc-ci-test\tests\tools\api_coverage.go`。
- 黑名单文件放在 `D:\code\projects\yslab\ModelingAndSimulation-tc-ci-test\tests\api` 下。

## 已完成工作

### 用户、实验、仿真轻量接口

已添加或修复：

- `TestExperimentDefaultSet`
- `TestGetSimulationRecordFinishedV2`
- `TestGetUserSpaceUsage`
- `TestClearUserSpaceTaskLogs`
- `TestTerminateAllUserSpaceTasks`

涉及文件：

- `tests/api/experiment_crud_test.go`
- `tests/api/simulation_api_test.go`
- `tests/api/user_api_test.go`
- `tests/helpers/client.go`
- `tests/helpers/simulation/experiment.go`

其中 `TestGetSimulationRecordFinishedV2` 已修复后通过，修复点是后端可能把 nil slice 编码为 null，测试中统一按空列表处理。

### 模型相关接口

已补充一批模型接口测试和 client 方法，主要涉及：

- `TestGetPackageAndVersion`
- `TestCheckModelV2`
- `TestTranslateModelV3`
- `TestParseTranslateLog`
- `TestUpdateDiagramShapes`
- `TestMoveClassToPackage`

涉及文件：

- `tests/api/model_api_test.go`
- `tests/api/model_edit_api_test.go`
- `tests/api/model_graphics_api_test.go`
- `tests/helpers/client.go`
- `tests/resources` 下相关模型资源

之前分批跑过目标测试，均已通过。若需要精确数量，请重新从代码或 JSON 结果统计，不要凭记忆报数。

### 管网相关接口

已新增：

- `tests/helpers/pipenet/fixture.go`
- `tests/api/pipenet_api_test.go`

覆盖内容包括：

- 管网信息文件 CRUD
- 管网映射配置 CRUD/详情
- 实例映射
- 管网模型生成

用户确认管网资源位于：

`D:\code\projects\yslab\ModelingAndSimulation-tc-ci-test\tests\resources\pipenet`

并有 `README.md` 说明。建议加载管网所需模型库到默认工作空间后复用，不要每次测试都上传。

### 当前未完成：`TestGetSimulationResultFileV2`

已在 `tests/api/simulation_api_test.go` 中实现，但尚未验证通过，因为用户停止了长时间运行的测试。

它目前覆盖：

- 正常导出仿真结果文件
- 不存在的实验 ID
- 缺少 `experiment_id`
- 成功场景要求 `data.url` 非空，且后缀为 `.mat` 或 `.zip`

相关 client 方法已加到 `tests/helpers/client.go`：

- `GetSimulationResultFileV2(experimentID string)`
- `GetSimulationResultFileV2Raw(body any)`

正常场景依赖：

- `createFinishedSimulationExperiment`
- `waitAfterSimulationPollForMatInspection`
- `waitForSimulationResultServiceReady`
- 主测接口 `/api/v2/file/result/all/get`

当前风险：测试长时间无输出，用户无法判断卡在哪一步。下一步应优先增强这个测试的阶段日志，而不是直接长时间重跑。

## 已执行验证及结果

已知通过的目标测试批次输出文件：

- `tmp\ApiTestModelBatch_20260805_185954.jsonl`
- `tmp\ApiTestModelAsyncEdit_20260805_190840.jsonl`
- `tmp\ApiTestPipeNetCrud_20260805_191659.jsonl`
- `tmp\ApiTestPipeNetModel_20260805_191935.jsonl`
- `tmp\ApiTestPipeNetModelCleanup_20260805_192021.jsonl`
- `tmp\ApiTestSimulationLight_20260805_192734.jsonl`
- `tmp\ApiTestUserSpaceTasks_20260805_193134.jsonl`

注意：没有完成最近这轮 `TestGetSimulationResultFileV2` 的验证，不要声称它通过。

## 已知问题

1. `TestGetSimulationResultFileV2` 长时间运行时缺少阶段性输出，用户看不到当前卡在仿真提交、poll 等待、MAT 同步等待、结果服务检查，还是主测接口。
2. 仿真结果类接口依赖 147 环境产物和手动 MAT/FMU 同步，失败时要区分测试逻辑问题、后端仿真失败、文件同步问题。
3. 本地/147 环境切换会造成文件路径不一致。当前结论是默认打 147 服务。
4. 之前用户停止过一次测试，下一次运行前建议检查是否有残留 `go test` 或相关进程，但不要做破坏性清理。
5. 覆盖率工具和黑名单已有工作，但用户让另一个会话处理过部分脚本问题。继续时以仓库当前代码为准。

## 尚未完成工作

1. 验证并必要时调整 `TestGetSimulationResultFileV2`。
2. 继续补齐未覆盖接口，按用户确认的顺序建议：
   - 先仿真相关
   - 再模型相关
   - 最后管网相关
3. 对新增/修改测试运行目标测试。
4. 后续再跑全量 API 测试，并输出通过、失败、跳过数量和 JSON 文件路径。
5. 根据覆盖率工具重新查看未覆盖接口，确认黑名单是否仍需更新。

## 明确下一步

建议新会话按下面顺序开始：

1. 在 `D:\code\projects\yslab\ModelingAndSimulation-tc-ci-test` 执行 `git branch --show-current`，确认是 `tc-ci-test`。
2. 读取 `yslab-api-test` Skill 及这次涉及的 references：
   - `writing-conventions.md`
   - `environment-and-run.md`
   - `simulation-ci.md`
3. 查看 `tests/api/simulation_api_test.go` 中 `TestGetSimulationResultFileV2`、`createFinishedSimulationExperiment`、`waitAfterSimulationPollForMatInspection`、`waitForSimulationResultServiceReady` 的当前实现。
4. 给 `TestGetSimulationResultFileV2` 增加必要的 `t.Log` 阶段输出，例如：
   - 开始创建仿真实验
   - 已提交仿真，开始 poll
   - poll 完成，进入 MAT 同步等待
   - 开始检查结果服务是否可用
   - 开始请求 `/api/v2/file/result/all/get`
5. 只运行该测试函数，使用 `go test -json -tags api_test`，同时让输出进控制台和 JSON 文件。仿真相关可设置较长 timeout，但不要无声等待。
6. 如果仍卡住，基于阶段日志定位卡点，再决定是调整等待条件、缩短固定等待、还是判断 147 服务/仿真产物问题。

## 实时仿真遗留问题（2026-08-07）

实时仿真接口测试已按讨论更新为正式用例，但当前 147 环境仍有实时样本链路问题，需要后续单独排查。

- 已通过：`TestSetRealtimeFmuVariables`，说明实时仿真启动、`task_id` 解析和变量设置接口本身可用。
- 未通过：`TestSetRealtimeFmuParameters`、`TestGetRealtimeFmuStream`。
- 失败现象：测试能拿到本次实时仿真的 `task_id`，但等待 Redis Stream 样本超时，典型错误为 `等待实时 Redis Stream 样本超时 key=fmu:interactive:<task_id>:samples task_id=<task_id> last_len=0`。
- 关联日志：测试期间反复出现 task-management gRPC resolver 报错 `Received error from the name resolver: produced zero addresses`。
- 当前判断：问题更像是实时计算 / task center / Redis 样本写入链路未正常输出，暂不归因于 SSE 解析或测试断言。
- 参考结果：`tmp\ApiTestRealtimeFmuVariables_20260807_155558.jsonl`、`tmp\ApiTestRealtimeFmuRetry4_20260807_155348.jsonl`。
