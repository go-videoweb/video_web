package ffmpeg_test

import (
	"context"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
	"util/ffmpeg"

	"github.com/stretchr/testify/require"
)

// newVideoFixture 生成两秒的 AVI 测试视频，不依赖仓库外的素材或向项目目录写产物。
func newVideoFixture(t *testing.T) (context.Context, string) {
	t.Helper()
	binary, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skipf("需要 ffmpeg: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	t.Cleanup(cancel)
	input := filepath.Join(t.TempDir(), "input.avi")
	output, err := exec.CommandContext(ctx, binary, "-hide_banner", "-loglevel", "error",
		"-f", "lavfi", "-i", "color=c=black:s=64x64:r=10", "-t", "2", "-c:v", "mpeg4", input).CombinedOutput()
	require.NoError(t, err, "%s", output)
	return ctx, input
}

// TestConvertToMP4 验证转换命令可生成 MP4 文件。
func TestConvertToMP4(t *testing.T) {
	ctx, input := newVideoFixture(t)
	output := filepath.Join(t.TempDir(), "output.mp4")
	require.NoError(t, ffmpeg.OtherToMP4(ctx, input, output))
	require.FileExists(t, output)
}

// TestGetVideoDuration 验证探测命令返回实际视频时长。
func TestGetVideoDuration(t *testing.T) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skipf("需要 ffprobe: %v", err)
	}
	ctx, input := newVideoFixture(t)
	duration, err := ffmpeg.GetVideoDuration(ctx, input)
	require.NoError(t, err)
	require.EqualValues(t, 2, duration)
}

// TestMakeDash 验证非 MP4 输入可生成清单和分片，并清理中间文件。
func TestMakeDash(t *testing.T) {
	ctx, input := newVideoFixture(t)
	output := filepath.Join(t.TempDir(), "dash")
	require.NoError(t, ffmpeg.MakeDASH(ctx, input, output, "dash.mpd"))
	require.FileExists(t, filepath.Join(output, "dash.mpd"))
	segments, err := filepath.Glob(filepath.Join(output, "chunk-*.m4s"))
	require.NoError(t, err)
	require.NotEmpty(t, segments)
	require.NoFileExists(t, filepath.Join(filepath.Dir(input), "input.mp4"))
}
