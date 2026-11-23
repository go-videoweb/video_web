package zentao

import (
	"html"
	"path/filepath"
	"regexp"
	"strings"
)

var (
	// fileIDPattern 提取 fileID 后面的数字。
	// 结构：(?i) 表示不区分大小写；(?:fileID=|fileid=) 匹配两种写法；(\d+) 把后面的数字取出来。
	// 人话：只要链接或图片说明里写了 fileID，就把它后面的编号拿出来。
	// 示例1：`index.php?m=file&f=download&fileID=1862` 会提取 `1862`。
	// 示例2：`index.php?m=file&f=read&t=png&fileid=1291` 会提取 `1291`。
	fileIDPattern = regexp.MustCompile(`(?i)(?:fileID=|fileid=)(\d+)`)
	// braceFilenamePattern 提取花括号里的文件名提示。
	// 结构：\{([^{}]+)\} 表示取出一对花括号中间的内容。
	// 人话：像 "{1291.png}" 这种写法，就把中间那段文件名拿出来。
	// 示例：`{1291.png}` 会提取 `1291.png`。
	braceFilenamePattern = regexp.MustCompile(`\{([^{}]+)\}`)
	// htmlTagPattern 用于识别并移除 steps_raw 中的 HTML 标签。
	// 结构：(?s)<[^>]+> 会把一整段 <...> 当成一个标签，包含换行也能识别。
	// 人话：把 <img>、<p> 这些标签整段去掉，只留下文字。
	// 示例1：`<img src="{1.png}" />` 会被识别为一个标签。
	// 示例2：`<p class="desc">` 会被识别为一个标签。
	htmlTagPattern = regexp.MustCompile(`(?s)<[^>]+>`)
	// spacePattern 用于把单行文本里连续出现的空白合并成一个空格。
	// 结构：[...] 同时匹配普通空格、Tab 和不换行空格。
	// 人话：把一串空白压成一个空格，避免文本看起来挤成一团。
	// 示例1：`张三\t 李四` 中的连续空白会合并为一个空格。
	// 示例2：`A\u00a0\u00a0B` 中的不换行空格会合并为一个空格。
	spacePattern = regexp.MustCompile(`[ \t\x{00a0}]+`)
)

// cleanID 整理禅道导出中的编号字段。
//
// 作用：去除 Bug 编号等字段中的首尾空白和 Excel 保护用制表符。
// 输入：原始编号字符串，例如 "878\t" 或 " 1108 "。
// 输出：整理后的编号字符串；如果输入为空白，则输出空字符串。
// 示例：cleanID("878\t") 返回 "878"。
func cleanID(value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimSuffix(value, "\t")
	return strings.TrimSpace(value)
}

// cleanText 整理多行文本，并把网页转义文字还原成普通字符。
//
// 作用：统一换行符、去除首尾空白，并把 &lt;、&amp; 这类写法还原成 <、&。
// 输入：禅道 XML/HTML 中读取到的原始文本，例如 "a\r\n&lt;img&gt;"。
// 输出：整理后的文本；保留换行，但去掉首尾空白。
// 示例：cleanText("  [步骤]&lt;b&gt;x&lt;/b&gt;  ") 返回 "[步骤]<b>x</b>"。
func cleanText(value string) string {
	value = strings.ReplaceAll(value, "\r\n", "\n")
	value = strings.ReplaceAll(value, "\r", "\n")
	return strings.TrimSpace(html.UnescapeString(value))
}

// cleanInlineText 将字段值整理为适合 JSON 单行展示的文本。
//
// 作用：在 cleanText 基础上把换行压成空格，并合并连续空白。
// 输入：标题、状态、人员等应单行展示的字段文本。
// 输出：单行文本；不会包含换行或连续空白。
// 示例：cleanInlineText("张三\n 李四") 返回 "张三 李四"。
func cleanInlineText(value string) string {
	value = cleanText(value)
	value = strings.ReplaceAll(value, "\n", " ")
	value = spacePattern.ReplaceAllString(value, " ")
	return strings.TrimSpace(value)
}

