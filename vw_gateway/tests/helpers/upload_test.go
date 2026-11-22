package helpers

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestWaitForVideoUpload 验证状态可跳跃推进，不发送任何写入请求，并保留大 ID。
func TestWaitForVideoUpload(t *testing.T) {
	var calls atomic.Int32
	states := []string{"waiting_cover", "waiting_video", "uploading", "processing", "completed"}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/api/v1/video/9007199254740993/upload-status" {
			t.Error("发送了非状态请求")
		}
		if r.Header.Get("Refresh-Token") != "Bearer refresh" {
			t.Error("没有保留发布者会话")
		}
		index := int(calls.Add(1)) - 1
		if index >= len(states) {
			t.Error("完成后继续轮询")
			index = len(states) - 1
		}
		fmt.Fprintf(w, `{"status":%q,"updatedTime":null}`, states[index])
	}))
	defer server.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	client := NewAPIClient(TestConfig{BaseURL: server.URL}).WithTokens("access", "refresh")
	status, err := client.WaitForVideoUpload(ctx, "9007199254740993", time.Millisecond)
	if err != nil || status.Status != "completed" || calls.Load() != 5 {
		t.Fatalf("轮询失败: %v，次数=%d", err, calls.Load())
	}
}

// TestWaitForVideoUpload_StopsOnFailure 验证业务失败、协议错误和 HTTP 错误均不重复查询。
func TestWaitForVideoUpload_StopsOnFailure(t *testing.T) {
	for index, body := range []string{`{"status":"failed","uploadErrorMsg":"secret"}`, `{"status":"unknown"}`, `{"code":3101,"reason":"X","message":"secret"}`, `{"status":`, `{"status":"completed"} {}`} {
		t.Run(fmt.Sprintf("响应%d", index), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { calls.Add(1); fmt.Fprint(w, body) }))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			_, err := NewAPIClient(TestConfig{BaseURL: server.URL}).WaitForVideoUpload(ctx, "1", time.Millisecond)
			if err == nil || calls.Load() != 1 || strings.Contains(err.Error(), "secret") {
				t.Fatal("失败未立即停止或泄露消息")
			}
		})
	}
}

// TestWaitForVideoUpload_HTTPAndDeadline 验证代理故障不重试，总截止时间覆盖等待间隔。
func TestWaitForVideoUpload_HTTPAndDeadline(t *testing.T) {
	for _, httpStatus := range []int{200, 503} {
		t.Run(fmt.Sprintf("HTTP%d", httpStatus), func(t *testing.T) {
			var calls atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				calls.Add(1)
				w.WriteHeader(httpStatus)
				fmt.Fprint(w, `{"status":"processing"}`)
			}))
			defer server.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
			defer cancel()
			_, err := NewAPIClient(TestConfig{BaseURL: server.URL}).WaitForVideoUpload(ctx, "1", time.Hour)
			if err == nil || calls.Load() != 1 {
				t.Fatalf("未停止或重复请求: %v", err)
			}
			if httpStatus == 200 && !errors.Is(err, context.DeadlineExceeded) {
				t.Fatalf("截止时间未传播: %v", err)
			}
		})
	}
}

// TestWaitForVideoUpload_Cancellation 验证等待期间响应取消，并拒绝无界等待和非法输入。
func TestWaitForVideoUpload_Cancellation(t *testing.T) {
	var cancel context.CancelFunc
	ctx, stop := context.WithTimeout(context.Background(), time.Second)
	cancel = stop
	defer stop()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{"status":"processing"}`); cancel() }))
	defer server.Close()
	client := NewAPIClient(TestConfig{BaseURL: server.URL})
	_, err := client.WaitForVideoUpload(ctx, "1", time.Hour)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("取消未传播: %v", err)
	}
	if _, err := client.WaitForVideoUpload(context.Background(), "1", time.Second); err == nil {
		t.Fatal("接受了无截止时间的等待")
	}
	if _, err := client.WaitForVideoUpload(ctx, "-1", time.Second); err == nil {
		t.Fatal("接受了非法 ID")
	}
	if _, err := client.WaitForVideoUpload(ctx, "1", 0); err == nil {
		t.Fatal("接受了非法间隔")
	}
}
