package ffmpeg

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

// TestMediaBuildersKeepConcreteType 验证公共链式方法返回原实例，且专属方法与参数顺序不变。
func TestMediaBuildersKeepConcreteType(t *testing.T) {
	mpeg := NewFFmpeg()
	video := mpeg.AddGlobalArgs("-y").SetLogLevel("error").
		AddInputInfo("input video.mp4").SetCopyCodec("output.mpd").SetFormatDash("output.mpd")
	wantVideo := []string{"-y", "-v", "error", "-i", "input video.mp4", "-c:v", "copy", "-c:a", "copy", "-f", "dash", "output.mpd"}
	if video != mpeg || !slices.Equal(video.buildArguments(), wantVideo) {
		t.Fatalf("ffmpeg 未返回原实例或参数错误: %v", video.buildArguments())
	}

	probe := NewFFprobe()
	metadata := probe.AddInputInfo("input video.mp4").SetLogLevel("quiet").
		ShowEntries("input video.mp4", "duration", "size").SetOutputFormat("input video.mp4", "csv=p=0")
	wantMetadata := []string{"-v", "quiet", "-show_entries", "format=duration,size", "-output_format", "csv=p=0", "-i", "input video.mp4"}
	if metadata != probe || !slices.Equal(metadata.buildArguments(), wantMetadata) {
		t.Fatalf("ffprobe 未返回原实例或参数错误: %v", metadata.buildArguments())
	}
	if args := NewFFmpeg().buildArguments(); len(args) != 0 {
		t.Fatalf("新命令继承了旧命令的参数: %v", args)
	}
}

// TestMediaCommandHelper 模拟外部程序，回归测试无需依赖实际 ffmpeg 来制造超时和失败。
func TestMediaCommandHelper(t *testing.T) {
	switch os.Getenv("VIDEOWEB_MEDIA_TEST_MODE") {
	case "fail":
		fmt.Fprint(os.Stderr, "decoder failed")
		os.Exit(17)
	case "output":
		fmt.Fprint(os.Stdout, "12.5")
		os.Exit(0)
	case "wait":
		if err := os.WriteFile(os.Getenv("VIDEOWEB_MEDIA_TEST_READY"), nil, 0600); err != nil {
			t.Fatal(err)
		}
		time.Sleep(15 * time.Second)
		os.Exit(0)
	}
}

// newMediaTestCommand 用当前测试程序替代 ffmpeg，仍通过实际的进程执行路径运行。
func newMediaTestCommand(t *testing.T, mode string) *FFmpeg {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("VIDEOWEB_MEDIA_TEST_MODE", mode)
	cmd := NewFFmpeg().AddGlobalArgs("-test.run=^TestMediaCommandHelper$")
	cmd.command = executable
	return cmd
}

// TestRunCancelsStartedProcess 验证取消能结束已经运行的子进程，且保留可识别的取消错误。
func TestRunCancelsStartedProcess(t *testing.T) {
	ready := filepath.Join(t.TempDir(), "started")
	t.Setenv("VIDEOWEB_MEDIA_TEST_READY", ready)
	cmd := newMediaTestCommand(t, "wait")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- cmd.Run(ctx) }()

	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		} else if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		select {
		case err := <-done:
			t.Fatalf("子进程未进入等待阶段: %v", err)
		case <-ctx.Done():
			t.Fatal("等待子进程启动超时")
		case <-ticker.C:
		}
	}

	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("未保留取消错误: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("取消未停止子进程")
	}
}

// TestRunReportsCommandFailure 验证错误既保留进程退出原因，也提供 stderr 中的诊断。
func TestRunReportsCommandFailure(t *testing.T) {
	err := newMediaTestCommand(t, "fail").Run(context.Background())
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 17 || !strings.Contains(err.Error(), "decoder failed") {
		t.Fatalf("执行错误或 stderr 丢失: %v", err)
	}

	cmd := NewFFmpeg()
	cmd.command = "videoweb-missing-media-command-for-test"
	if err := cmd.Run(context.Background()); !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("程序找不到时未保留原始错误: %v", err)
	}
}

// TestRunCombinedOutputContext 验证合并输出保持可读，且已经超时的 context 不会启动命令。
func TestRunCombinedOutputContext(t *testing.T) {
	cmd := newMediaTestCommand(t, "output")
	output, err := cmd.RunCombinedOutput(context.Background())
	if err != nil || string(output) != "12.5" {
		t.Fatalf("输出=%q，错误=%v", output, err)
	}

	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer cancel()
	output, err = cmd.RunCombinedOutput(ctx)
	if len(output) != 0 || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("超时后输出=%q，错误=%v", output, err)
	}
}
