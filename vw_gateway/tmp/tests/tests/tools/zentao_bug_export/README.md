# 禅道 Bug 导出解析工具

`zentao_bug_export` 用于把禅道导出的 Bug 列表转换为结构化 JSON，供 AI 后续生成 Bug Card、澄清问题和回归测试用例使用。

## 设计目标

- 主要读取禅道 XML 导出，解析每个 Bug 的标题、模块、状态、重现步骤、解决信息等字段。
- 以禅道 HTML 导出作为附件补充源，按 Bug 编号提取附件名称、`fileID` 和下载链接。
- 可选下载模型类资源附件到临时目录，并在 JSON 中只记录 `local_path`，避免把 `.mo` / `.zip` 等大文件内容交给 AI。
- 从 XML `steps` 字段中提取内嵌图片引用，例如 `{1291.png}` 和 `fileID=1291`。
- 标记 `has_image_attachment`、`has_video_attachment`、`has_model_resource_attachment`，但默认不分析视频内容。
- 输出稳定 JSON，减少 AI 直接读取原始 HTML/XML 的 token 消耗和解析不确定性。

## 目录结构

```text
tests/tools/zentao_bug_export/
  main.go          # CLI 入口
  config/          # 命令行参数解析
  output/          # JSON 输出
  zentao/          # 禅道 HTML/XML 解析、文本处理和数据定义
```

## 基本用法

从仓库根目录运行：

```bash
go run ./tests/tools/zentao_bug_export \
  -xml "C:/path/to/zentao-bugs.xml" \
  -html "C:/path/to/zentao-bugs.html" \
  -resource-dir "tmp/zentao_bug_attachments" \
  -out "tests/bugRegression/zentao_bugs.json"
```

如果禅道附件链接需要登录态，把 Cookie 写入本地私有文件，工具默认读取 `tmp/zentao_cookie.txt`。文件内容建议只写 Cookie 值，例如 `zentaosid=...; lang=zh-cn`；如果从浏览器复制成 `Cookie: zentaosid=...`，工具也会自动去掉 `Cookie:` 前缀：

```powershell
Set-Content -LiteralPath "tmp/zentao_cookie.txt" -Value "zentaosid=...; lang=zh-cn"
go run ./tests/tools/zentao_bug_export `
  -xml "C:/path/to/zentao-bugs.xml" `
  -html "C:/path/to/zentao-bugs.html" `
  -bug-id 2132 `
  -resource-dir "tmp/zentao_bug_attachments" `
  -out "tests/bugRegression/BUG_2132/bug.json"
```

如需使用其他 Cookie 文件路径，传入 `-cookie-file`（也兼容 `-cookie_file`），或设置 `ZENTAO_COOKIE_FILE` 环境变量。`-cookie` 明文参数已弃用，工具会拒绝使用。

只解析单个 Bug：

```bash
go run ./tests/tools/zentao_bug_export \
  -xml "C:/path/to/zentao-bugs.xml" \
  -html "C:/path/to/zentao-bugs.html" \
  -bug-id 1108 \
  -out "tests/bugRegression/BUG_1108/bug.json"
```

输出到 stdout：

```bash
go run ./tests/tools/zentao_bug_export \
  -xml "C:/path/to/zentao-bugs.xml" \
  -html "C:/path/to/zentao-bugs.html" \
  -bug-id 1108 \
  -out -
