# 评论、弹幕与点赞接口

## 升级边界

本轮只实现后端，不更新前端工程、不自动执行数据库变更。先暂停相关写入、备份并审阅
`database/migrations/20261031_comment_barrage/update_schema.sql`，补齐索引、约束和历史回复数后，
协调升级视频服务、网关和前端。旧小时/分钟/秒字段、页码参数及旧切换型点赞请求不再使用。
游标已改为结构化对象，不再接收或返回 Base64 字符串；HTTP 和 RPC 必须配套升级，不做旧游标回退。
用户已将弹幕时间列改为 `publish_time` 整数秒，并删除 minute/second，脚本不再重复调整这些列。
迁移脚本不是可重复运行脚本，预检查询非空或相关索引、约束已存在时必须停止并先核对。

评论与弹幕点赞只维护视频域的点赞关系和对象计数，不调用用户服务修改获赞总数。
有效关系不设 TTL，取消时硬删除。其他视频互动的 Saga 与 GetUserVideoStatus 位掩码不在本轮重构范围。
旧评论补偿、旧弹幕分支和状态写入 RPC 仍明确拒绝，不能作为新点赞流程调用。

## 评论读取

- `GET /api/v1/video/{video_id}/comment`：只返回主评论，不携带回复。
- `GET /api/v1/video/{video_id}/comments/{comment_id}/replies`：comment_id 必须是主评论，读取整楼平铺回复。
- 两者均需登录，身份只来自鉴权 context。每次最多 30 条；额外预读一条判断 hasMore。
- `sort_by=created_at|cnt_upvote`，`order=asc|desc`；点赞数仅支持 desc。
- 默认主评论 created_at desc，回复 created_at asc；同排序值以 comment_id 同向排序。
- 首批不传 cursor，后续将上批 nextCursor 对象的字段展开为 GET 查询参数；切换视频、楼层或排序须清空游标。
- 响应保留 comments/replies，增加 nextCursor、hasMore；主评论 totalCount 为该视频有效主评论总数。
- 空列表为 []，到末尾 hasMore=false 且 nextCursor=null。显式空游标对象不是首批，仍须通过字段校验。
- 热度变化可能重复或遗漏，不提供排名快照。
- 每条包含 isUpvoted、parentId；回复的 reference 包含直接回复对象 ID、发布者 ID 和完整正文，不递归引用。
- 已删除、缺失或不属于同视频同楼的引用返回 unavailable=true，不泄露旧正文。
- root_id 表示所属主评论，parent_id 表示直接回复对象。主评论 parent_id=-1，root_id 等于自身 ID。
- 主评论 cntReplies 表示整楼有效回复数；回复条目的 cntReplies 不用于展示嵌套层级。

## 点赞写入

保留 PUT 路由，评论请求提供 `liked: true|false`；弹幕请求同样提供 liked。
字段必须显式存在，false 表示取消，不是省略。网关不预读状态，视频服务校验对象属于目标视频。
评论响应增加 isUpvoted/upvoteCount，弹幕响应同样返回最终目标状态和计数。
重复点赞或取消不重复改计数；关系和原子计数在同一本地事务内提交，计数下溢或写入失败整体回滚。
读取批次的状态由一条关系查询补齐，不逐条查询，也不初始化未点赞记录。

## 弹幕

- `GET /api/v1/video/{video_id}/barrages?start_second=0&end_second=60`。
- 需登录，区间为左闭右开 [start_second, end_second)，单次范围最多 3600 秒。
- 时间均为非负整数秒，数据库字段 publish_time，发送字段 position_second，读取条目 positionSecond。
- 每批最多 500 条，按数据库 publish_time asc、barrage_id asc；同秒弹幕可继续翻页。
- 响应 barrages/nextCursor/hasMore；同一区间须读取至 hasMore=false 后才能视为完整加载。
- 游标绑定视频和区间。拖动播放进度后直接查询新区间，不复用原游标。
- 条目包含弹幕/视频/发布者 ID、正文、颜色、播放位置、点赞总数及个人点赞状态。
- 发送内容 1-150 字符，颜色格式 #RRGGBB；客户端不能指定初始点赞数。
- 视频详情不再全量加载弹幕；旧详情 RPC 的 barrages 字段保留为空，仅供协议退役过渡。

ID 和 int64/uint64 沿用 protobuf JSON 的字符串表示。下游 gRPC 只应暴露给可信服务。

## 结构化游标

评论的 nextCursor 是包含 videoId、rootId、sortBy、order、commentId、createdAt、upvoteCount 的对象。
createdAt 使用 RFC3339 时间，保留服务端返回的精度；commentId、videoId、rootId 和 upvoteCount 使用字符串，
不要在 JavaScript 中先转为 Number 再传回。rootId=0 表示主评论，回复列表使用所属主评论 ID。

GET 参数使用点号展开，snake_case 和 lowerCamelCase 都可绑定。例如按时间倒序续查：

```text
GET /api/v1/video/9/comment?sort_by=created_at&order=desc&cursor.video_id=9&cursor.root_id=0&cursor.sort_by=created_at&cursor.order=desc&cursor.comment_id=123&cursor.created_at=2026-11-01T12%3A00%3A00.123Z&cursor.upvote_count=3
```

弹幕的 nextCursor 包含 videoId、startSecond、endSecond、positionSecond、barrageId，
同样展开为 cursor.video_id、cursor.start_second 等参数。零秒是有效位置，不能据此判断游标不存在。
客户端应通过 URLSearchParams 等标准 API 编码参数，而不是把整个对象变成 JSON 字符串作为 cursor 参数。

下一批的位置取本批最后一条实际返回的记录。第 31 条评论或第 501 条弹幕只用于判断 hasMore，
不作为游标；否则下一次严格比较会跳过这条尚未返回的记录。同一时间、点赞数或播放秒数下，
继续用对象 ID 作同向比较，保证同值记录仍能被逐批读取。
