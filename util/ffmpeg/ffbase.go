package ffmpeg

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"

	"github.com/go-kratos/kratos/v2/log"
)

// ffBase 共享参数构造与执行，T 仅用于让链式方法返回所属的具体工具。
type ffBase[T any] struct {
	self          T          // 构造时绑定的外层工具实例。
	command       string     // 要执行的程序名或路径。
	globalArgs    globalArgs // 放在输入和输出之前的全局参数。
	inputFileArgs inputArgs  // 按输入路径保存该输入的参数。
	outputArgs    outputArgs // 按输出路径保存参数，只由支持输出的工具配置。
}

// newFFBase 初始化单条命令的参数，并绑定外层实例供链式调用返回。
func newFFBase[T any](command string, self T) *ffBase[T] {
	return &ffBase[T]{
		self:          self,
		command:       command,
		globalArgs:    globalArgs{},
		inputFileArgs: make(inputArgs),
		outputArgs:    make(outputArgs),
	}
}

// AddInputInfo 添加输入路径与该输入的参数，返回当前工具继续配置。
func (f *ffBase[T]) AddInputInfo(inputFilePath string, arguments ...string) T {
	f.inputFileArgs.addInput(inputFilePath, arguments...)
	return f.self
}

// AddGlobalArgs 按传入顺序追加全局参数，返回当前工具继续配置。
func (f *ffBase[T]) AddGlobalArgs(args ...string) T {
	f.globalArgs.addGlobal(args...)
	return f.self
}

// SetLogLevel 设置媒体工具本身的输出级别，不影响 Go 应用的日志级别。
func (f *ffBase[T]) SetLogLevel(level string) T {
	return f.AddGlobalArgs("-v", level)
}

// Run 执行命令并丢弃标准输出；ctx 控制子进程，失败时保留执行错误和标准错误输出。
func (f *ffBase[T]) Run(ctx context.Context) error {
	command := exec.CommandContext(ctx, f.command, f.buildArguments()...)
	var stderr bytes.Buffer
	command.Stderr = &stderr

	err := command.Run()
	if err != nil {
		return f.commandError(ctx, err, stderr.Bytes())
	}
	return nil
}

// RunCombinedOutput 执行命令并返回合并后的输出；即使失败也保留已读取的输出。
func (f *ffBase[T]) RunCombinedOutput(ctx context.Context) ([]byte, error) {
	command := exec.CommandContext(ctx, f.command, f.buildArguments()...)
	output, err := command.CombinedOutput()
	if err != nil {
		return output, f.commandError(ctx, err, output)
	}
	return output, nil
}

// commandError 保留进程失败原因；取消导致的进程退出也能用 errors.Is 识别 context 错误。
func (f *ffBase[T]) commandError(ctx context.Context, err error, output []byte) error {
	if ctxErr := ctx.Err(); ctxErr != nil && !errors.Is(err, ctxErr) {
		err = errors.Join(err, ctxErr)
	}
	if detail := strings.TrimSpace(string(output)); detail != "" {
		return fmt.Errorf("%s: %w: %s", f.command, err, detail)
	}
	return fmt.Errorf("%s: %w", f.command, err)
}

// BuildCommand 返回用于诊断的参数文本，不作为 shell 命令执行。
func (f *ffBase[T]) BuildCommand() string {
	return strings.Join(f.buildArguments(), " ")
}

// ShowCommand 将命令及参数写入调试日志，不启动子进程。
func (f *ffBase[T]) ShowCommand() T {
	log.Debugw("msg", "媒体处理命令", "program", f.command, "args", f.buildArguments())
	return f.self
}

// buildArguments 按全局、输入、输出的分组构造 argv，每组文件选项紧邻其路径。
func (f *ffBase[T]) buildArguments() []string {
	var args []string

	// add global arguments
	args = append(args, f.globalArgs...)

	// add input arguments
	for inputFilePath, arguments := range f.inputFileArgs {
		args = append(args, arguments...)
		args = append(args, "-i", inputFilePath)
	}

	// add output arguments
	for outputFilePath, arguments := range f.outputArgs {
		args = append(args, arguments...)
		args = append(args, outputFilePath)
	}

	return args
}
