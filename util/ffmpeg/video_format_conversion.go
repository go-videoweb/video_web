package ffmpeg

import (
	"context"
	"os"
	"path"
	"path/filepath"
	"util/helper/file"
)

// OtherToMP4 使用 ffmpeg 将输入转为 MP4，ctx 控制进程的取消和超时。
func OtherToMP4(ctx context.Context, input string, output string) error {
	ffmpegOperator := NewFFmpeg()
	ffmpegOperator.
		AddGlobalArgs("-y").
		AddInputInfo(input).AddOutputInfo(output).
		SetAudioCodec(output, "aac").
		SetVideoCodec(output, "copy")
	return ffmpegOperator.Run(ctx)
}

// MakeDASH 在输出目录生成 DASH 资源；非 MP4 输入会先转换，两步共用同一个 ctx。
// mpdFileName 为空时使用 output.mpd；失败时不保证输出目录中没有部分产物。
func MakeDASH(ctx context.Context, inputFilePath string, outputFolderPath string, mpdFileName string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if mpdFileName == "" {
		mpdFileName = "output.mpd"
	}
	if filepath.Ext(mpdFileName) != ".mpd" {
		mpdFileName += ".mpd"
	}
	// Convert input file to mp4 format
	if filepath.Ext(inputFilePath) != ".mp4" {
		originInputFilePath := inputFilePath
		inputFilePath = file.ChangeFileExtension(inputFilePath, "mp4")
		defer func() {
			_ = os.Remove(inputFilePath)
		}()
		err := OtherToMP4(ctx, originInputFilePath, inputFilePath)
		if err != nil {
			return err
		}
	}

	// Create output folder if not exists
	err := os.MkdirAll(outputFolderPath, os.ModePerm)
	if err != nil {
		return err
	}

	// Use path.Join instead of filepath.Join to avoid windows path separator('\') issue:
	// It seems that ffmpeg command cannot handle windows path separator correctly.
	outputFilePath := path.Join(outputFolderPath, mpdFileName)
	ffmpegOperator := NewFFmpeg().
		AddGlobalArgs("-y").
		AddInputInfo(inputFilePath).
		AddOutputInfo(outputFilePath).
		SetCopyCodec(outputFilePath).ShowCommand()
	return ffmpegOperator.Run(ctx)
}