```

## 参数说明

| 参数 | 必填 | 默认值 | 说明 |
| --- | --- | --- | --- |
| `-xml` | 是 | 无 | 禅道 XML 导出文件，作为主数据源。 |
| `-html` | 否 | 空 | 禅道 HTML 导出文件，用于补充附件 `fileID` 和下载链接。 |
| `-out` | 否 | `-` | JSON 输出路径；`-` 表示输出到 stdout。 |
| `-resource-dir` | 否 | 空 | 模型类附件下载目录；设置后只下载 `model` / `archive` 附件，例如 `.mo`、`.fmu`、`.mat`、`.zip`。 |
| `-cookie-file` / `-cookie_file` | 否 | `tmp/zentao_cookie.txt` 或 `ZENTAO_COOKIE_FILE` | 禅道附件下载 Cookie 文件；附件链接返回登录页时需要提供有效 Cookie。 |
| `-cookie` | 否 | 无 | 已弃用；不要使用，改为 `-cookie-file`。 |
| `-bug-id` | 否 | 空 | 只输出指定 Bug 编号；为空时输出全部 Bug。 |
| `-pretty` | 否 | `true` | 是否输出缩进后的 JSON。 |

## 输出结构

工具始终输出统一 envelope：

```json
{
  "source": {
    "system": "zentao",
    "xml_path": "...",
    "html_path": "..."
  },
  "summary": {
    "total": 136,
    "with_images": 97,
    "with_videos": 18,
    "with_model_resources": 4
  },
  "bugs": []
}
```

单个 Bug 的关键字段包括：

- `bug_id`、`title`、`product`、`module`、`project`
- `severity`、`priority`、`type`、`status`
- `steps_raw`：保留 `<img>` 等 HTML 片段的原始步骤
- `steps_text`：去除 HTML 标签后的纯文本步骤
- `files_raw`：XML `<files>` 原始附件文本
- `has_image_attachment`、`has_video_attachment`、`has_model_resource_attachment`
- `images`、`videos`、`model_resources`、`attachments`

## 附件处理规则

- XML `steps` 中的内嵌图片会进入 `images`，来源为 `source: "steps"`。
- HTML 附件列中的文件会进入 `attachments`，并按扩展名归类为 `image`、`video`、`model`、`archive` 或 `other`。
- HTML 附件中的图片会同时追加到 `images`。
- HTML 附件中的视频会同时追加到 `videos`，并设置 `has_video_attachment: true`。
- HTML 附件中的 `.mo`、`.mos`、`.fmu`、`.mat`、`.zip`、`.rar`、`.7z` 等模型类资源会同时追加到 `model_resources`，并设置 `has_model_resource_attachment: true`。
- 设置 `-resource-dir` 后，工具会尝试下载 `model_resources` 到 `tmp/zentao_bug_attachments/BUG_xxx/` 这类本地目录，并把路径写入 `local_path`；下载失败、返回禅道登录页或未提供 Cookie 时，保留 `download_url` 并写入 `collection_error` 和 `summary.warnings`。
- 设置 `-bug-id` 时，工具只下载目标 Bug 的模型类资源附件，避免单 Bug 解析误收集其他 Bug 的大文件。
- 工具不下载图片、不解析视频、不抽关键帧；视频内容由测试者在 AI 澄清环节总结。

## 模型资源附件规则

- 如果 Bug 单存在模型类资源附件，后续 Bug 回归测试必须使用这些附件，而不是让 AI 重新编造或手写近似模型。
- 如果 `has_model_resource_attachment: true` 但 `model_resources[*].local_path` 为空，说明附件尚未成功收集；该 Bug 不能判定为“可直接编写”，应先由测试者提供附件或重新运行工具下载。
- 大文件只通过 `local_path` 传递给 AI；不要把 `.zip`、`.fmu`、大型 `.mo` 的完整内容粘贴到对话中。

## 与 AI 流程的关系

当用户提供禅道导出文件并要求生成 Bug 回归测试时，AI 应先运行本工具，再读取 JSON 输出。除非工具失败或禅道导出格式变化，否则不要直接让 AI 阅读完整原始 HTML/XML。

推荐流程：

```text
禅道 XML/HTML 导出
→ go run ./tests/tools/zentao_bug_export -resource-dir tmp/zentao_bug_attachments
→ 结构化 JSON
→ AI 生成 Bug Card / 澄清问题 / 回归测试用例
```

## 验证

运行工具单元测试：

```bash
go test ./tests/tools/zentao_bug_export/...
```

在 Windows 本地如果环境变量中遗留了非 Windows `GOOS`，可显式指定：

```powershell
$env:GOOS = "windows"
go test ./tests/tools/zentao_bug_export/...
```

## 维护注意事项

- 如果禅道导出字段变化，优先调整 `zentao/xml.go` 中的 `xmlRow`。
- 如果 HTML 表格列名变化，优先调整 `zentao/html.go` 中的表头定位逻辑。
- 如果需要支持附件下载，应新增独立包，不要把下载逻辑混入 XML/HTML 解析函数。
- 如果需要支持视频关键帧，仍应作为可选后处理步骤，默认流程继续只记录视频存在性。
