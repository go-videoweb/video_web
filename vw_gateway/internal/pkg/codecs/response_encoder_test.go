package codecs

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	khttp "github.com/go-kratos/kratos/v2/transport/http"
	userfilev1 "vw_gateway/api/v1/user/userfile"
	videoinfov1 "vw_gateway/api/v1/video/videoinfo"
)

// TestBinaryResponseHTTP 验证内存头像和各类临时文件共用发送逻辑，正确处理正文、缓存、Range 和资源清理。
// 使用真实 Kratos writer，避免 Recorder 直连掩盖 304、HEAD 等无正文响应的状态码问题。
func TestBinaryResponseHTTP(t *testing.T) {
	content := []byte("0123456789")
	etag := fmt.Sprintf("\"%x\"", sha256.Sum256(content))
	modified := time.Date(2020, time.January, 2, 3, 4, 5, 0, time.UTC)
	for _, source := range []struct {
		name        string // 数据来源或媒体类型。
		avatar      bool   // true 使用内存图片，false 使用网关临时文件。
		contentType string // 空值用于验证默认二进制类型。
	}{
		{name: "avatar", avatar: true, contentType: "image/png"},
		{name: "cover", contentType: "image/jpeg"},
		{name: "mpd", contentType: "application/dash+xml"},
		{name: "segment", contentType: "video/iso.segment"},
		{name: "download fallback"},
	} {
		for _, scenario := range []struct {
			name   string      // HTTP 请求场景。
			method string      // 请求方法。
			header http.Header // 条件或范围请求头。
			status int         // 预期 HTTP 状态码。
			body   string      // 预期正文；416 由标准库生成，不比较具体文案。
		}{
			{name: "full", method: http.MethodGet, status: 200, body: string(content)},
			{name: "range", method: http.MethodGet, header: http.Header{"Range": {"bytes=2-5"}}, status: 206, body: "2345"},
			{name: "invalid range", method: http.MethodGet, header: http.Header{"Range": {"bytes=20-30"}}, status: 416},
			{name: "unchanged", method: http.MethodGet, header: http.Header{"If-None-Match": {etag}}, status: 304},
			{name: "changed", method: http.MethodGet, header: http.Header{"If-None-Match": {"\"old-content\""}}, status: 200, body: string(content)},
			{name: "head", method: http.MethodHead, status: 200},
			{name: "head unchanged", method: http.MethodHead, header: http.Header{"If-None-Match": {etag}}, status: 304},
			{name: "modified since", method: http.MethodGet, header: http.Header{"If-Modified-Since": {modified.Format(http.TimeFormat)}}, status: 304},
		} {
			if source.avatar && scenario.name == "modified since" {
				// 内存头像没有文件时间，只通过内容 ETag 验证，不使用临时文件时间。
				continue
			}
			t.Run(source.name+"/"+scenario.name, func(t *testing.T) {
				var response interface{}
				var tempPath string
				if source.avatar {
					response = &userfilev1.AvatarResp{Content: content, ContentType: source.contentType}
				} else {
					tempPath = filepath.Join(t.TempDir(), "response.tmp")
					if err := os.WriteFile(tempPath, content, 0600); err != nil {
						t.Fatal(err)
					}
					if err := os.Chtimes(tempPath, modified, modified); err != nil {
						t.Fatal(err)
					}
					response = &videoinfov1.FileResp{FilePath: tempPath, Filename: "original.mp4", Headers: map[string]*videoinfov1.FileResp_HeaderValues{
						"Content-Type":        {Value: []string{source.contentType}},
						"Content-Disposition": {Value: []string{`attachment; filename="original.mp4"`}},
						"ETag":                {Value: []string{etag}},
						"X-Download-Value":    {Value: []string{"first", "second"}},
						"X-Empty":             nil,
					}}
				}
				server := khttp.NewServer(khttp.ResponseEncoder(ResponseEncoder))
				route := server.Route("/")
				handler := func(ctx khttp.Context) error { return ctx.Result(http.StatusOK, response) }
				route.GET("/content", handler)
				route.HEAD("/content", handler)
				request := httptest.NewRequest(scenario.method, "/content", nil)
				if scenario.header != nil {
					request.Header = scenario.header.Clone()
				}
				writer := httptest.NewRecorder()
				server.ServeHTTP(writer, request)
				if writer.Code != scenario.status {
					t.Fatalf("status = %d, want %d: %s", writer.Code, scenario.status, writer.Body.String())
				}
				if scenario.status != 416 && writer.Body.String() != scenario.body {
					t.Fatalf("body = %q, want %q", writer.Body.String(), scenario.body)
				}
				if scenario.status == 200 || scenario.status == 206 {
					wantType := source.contentType
					if wantType == "" {
						wantType = "application/octet-stream"
					}
					if got := writer.Header().Get("Content-Type"); got != wantType {
						t.Fatalf("Content-Type = %q, want %q", got, wantType)
					}
					wantLength := len(content)
					if scenario.status == 206 {
						wantLength = 4
					}
					if writer.Header().Get("Content-Length") != strconv.Itoa(wantLength) || writer.Header().Get("Accept-Ranges") != "bytes" {
						t.Fatal("内容长度或 Range 响应头不正确")
					}
				}
				if scenario.status == 206 && writer.Header().Get("Content-Range") != "bytes 2-5/10" {
					t.Fatal("分段内容范围不正确")
				}
				if scenario.status != 416 && writer.Header().Get("ETag") != etag {
					t.Fatal("ETag 未保留")
				}
				if source.avatar {
					if writer.Header().Get("X-Content-Type-Options") != "nosniff" {
						t.Fatal("头像安全头未保留")
					}
					// ServeContent 默认移除 416 的缓存头，避免将范围错误缓存为图片。
					if scenario.status != 416 && writer.Header().Get("Cache-Control") != "no-cache" {
						t.Fatal("头像缓存头未保留")
					}
					if writer.Header().Get("Last-Modified") != "" || !bytes.Equal(content, []byte("0123456789")) {
						t.Fatal("内存头像被修改或使用了磁盘时间")
					}
				} else {
					if writer.Header().Get("Content-Disposition") != `attachment; filename="original.mp4"` || len(writer.Header().Values("X-Download-Value")) != 2 {
						t.Fatal("下载文件名或多值响应头未保留")
					}
					if (scenario.status == 200 || scenario.status == 206) && writer.Header().Get("Last-Modified") != modified.Format(http.TimeFormat) {
						t.Fatal("文件修改时间未保留")
					}
					if _, err := os.Stat(tempPath); !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("临时文件未在发送后清理: %v", err)
					}
				}
			})
		}
	}
}

