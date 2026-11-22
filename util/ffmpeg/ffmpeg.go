package ffmpeg

// FFmpeg 配置一条 ffmpeg 命令，通过 NewFFmpeg 构造；配置期间不并发复用。
type FFmpeg struct {
	*ffBase[*FFmpeg]
}

// NewFFmpeg 创建独立的 ffmpeg 命令构造器。
func NewFFmpeg() *FFmpeg {
	ffmpegOperator := &FFmpeg{}
	ffmpegOperator.ffBase = newFFBase(ffmpeg, ffmpegOperator)
	return ffmpegOperator
}

// AddOutputInfo 添加输出路径与仅作用于该输出的参数。
func (f *FFmpeg) AddOutputInfo(outputFilePath string, args ...string) *FFmpeg {
	f.outputArgs.addOutput(outputFilePath, args...)
	return f
}

// SetAudioCodec 配置指定输出的音频编码器。
func (f *FFmpeg) SetAudioCodec(outputFilePath string, codec string) *FFmpeg {
	return f.AddOutputInfo(outputFilePath, "-c:a", codec)
}

// SetVideoCodec 配置指定输出的视频编码器。
func (f *FFmpeg) SetVideoCodec(outputFilePath string, codec string) *FFmpeg {
	return f.AddOutputInfo(outputFilePath, "-c:v", codec)
}

// SetCopyCodec 直接复制指定输出的音视频流，不重新编码。
func (f *FFmpeg) SetCopyCodec(outputFilePath string) *FFmpeg {
	return f.SetVideoCodec(outputFilePath, "copy").SetAudioCodec(outputFilePath, "copy")
}

// SetSegmentTime 配置指定输出的分段时长，单位为秒。
func (f *FFmpeg) SetSegmentTime(outputFilePath string, duration float64) *FFmpeg {
	return f.AddOutputInfo(outputFilePath, "-segment_time", formatFloatTime(duration, 0))
}

// SetFormatDash 将指定输出格式设为 DASH。
func (f *FFmpeg) SetFormatDash(outputFilePath string) *FFmpeg {
	return f.AddOutputInfo(outputFilePath, "-f", "dash")
}
