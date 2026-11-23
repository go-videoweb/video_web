package zentao

import (
	"bufio"
	"fmt"
	"html"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const (
	// modelResourceDownloadTimeout 表示单个模型类附件下载的超时时间。
	// 说明：模型库 zip 可能较大，因此这里使用分钟级超时；超时后工具会在 collection_error 中记录失败原因。
	// 示例：下载 Cautosim.zip 超过 10 分钟仍未完成时，该附件会保留 download_url，但不会产生 local_path。
	modelResourceDownloadTimeout = 10 * time.Minute
)

// CollectModelResourceAttachments 函数作用：把 HTML 附件列里的模型文件下载到本地目录，并把 local_path 写回对应附件。
// 入参：index 是按 Bug 编号保存的附件列表；resourceDir 为资源下载根目录，空字符串表示不下载；cookie 为访问禅道下载链接的可选 Cookie。
// 返回值：返回带 local_path 或 collection_error 的附件列表、提醒信息和 error；目录创建失败返回 error，单个附件下载失败只写入提醒信息。
// 示例：CollectModelResourceAttachments(index, "tmp/zentao_bug_attachments", "zentaosid=...") 会把 BUG_1108 的 .mo/.zip 附件下载到 tmp/zentao_bug_attachments/BUG_1108/。
func CollectModelResourceAttachments(index AttachmentIndex, resourceDir string, cookie string) (AttachmentIndex, []string, error) {
	if resourceDir == "" {
		return index, nil, nil
	}
	if err := os.MkdirAll(resourceDir, 0o755); err != nil {
		return nil, nil, fmt.Errorf("create resource dir: %w", err)
	}

	warnings := []string{}
	collected := AttachmentIndex{}
	client := &http.Client{Timeout: modelResourceDownloadTimeout}

	for bugID, attachments := range index {
		bugDir := filepath.Join(resourceDir, "BUG_"+cleanID(bugID))
		updated := make([]Attachment, len(attachments))
		copy(updated, attachments)

		for i := range updated {
			if !isModelResourceAttachment(updated[i]) {
				continue
			}

			localPath, err := downloadModelResourceAttachment(client, bugDir, updated[i], cookie)
			if err != nil {
				updated[i].CollectionError = err.Error()
				warnings = append(warnings, fmt.Sprintf("bug %s attachment %q collect failed: %v", bugID, updated[i].Filename, err))
				continue
			}
			updated[i].LocalPath = localPath
		}

		collected[bugID] = updated
	}
	return collected, warnings, nil
}

// FilterAttachmentIndex 函数作用：只保留指定 Bug 的 HTML 附件，避免单 Bug 解析时下载其他 Bug 的附件。
// 入参：index 是按 Bug 编号保存的附件列表；bugIDFilter 为可选 Bug 编号，支持 "2132" 或 "BUG_2132"。
// 返回值：筛选后的附件列表；bugIDFilter 为空时返回原列表，找不到目标 Bug 时返回空列表。
// 示例：FilterAttachmentIndex(index, "BUG_2132") 只保留 index["2132"] 的附件。
func FilterAttachmentIndex(index AttachmentIndex, bugIDFilter string) AttachmentIndex {
	bugID := cleanID(bugIDFilter)
	bugID = strings.TrimPrefix(strings.ToUpper(bugID), "BUG_")
	if bugID == "" {
		return index
	}
	if attachments, ok := index[bugID]; ok {
		return AttachmentIndex{bugID: attachments}
	}
	return AttachmentIndex{}
}

// isModelResourceAttachment 函数作用：判断附件是否属于 Bug 回归测试必须保留的模型类资源。
// 入参：attachment 为单个禅道附件信息。
// 返回值：如果 kind 为 model 或 archive 返回 true，否则返回 false。
// 示例：isModelResourceAttachment(Attachment{Kind:"archive", Filename:"Cautosim.zip"}) 返回 true。
func isModelResourceAttachment(attachment Attachment) bool {
	return attachment.Kind == "model" || attachment.Kind == "archive"
}

// downloadModelResourceAttachment 函数作用：下载单个模型类附件到指定 Bug 资源目录。
// 入参：client 为 HTTP 客户端；bugDir 为 BUG_xxx 目录；attachment 包含 download_url、filename 和 file_id；cookie 为可选禅道登录 Cookie。
// 返回值：成功时返回本地文件路径；失败时返回 error，例如缺少 download_url、URL 非法、HTTP 状态异常或写文件失败。
// 示例：downloadModelResourceAttachment(client, "tmp/BUG_1108", Attachment{Filename:"Model.mo", DownloadURL:"http://zentao/..."}) 会返回 tmp/BUG_1108/Model.mo。
func downloadModelResourceAttachment(client *http.Client, bugDir string, attachment Attachment, cookie string) (string, error) {
	if strings.TrimSpace(attachment.DownloadURL) == "" {
		return "", fmt.Errorf("missing download_url")
	}

	downloadURL := html.UnescapeString(strings.TrimSpace(attachment.DownloadURL))
	parsedURL, err := url.Parse(downloadURL)
	if err != nil || parsedURL.Scheme == "" || parsedURL.Host == "" {
		return "", fmt.Errorf("invalid download_url %q", attachment.DownloadURL)
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return "", fmt.Errorf("unsupported download_url scheme %q", parsedURL.Scheme)
	}

	req, err := http.NewRequest(http.MethodGet, downloadURL, nil)
	if err != nil {
		return "", fmt.Errorf("build download request: %w", err)
	}
	req.Header.Set("User-Agent", "zentao-bug-export/1.0")
	if strings.TrimSpace(cookie) != "" {
		req.Header.Set("Cookie", strings.TrimSpace(cookie))
	}

	resp, err := client.Do(req)
	if err != nil {
		return "", fmt.Errorf("download attachment: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("download attachment status %d", resp.StatusCode)
	}
	reader := bufio.NewReader(resp.Body)
	if looksLikeHTMLDownload(resp, reader) {
		return "", fmt.Errorf("download returned html page; zentao login cookie may be required")
	}
	if err := os.MkdirAll(bugDir, 0o755); err != nil {
		return "", fmt.Errorf("create bug resource dir: %w", err)
	}

	targetPath := filepath.Join(bugDir, attachmentLocalFilename(attachment))
	tmpFile, err := os.CreateTemp(bugDir, ".download-*")
	if err != nil {
		return "", fmt.Errorf("create temp attachment file: %w", err)
	}
	tmpPath := tmpFile.Name()
	if _, err := io.Copy(tmpFile, reader); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("write attachment file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("close attachment file: %w", err)
	}
	_ = os.Remove(targetPath)
	if err := os.Rename(tmpPath, targetPath); err != nil {
		_ = os.Remove(tmpPath)
		return "", fmt.Errorf("move attachment file: %w", err)
	}
	return targetPath, nil
}

// looksLikeHTMLDownload 函数作用：判断附件下载结果是否像 HTML 页面，而不是模型或压缩包文件。
// 入参：resp 为下载结果的状态和头信息；reader 为下载内容的读取器，提前看过的内容仍可继续写入文件。
// 返回值：疑似 HTML 登录页、错误页或普通网页时返回 true；正常二进制或模型文本返回 false。
// 示例：Content-Type 为 text/html 且正文以 <!DOCTYPE html> 开头时返回 true。
func looksLikeHTMLDownload(resp *http.Response, reader *bufio.Reader) bool {
	contentType := strings.ToLower(resp.Header.Get("Content-Type"))
	if strings.Contains(contentType, "text/html") {
		return true
	}
	peek, _ := reader.Peek(512)
	trimmed := strings.TrimSpace(strings.ToLower(string(peek)))
	return strings.HasPrefix(trimmed, "<!doctype html") || strings.HasPrefix(trimmed, "<html")
}

// attachmentLocalFilename 函数作用：生成用于本地保存附件的稳定安全文件名。
// 入参：attachment 包含原始 filename 和 file_id。
// 返回值：处理后的文件名；有 file_id 时加前缀，避免同名附件覆盖。
// 示例：attachmentLocalFilename(Attachment{FileID:"4058", Filename:"ZH3ELib.mo"}) 返回 "4058_ZH3ELib.mo"。
func attachmentLocalFilename(attachment Attachment) string {
	filename := safeAttachmentFilename(attachment.Filename)
	if attachment.FileID != "" {
		return safeAttachmentFilename(attachment.FileID + "_" + filename)
	}
	return filename
}

// safeAttachmentFilename 函数作用：将禅道附件名处理成 Windows 和 Linux 都可安全写入的文件名。
// 入参：filename 为禅道附件显示名，可能包含空白、路径分隔符或 Windows 非法字符。
// 返回值：安全文件名；输入为空或处理后为空时返回 attachment。
// 示例：safeAttachmentFilename("BikeTrailer (1).zip") 返回 "BikeTrailer (1).zip"，safeAttachmentFilename("a/b.mo") 返回 "a_b.mo"。
func safeAttachmentFilename(filename string) string {
	filename = strings.TrimSpace(filename)
	filename = strings.Map(func(r rune) rune {
		if r < 32 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, filename)
	filename = strings.Trim(filename, " .")
	if filename == "" {
		return "attachment"
	}
	return filename
}
