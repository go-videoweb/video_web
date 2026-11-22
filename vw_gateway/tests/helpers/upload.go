package helpers

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

// VideoUploadStatus 保存发布者可见的投稿处理结果，不将上传成功等同于处理完成。
// UploadErrorMsg 为原始业务提示，只供断言使用，不应直接打印到 CI 日志。
type VideoUploadStatus struct {
	Status         string  `json:"status"`
	UpdatedTime    *string `json:"updatedTime"`
	UploadErrorMsg string  `json:"uploadErrorMsg"`
}

// WaitForVideoUpload 轮询投稿状态直到完成、失败或 context 结束。
// 输入：videoID 为正 int64 十进制字符串；interval 必须为正，ctx 必须设置总截止时间。
// 输出：返回最后读取的状态及错误；只查询状态，不自动登录、刷新、重传或重试失败请求。
func (c *APIClient) WaitForVideoUpload(ctx context.Context, videoID string, interval time.Duration) (*VideoUploadStatus, error) {
	id, err := strconv.ParseInt(videoID, 10, 64)
	if err != nil || id <= 0 {
		return nil, fmt.Errorf("投稿视频 ID 必须是正 int64")
	}
	if interval <= 0 {
		return nil, fmt.Errorf("轮询间隔必须为正")
	}
	if _, ok := ctx.Deadline(); !ok {
		return nil, fmt.Errorf("投稿轮询必须设置总截止时间")
	}
	var last *VideoUploadStatus
	for {
		if err := ctx.Err(); err != nil {
			return last, fmt.Errorf("投稿状态等待结束: %w", err)
		}
		response, err := c.Do(ctx, http.MethodGet, "/api/v1/video/"+strconv.FormatInt(id, 10)+"/upload-status", nil, "")
		if err != nil {
			return last, err
		}
		if response.StatusCode != http.StatusOK {
			return last, fmt.Errorf("投稿状态查询异常: %s", response.Diagnostic())
		}
		apiErr, err := response.BusinessError()
		if err != nil {
			return last, err
		}
		if apiErr != nil {
			return last, apiErr
		}
		var status VideoUploadStatus
		if err := response.DecodeJSON(&status); err != nil {
			return last, err
		}
		last = &status
		switch status.Status {
		case "completed":
			return last, nil
		case "failed":
			return last, fmt.Errorf("投稿处理失败（业务提示请使用断言检查，日志已省略）")
		case "waiting_cover", "waiting_video", "uploading", "processing":
		default:
			return last, fmt.Errorf("投稿状态缺失或不受支持")
		}
		timer := time.NewTimer(interval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return last, fmt.Errorf("投稿状态等待结束: %w", ctx.Err())
		case <-timer.C:
		}
	}
}
