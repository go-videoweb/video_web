# tests/helpers 公共函数清单

本清单是 `tests/helpers` 公共函数的唯一共享 catalog，供 `yslab-api-test` 和 `bug-regression-test` 两个 Skill 在编写、维护或排查测试时使用。优先复用这些 helper，不要在测试文件或 Skill 内复制请求封装、响应解析、异步等待、夹具创建、远端文件路径或清理逻辑。

## 通用约定

适用场景：
- 编写 API 接口测试或 API 类 Bug 回归测试，尤其是需要真实后端、工作空间、模型、仿真、资源文件或数据库状态的用例。
- 将接口测试需求或自由格式 Bug 归一化为自动化用例时，用于判断已有 helper 是否足以支撑测试路径和清理路径。

主要文件：
- tests/helpers/*.go
- tests/helpers/model/*.go
- tests/helpers/simulation/*.go
- tests/helpers/workspace/*.go
- tests/helpers/resource/*.go
- tests/helpers/pipenet/*.go

常用函数：
- helpers.NewAssertResponse(t)：创建统一响应断言器；入参为 `*testing.T`；返回 `*AssertResponse`；本身不失败。
- helpers.RunWithHooks(t, hooks, fn)：包裹测试前置、主体和清理；关键入参为 `TestHooks{Before, After}` 和测试函数；无返回值；`Before` 失败时跳过主体但仍执行 `After`。
- testClient.Xxx(...)：直接调用已有 APIClient 方法；返回 `*helpers.Response` 或原始 `*http.Response`；失败通常返回 error 或接口业务错误响应。
- helpers.ObjectData / ArrayData / ObjectListData：解析统一响应 `data`；返回强类型结构和 error；类型不匹配时返回错误，测试中通常 `t.Fatal`。

使用注意：
- API 接口测试通常需要 `api_test` build tag；Bug 回归测试必须且只能使用 `//go:build bug_regression_test`，即使复用 APIClient 或其他接口调用 helper，也禁止叠加 `api_test`。
- 请求默认依赖 `TEST_USERNAME`、`TEST_SPACE_ID`、`PORT`、Debug DB/Redis/Mongo/Nacos 等 API 测试环境变量。
- `Primary()`、`PrimaryGet` 和 `PrimaryPost` 会输出 API 覆盖统计标记；是否使用由当前测试体系的 Skill 策略决定。
- 创建模型、实验、资源、管网文件后必须用 `RunWithHooks`、`After` 或 `defer` 清理。
- 涉及远端 MAT/FMU/static 文件时，默认请求 147 环境；不要切到本地后端规避同步问题。

## 响应断言与数据解析

适用场景：
- 所有 API 类自动化测试都应优先使用这一类 helper 做业务响应断言和 `data` 类型解析。
- 当测试场景依赖响应字段、空数组、空对象、SSE、原始 HTTP body 或 JSON 结构等价时使用。

主要文件：
- tests/helpers/response.go
- tests/helpers/assertions.go
- tests/helpers/response_helpers.go
- tests/helpers/numeric.go

常用函数：
- NewAssertResponse(t)：创建响应断言器；入参为 `*testing.T`；返回 `*AssertResponse`；本身不失败。
- (*AssertResponse).IsSuccess(resp, message)：断言 `resp.Status == 0`；入参为统一响应和场景说明；返回 bool；失败时调用 `t.Errorf` 并返回 false。
- (*AssertResponse).IsError(resp, expectedCode, message)：断言业务错误码；关键入参为期望 status；返回 bool；不匹配时 `t.Errorf`。
- (*AssertResponse).HasField / HasFields / FieldEquals：断言 `data` object 中字段存在或相等；返回 bool；`data` 类型不符或字段不符时 `t.Errorf`。
- (*AssertResponse).DataIsNotEmpty / DataIsArray / ArrayLength / ArrayLengthGreaterThan：断言 `data` 非空、数组类型和长度；返回 bool；不满足时 `t.Errorf`。
- (*AssertResponse).ValueIsStructurallyEmpty(value, message)：判断 nil、空 map/slice/array/struct 或递归空结构；返回 bool；不为空时 `t.Errorf`。
- IsStructurallyEmpty(value)：纯判断函数；返回 bool；不依赖 `testing.T`，不会主动失败。
- ReadAndCloseRawBody(resp)：读取并关闭原始 HTTP body；返回 `[]byte, error`；读取失败返回 error。
- UnmarshalResponse(body)：将原始 body 解析为统一 `Response`；返回 `*Response, error`；JSON 非法时返回带 body 的 error。
- ObjectData(resp, message)：读取 object 类型 `data`；返回 `map[string]any, error`；类型错误返回 error。
- ArrayData(resp, message)：读取 array 类型 `data`；返回 `[]any, error`；类型错误返回 error。
- ObjectListData(resp, message)：读取 object 数组类型 `data`；返回 `[]map[string]any, error`；数组项类型错误返回 error。
- AssertJSONEquivalent(t, got, want, message)：JSON 归一化后比较结构等价；无返回值；不一致时 `t.Errorf`。
- AssertSSEContentType(t, resp, message)：断言 `Content-Type` 包含 `text/event-stream`；无返回值；失败时 `t.Fatalf`。
- ReadSSEEvent(reader)：读取一个 SSE event；返回事件名、payload 和 error；缺 event 或读取失败返回 error。
- NormalizeJSONValue(value)：将 JSON 兼容值归一化；返回归一化值和 error；序列化/反序列化失败返回 error。
- NumberAsFloat64(value)：把 JSON 数字或基础数值统一转成 float64；返回值和 bool；不支持的类型返回 false。
- FloatEqual(actual, expected, epsilon)：浮点误差比较；返回 bool；不主动失败。

使用注意：
- `ObjectData` / `ArrayData` / `ObjectListData` 只做类型解析，不会自动断言 `Status == 0`，通常先调用 `assert.IsSuccess`。
- 原始响应 helper 会关闭 body；读取后不要再次读取同一 body。
- SSE helper 适合实时仿真、参数标定列表/详情等流式接口。
- 这些 helper 不创建或删除任何业务数据，适合 API 接口测试和 Bug 回归测试直接复用。

## 测试生命周期与环境初始化

适用场景：
- 测试需要创建临时模型、实验、资源、管网文件或数据库记录，并且必须保证清理执行。
- 测试包需要 suite 级 setup/teardown，或在真实后端环境变量缺失时跳过。

主要文件：
- tests/helpers/hooks.go
- tests/helpers/suite.go
- tests/helpers/setup.go

常用函数：
- RunWithHooks(t, hooks, fn)：按 `Before -> fn -> After` 执行；`After` 使用 defer 保证执行；无返回值；`Before` 标记失败后不执行主体。
- SubTestHooks(before, after, fn)：为 `t.Run` 子测试生成独立 hook；返回 `func(*testing.T)`；内部复用 `RunWithHooks`。
- DefaultTestConfig()：生成默认测试配置；返回 `*TestConfig`；依赖环境变量并提供默认值。
- WaitForServer(baseURL, maxRetries)：轮询服务是否可访问；返回 error；超过重试次数失败。
- CleanupTestData(client, modelNames)：清理指定模型测试数据；返回 error；清理失败返回错误。
- (*Suite).Run(m)：执行 suite setup、`m.Run()`、teardown；返回退出码；setup 失败时返回 1。
- RunSuite(m, setup, teardown)：Suite.Run 快捷方式；返回退出码。
- NoopSuite()：返回空 suite；适合暂不需要 suite hook 的包。
- LogSetupStep(step) / LogTeardownStep(step)：输出 setup/teardown 进度；无返回值。
- SkipIfNotReady(t)：检查 `TEST_SPACE_ID` 和 `TEST_USERNAME`；缺失时 `t.Skip`。

使用注意：
- `RunWithHooks` 是 API 接口测试和 Bug 回归测试首选结构，尤其适合 `Before` 创建数据、`After` 清理数据。
- `After` 中的清理逻辑应尽量幂等，不要因为资源已不存在导致测试失败。
- `DefaultTestConfig` 和真实后端测试依赖环境变量；本地运行前需按 API 测试环境配置。
- 生命周期 helper 本身不负责业务创建/删除，但经常包裹会创建/删除数据的代码。

## APIClient 基础请求

适用场景：
- 需要直接调用 HTTP 接口、原始响应、SSE、非法请求体或尚未封装的接口。
- 需要复用统一请求头、统一响应解析、工作空间 Ready 检查和限流退避逻辑。

主要文件：
- tests/helpers/client.go
- tests/helpers/rate_limit.go

常用函数：
- NewAPIClient(baseURL)：创建基础 APIClient；关键入参为 baseURL；返回 `*APIClient`；不自动加载工作空间。
- NewAPIClientWithConfig(config)：按测试配置创建客户端；返回 `*APIClient`；首次创建会确保工作空间 Ready，失败 panic。
- (*APIClient).Primary()：API 接口覆盖统计专用；返回主测接口客户端视图并在请求完成后输出 `API_MARK`；是否使用由当前测试体系的 Skill 策略决定。
- MarkAPI(method, path)：API 接口覆盖统计专用；手动输出覆盖标记；无返回值；method/path 为空时忽略。
- (*APIClient).Get(path, params, headers)：发送 GET；返回 `*Response, error`；请求失败或 JSON 解析失败返回 error。
- (*APIClient).Post(path, body, headers)：发送 POST JSON；返回 `*Response, error`；请求失败或 JSON 解析失败返回 error。
- (*APIClient).PrimaryGet / PrimaryPost：API 接口覆盖统计专用；发送 GET/POST 并输出覆盖标记；是否使用由当前测试体系的 Skill 策略决定。
- (*APIClient).GetRawResponse(path, params)：获取原始 GET 响应；返回 `*http.Response, error`；调用方负责读取和关闭 body。
- (*APIClient).PostRawResponse(path, body)：获取原始 POST 响应；返回 `*http.Response, error`；调用方负责读取和关闭 body。

使用注意：
- `Primary()`、`PrimaryGet`、`PrimaryPost` 只用于 API 接口覆盖统计；辅助请求通常直接调用 `testClient.Xxx(...)` 或 `Get/Post`。
- `client.go` 内部会为多数依赖 SMC 的接口自动确保工作空间 Ready。
- `rate_limit.go` 会在底层对限流接口主动降速并对 HTTP 429 退避重试，测试中通常不直接调用这些未导出函数。
- Raw response 方法适合 HTTP 400、SSE、文件流或非标准 JSON 响应断言。

## APIClient 业务接口封装索引

适用场景：
- 编写 API 接口测试或 API 类 Bug 回归测试前，先检查目标接口或准备/清理接口是否已被封装。
- 需要快速定位同业务模块的可用请求方法，而不想重新手写 HTTP 请求。

主要文件：
- tests/helpers/client.go

常用函数：
- 工作空间/用户设置：`RestartSMC`、`WorkspaceChangePoll`、`GetLoadStatus`、`LoadstatusSet`、`GetUserSettings`、`SetUserSettings`、`SetUserSettingsFull`、`GetUserSpaceNew`、`GetUserSpaceV2`、`GetUserSpaceUsage`、`CreateUserSpace`、`DeleteUserSpace`、`EditUserSpace`、`CollectUserSpace`、`TerminateAllUserSpaceTasks`；返回统一响应；失败为请求/解析 error 或业务 status 非 0。
- 模型加载/查询：`GetSysRootModel`、`GetUserRootModel`、`GetUserPackage`、`LoadModel`、`LoadUserspaceModel`、`UnLoadModel`、`UnloadPackageByID`、`GetListModel`、`GetAllUserspaceModel`、`SearchModel`、`ModelExists`、`GetModelCode`、`GetModelComponents`、`GetModelDocument`、`GetUML`、`GetExtendedModel`；常用于准备模型状态和验证模型查询结果。
- 模型编辑/图形/连线：`SetModelParameters`、`AddModelParameters`、`DeleteModelParameters`、`SetModelProperties`、`AddModelComponent`、`DeleteModelComponent`、`UpdateModelComponent`、`RenameModel`、`CopyModelComponent`、`CreateConnection`、`DeleteConnection`、`UpdateConnection`、`UpdateIconShapes`、`UpdateDiagramShapes`、`UpdateDiagramShapesRaw`、`MoveClass`、`MoveClassToPackage`、`MoveClassToPackageRaw`、`CopyClass`、`DeletePackage`。
- 模型文件和 Resources：`CreateModelPackage`、`UpdateModelPackage`、`GetPackageFileList`、`GetPackageFile`、`SaveModelCode`、`UploadModelPackage`、`UploadModelIcon`、`UploadModelVarFile`、`GetPackageResourcesList`、`CreateResourcesDir`、`DeleteResourcesDirAndFile`、`UploadResourcesFile`、`DownloadResourcesFile`、`GetResourcesFileDataRaw`、`GetResourcesImagesPath`、`GetResourcesImageRaw`。
- 资源库：`CreateResourceFolder`、`UploadResourceFile`、`GetResourceFileContent`、`ParseResourceFileContent`、`CopyLibFileToResources`、`EditResourceInfo`、`DeleteResource`、`GetResourceRootList`、`GetResourceList`、`SearchResource`。
- 仿真/实验/结果：`ModelSimulateWithTranslate`、`ModelSimulateWithTranslateRaw`、`ExperimentCreate`、`ExperimentGet`、`ExperimentDelete`、`ExperimentOptionsSet`、`ExperimentParametersSet`、`ExperimentExportVariableSet`、`ExperimentDetails`、`GetSimulationRecordListV2`、`GetSimulationRecordTreeV2`、`GetSimulationResultFileV2`、`GetSimulationResultV2`、`GetSimulationResultSingularV2`、`SimulationRecordTerminateV3`、`ExperimentCleanup`。
- 实时仿真：`SetRealtimeFmuVariables`、`SetRealtimeFmuVariablesRaw`、`SetRealtimeFmuParameters`、`SetRealtimeFmuParametersRaw`、`GetRealtimeFmuStream`、`GetRealtimeFmuStreamRaw`。
- 参数标定/FMU：`CalibrationTranslateFmu`、`CalibrationTranslateFmuRaw`、`ExportFmuRaw`、`UploadCalibrationData`、`CalibrationParseCSVColumns`、`CalibrationCreate`、`CalibrationListRaw`、`CalibrationTree`、`CalibrationEdit`、`CalibrationDelete`、`CalibrationStart`、`CalibrationStop`、`CalibrationDetailRaw`、`CalibrationSaveExperimentParameter`、`CalibrationSaveNewExperimentParameter`。
- 管网：`UploadPipeNetInfoFile`、`DownloadPipeNetInfoFile`、`DeletePipeNetInfoFile`、`GetPipeNetInfoFileList`、`EditPipeNetInfoFile`、`CopyPipeNetInfoFile`、`GetPipeNetInfoFile`、`UploadMappingConfig`、`GetMappingConfigList`、`GetMappingConfigDetails`、`EditMappingConfigDetails`、`GetPipeNetInstanceMapping`、`CreatePipeNetModel`。

使用注意：
- 这些方法大多只负责请求构造和响应解析，不自动判断业务成功；测试中仍需 `assert.IsSuccess` 或 `assert.IsError`。
- 后缀 `Raw` 的方法返回原始 HTTP 响应，适合缺必填参数、SSE、下载流、非 JSON 响应等场景。
- 编写测试时按当前 Skill 的覆盖标记策略决定是否使用 `Primary()`；准备数据、轮询和清理通常直接调用 `testClient.Xxx(...)`。
- 若目标接口未封装，优先补充 `APIClient` 方法；临时场景可用 `Get` / `Post`。

## 模型夹具、加载与源码资源

适用场景：
- 测试场景需要临时模型、临时 package、上传模型库、加载系统库/用户库，或读取 `tests/resources` 固定模型文件。
- 需要避免 147 测试环境中历史残留模型名冲突。

主要文件：
- tests/helpers/model/fixture.go
- tests/helpers/model/library_setup.go
- tests/helpers/model/compile_wait.go

常用函数：
- modelhelpers.CreateTempModelFixture(client, sourcePackageID, sourceModelName)：从源模型复制临时模型，失败时退回新建模型并写源码；返回 packageID、modelName、error；创建失败返回 error。
- modelhelpers.ImportModelLibraryFixture(client, libraryName, fileName, content)：上传并加载指定测试模型库；返回 packageID、modelName、error；上传/加载失败返回 error。
- modelhelpers.CreateTempPackageFixture(client, namePrefix)：创建临时 package 类型模型；返回 packageID、modelName、error；创建失败返回 error。
- modelhelpers.CleanupTempModelFixture(client, packageID, modelName)：删除临时模型；无返回值；空 ID 或空模型名忽略，清理失败不阻断主测试。
- modelhelpers.TryUnloadPackage(client, packageID)：尝试卸载模型包；无返回值；失败忽略，适合清理路径。
- modelhelpers.EnsureModelLoaded(client, packageID, modelName, message)：确保用户模型已加载；返回 error；接口失败或业务非成功返回 error。
- modelhelpers.EnsureSystemModelLoaded(client, packageID, modelName, message)：确保系统库模型已加载；返回 error。
- modelhelpers.UniqueModelName(prefix)：生成唯一模型名；返回 string；不失败。
- modelhelpers.DefaultModelSource(modelName)：生成基础测试模型源码；返回 string；不失败。
- modelhelpers.WaitCompileFinished(client, packageID, modelName, contextName)：轮询 workspace 编译状态直到结束；返回 error；超时或状态异常返回 error。
- modelhelpers.EnsureCommonModelLibrariesLoaded(client, modelicaPackageID)：确保公共依赖模型库已加载；返回 error。
- modelhelpers.ReadModelTestResource(pathParts...)：读取 `tests/resources` 下模型资源；返回 bytes 和 error；文件不存在返回 error。

使用注意：
- 会创建模型或 package 的 helper 必须配套 `CleanupTempModelFixture`、`TryUnloadPackage` 或接口清理。
- 上传/加载模型依赖 147 环境和工作空间 Ready 状态。
- `CreateTempModelFixture` 可能写入临时源码，适合自动化测试直接复用，但用例应断言真正的目标行为而不只断言创建成功。
- `ReadModelTestResource` 只读本仓库资源，不产生副作用。

## 远端 static 文件校验

适用场景：
- 测试场景涉及模型文件、`package.order`、Resources 文件、MAT/FMU 产物或数据库中保存的 `static/...` 路径。
- 需要验证服务端文件副作用，同时避免错误读取本地 checkout 下的旧文件。

主要文件：
- tests/helpers/model/file_wait.go

常用函数：
- modelhelpers.ModelPackageRootPath(packageID)：从数据库读取模型路径并映射到 `/YSSIM/cloud/apps/yssim-go`；返回目录路径和 error；packageID 空、DB 查询失败或路径非法返回 error。
- modelhelpers.LocalStaticPath(serverPath)：把服务端 `static/...` 或包含 `/static/` 的路径映射到 `/YSSIM/cloud/apps/yssim-go/static/...`；返回本地可打开路径和 error；空路径返回 error。
- modelhelpers.FileExpectationSkipReason()：判断是否应跳过远端文件校验；返回 reason 和 bool；仅当 `SKIP_REMOTE_FILE_ASSERT` 为真且 `/YSSIM` 不可访问时返回 true。
- modelhelpers.WaitForFileExpectations(expectations, options)：等待文件/目录存在性和文本内容符合期望；返回 error；超时返回最后失败原因。
- modelhelpers.FileExpectation：声明文件期望；关键字段为 `Path`、`ShouldExist`、`Contains`、`NotContains`、`ContainsLines`、`NotContainsLines`。
- modelhelpers.FileWaitOptions：配置等待；关键字段为 `Timeout`、`InitialDelay`、`MaxDelay`；零值使用默认。

使用注意：
- 依赖 `/YSSIM/cloud/apps/yssim-go` 挂载；本地 Windows 挂载不稳定时不要用项目 `static` 目录兜底。
- 只在用户显式设置 `SKIP_REMOTE_FILE_ASSERT=1/true/yes/on` 且 `/YSSIM` 不可访问时跳过文件校验。
- 文件校验适合 CI/TeamCity 中具备 `/YSSIM` 挂载的测试；本地调试可以跳过，但接口响应和 DB/AST 断言仍应保留。
- `ContainsLines` / `NotContainsLines` 适合 `package.order`，会归一化空白和换行。

## 仿真实验与结果等待

适用场景：
- 测试场景需要创建实验、设置仿真选项/参数、启动新版仿真、等待编译和仿真完成，或检查实验记录。
- 需要避免旧仿真状态接口误判，统一通过 workspace poll 等待。

主要文件：
- tests/helpers/simulation/experiment.go
- tests/helpers/simulation/wait.go
- tests/helpers/simulation/db.go
- tests/helpers/workspace/poll.go

常用函数：
- simulationhelpers.DefaultSimulateVarData()：返回创建实验常用默认参数；返回 map；不失败。
- simulationhelpers.DefaultExperimentOptionsData()：返回 v2 实验选项默认请求体；返回 map；不失败。
- simulationhelpers.CreateExperiment(client, opts)：创建测试实验；关键入参为 packageID、modelName、namePrefix、可选仿真参数；返回 experimentID、实际名称、error；接口失败或响应格式错误返回 error。
- simulationhelpers.DeleteExperiment(client, experimentID)：幂等清理实验；无返回值；会先取消默认标记，删除失败忽略。
- simulationhelpers.GetExperimentRecord(experimentID)：查 DB 实验记录；返回 `YssimExperimentRecord, error`；不存在或 DB 失败返回 error。
- simulationhelpers.ExperimentIsDefault(experimentID)：查询实验是否默认；返回 bool 和 error。
- simulationhelpers.ExportVariableInList(vars, name)：判断导出变量列表是否包含变量；返回 bool；不失败。
- simulationhelpers.WaitWithTranslateFinished(client, packageID, modelName, experimentID, contextName)：等待 `simulate-with-translate` 编译和仿真完成；返回 error；超时、停止态或 poll 失败返回 error。
- simulationhelpers.WaitTerminateFinished(client, packageID, modelName, experimentID, contextName)：等待终止中的仿真结束；返回 error；超时或状态异常返回 error。
- simulationhelpers.WaitSimulationRunning / WaitSimulationFinished：等待指定实验进入运行态或结束态；返回 error；状态不达标或超时返回 error。
- simulationhelpers.WaitModelSimulationStatus(...)：等待模型级仿真状态到达指定值；返回 `workspace.ModelSimulationStatus, error`。
- workspacehelpers.Poll(client, packageID, modelName, tabID)：调用 `/user/userspace/change/poll` 并提取状态；返回 `PollStatuses, error`。
- workspacehelpers.WaitTaskFinished(client, packageID, modelName, statusField, tabID, contextName, requireObserved)：等待空间级编译/仿真任务结束；返回 error。

使用注意：
- 创建实验后必须调用 `DeleteExperiment` 清理，建议放入 `RunWithHooks.After` 或 `defer`。
- 启动仿真后优先使用 `WaitWithTranslateFinished`，不要使用旧 `/api/v3/simulation/state/get` 作为完成依据。
- 这些 helper 会访问真实后端、DB 和 workspace poll，依赖 API 测试环境变量。
- 如果测试只需要验证接口参数校验，不要无谓创建真实仿真实验。

## 实时仿真 helper

适用场景：
- 测试场景涉及实时 FMU 变量订阅、实时参数、SSE stream、Redis stream 样本或交互式仿真开关。

主要文件：
- tests/helpers/simulation/realtime.go
- tests/helpers/client.go
- tests/helpers/response_helpers.go

常用函数：
- simulationhelpers.SubscribeRealtimeVariables(client, experimentID, variables, contextName, maxWait)：反复调用实时变量设置接口直到成功；返回唯一 taskID 和 error；超时或响应格式异常返回 error。
- simulationhelpers.RealtimeSingleTaskIDFromVariablesResponse(resp, contextName)：从响应 `data.task_ids` 解析唯一 taskID；返回 string 和 error；缺失或非唯一返回 error。
- simulationhelpers.WaitRealtimeSampleStreamAvailable(taskID, contextName, maxWait)：等待 Redis Stream 至少出现一条样本；等待期间输出 Redis/订阅 key 诊断，超时时补充 TIC/DB 记录诊断；返回 error；超时或 Redis 错误返回 error。
- (*APIClient).SetRealtimeFmuVariables / SetRealtimeFmuVariablesRaw：设置实时输出变量；返回统一响应或原始响应。
- (*APIClient).SetRealtimeFmuParameters / SetRealtimeFmuParametersRaw：设置实时在线调参参数；返回统一响应或原始响应。
- (*APIClient).GetRealtimeFmuStream / GetRealtimeFmuStreamRaw：访问实时 FMU stream；Raw 版本用于 SSE 正常场景。
- helpers.AssertSSEContentType(t, resp, message)：断言 SSE content type；失败 `t.Fatalf`。
- helpers.ReadSSEEvent(reader)：读取一个 SSE event；返回事件名、payload、error。

使用注意：
- 实时仿真通常依赖用户设置中的 interactive 开关，测试结束后要恢复/关闭。
- `WaitRealtimeSampleStreamAvailable` 依赖 Redis 连接配置。
- SSE Raw 响应 body 需要调用方关闭；可配合 `ReadAndCloseRawBody` 或 defer close。
- 实时仿真测试通常创建实验、设置短 stop time，并等待自然结束或显式清理。

## 资源文件与 multipart helper

适用场景：
- 测试场景涉及模型 Resources、资源库文件、图片上传/下载、multipart 原始请求或上传响应 path。

主要文件：
- tests/helpers/resource/file.go
- tests/helpers/resource/multipart.go
- tests/helpers/client.go

常用函数：
- resourcehelpers.ExtractPathOrFallback(resp, packageName, parent, fileName)：从上传响应读取 path，缺失时按 Modelica URI 构造兜底；返回 string；不主动失败。
- resourcehelpers.BuildModelicaResourceURI(packageName, parent, fileName)：构造 `modelica://.../Resources/...` URI；返回 string；不失败。
- resourcehelpers.ReadTestPicturePNG()：读取固定测试图片；返回 bytes 和 error；文件缺失返回 error。
- resourcehelpers.PostMultipartRaw(path, fields, includeFile, fileField, fileName, content)：发送 multipart POST；返回原始响应、body、error；请求失败或读 body 失败返回 error。
- (*APIClient).UploadResourcesFile / DownloadResourcesFile / GetResourcesFileDataRaw：模型 Resources 文件上传、下载和原始读取。
- (*APIClient).CreateResourceFolder / UploadResourceFile / GetResourceFileContent / ParseResourceFileContent / DeleteResource：资源库相关操作。

使用注意：
- 上传资源、创建文件夹或资源库记录后必须清理。
- `PostMultipartRaw` 适合缺文件字段、错误字段名、非法 multipart 这类负向回归。
- `ReadTestPicturePNG` 只读固定资源，不创建数据。
- Resources 路径和服务端 static 文件不同；需要验证磁盘副作用时再结合 `modelhelpers.LocalStaticPath`。

## 管网 helper

适用场景：
- 测试场景涉及管网信息 XML、映射表 JSON、管网模型生成、下载记录、列表可见性或 DB 存在性。

主要文件：
- tests/helpers/pipenet/fixture.go
- tests/helpers/client.go

常用函数：
- pipenethelpers.UniqueName(prefix)：生成唯一管网测试记录名；返回 string；不失败。
- pipenethelpers.ReadPipeNetResource(fileName)：读取 `tests/resources/pipenet` 固定资源；返回 bytes 和 error。
- pipenethelpers.UploadInfoFileFixture(client, name, description)：上传管网信息 XML fixture；返回记录 ID 和 error；上传失败返回 error。
- pipenethelpers.UploadMappingConfigFixture(client, name, description)：上传映射表 JSON fixture；返回记录 ID 和 error。
- pipenethelpers.CleanupInfoFiles(client, ids)：清理管网信息文件记录；返回 error；接口失败返回 error。
- pipenethelpers.CleanupMappingConfigs(client, ids)：清理映射表记录；返回 error。
- pipenethelpers.CleanupGeneratedModel(client, username, packageID, modelName)：清理管网生成模型和下载记录；返回 error。
- pipenethelpers.FindInfoFileIDByName / FindMappingConfigIDByName：按名称查询 DB 记录 ID；返回 string 和 error。
- pipenethelpers.InfoFileExists / MappingConfigExists：按 ID 判断 DB 记录存在性；返回 bool 和 error。
- pipenethelpers.ResponseListContainsID(resp, listKey, id)：检查响应 data 中某列表是否包含 ID；返回 bool 和 error。
- pipenethelpers.URLFromResponse(resp, message)：从统一响应 `data.url` 获取下载路径；返回 string 和 error。
- pipenethelpers.StringValue(value)：安全转 string；返回 string；不失败。

使用注意：
- 上传管网信息文件和映射表会创建数据库记录，必须调用对应 cleanup。
- 生成管网模型会创建模型和下载记录，需使用 `CleanupGeneratedModel` 或等价清理。
- 管网 helper 依赖固定测试资源和真实后端/DB。
- 适合 API 接口测试和 Bug 回归测试直接复用，尤其是准备 XML/JSON fixture 和验证列表/DB 副作用。

## 用户设置 helper

适用场景：
- 测试场景涉及用户设置、实时仿真开关、网格显示或设置恢复。

主要文件：
- tests/helpers/user_settings.go
- tests/helpers/client.go

常用函数：
- helpers.ParseUserSettings(resp)：从 `/user/settings/get` 响应解析完整设置；返回 `UserSettings, error`；响应 data 类型错误或字段缺失返回 error。
- (*APIClient).GetUserSettings()：获取用户设置；返回统一响应和 error。
- (*APIClient).SetUserSettings(gridDisplay)：设置网格显示；返回统一响应和 error。
- (*APIClient).SetUserSettingsFull(gridDisplay, interactive)：设置网格显示和实时仿真 interactive；返回统一响应和 error。

使用注意：
- 修改用户设置会影响后续测试，必须在 `After` 中恢复或按约定关闭。
- 实时仿真测试通常需要先开启 interactive，测试后关闭。
- 依赖当前测试用户和 workspace 环境变量。
- 适合 API 接口测试和 Bug 回归测试直接复用，但不要把用户设置修改留到测试结束后。

## 常见组合模式

适用场景：
- 需要把接口测试需求或 Bug Card 转为自动化测试草案或实现时，可从这里复制结构再填入业务细节。

主要文件：
- tests/helpers/hooks.go
- tests/helpers/assertions.go
- tests/helpers/model/file_wait.go
- tests/helpers/simulation/experiment.go
- tests/helpers/simulation/wait.go

常用函数：
- RunWithHooks + NewAssertResponse + APIClient 方法调用：标准 API/Bug 自动化测试结构；API 接口测试的主测接口按 `yslab-api-test` 使用 `Primary()`、`PrimaryGet()` 或 `PrimaryPost()`，Bug 回归测试按 `bug-regression-test` 默认直接调用 `testClient.Xxx(...)`。
- CreateExperiment + ModelSimulateWithTranslate + WaitWithTranslateFinished：创建并完成一次新版仿真；返回实验 ID/响应/error；失败需要 `t.Fatal`。
- FileExpectationSkipReason + ModelPackageRootPath + WaitForFileExpectations：远端文件副作用断言链；返回 skip 原因或 error；超时说明文件状态不符合预期。
- GetRawResponse/PostRawResponse + ReadAndCloseRawBody/AssertSSEContentType/ReadSSEEvent：原始 HTTP/SSE 断言链；失败返回 error 或 `t.Fatalf`。

使用注意：
- 组合模式不是替代具体 helper 文档；实现前仍要确认目标接口已有封装和清理路径。
- 创建实验、模型、资源、管网文件都需要清理。
- 远端文件断言只在 `/YSSIM` 可访问时可靠；本地调试可显式跳过文件断言，但不能跳过核心接口和 DB/响应断言。

示例：

```go
assert := helpers.NewAssertResponse(t)
var expID string

helpers.RunWithHooks(t, helpers.TestHooks{
    Before: func(t *testing.T) {
        var err error
        expID, _, err = simulationhelpers.CreateExperiment(testClient, simulationhelpers.ExperimentOptions{
            PackageID:  testPackageID,
            ModelName:  testModelName,
            NamePrefix: "bug_1234",
        })
        if err != nil {
            t.Fatal(err)
        }
    },
    After: func(t *testing.T) {
        simulationhelpers.DeleteExperiment(testClient, expID)
    },
}, func(t *testing.T) {
    resp, err := testClient.ModelSimulateWithTranslate(expID)
    if err != nil {
        t.Fatalf("提交仿真失败: %v", err)
    }
    assert.IsSuccess(resp, "提交仿真")
    if err := simulationhelpers.WaitWithTranslateFinished(testClient, testPackageID, testModelName, expID, "BUG_1234 仿真"); err != nil {
        t.Fatal(err)
    }
})
```
