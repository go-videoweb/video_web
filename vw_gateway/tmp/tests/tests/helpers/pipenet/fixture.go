package pipenet

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/google/uuid"

	"yssim-go/app/DataBase"
	"yssim-go/config"
	"yssim-go/tests/helpers"
)

const (
	// MappingConfigResourceName 是管网映射表测试使用的固定 JSON 文件。
	MappingConfigResourceName = "map_模型映射表.json"
	// InfoFileResourceName 是管网信息文件测试使用的固定 XML 文件。
	InfoFileResourceName = "pipe_管网信息文件.xml"
)

// UniqueName 生成管网测试记录名称，避免和 147 测试环境中已有记录冲突。
func UniqueName(prefix string) string {
	return fmt.Sprintf("%s_%s", prefix, uuid.NewString()[:8])
}

// ReadPipeNetResource 读取 tests/resources/pipenet 下的固定测试资源。
func ReadPipeNetResource(fileName string) ([]byte, error) {
	resourcePath := pipeNetResourcePath(fileName)
	data, err := os.ReadFile(resourcePath)
	if err != nil {
		return nil, fmt.Errorf("读取管网测试资源 %s 失败: %w", fileName, err)
	}
	return data, nil
}

// UploadInfoFileFixture 上传一份管网信息 XML 测试文件并返回记录 ID。
func UploadInfoFileFixture(client *helpers.APIClient, name, description string) (string, error) {
	content, err := ReadPipeNetResource(InfoFileResourceName)
	if err != nil {
		return "", err
	}
	resp, err := client.UploadPipeNetInfoFile(name, description, InfoFileResourceName, content)
	if err != nil {
		return "", err
	}
	if resp.Status != 0 {
		return "", fmt.Errorf("上传管网信息文件失败: status=%d msg=%q err=%q", resp.Status, resp.Msg, resp.Err)
	}
	id, ok := resp.Data.(string)
	if !ok || strings.TrimSpace(id) == "" {
		return "", fmt.Errorf("上传管网信息文件返回 id 为空或类型不正确: %T %v", resp.Data, resp.Data)
	}
	return id, nil
}

// UploadMappingConfigFixture 上传一份管网映射表 JSON 测试文件并返回记录 ID。
func UploadMappingConfigFixture(client *helpers.APIClient, name, description string) (string, error) {
	content, err := ReadPipeNetResource(MappingConfigResourceName)
	if err != nil {
		return "", err
	}
	resp, err := client.UploadMappingConfig(name, description, MappingConfigResourceName, content)
	if err != nil {
		return "", err
	}
	if resp.Status != 0 {
		return "", fmt.Errorf("上传管网映射表失败: status=%d msg=%q err=%q", resp.Status, resp.Msg, resp.Err)
	}
	id, ok := resp.Data.(string)
	if !ok || strings.TrimSpace(id) == "" {
		return "", fmt.Errorf("上传管网映射表返回 id 为空或类型不正确: %T %v", resp.Data, resp.Data)
	}
	return id, nil
}

// CleanupInfoFiles 通过接口清理管网信息文件记录。
func CleanupInfoFiles(client *helpers.APIClient, ids []string) error {
	ids = compactStrings(ids)
	if len(ids) == 0 {
		return nil
	}
	resp, err := client.DeletePipeNetInfoFile(ids)
	if err != nil {
		return err
	}
	if resp.Status != 0 {
		return fmt.Errorf("清理管网信息文件失败: status=%d msg=%q err=%q", resp.Status, resp.Msg, resp.Err)
	}
	return nil
}

// CleanupMappingConfigs 通过接口清理管网映射表记录。
func CleanupMappingConfigs(client *helpers.APIClient, ids []string) error {
	ids = compactStrings(ids)
	if len(ids) == 0 {
		return nil
	}
	resp, err := client.DeleteMappingConfig(ids)
	if err != nil {
		return err
	}
	if resp.Status != 0 {
		return fmt.Errorf("清理管网映射表失败: status=%d msg=%q err=%q", resp.Status, resp.Msg, resp.Err)
	}
	return nil
}

