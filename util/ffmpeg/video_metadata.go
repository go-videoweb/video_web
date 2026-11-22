package ffmpeg

import (
	"context"
	"strconv"
	"strings"
)

// GetVideoDuration 使用 ffprobe 读取时长并四舍五入到整秒；ctx 控制探测进程。
func GetVideoDuration(ctx context.Context, inputFilePath string) (int64, error) {
	ffprobeOperator := NewFFprobe()

	outData, err := ffprobeOperator.
		AddInputInfo(inputFilePath).
		AddGlobalArgs("-v", "quiet").
		ShowEntries(inputFilePath, "duration").
		SetOutputFormat(inputFilePath, "csv=p=0").
		RunCombinedOutput(ctx)
	if err != nil {
		return 0, err
	}
	tmp, _ := strconv.ParseFloat(strings.TrimRight(string(outData), "\r\n"), 64) //注意去除字符串末尾的\r\n
	return roundOff(tmp), nil
}
