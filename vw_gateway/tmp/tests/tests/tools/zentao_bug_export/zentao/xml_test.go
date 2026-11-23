package zentao

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestParseXMLExportMergesAttachmentsAndInlineImages 验证 XML 中的 Bug 信息能和 HTML 中的附件放到同一个 Bug 里。
//
// 作用：确保 XML 字段、steps 内嵌图片、HTML 附件，以及图片/视频/模型资源这三个“是否存在”的判断结果，能合并到同一个 ParsedBug。
// 输入：测试内构造的临时 XML 文件，以及 Bug 1108 的视频和模型附件。
// 输出：测试通过或失败；通过表示 Summary、Images、Videos、ModelResources、StepsText 均符合预期。
// 示例：steps 中的 {1291.png}/fileID=1291 应进入 Images，HTML 附件参数受控.mp4/fileID=1862 应进入 Videos。
func TestParseXMLExportMergesAttachmentsAndInlineImages(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "bugs.xml")
	xml := `<?xml version='1.0' encoding='utf-8'?><xml><rows>
<row>
  <id>1108</id>
  <product>YSSIM(#19)</product>
  <module>/YSSIM-机理建模(#151)</module>
  <project>YSLab-机理建模(#66)</project>
  <execution>0</execution>
  <story>0</story>
  <task>(#0)</task>
  <title>组件参数受控异常</title>
  <keywords></keywords>
  <severity>一般(#1)</severity>
  <pri>中(#2)</pri>
  <type>功能问题(#functionality)</type>
  <os></os>
  <browser></browser>
  <steps>[步骤]打开参数页&lt;img src=&quot;{1291.png}&quot; alt=&quot;index.php?m=file&amp;f=read&amp;t=png&amp;fileID=1291&quot; /&gt;[结果]异常[期望]正常</steps>
  <status>已关闭(#closed)</status>
  <deadline></deadline>
  <activatedCount>0</activatedCount>
  <confirmed>已确认</confirmed>
  <mailto></mailto>
  <openedBy>曹闯</openedBy>
  <openedDate>2025-08-12 10:00:00</openedDate>
  <openedBuild>主干(#trunk)</openedBuild>
  <assignedTo>张恩阳</assignedTo>
  <assignedDate>2025-08-12 11:00:00</assignedDate>
  <resolvedBy>张恩阳</resolvedBy>
  <resolution>修正(#fixed)</resolution>
  <resolvedBuild>主干(#trunk)</resolvedBuild>
  <resolvedDate>2025-08-13 09:09:00</resolvedDate>
  <closedBy>曹闯</closedBy>
  <closedDate>2025-08-13 10:52:37</closedDate>
  <duplicateBug>0</duplicateBug>
  <relatedBug></relatedBug>
  <case>(#0)</case>
  <lastEditedBy>曹闯</lastEditedBy>
  <lastEditedDate>2025-08-13 10:52:37</lastEditedDate>
  <files>参数受控.mp4 </files>
  <feedbackBy></feedbackBy>
  <notifyEmail></notifyEmail>
</row>
</rows></xml>`
	if err := os.WriteFile(path, []byte(xml), 0o644); err != nil {
		t.Fatalf("write xml: %v", err)
	}

	attachments := AttachmentIndex{"1108": []Attachment{{
		Source:      "files",
		Kind:        "video",
		Filename:    "参数受控.mp4",
		FileID:      "1862",
		DownloadURL: "http://zentao/index.php?m=file&f=download&fileID=1862",
	}, {
		Source:      "files",
		Kind:        "model",
		Filename:    "ZH3ELib.mo",
		FileID:      "4058",
		DownloadURL: "http://zentao/index.php?m=file&f=download&fileID=4058",
		LocalPath:   "tmp/zentao_bug_attachments/BUG_1108/4058_ZH3ELib.mo",
	}}}

	export, err := ParseXMLExport(path, "bugs.html", attachments, "", nil)
	if err != nil {
		t.Fatalf("ParseXMLExport: %v", err)
	}
	if export.Summary.Total != 1 || export.Summary.WithImages != 1 || export.Summary.WithVideos != 1 || export.Summary.WithModelResources != 1 {
		t.Fatalf("unexpected summary: %+v", export.Summary)
	}

	bug := export.Bugs[0]
	if bug.BugID != "1108" || bug.Title != "组件参数受控异常" {
		t.Fatalf("unexpected bug identity: %+v", bug)
	}
	if !bug.HasImageAttachment || !bug.HasVideoAttachment || !bug.HasModelResourceAttachment {
		t.Fatalf("attachment flags not set: image=%v video=%v model=%v", bug.HasImageAttachment, bug.HasVideoAttachment, bug.HasModelResourceAttachment)
	}
	if len(bug.Images) != 1 || bug.Images[0].FileID != "1291" || bug.Images[0].FilenameHint != "1291.png" {
		t.Fatalf("unexpected images: %+v", bug.Images)
	}
	if len(bug.Videos) != 1 || bug.Videos[0].FileID != "1862" {
		t.Fatalf("unexpected videos: %+v", bug.Videos)
	}
	if len(bug.ModelResources) != 1 || bug.ModelResources[0].FileID != "4058" || bug.ModelResources[0].LocalPath == "" {
		t.Fatalf("unexpected model resources: %+v", bug.ModelResources)
	}
	if strings.Contains(bug.StepsText, "<img") || !strings.Contains(bug.StepsText, "[结果]异常") {
		t.Fatalf("unexpected steps text: %q", bug.StepsText)
	}
}

// TestParseXMLExportFiltersBugID 验证单 Bug 过滤能力。
//
// 作用：确保 -bug-id 对应的过滤逻辑只输出指定 Bug。
// 输入：测试内构造的包含 Bug 1 和 Bug 2 的 XML 文件，过滤条件为 "2"。
// 输出：测试通过或失败；通过表示 Export.Bugs 只包含 BugID=2。
// 示例：ParseXMLExport(path, "", nil, "2", nil) 的 Summary.Total 应为 1。
func TestParseXMLExportFiltersBugID(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "bugs.xml")
	xml := `<?xml version='1.0' encoding='utf-8'?><xml><rows>
<row><id>1</id><title>one</title><steps></steps></row>
<row><id>2</id><title>two</title><steps></steps></row>
</rows></xml>`
	if err := os.WriteFile(path, []byte(xml), 0o644); err != nil {
		t.Fatalf("write xml: %v", err)
	}

	export, err := ParseXMLExport(path, "", nil, "2", nil)
	if err != nil {
		t.Fatalf("ParseXMLExport: %v", err)
	}
	if export.Summary.Total != 1 || export.Bugs[0].BugID != "2" {
		t.Fatalf("unexpected filtered export: %+v", export)
	}
}
