package resource

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"yssim-go/tests/helpers"
)

// ExtractPathOrFallback 从上传资源响应中读取 path；响应缺少 path 时按 Modelica URI 规则生成兜底路径。
func ExtractPathOrFallback(resp *helpers.Response, packageName, parent, fileName string) string {
	if resp != nil {
		if dataMap, ok := resp.Data.(map[string]any); ok {
			if pathValue, ok := dataMap["path"].(string); ok && strings.TrimSpace(pathValue) != "" {
				return pathValue
			}
		}
	}
	return BuildModelicaResourceURI(packageName, parent, fileName)
}

// BuildModelicaResourceURI 按 package/Resources/parent/fileName 规则构造 modelica:// 资源路径。
func BuildModelicaResourceURI(packageName, parent, fileName string) string {
	parts := []string{packageName, "Resources"}
	if strings.TrimSpace(parent) != "" {
		parts = append(parts, parent)
	}
	if strings.TrimSpace(fileName) != "" {
		parts = append(parts, fileName)
	}
	return "modelica://" + strings.Join(parts, "/")
}

// ReadTestPicturePNG 读取 tests/resources/test_picture.png，供上传图片和资源图片接口测试复用。
func ReadTestPicturePNG() ([]byte, error) {
	candidates := []string{
		"../resources/test_picture.png",
		"tests/resources/test_picture.png",
	}
	for _, candidate := range candidates {
		data, err := os.ReadFile(candidate)
		if err == nil {
			return data, nil
		}
	}

	wd, _ := os.Getwd()
	for _, candidate := range candidates {
		full := filepath.Join(wd, candidate)
		data, err := os.ReadFile(full)
		if err == nil {
			return data, nil
		}
	}

	return nil, fmt.Errorf("读取测试图片失败: %v", candidates)
}
