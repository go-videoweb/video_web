package zentao

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestCollectModelResourceAttachmentsDownloadsModelFiles 验证模型类附件会被下载到本地目录并写回 local_path。
func TestCollectModelResourceAttachmentsDownloadsModelFiles(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("fileID") != "4058" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte("model ZH3ELib end ZH3ELib;"))
	}))
	defer server.Close()

	resourceDir := filepath.Join(t.TempDir(), "attachments")
	index := AttachmentIndex{"1108": []Attachment{{
		Source:      "files",
		Kind:        "model",
		Filename:    "ZH3ELib.mo",
		FileID:      "4058",
		DownloadURL: server.URL + "/index.php?m=file&f=download&fileID=4058",
	}, {
		Source:      "files",
		Kind:        "image",
		Filename:    "截图.png",
		FileID:      "1291",
		DownloadURL: server.URL + "/index.php?m=file&f=download&fileID=1291",
	}}}

	collected, warnings, err := CollectModelResourceAttachments(index, resourceDir, "")
	if err != nil {
		t.Fatalf("CollectModelResourceAttachments: %v", err)
	}
	if len(warnings) != 0 {
		t.Fatalf("warnings = %v", warnings)
	}

	model := collected["1108"][0]
	if model.LocalPath == "" || !strings.HasSuffix(model.LocalPath, filepath.Join("BUG_1108", "4058_ZH3ELib.mo")) {
		t.Fatalf("unexpected model local path: %+v", model)
	}
	data, err := os.ReadFile(model.LocalPath)
	if err != nil {
		t.Fatalf("read downloaded model: %v", err)
	}
	if string(data) != "model ZH3ELib end ZH3ELib;" {
		t.Fatalf("unexpected downloaded data: %q", string(data))
	}
	if collected["1108"][1].LocalPath != "" {
		t.Fatalf("image attachment should not be downloaded as model resource: %+v", collected["1108"][1])
	}
}

// TestCollectModelResourceAttachmentsRecordsCollectionError 验证单个模型附件下载失败时保留原附件信息并记录错误。
func TestCollectModelResourceAttachmentsRecordsCollectionError(t *testing.T) {
	t.Parallel()

	index := AttachmentIndex{"1108": []Attachment{{
		Source:   "files",
		Kind:     "archive",
		Filename: "Cautosim.zip",
		FileID:   "3774",
	}}}

	collected, warnings, err := CollectModelResourceAttachments(index, t.TempDir(), "")
	if err != nil {
		t.Fatalf("CollectModelResourceAttachments: %v", err)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v", warnings)
	}
	archive := collected["1108"][0]
	if archive.LocalPath != "" || !strings.Contains(archive.CollectionError, "missing download_url") {
		t.Fatalf("unexpected archive collection result: %+v", archive)
	}
}

// TestCollectModelResourceAttachmentsRejectsHTMLLoginPage 验证禅道登录页不会被误认为模型附件下载成功。
func TestCollectModelResourceAttachmentsRejectsHTMLLoginPage(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<!DOCTYPE html><title>用户登录 - 禅道</title>"))
	}))
	defer server.Close()

	index := AttachmentIndex{"2132": []Attachment{{
		Source:      "files",
		Kind:        "model",
		Filename:    "ZH3ELib.mo",
		FileID:      "4058",
		DownloadURL: server.URL + "/index.php?m=file&f=download&fileID=4058",
	}}}

	collected, warnings, err := CollectModelResourceAttachments(index, t.TempDir(), "")
	if err != nil {
		t.Fatalf("CollectModelResourceAttachments: %v", err)
	}
	if len(warnings) != 1 {
		t.Fatalf("warnings = %v", warnings)
	}
	model := collected["2132"][0]
	if model.LocalPath != "" || !strings.Contains(model.CollectionError, "login cookie") {
		t.Fatalf("unexpected model collection result: %+v", model)
	}
}

// TestFilterAttachmentIndex 验证单 Bug 模式只保留目标 Bug 附件。
func TestFilterAttachmentIndex(t *testing.T) {
	t.Parallel()

	index := AttachmentIndex{
		"2132": []Attachment{{Filename: "ZH3ELib.mo"}},
		"2555": []Attachment{{Filename: "Cautosim.zip"}},
	}

	filtered := FilterAttachmentIndex(index, "BUG_2132")
	if len(filtered) != 1 || len(filtered["2132"]) != 1 || filtered["2132"][0].Filename != "ZH3ELib.mo" {
		t.Fatalf("unexpected filtered index: %+v", filtered)
	}
	if len(FilterAttachmentIndex(index, "9999")) != 0 {
		t.Fatalf("missing bug filter should return empty index")
	}
}