// stripHTML 将包含 HTML 标签的步骤文本转换为纯文本。
//
// 作用：去掉 <img>、<p> 等 HTML 标签，保留标签外文本，供 AI 优先阅读。
// 输入：steps_raw，例如 "[结果]<img src=\"{1.png}\" />[期望]"。
// 输出：去标签后的 steps_text；原始 HTML 仍保存在 steps_raw 中。
// 示例：stripHTML("[结果]<img src=\"{1.png}\" />[期望]") 返回 "[结果] [期望]"。
func stripHTML(value string) string {
	value = cleanText(value)
	value = htmlTagPattern.ReplaceAllString(value, " ")
	value = spacePattern.ReplaceAllString(value, " ")
	value = strings.ReplaceAll(value, "\n ", "\n")
	return strings.TrimSpace(value)
}

// extractFileID 从禅道链接或 HTML 片段中提取 fileID。
//
// 作用：识别 index.php?m=file&...&fileID=1862 这类附件 ID。
// 输入：任意包含禅道 fileID 的字符串，例如 href 或 img alt。
// 输出：fileID 字符串；未找到时返回空字符串。
// 示例：extractFileID("index.php?m=file&f=download&fileID=1862") 返回 "1862"。
func extractFileID(value string) string {
	match := fileIDPattern.FindStringSubmatch(html.UnescapeString(value))
	if len(match) == 2 {
		return match[1]
	}
	return ""
}

// extractFilenameHint 从禅道内嵌图片 src 中提取文件名提示。
//
// 作用：识别 src="{1291.png}" 这种没有真实文件名但有提示名的图片引用。
// 输入：通常为 img 的 src 属性值，例如 "{1291.png}"。
// 输出：花括号内的文件名提示；未匹配时返回空字符串。
// 示例：extractFilenameHint("{1291.png}") 返回 "1291.png"。
func extractFilenameHint(value string) string {
	match := braceFilenamePattern.FindStringSubmatch(value)
	if len(match) == 2 {
		return strings.TrimSpace(match[1])
	}
	return ""
}

// classifyFile 根据文件后缀判断附件类型。
//
// 作用：把禅道附件分成图片、视频、模型文件、压缩包或其他文件。
// 输入：附件文件名，例如 "参数受控.mp4" 或 "model.mo"。
// 输出：附件类型；不认识的后缀返回 "other"。
// 示例：classifyFile("参数受控.mp4") 返回 "video"。
func classifyFile(filename string) string {
	ext := strings.ToLower(filepath.Ext(strings.TrimSpace(filename)))
	switch ext {
	case ".png", ".jpg", ".jpeg", ".gif", ".bmp", ".webp", ".svg":
		return "image"
	case ".mp4", ".mov", ".avi", ".mkv", ".webm", ".wmv", ".flv":
		return "video"
	case ".mo", ".mos", ".fmu", ".mat":
		return "model"
	case ".zip", ".rar", ".7z", ".tar", ".gz", ".tgz":
		return "archive"
	default:
		return "other"
	}
}

// classifyFileListRaw 根据 XML files 原始文本判断是否存在图片、视频或模型资源附件。
//
// 作用：当没有 HTML 附件列信息可用时，仍能根据文件名后缀判断是否有图片、视频和模型资源。
// 输入：XML <files> 字段原文，例如 "截图.png 参数受控.mp4 Cautosim.zip"。
// 输出：三个判断结果；依次表示是否存在图片附件、视频附件、模型资源附件。
// 示例：classifyFileListRaw("截图.png 参数受控.mp4 Cautosim.zip") 返回 true, true, true。
func classifyFileListRaw(filesRaw string) (hasImage bool, hasVideo bool, hasModelResource bool) {
	lower := strings.ToLower(filesRaw)
	for _, ext := range []string{".png", ".jpg", ".jpeg", ".gif", ".bmp", ".webp", ".svg"} {
		if strings.Contains(lower, ext) {
			hasImage = true
			break
		}
	}
	for _, ext := range []string{".mp4", ".mov", ".avi", ".mkv", ".webm", ".wmv", ".flv"} {
		if strings.Contains(lower, ext) {
			hasVideo = true
			break
		}
	}
	for _, ext := range []string{".mo", ".mos", ".fmu", ".mat", ".zip", ".rar", ".7z", ".tar", ".gz", ".tgz"} {
		if strings.Contains(lower, ext) {
			hasModelResource = true
			break
		}
	}
	return hasImage, hasVideo, hasModelResource
}
