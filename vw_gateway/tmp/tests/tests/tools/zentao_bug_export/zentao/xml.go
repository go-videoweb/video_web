package zentao

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
)

type xmlExport struct {
	// Rows 是禅道 XML <rows> 下的所有 <row>，每个 row 对应一个 Bug。
	Rows []xmlRow `xml:"rows>row"`
}

type xmlRow struct {
	// ID 对应 XML <id>，即 Bug 编号。
	ID string `xml:"id"`
	// Product 对应 XML <product>，即所属产品。
	Product string `xml:"product"`
	// Module 对应 XML <module>，即所属模块。
	Module string `xml:"module"`
	// Project 对应 XML <project>，即所属项目。
	Project string `xml:"project"`
	// Execution 对应 XML <execution>，即所属执行。
	Execution string `xml:"execution"`
	// Story 对应 XML <story>，即相关需求。
	Story string `xml:"story"`
	// Task 对应 XML <task>，即相关任务。
	Task string `xml:"task"`
	// Title 对应 XML <title>，即 Bug 标题。
	Title string `xml:"title"`
	// Keywords 对应 XML <keywords>，即关键词。
	Keywords string `xml:"keywords"`
	// Severity 对应 XML <severity>，即严重程度。
	Severity string `xml:"severity"`
	// Pri 对应 XML <pri>，即优先级。
	Pri string `xml:"pri"`
	// Type 对应 XML <type>，即 Bug 类型。
	Type string `xml:"type"`
	// OS 对应 XML <os>，即操作系统。
	OS string `xml:"os"`
	// Browser 对应 XML <browser>，即浏览器。
	Browser string `xml:"browser"`
	// Steps 对应 XML <steps>，即重现步骤，可能包含转义后的 HTML。
	Steps string `xml:"steps"`
	// Status 对应 XML <status>，即 Bug 状态。
	Status string `xml:"status"`
	// Deadline 对应 XML <deadline>，即截止日期。
	Deadline string `xml:"deadline"`
	// ActivatedCount 对应 XML <activatedCount>，即激活次数。
	ActivatedCount string `xml:"activatedCount"`
	// Confirmed 对应 XML <confirmed>，即是否确认。
	Confirmed string `xml:"confirmed"`
	// Mailto 对应 XML <mailto>，即抄送给。
	Mailto string `xml:"mailto"`
	// OpenedBy 对应 XML <openedBy>，即创建人。
	OpenedBy string `xml:"openedBy"`
	// OpenedDate 对应 XML <openedDate>，即创建日期。
	OpenedDate string `xml:"openedDate"`
	// OpenedBuild 对应 XML <openedBuild>，即影响版本。
	OpenedBuild string `xml:"openedBuild"`
	// AssignedTo 对应 XML <assignedTo>，即指派给。
	AssignedTo string `xml:"assignedTo"`
	// AssignedDate 对应 XML <assignedDate>，即指派日期。
	AssignedDate string `xml:"assignedDate"`
	// ResolvedBy 对应 XML <resolvedBy>，即解决者。
	ResolvedBy string `xml:"resolvedBy"`
	// Resolution 对应 XML <resolution>，即解决方案。
	Resolution string `xml:"resolution"`
	// ResolvedBuild 对应 XML <resolvedBuild>，即解决版本。
	ResolvedBuild string `xml:"resolvedBuild"`
	// ResolvedDate 对应 XML <resolvedDate>，即解决日期。
	ResolvedDate string `xml:"resolvedDate"`
	// ClosedBy 对应 XML <closedBy>，即关闭人。
	ClosedBy string `xml:"closedBy"`
	// ClosedDate 对应 XML <closedDate>，即关闭日期。
	ClosedDate string `xml:"closedDate"`
	// DuplicateBug 对应 XML <duplicateBug>，即重复 Bug 编号。
	DuplicateBug string `xml:"duplicateBug"`
	// RelatedBug 对应 XML <relatedBug>，即相关 Bug。
	RelatedBug string `xml:"relatedBug"`
	// Case 对应 XML <case>，即相关用例。
	Case string `xml:"case"`
	// LastEditedBy 对应 XML <lastEditedBy>，即最后修改者。
	LastEditedBy string `xml:"lastEditedBy"`
	// LastEditedDate 对应 XML <lastEditedDate>，即修改日期。
	LastEditedDate string `xml:"lastEditedDate"`
	// Files 对应 XML <files>，即附件文本列表；通常只有文件名，没有 fileID。
	Files string `xml:"files"`
	// FeedbackBy 对应 XML <feedbackBy>，即反馈者。
	FeedbackBy string `xml:"feedbackBy"`
	// NotifyEmail 对应 XML <notifyEmail>，即通知邮箱。
	NotifyEmail string `xml:"notifyEmail"`
}

