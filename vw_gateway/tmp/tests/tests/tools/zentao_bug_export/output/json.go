package output

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// WriteJSON 将 Go 数据转成 JSON 文本，并写到命令行窗口或指定文件。
//
// 作用：统一工具 JSON 输出格式，并保留 steps_raw 中的 <img> 等 HTML 片段，便于 AI 和开发者阅读。
// 输入：path 为输出路径或 "-"，data 为要输出的数据，pretty 控制是否换行缩进。
// 输出：error；成功时返回 nil，失败时返回 JSON 生成、目录创建或文件写入错误。
// 示例：WriteJSON("-", export, true) 会把带缩进的 JSON 打印到命令行窗口。
func WriteJSON(path string, data any, pretty bool) error {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if pretty {
		encoder.SetIndent("", "  ")
	}
	if err := encoder.Encode(data); err != nil {
		return fmt.Errorf("marshal json: %w", err)
	}
	content := buf.Bytes()

	if path == "" || path == "-" {
		_, err := os.Stdout.Write(content)
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return fmt.Errorf("write output json: %w", err)
	}
	return nil
}