// CleanupGeneratedModel 清理管网生成接口创建的顶层模型和下载记录。
func CleanupGeneratedModel(client *helpers.APIClient, username, packageID, modelName string) error {
	if strings.TrimSpace(packageID) != "" && strings.TrimSpace(modelName) != "" {
		_, _ = client.DeletePackage(packageID, "", modelName)
	}
	if strings.TrimSpace(packageID) == "" && strings.TrimSpace(modelName) == "" {
		return nil
	}
	if err := config.DB.Where("username = ? AND (package_id = ? OR model_name = ?)", username, packageID, modelName).Delete(&DataBase.YssimPipeNetCadDownload{}).Error; err != nil {
		return fmt.Errorf("清理管网模型下载记录失败: %w", err)
	}
	return nil
}

// FindInfoFileIDByName 从数据库中按名称查找管网信息文件记录 ID。
func FindInfoFileIDByName(username, name string) (string, error) {
	var record DataBase.YssimPipeNetCad
	if err := config.DB.Where("username = ? AND name = ?", username, name).First(&record).Error; err != nil {
		return "", err
	}
	return record.ID, nil
}

// FindMappingConfigIDByName 从数据库中按名称查找管网映射表记录 ID。
func FindMappingConfigIDByName(username, name string) (string, error) {
	var record DataBase.YssimMappingConfig
	if err := config.DB.Where("username = ? AND name = ?", username, name).First(&record).Error; err != nil {
		return "", err
	}
	return record.ID, nil
}

// InfoFileExists 根据 ID 判断管网信息文件记录是否仍存在。
func InfoFileExists(username, id string) (bool, error) {
	var count int64
	if err := config.DB.Model(&DataBase.YssimPipeNetCad{}).Where("username = ? AND id = ?", username, id).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// MappingConfigExists 根据 ID 判断管网映射表记录是否仍存在。
func MappingConfigExists(username, id string) (bool, error) {
	var count int64
	if err := config.DB.Model(&DataBase.YssimMappingConfig{}).Where("username = ? AND id = ?", username, id).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// ResponseListContainsID 判断统一响应 data 中指定列表字段是否包含目标 id。
func ResponseListContainsID(resp *helpers.Response, listKey, id string) (bool, error) {
	dataMap, err := helpers.ObjectData(resp, "读取列表响应")
	if err != nil {
		return false, err
	}
	rawList, ok := dataMap[listKey].([]any)
	if !ok {
		return false, fmt.Errorf("列表字段 %q 不是数组: %T", listKey, dataMap[listKey])
	}
	for _, item := range rawList {
		itemMap, ok := item.(map[string]any)
		if !ok {
			return false, fmt.Errorf("列表字段 %q 中存在非 object 项: %T", listKey, item)
		}
		if itemMap["id"] == id {
			return true, nil
		}
	}
	return false, nil
}

// URLFromResponse 从统一响应 data.url 中读取下载文件路径。
func URLFromResponse(resp *helpers.Response, message string) (string, error) {
	dataMap, err := helpers.ObjectData(resp, message)
	if err != nil {
		return "", err
	}
	urlValue, ok := dataMap["url"].(string)
	if !ok || strings.TrimSpace(urlValue) == "" {
		return "", fmt.Errorf("%s: data.url 为空或类型不正确: %T %v", message, dataMap["url"], dataMap["url"])
	}
	return urlValue, nil
}

// StringValue 将响应中的动态字段安全转换为 string。
func StringValue(value any) string {
	if value == nil {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return text
}

// compactStrings 去掉空字符串，避免清理接口收到无意义 ID。
func compactStrings(values []string) []string {
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			result = append(result, value)
		}
	}
	return result
}

// pipeNetResourcePath 返回管网测试资源的绝对路径，兼容从仓库根目录或 tests/api 包目录运行。
func pipeNetResourcePath(fileName string) string {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		return filepath.Join("tests", "resources", "pipenet", fileName)
	}
	return filepath.Join(filepath.Dir(currentFile), "..", "..", "resources", "pipenet", fileName)
}
