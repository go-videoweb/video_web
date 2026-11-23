package zentao

import (
	"fmt"
	"os"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

// ParseHTMLAttachmentIndex 从禅道 HTML 导出中读取附件列表。
//
// 作用：读取 HTML 表格，按 Bug 编号保存附件，用于把 XML 中没有的 fileID 和下载链接一起带出来。
// 输入：path 为禅道 HTML 导出文件路径；空字符串表示这次不读取 HTML。
// 输出：按 Bug 编号保存的附件列表、提醒信息和 error；如果 HTML 有问题但还能继续，会把问题写入提醒信息。
// 示例：ParseHTMLAttachmentIndex("bugs.html") 可返回 1108 对应的附件 []Attachment{{Filename:"参数受控.mp4", FileID:"1862"}}。
func ParseHTMLAttachmentIndex(path string) (AttachmentIndex, []string, error) {
	if path == "" {
		return AttachmentIndex{}, nil, nil
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, nil, fmt.Errorf("open html export: %w", err)
	}
	defer file.Close()

	doc, err := goquery.NewDocumentFromReader(file)
	if err != nil {
		return nil, nil, fmt.Errorf("parse html export: %w", err)
	}

	index := AttachmentIndex{}
	warnings := []string{}
	foundTable := false

	doc.Find("table").EachWithBreak(func(_ int, table *goquery.Selection) bool {
		headers := table.Find("tr").First().ChildrenFiltered("th,td")
		bugCol := -1
		filesCol := -1
		headers.Each(func(i int, cell *goquery.Selection) {
			header := cleanInlineText(cell.Text())
			switch header {
			case "Bug编号":
				bugCol = i
			case "附件":
				filesCol = i
			}
		})
		if bugCol < 0 || filesCol < 0 {
			return true
		}

		foundTable = true
		table.Find("tr").Each(func(rowIndex int, row *goquery.Selection) {
			if rowIndex == 0 {
				return
			}

			cells := row.ChildrenFiltered("td,th")
			if cells.Length() <= bugCol || cells.Length() <= filesCol {
				return
			}

			bugID := cleanID(cells.Eq(bugCol).Text())
			if bugID == "" {
				return
			}

			attachments := extractHTMLAttachments(cells.Eq(filesCol))
			if len(attachments) > 0 {
				index[bugID] = append(index[bugID], attachments...)
			}
		})
		return false
	})

	if !foundTable {
		warnings = append(warnings, fmt.Sprintf("html export %q does not contain Bug编号/附件 table", path))
	}
	return index, warnings, nil
}

// extractHTMLAttachments 从 HTML 的“附件”单元格中读取所有 <a> 附件链接。
//
// 作用：读取附件列中的文件名、href、fileID，并根据文件后缀判断是图片、视频、模型文件还是压缩包。
// 输入：filesCell 是 goquery 选中的“附件”列单元格。
// 输出：附件切片；没有 <a> 或链接文本为空时返回空切片。
// 示例：包含 <a href="...fileID=1862">参数受控.mp4</a> 的单元格会输出 kind=video、file_id=1862。
func extractHTMLAttachments(filesCell *goquery.Selection) []Attachment {
	attachments := []Attachment{}
	filesCell.Find("a").Each(func(_ int, link *goquery.Selection) {
		filename := cleanInlineText(link.Text())
		if filename == "" {
			return
		}

		href, _ := link.Attr("href")
		href = strings.TrimSpace(href)
		attachments = append(attachments, Attachment{
			Source:      "files",
			Kind:        classifyFile(filename),
			Filename:    filename,
			FileID:      extractFileID(href),
			DownloadURL: href,
		})
	})
	return attachments
}
