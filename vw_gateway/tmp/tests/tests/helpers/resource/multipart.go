package resource

import (
	"bytes"
	"fmt"
	"mime/multipart"
	"net/http"

	"yssim-go/tests/helpers"
)

// PostMultipartRaw 发送 multipart POST 请求，并返回原始 HTTP 响应和完整响应体。
func PostMultipartRaw(path string, fields map[string]string, includeFile bool, fileField, fileName string, content []byte) (*http.Response, []byte, error) {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for k, v := range fields {
		if err := writer.WriteField(k, v); err != nil {
			return nil, nil, fmt.Errorf("写入 multipart 字段失败: %w", err)
		}
	}
	if includeFile {
		part, err := writer.CreateFormFile(fileField, fileName)
		if err != nil {
			return nil, nil, fmt.Errorf("创建 multipart 文件字段失败: %w", err)
		}
		if _, err := part.Write(content); err != nil {
			return nil, nil, fmt.Errorf("写入 multipart 文件内容失败: %w", err)
		}
	}
	if err := writer.Close(); err != nil {
		return nil, nil, fmt.Errorf("关闭 multipart writer 失败: %w", err)
	}

	cfg := helpers.DefaultTestConfig()
	req, err := http.NewRequest(http.MethodPost, cfg.BaseURL+path, &body)
	if err != nil {
		return nil, nil, fmt.Errorf("创建 HTTP 请求失败: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if cfg.Username != "" {
		req.Header.Set("username", cfg.Username)
	}
	if cfg.SpaceID != "" {
		req.Header.Set("space-id", cfg.SpaceID)
	}
	if cfg.Token != "" {
		req.Header.Set("Authorization", cfg.Token)
	}

	httpClient := &http.Client{Timeout: cfg.Timeout}
	resp, err := httpClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("请求失败: %w", err)
	}
	bodyBytes, err := helpers.ReadAndCloseRawBody(resp)
	if err != nil {
		return nil, nil, err
	}
	return resp, bodyBytes, nil
}
