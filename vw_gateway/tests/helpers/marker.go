package helpers

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// APIMarker 描述测试实际覆盖的接口模板。
type APIMarker struct {
	Method    string
	Path      string
	Operation string
	TestName  string
}

// EmitAPIMarker 向指定输出写入稳定的 API_MARK 行。
// 输入：w 为输出目标；marker 提供 HTTP 方法、显式 proto 路径模板、操作名和测试名。
// 输出：写入成功返回 nil；任一字段为空或输出失败时返回错误。
// 示例：err := helpers.EmitAPIMarker(os.Stdout, helpers.APIMarker{Method: "GET", Path: "/api/v1/videos/{video_id}", Operation: "get_video", TestName: "TestVideoGet"})。
func EmitAPIMarker(w io.Writer, marker APIMarker) error {
	method := strings.ToUpper(strings.TrimSpace(marker.Method))
	path := strings.TrimSpace(marker.Path)
	operation := strings.TrimSpace(marker.Operation)
	testName := strings.TrimSpace(marker.TestName)
	if method == "" || path == "" || operation == "" || testName == "" {
		return fmt.Errorf("API_MARK 字段不能为空")
	}
	_, err := fmt.Fprintf(w, "API_MARK method=%s path=%s operation=%s test=%s\n", method, path, operation, testName)
	return err
}

// PrintAPIMarker 将 API_MARK 输出到标准输出，供后续覆盖率工具消费。
// 输入：marker 提供待输出的接口覆盖标记，字段规则与 EmitAPIMarker 相同。
// 输出：写入成功返回 nil；字段缺失或标准输出写入失败时返回错误。
// 示例：err := helpers.PrintAPIMarker(helpers.APIMarker{Method: "GET", Path: "/api/v1/videos", Operation: "list_videos", TestName: "TestVideoList"})。
func PrintAPIMarker(marker APIMarker) error { return EmitAPIMarker(os.Stdout, marker) }
