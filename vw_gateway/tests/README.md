# API 测试骨架

本目录提供 `vw_gateway` 的接口测试基础设施和用户身份验证用例，尚未实现接口覆盖率工具或 TeamCity 配置。2026-11-22 已适配双 Token、统一业务错误、媒体条件请求和异步投稿轮询；2026-11-27 新增登录、注册、退出场景。

## 目录约定

- `api/`：带 `api_test` 构建标签的远程 API 测试入口。
- `helpers/`：HTTP 客户端、响应解析、断言、生命周期钩子和覆盖标记。
- `identity/`：可复用的登录、注册、退出场景及本地 HTTP 接线测试，运行与覆盖边界见 [身份测试说明](identity/README.md)。
- `resources/`：测试需要上传的固定资源文件。

接口测试默认通过环境变量配置，不依赖 `tmp/tests` 中的外部环境、数据库或业务配置：

```powershell
$env:API_TEST_BASE_URL = 'http://127.0.0.1:8001'
$env:API_TEST_USERNAME = 'test-user'
$env:API_TEST_PASSWORD = 'test-password'
go test -mod=readonly -tags api_test -json -count=1 ./tests/api/...
```

带标签入口检查配置后执行所选业务用例，会真实登录和注销专用测试账号。不要与人工操作或其他测试进程共享账号。没有配置、只选配置检查、零用例或全跳过都不能形成成功回归。业务测试通过 `runBusinessCase(t, fn)` 执行，只有未跳过的业务场景才计数。成功注册默认跳过，必须按身份测试说明显式启用；默认通过不代表成功注册已经验证。

## 离线验证

在 vw_gateway module 内执行：

```text
go test -mod=readonly -count=1 ./tests/helpers ./tests/identity
```

本地测试使用 httptest 回环服务；身份测试沿用真实网关 HTTP 路由、JSON 编解码和 JWT，用户 RPC 仓储及 Redis 使用内存替身，不需要 MySQL、Consul、DTM、SMTP 或实际媒体。替身不证明用户服务校验、数据持久化或邮件链路正确。当前不运行全模块测试，以免触发现有业务库和生成测试；普通运行不会激活带 api_test 标签的入口。

## 请求与身份

匿名客户端不设置 Token；登录后从响应读取两个 Token，显式创建身份视图：

```go
anonymous := helpers.NewAPIClient(cfg)
viewer := anonymous.WithTokens(accessToken, refreshToken)
resp, err := viewer.Do(ctx, http.MethodGet, "/api/v1/video/list", nil, "", helpers.RequestOptions{
    Query: url.Values{"class": {"测试分类"}, "page_num": {"1"}, "page_size": {"20"}},
})
```

`WithTokens("", "")` 返回匿名视图，不影响已有会话。AccessToken 和 RefreshToken 分别对应 Authorization、Refresh-Token；不再存在 AnonymousToken/SessionToken 的回退语义。需要负向鉴权时可故意只设置一个 Token，不自动补齐。

RequestOptions 可传给 Do、DoJSON、DoMultipart，最多一项。Query 按键替换已有查询参数，重复值保留；结构化游标按当前 Kratos 查询绑定传入，例如 cursor.video_id，不手拼未经编码的 JSON。Headers 在默认认证头之后应用，可设置 Range、If-None-Match 或覆盖非法凭证；空值列表删除对应头。

客户端仅接受相对路径或同源绝对 URL，不自动跟随重定向，不自动登录、刷新或重试业务。BaseURL 可包含反向代理路径前缀，但不能包含 userinfo、查询参数或片段。编码路径和点段保持原样，供路径校验用例使用。共享身份视图可并发发送请求，但不要同时修改同一个客户端的 Token。

响应默认最多读取 8 MiB，API_TEST_RESPONSE_SIZE 可按夹具体积设置更大上限。媒体测试保留原始 Body/Header，不强制解码 JSON；304 应断言状态和空正文，206 应核对 Content-Range 及实际字节。不要取消大小限制来下载不受控原片。

## 业务错误与诊断