// ParseXMLExport 解析禅道 XML 导出，并合并 HTML 中读到的附件列表。
//
// 作用：把 XML 中的每个 <row> 转换为 ParsedBug，并把 HTML 中读到的附件 fileID/download_url 一起带出来。
// 输入：path 为 XML 路径，htmlPath 记录来源，attachmentIndex 为 HTML 中读到的附件列表，bugIDFilter 可限制单个 Bug，htmlWarnings 会原样带到 Summary。
// 输出：Export 和 error；Export.Bugs 为整理后的 Bug 列表，Summary 记录总数、图片数、视频数、模型资源数和警告。
// 示例：ParseXMLExport("bugs.xml", "bugs.html", index, "1108", nil) 只输出 Bug 1108 的整理结果。
func ParseXMLExport(path string, htmlPath string, attachmentIndex AttachmentIndex, bugIDFilter string, htmlWarnings []string) (*Export, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read xml export: %w", err)
	}
	data = bytes.TrimPrefix(data, []byte{0xef, 0xbb, 0xbf})

	var parsed xmlExport
	if err := xml.Unmarshal(data, &parsed); err != nil {
		return nil, fmt.Errorf("parse xml export: %w", err)
	}

	export := &Export{
		Source:  Source{System: "zentao", XMLPath: path, HTMLPath: htmlPath},
		Summary: Summary{Warnings: append([]string{}, htmlWarnings...)},
		Bugs:    []ParsedBug{},
	}

	for _, row := range parsed.Rows {
		bug := buildParsedBug(row, attachmentIndex[cleanID(row.ID)])
		if bug.BugID == "" {
			continue
		}
		if bugIDFilter != "" && bug.BugID != bugIDFilter {
			continue
		}

		export.Bugs = append(export.Bugs, bug)
		if bug.HasImageAttachment {
			export.Summary.WithImages++
		}
		if bug.HasVideoAttachment {
			export.Summary.WithVideos++
		}
		if bug.HasModelResourceAttachment {
			export.Summary.WithModelResources++
		}
	}

	export.Summary.Total = len(export.Bugs)
	if bugIDFilter != "" && export.Summary.Total == 0 {
		export.Summary.Warnings = append(export.Summary.Warnings, fmt.Sprintf("bug_id %s not found", bugIDFilter))
	}
	return export, nil
}

// buildParsedBug 将单条 XML row 转换为 ParsedBug。
//
// 作用：清洗 XML 字段、生成 steps_raw/steps_text、提取内嵌图片，并把图片、视频、模型资源等附件放到对应列表。
// 输入：row 为从 XML 读出来的单条 Bug，fileAttachments 为 HTML 中同一 Bug 的附件列表。
// 输出：ParsedBug；其中 HasImageAttachment/HasVideoAttachment/HasModelResourceAttachment 会根据内嵌图片、HTML 附件或 files_raw 文件后缀自动设置。
// 示例：buildParsedBug(row1108, []Attachment{{Kind:"video", Filename:"参数受控.mp4"}}) 会返回 has_video_attachment=true 的 Bug。
func buildParsedBug(row xmlRow, fileAttachments []Attachment) ParsedBug {
	stepsRaw := cleanText(row.Steps)
	inlineImages := extractInlineImages(stepsRaw)
	filesRaw := cleanText(row.Files)

	bug := ParsedBug{
		BugID:          cleanID(row.ID),
		Title:          cleanInlineText(row.Title),
		Product:        cleanInlineText(row.Product),
		Module:         cleanInlineText(row.Module),
		Project:        cleanInlineText(row.Project),
		Execution:      cleanInlineText(row.Execution),
		Story:          cleanInlineText(row.Story),
		Task:           cleanInlineText(row.Task),
		Keywords:       cleanInlineText(row.Keywords),
		Severity:       cleanInlineText(row.Severity),
		Priority:       cleanInlineText(row.Pri),
		Type:           cleanInlineText(row.Type),
		OS:             cleanInlineText(row.OS),
		Browser:        cleanInlineText(row.Browser),
		StepsRaw:       stepsRaw,
		StepsText:      stripHTML(stepsRaw),
		Status:         cleanInlineText(row.Status),
		Deadline:       cleanInlineText(row.Deadline),
		ActivatedCount: cleanInlineText(row.ActivatedCount),
		Confirmed:      cleanInlineText(row.Confirmed),
		Mailto:         cleanInlineText(row.Mailto),
		OpenedBy:       cleanInlineText(row.OpenedBy),
		OpenedDate:     cleanInlineText(row.OpenedDate),
		OpenedBuild:    cleanInlineText(row.OpenedBuild),
		AssignedTo:     cleanInlineText(row.AssignedTo),
		AssignedDate:   cleanInlineText(row.AssignedDate),
		ResolvedBy:     cleanInlineText(row.ResolvedBy),
		Resolution:     cleanInlineText(row.Resolution),
		ResolvedBuild:  cleanInlineText(row.ResolvedBuild),
		ResolvedDate:   cleanInlineText(row.ResolvedDate),
		ClosedBy:       cleanInlineText(row.ClosedBy),
		ClosedDate:     cleanInlineText(row.ClosedDate),
		DuplicateBug:   cleanInlineText(row.DuplicateBug),
		RelatedBug:     cleanInlineText(row.RelatedBug),
		Case:           cleanInlineText(row.Case),
		LastEditedBy:   cleanInlineText(row.LastEditedBy),
		LastEditedDate: cleanInlineText(row.LastEditedDate),
		FilesRaw:       filesRaw,
		FeedbackBy:     cleanInlineText(row.FeedbackBy),
		NotifyEmail:    cleanInlineText(row.NotifyEmail),
		Images:         append([]Attachment{}, inlineImages...),
		Attachments:    append([]Attachment{}, fileAttachments...),
	}

	for _, attachment := range fileAttachments {
		switch attachment.Kind {
		case "image":
			bug.Images = append(bug.Images, attachment)
		case "video":
			bug.Videos = append(bug.Videos, attachment)
		case "model", "archive":
			bug.ModelResources = append(bug.ModelResources, attachment)
		}
	}

	if len(fileAttachments) == 0 && filesRaw != "" {
		hasImage, hasVideo, hasModelResource := classifyFileListRaw(filesRaw)
		bug.HasImageAttachment = hasImage
		bug.HasVideoAttachment = hasVideo
		bug.HasModelResourceAttachment = hasModelResource
	}
	bug.HasImageAttachment = bug.HasImageAttachment || len(bug.Images) > 0
	bug.HasVideoAttachment = bug.HasVideoAttachment || len(bug.Videos) > 0
	bug.HasModelResourceAttachment = bug.HasModelResourceAttachment || len(bug.ModelResources) > 0
	return bug
}
