package report

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"yssim-go/tests/tools/api_coverage/coverage"
)

// WriteJSON 将覆盖率报告写入 UTF-8 JSON 文件。
// 相比 stdout 文本，JSON 报告便于 TeamCity artifact、后续阈值判断和历史趋势分析。
func WriteJSON(outputPath string, report coverage.CoverageReport) error {
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return fmt.Errorf("create report directory: %w", err)
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal coverage report: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(outputPath, data, 0o644); err != nil {
		return fmt.Errorf("write coverage report %s: %w", outputPath, err)
	}
	return nil
}