// TestResponseEncoderMissingFile 验证文件打开失败会返回原始错误，不提前写出成功响应。
func TestResponseEncoderMissingFile(t *testing.T) {
	writer := httptest.NewRecorder()
	err := ResponseEncoder(writer, httptest.NewRequest(http.MethodGet, "/content", nil), &videoinfov1.FileResp{
		FilePath: filepath.Join(t.TempDir(), "missing.tmp"),
	})
	if !errors.Is(err, os.ErrNotExist) || writer.Body.Len() != 0 || len(writer.Header()) != 0 {
		t.Fatalf("打开失败未正常传播: err=%v, header=%v, body=%q", err, writer.Header(), writer.Body.String())
	}
}

// TestResponseEncoderJSON 验证非文件响应继续使用原有 JSON 编码，不被二进制适配误处理。
func TestResponseEncoderJSON(t *testing.T) {
	writer := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/json", nil)
	request.Header.Set("Accept", "application/json")
	if err := ResponseEncoder(writer, request, &userfilev1.UpdateAvatarResp{AvatarUrl: "/avatar"}); err != nil {
		t.Fatal(err)
	}
	if writer.Header().Get("Content-Type") != "application/json" || !bytes.Contains(writer.Body.Bytes(), []byte(`"avatarUrl":"/avatar"`)) {
		t.Fatalf("JSON 响应改变: %s", writer.Body.String())
	}
}
