package zentao

import (
	"html"
	"regexp"
	"strings"
)

var (
	// imgTagPattern 用于从重现步骤 HTML 中找出完整的 <img ...> 图片标签。
	// 结构：(?is) 让匹配不分大小写并支持跨行；<img\b[^>]*> 抓完整图片标签。
	// 人话：不管是 img 还是 IMG，也不管标签有没有换行，都能把图片标签找出来。
	// 示例1：`<img src="{1291.png}" />` 会被识别为图片标签。
	// 示例2：`<IMG width="608" src="{1291.png}" alt="...fileID=1291" />` 也会被识别。
	imgTagPattern = regexp.MustCompile(`(?is)<img\b[^>]*>`)
	// attrPattern 用于从单个 HTML 标签中读取 src/alt 的值。
	// 结构：(?is)\b(src|alt)\s*=\s*("[^"]*"|'[^']*') 既抓 src/alt，又兼容大小写、空格和单双引号。
	// 人话：从图片标签里把 src 和 alt 的值抠出来。
	// 示例1：`src="{1291.png}"` 会提取 key=src、value={1291.png}。
	// 示例2：`alt='index.php?m=file&f=read&fileID=1291'` 会提取 key=alt、value=index.php?m=file&f=read&fileID=1291。
	// 示例3：`SRC = "{1861.png}"` 也会被识别，并在后续转为小写 key。
	attrPattern = regexp.MustCompile(`(?is)\b(src|alt)\s*=\s*("[^"]*"|'[^']*')`)
)

// extractInlineImages 从 Bug 重现步骤 HTML 中提取内嵌图片引用。
//
// 作用：识别 steps_raw 中的 <img> 标签，并读出 source=steps、kind=image、fileID 和 filename_hint。
// 输入：stepsRaw 为 XML <steps> 字段整理后的内容，可能包含网页转义文字或 <img> 标签。
// 输出：图片附件切片；没有内嵌图片时返回空切片。
// 示例：extractInlineImages(`<img src="{1291.png}" alt="...fileID=1291" />`) 返回 FileID=1291、FilenameHint=1291.png。
func extractInlineImages(stepsRaw string) []Attachment {
	unescaped := html.UnescapeString(stepsRaw)
	imageTags := imgTagPattern.FindAllString(unescaped, -1)
	images := make([]Attachment, 0, len(imageTags))

	for _, tag := range imageTags {
		attrs := extractAttributes(tag)
		src := attrs["src"]
		alt := attrs["alt"]
		filenameHint := extractFilenameHint(src)
		fileID := extractFileID(tag)
		if fileID == "" {
			fileID = extractFileID(alt)
		}

		attachment := Attachment{
			Source:       "steps",
			Kind:         "image",
			FilenameHint: filenameHint,
			FileID:       fileID,
		}
		if attachment.FilenameHint == "" && src != "" {
			attachment.FilenameHint = strings.Trim(src, "{}")
		}
		images = append(images, attachment)
	}
	return images
}

// extractAttributes 读取 HTML 标签中的 src/alt 内容。
//
// 作用：只从单个图片标签里读取 src 和 alt，不把整段 steps 当成完整网页处理。
// 输入：单个 HTML 标签字符串，例如 `<img src="{1291.png}" alt="...fileID=1291" />`。
// 输出：可按名称查值的结果；当前只读取 src 和 alt，并会把 &amp; 这类网页写法还原成普通字符。
// 示例：extractAttributes(`<img src="{1291.png}" />`)["src"] 返回 "{1291.png}"。
func extractAttributes(tag string) map[string]string {
	attrs := map[string]string{}
	for _, match := range attrPattern.FindAllStringSubmatch(tag, -1) {
		if len(match) != 3 {
			continue
		}
		key := strings.ToLower(match[1])
		value := strings.Trim(match[2], `"'`)
		attrs[key] = html.UnescapeString(value)
	}
	return attrs
}