经网关 ErrorEncoder 的错误统一为 HTTP 200，业务 code/reason/message/metadata 在响应体内。路由不匹配、代理错误或媒体 Range 等仍应独立断言 HTTP 状态，不能假定所有响应都是 200。

- `AssertHTTP2xx` / `Response.IsSuccess` 只判断 HTTP 状态，不代表业务成功。
- `AssertAPIJSONSuccess` 判断 JSON 响应没有 Kratos 错误；各接口 statusCode、resp 和业务数据仍须另行断言。
- `AssertAPIError` 校验 HTTP 200、业务码和可选 reason，返回 metadata 供刷新或部分成功断言使用。
- `DecodeJSON` 使用 json.Number 并拒绝尾随 JSON；ID 的字符串表示契约仍需类型化断言，不将大 ID 转成 float64。
- `Diagnostic` 只保留有限 JSON 结构、数字和布尔值，字符串和 metadata 均省略；非 JSON 正文不输出。不要直接打印 Text、Body、Token 或 APIError.Metadata。JSON 比较失败也不输出原始实际值。

错误码从当前各服务 ecode 定义及接口契约核对，不沿用历史 400/404/501 假设，不通过放宽断言接受任意业务失败。刷新只先断言原始错误与 metadata，再由专门用例显式更新 Token、重发请求。

## 投稿轮询与夹具

```go
waitCtx, cancel := context.WithTimeout(ctx, 2*time.Minute)
defer cancel()
status, err := viewer.WaitForVideoUpload(waitCtx, videoID, time.Second)
```

WaitForVideoUpload 只请求发布者状态接口。ctx 必须有总截止时间，轮询间隔必须为正；completed 成功，failed 立即失败，未知状态、网络/HTTP/业务错误立即停止，不重传原片。返回最后状态便于检查，UploadErrorMsg 不应直接写入日志。

投稿按“元信息 -> 封面 -> 原片 -> 处理完成”组织。原片落盘返回不代表 DASH 完成；公开详情、列表及媒体验证须使用已完成夹具。轮询可能跳过中间状态，不要求每次观察到完整序列。

同发布者同标题会复用原视频，独立用例要使用带 run ID 的唯一标题；验证幂等复用时才显式使用同一标题。身份、现有视频/分类和媒体由测试环境准备，不在骨架启动时自动创建。用户、视频、评论缺少删除接口，写入测试仍需明确独立环境和重置策略；查询详情和登录也可能有计数、观看时间或会话副作用。

## 后续用例与 CI

当前生成代码显式注册 49 个 HTTP 路由（9 个服务）。清单需从当前 descriptor 与实际注册核对，不能将 49 硬编码为未来分母，也不能把未匹配路由的任意 HTTP 响应算作接口执行覆盖。

| 范围 | 后续用例重点 |
| --- | --- |
| 登录/注册/退出 | 已有首批用例，详见 identity/README.md；故障注入与并发一致性仍待补充 |
| 投稿 | 同名复用、原片前置封面、发布者权限、异步完成/失败、未完成不公开 |
| 收藏/详情 | favorite=1/-1、取消必须携带 favoriteRecordId、重复操作、游客无个人状态 |
| 评论/弹幕 | 游标绑定视频/楼层/排序，liked=false 与字段省略不同，不沿用旧停用预期 |
| 头像/媒体 | avatar multipart、5 MiB 限制、公开读取、ETag/304/Range；旧匿名上传路由已移除 |
| 验证码/找回 | purpose、发送/验证配额、原始业务码、会话失效与部分成功；真实邮件单独启用 |

覆盖工具和 TeamCity 仍未实施；后续须保留真实测试退出码，失败也归档本轮日志，记录源码/部署版本，且不把零业务用例或单纯配置校验当作成功。轮询、登录前置和清理不标记为主测；实际主测使用显式 method、path template 与 operation，不能猜测 ID 模板。

完整设计的历史来源为工作区根 API_TEST_ADOPTION_PLAN.md；本目录已提交的代码与本文是当前骨架状态的依据。分批修改记录见后端 docs/api-test-skeleton-batches.md。
