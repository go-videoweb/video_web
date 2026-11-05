package codecs

import (
	"bytes"
	"crypto/sha256"
	"fmt"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	khttp "github.com/go-kratos/kratos/v2/transport/http"
	userfilev1 "vw_gateway/api/v1/user/userfile"
	videoinfov1 "vw_gateway/api/v1/video/videoinfo"
)

const (
	baseContentType = "application"
)

// FileResponse 表示待发送的二进制内容，屏蔽内存图片与磁盘临时文件的数据来源差异。
type FileResponse struct {
	Content  io.ReadSeeker // 支持定位的内容源，供 ServeContent 计算长度和处理 Range。
	Filename string        // 原始文件名，不作为磁盘读取路径。
	Header   http.Header   // 内容类型、缓存策略及下载文件名等 HTTP 响应头。
	ModTime  time.Time     // 文件修改时间；内存头像使用零值，仅以 ETag 验证缓存。
	tempFile *os.File      // 本次请求拥有的网关临时文件；内存头像为 nil，不删除用户服务原图。
}

// newFileResponse 将已知的二进制响应适配为统一内容源；其他响应返回 nil，由默认编码器处理。
// 磁盘路径只来自服务端生成的临时文件；打开或读取元信息失败时返回错误。
func newFileResponse(v interface{}) (*FileResponse, error) {
	switch response := v.(type) {
	case *userfilev1.AvatarResp:
		if response == nil {
			return nil, fmt.Errorf("nil avatar response")
		}
		header := make(http.Header)
		header.Set("Content-Type", response.ContentType)
		header.Set("X-Content-Type-Options", "nosniff")
		// 稳定 URL 每次重新验证内容；替换后哈希变化，避免继续显示缓存的旧头像。
		header.Set("Cache-Control", "no-cache")
		header.Set("ETag", fmt.Sprintf("\"%x\"", sha256.Sum256(response.Content)))
		return &FileResponse{Content: bytes.NewReader(response.Content), Header: header}, nil
	case *videoinfov1.FileResp:
		if response == nil {
			return nil, fmt.Errorf("nil file response")
		}
		f, err := os.Open(response.FilePath)
		if err != nil {
			return nil, err
		}
		fr := &FileResponse{Content: f, Filename: response.Filename, Header: make(http.Header), tempFile: f}
		info, err := f.Stat()
		if err != nil {
			fr.close()
			return nil, err
		}
		fr.ModTime = info.ModTime()
		for key, values := range response.Headers {
			for _, value := range values.GetValue() {
				fr.Header.Add(key, value)
			}
		}
		return fr, nil
	default:
		return nil, nil
	}
}

// close 先关闭再删除本请求拥有的临时文件；内存内容无需清理。
// 响应可能已经发出，清理失败只记录日志，不再尝试重写 HTTP 响应。
func (fr *FileResponse) close() {
	if fr.tempFile == nil {
		return
	}
	if err := fr.tempFile.Close(); err != nil {
		log.Warnf("close response temporary file %q: %v", fr.tempFile.Name(), err)
	}
	if err := os.Remove(fr.tempFile.Name()); err != nil {
		log.Warnf("remove response temporary file %q: %v", fr.tempFile.Name(), err)
	}
}

// serveFileResponse 统一发送二进制响应，保留自定义响应头并支持条件请求与 Range。
// 输入内容须支持 Seek；不将磁盘文件整体读入内存，也不在此处决定文件清理策略。
func serveFileResponse(w http.ResponseWriter, r *http.Request, fr *FileResponse) {
	// Kratos 包装器只暂存 WriteHeader，直到 Write 才发送；304 和 HEAD 等无正文响应须使用底层 writer。
	if wrapped, ok := w.(interface{ Unwrap() http.ResponseWriter }); ok {
		w = wrapped.Unwrap()
	}
	for key, values := range fr.Header {
		for _, value := range values {
			w.Header().Add(key, value)
		}
	}
	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "application/octet-stream")
	}
	http.ServeContent(w, r, fr.Filename, fr.ModTime, fr.Content)
}

// ContentType returns the content-type with base prefix.
func ContentType(subtype string) string {
	return baseContentType + "/" + subtype
}

// ResponseEncoder encodes the object to the HTTP response.
func ResponseEncoder(w http.ResponseWriter, r *http.Request, v interface{}) error {
	fr, err := newFileResponse(v)
	if err != nil {
		return err
	}
	if fr != nil {
		defer fr.close()
		serveFileResponse(w, r, fr)
		return nil
	}

	// Below is the DefaultResponseEncoder logic
	if v == nil {
		return nil
	}
	if rd, ok := v.(khttp.Redirector); ok {
		url, code := rd.Redirect()
		http.Redirect(w, r, url, code)
		return nil
	}
	codec, _ := khttp.CodecForRequest(r, "Accept")
	data, err := codec.Marshal(v)
	if err != nil {
		return err
	}
	w.Header().Set("Content-Type", ContentType(codec.Name()))
	_, err = w.Write(data)
	if err != nil {
		return err
	}
	return nil
}
