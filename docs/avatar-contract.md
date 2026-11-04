# 头像读取与更新

## 注册后更新

注册请求不接收头像。用户服务生成用户 ID，并将头像指向共享默认图片
`resources/default/avatar.jpg`，不再复制默认头像或由上传流程生成用户 ID。

注册成功响应保留双 token，新增 `data.userId` 字符串。网关在返回成功前缓存 access token，
有效期与登录流程一致。前端在注册页面选择头像后，可以依次执行：

1. 注册，保存返回的用户 ID、access token 和 refresh token。
2. 调用头像更新接口，使用 `Authorization: Bearer <access token>` 与
   `Refresh-Token: Bearer <refresh token>` 两个请求头。
3. 头像失败时保留已注册账号，允许重试更新，不重复注册。

如果账号已创建但签发或缓存登录态失败，返回 `REGISTRATION_SESSION_FAILED`，
metadata 包含 `user_created=true`，不返回不可用 token。此时应转到登录，不盲目重新注册。
注册、登录态和头像不是跨服务的大事务。

## 更新本人头像

`PUT /api/v1/user/profile/avatar`

- 要求有效登录态，目标身份只从鉴权 context 获取，不接收目标用户 ID。
- 请求为 multipart/form-data，文件字段名为 `avatar`。表单或查询中伪造 user_id 不会改变目标。
- JPEG/PNG 文件最大 5 MiB，单边尺寸最多 4096。用户服务解析实际图片内容，不信任文件名、
  扩展名或声明的 Content-Type；JFIF 文件若实际为 JPEG，也按 JPEG 保存。
- 响应包含 statusCode、message、avatarUrl，例如 `/api/v1/users/123/avatar`。
- 新图片使用本用户目录下的唯一文件名。验证、写入并关闭后，才比较旧路径并切换数据库。
  只有成功切换才清理旧的自定义头像，共享默认头像不删除。发生旧路径冲突返回 409，
  不重试覆盖其他请求的新头像。
- 首次设置、旧文件丢失和后续替换统一走本接口。空上传、重复元信息、传输中断、
  非图片或过大数据不更新数据库，也不截断旧文件。

匿名 UploadAvatar HTTP/RPC 和旧 `PUT /api/v1/{user_id}/profile/avatar` 已移除，无旧入口回退。

## 公开读取

`GET /api/v1/users/{user_id}/avatar`

- 无需登录，直接返回图片二进制和实际 image/jpeg 或 image/png Content-Type，不是 JSON/base64。
- 仅接受用户 ID。用户不存在或已删除返回 404；用户存在但头像路径为空或文件丢失时回退默认头像。
  默认图片也不存在时返回 404，存储路径越界或图片损坏返回错误，不读取任意服务器文件。
- 文件读取仅允许本用户目录中的头像文件和固定共享默认图片，并验证符号链接解析后的范围。
- 响应包含 Content-Length、X-Content-Type-Options: nosniff、Cache-Control: no-cache 和内容哈希 ETag。
  浏览器可重新验证缓存；If-None-Match 匹配时返回 304，替换内容后返回新图片和新 ETag。
- 头像大小受限，内部使用一次 gRPC 返回图片；网关不创建本地临时头像文件。
  仅头像读取调用将接收消息上限设为 5 MiB 加 64 KiB 协议开销，图片本身仍不得超过 5 MiB。
  上传继续使用 32 KiB 分块，无需放宽用户服务接收上限或其他 RPC 的消息限制。
- 网关将内存头像与视频、封面、DASH 临时文件统一交给 `http.ServeContent` 发送，
  保留各自的内容类型与缓存头；只有网关临时文件在响应结束后关闭并删除，不删除用户服务原图。

用户资料及关注摘要保留原字段名 `avatarPath` 和字段编号，但值已改为上述 API 相对 URL。
前端必须相对于后端 API origin 解析，不按前端开发服务器的 origin 或磁盘路径解析。
不要把本地 Object URL 作为持久头像地址。

## 升级与验证边界

先升级用户服务，再升级网关，并同步前端头像请求及注册后流程。此轮不修改前端工程、
数据库 schema、部署清单或对象存储，也不移动、删除现有资源文件或执行真实业务写入。
已有数据库路径仍由用户服务内部读取，空值和丢失文件按上述规则处理，不做批量迁移。

本地文件与数据库不是原子分布式事务，进程崩溃可能留下未引用文件；数据库写入结果不确定、
文件删除失败或多副本资源目录不共享等运维情况不视为已经自动修复。当前方案继续要求
用户服务实例能够访问该用户的实际资源目录，不依赖网关和用户服务在同一主机。
