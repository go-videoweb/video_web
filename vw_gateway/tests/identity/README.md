# 用户 · 身份验证接口测试

本模块对应测试方案 U01 登录、U02 注册、U03 退出，基于 2026-11-27 当前源码。测试场景集中在 `suite.go`，远程入口位于 `../api/identity_api_test.go`，本地 HTTP 接线验证位于 `suite_test.go`。场景串行执行，不自动登录重试、刷新 Token 或重放注册。

## 已实现的场景

| 接口 | 检查内容 |
| --- | --- |
| `POST /api/v1/user/login` | 成功响应业务状态、正数且为字符串的 userId、双 Token；携带双 Token 访问受保护接口并核对身份；错误密码、不存在用户、空用户名、空密码；失败登录后原会话仍有效；只带一种 Token 被拒绝；重登产生新凭证、旧会话失效；错误 JSON 根类型被拒绝 |
| `POST /api/v1/user/register` | 无效邮箱、重复用户名、密码低于 6 字节/超过 72 字节、两次密码不一致、签名超过 25 个字符、缺验证码；显式启用后的成功注册返回 ID/双 Token、实际登录态可用、重复注册返回邮箱已占用 |
| `POST /api/v1/user/{user_id}/logout` | 当前账号退出成功、原 Token 无法访问保护接口、重复退出成功、重新登录后会话恢复；结束后注销测试会话 |

会话回读使用 `GET /api/v1/favorites/list`，核对返回身份及数组协议。它只是身份断言的辅助请求，不计为收藏模块主测。主测请求输出 `API_MARK`，登录前置、回读和清理不标记。登录/注册成功结果不只检查 HTTP 200，失败结果同时检查 HTTP 200 与业务码。日志不打印 Token、密码、邮箱、邮件码或原始错误 metadata。

当前空用户名实际返回 4005（用户不存在）、空密码返回 4006（密码错误），不使用尚未接入的空字段专用错误码。重登替换和退出导致的旧会话失效均返回 3002；仅缺一种 Token 返回 3003。注册校验按当前业务顺序断言：邮箱格式 4109、重复用户名 4007、密码长度 4110、不一致 4009、签名 4010、缺码 4114；已有邮箱优先返回 4102 `EMAIL_IN_USE`。改变校验顺序或错误契约时应同步审查用例，不能接受任意业务失败。

## 本地验证

在 `vw_gateway` module 中执行：

```powershell
go test -mod=readonly -count=1 ./tests/helpers ./tests/identity
```

使用真实 Kratos HTTP 路由、鉴权和网关业务逻辑，用户 RPC 仓储与 Redis 由内存替身代替。替身使用超过 JavaScript 安全整数的大 ID，验证字符串序列化；模拟注册后缓存失败，检查 3101 `REGISTRATION_SESSION_FAILED`、`user_created=true`、没有 Token 数据且没有自动重复创建。

注册负向错误由替身按序返回，这部分仅证明请求字段、路由、错误传播和场景断言接线正确，不证明真实用户服务会返回这些错误。远程运行才能验证实际用户服务、数据库和 Redis 的链路；本地通过不能替代联调结果。

## 远程执行

先准备可重复登录、拥有正常初始化数据的专用账号；所有选中场景串行执行，同一账号不得被其他进程使用。登录会替换该账号已有会话，退出会清除登录态。测试不会恢复测试前的登录会话。

```powershell
$env:API_TEST_BASE_URL = 'http://127.0.0.1:8001'
$env:API_TEST_USERNAME = 'identity-test-user'
$env:API_TEST_PASSWORD = '<专用账号密码>'
$env:API_TEST_REGISTER = '0'
go test -mod=readonly -tags api_test -count=1 -json -run '^TestIdentity(Login|Register|Logout)$' ./tests/api
```

默认注册只运行不会创建用户的前置失败路径；用户名和 `.invalid` 邮箱使用随机后缀隔离，不自动发送邮件。成功注册子场景会明确显示 SKIP，其他用例通过不代表注册成功路径通过。只运行某个接口时，将 `-run` 改为对应完整测试名。

需要验证成功注册时，另外准备未注册的用户名和邮箱，以及通过验证码接口或测试邮箱获得的 **register 用途、仍有效** 的邮件码。然后执行一次：

```powershell
$env:API_TEST_REGISTER = '1'
$env:API_TEST_REGISTER_USERNAME = '<未注册用户名，须与登录账号不同>'
$env:API_TEST_REGISTER_EMAIL = '<未注册邮箱>'
$env:API_TEST_REGISTER_PASSWORD = '<6 至 72 字节密码>'
$env:API_TEST_REGISTER_CODE = '<有效 register 邮件码>'
go test -mod=readonly -tags api_test -count=1 -json -run '^TestIdentityRegister$' ./tests/api
```

这会实际创建一个账号，成功后记录用户 ID，并注销新会话；没有账号删除接口，不自动删除用户或关联数据。下一次运行须重新准备新账号和验证码，或者关闭 `API_TEST_REGISTER`。验证码只用于首次成功创建请求；随后故意重复同一资料验证邮箱冲突，不应再次创建用户。成功注册响应不确定或会话创建失败时立即报错，请核对账号状态，不能直接重试。环境变量应由本地安全终端或 CI 凭据参数注入，不写入仓库或构建日志。

## 尚需环境或业务规则支持的场景

- 注册验证码的过期、错误次数耗尽、用途隔离与并发单次消费：需要可控验证码夹具和时钟，留在后续验证码模块及集成测试中验证。
- 密码 6/72 字节、签名 25 字符等成功边界、默认头像和关联记录初始化、注册事务回滚：需要独立邮件码及可重置的数据环境；当前尚未完整覆盖。
- 登录/退出的 Redis 故障、注册缓存失败的真实远程链路、并发重登与缓存一致性：需要故障注入，当前仅本地验证注册会话部分失败。
- 过期 Token 刷新、签名/issuer/nbf 等完整鉴权矩阵：属于公共鉴权模块，当前仅验证双 Token 缺失、会话替换及注销失效。
- 当前退出接口在公开白名单中，按路径 ID 删除会话；测试只操作本场景账号。跨用户注销需要先明确并修复权限契约，不把允许注销他人写成成功用例。

本轮不添加 TeamCity 配置。未来流水线应分别记录本地回归、远程基础场景和一次性邮件注册场景的结果，保留退出码及 SKIP 信息，避免把接口执行覆盖误认为场景覆盖完整。

## 本轮验证记录（2026-11-27）

- `tests/helpers`、`tests/identity` 和 `internal/service/user_service` 本地回归通过。
- `api_test` 标签的 `tests/api` 编译通过，未执行真实远程请求或发送邮件。
- 鉴权包全量测试仍因既有 `auth_test.go` 引用未定义的 `query.User` 无法编译；按项目约定单独运行 `auth.go optional_test.go policy_test.go session_id_test.go` 已通过，没有修改或运行该历史数据库测试。
- 使用 Go 1.26.8、离线依赖和 `-mod=readonly`；首次编译遇到 C 盘空间不足，随后使用 D 盘进程级临时目录和缓存完成上述验证。没有修改 Go module 依赖或业务实现。
