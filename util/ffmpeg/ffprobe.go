package ffmpeg

import "strings"

// FFprobe 配置一条媒体探测命令，通过 NewFFprobe 构造；配置期间不并发复用。
type FFprobe struct {
	*ffBase[*FFprobe]
}

// NewFFprobe 创建独立的 ffprobe 命令构造器。
func NewFFprobe() *FFprobe {
	ffprobeOperator := &FFprobe{}
	ffprobeOperator.ffBase = newFFBase(ffprobe, ffprobeOperator)
	return ffprobeOperator
}

// ShowEntries 指定要读取的媒体容器字段，如 duration。
func (f *FFprobe) ShowEntries(inputFilePath string, entries ...string) *FFprobe {
	return f.AddInputInfo(inputFilePath, "-show_entries", "format="+strings.Join(entries, ","))
}

// SetOutputFormat 配置探测结果的序列化格式，如 csv=p=0。
func (f *FFprobe) SetOutputFormat(inputFilePath string, outputFormat string) *FFprobe {
	return f.AddInputInfo(inputFilePath, "-output_format", outputFormat)
}
