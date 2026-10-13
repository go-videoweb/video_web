package zentao

import (
	"os"
	"path/filepath"
	"testing"
)

// TestParseHTMLAttachmentIndex 验证能按 Bug 编号读取 HTML 附件。
//
// 作用：确保工具能从禅道 HTML 表格附件列中提取图片、视频和模型资源附件。
// 输入：测试内构造的临时 HTML 文件，包含 Bug 1108 的 mp4/mo 附件和 Bug 990 的 png/zip 附件。
// 输出：测试通过或失败；通过表示每个 Bug 都保存了正确的 filename、fileID 和 kind。
// 示例：Bug 1108 的 <a ...fileID=1862>参数受控.mp4</a> 应解析为 kind=video、file_id=1862。
func TestParseHTMLAttachmentIndex(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "bugs.html")
	html := `<!doctype html><html><body><table>
<tr><th>Bug编号</th><th>Bug标题</th><th>附件</th></tr>
<tr><td><nobr>1108</nobr></td><td>title</td><td><a href="http://zentao/index.php?m=file&f=download&fileID=1862">参数受控.mp4</a><br /><a href="http://zentao/index.php?m=file&f=download&fileID=4058">ZH3ELib.mo</a></td></tr>
<tr><td>990</td><td>image</td><td><a href='http://zentao/index.php?m=file&f=download&fileID=1476'>屏幕截图.png</a><br /><a href='http://zentao/index.php?m=file&f=download&fileID=3306'>URDFModelica.zip</a></td></tr>
</table></body></html>`
	if err := os.WriteFile(path, []byte(html), 0o644); err != nil {
		t.Fatalf("write html: %v", err)
	}

	index, warnings, err := ParseHTMLAttachmentIndex(path)
	if err != nil {
		t.Fatalf("ParseHTMLAttachmentIndex: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}

	video := index["1108"][0]
	if video.Kind != "video" || video.FileID != "1862" || video.Filename != "参数受控.mp4" {
		t.Fatalf("unexpected video attachment: %+v", video)
	}
	model := index["1108"][1]
	if model.Kind != "model" || model.FileID != "4058" || model.Filename != "ZH3ELib.mo" {
		t.Fatalf("unexpected model attachment: %+v", model)
	}
	image := index["990"][0]
	if image.Kind != "image" || image.FileID != "1476" || image.Filename != "屏幕截图.png" {
		t.Fatalf("unexpected image attachment: %+v", image)
	}
	archive := index["990"][1]
	if archive.Kind != "archive" || archive.FileID != "3306" || archive.Filename != "URDFModelica.zip" {
		t.Fatalf("unexpected archive attachment: %+v", archive)
	}
}
