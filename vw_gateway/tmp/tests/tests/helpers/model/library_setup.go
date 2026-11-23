package model

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"

	"yssim-go/tests/helpers"
)

// EnsureCommonModelLibrariesLoaded 确保 API 测试公共依赖模型库已加载。
func EnsureCommonModelLibrariesLoaded(client *helpers.APIClient, modelicaPackageID string) error {
	if err := EnsureSystemModelLoaded(client, modelicaPackageID, "Modelica", "加载 Modelica 标准库"); err != nil {
		return err
	}

	loadedLibraries, err := loadedUserModelLibraries(client)
	if err != nil {
		return err
	}
	packageIDs, err := packageFileIDsByName(client)
	if err != nil {
		return err
	}

	for _, library := range requiredPipeNetLibraries {
		if loadedLibraries[library.Name] {
			continue
		}

		packageID := packageIDs[library.Name]
		if packageID == "" {
			packageID, err = uploadRequiredModelLibrary(client, library)
			if err != nil {
				return err
			}
			packageIDs[library.Name] = packageID
		}

		if err := EnsureModelLoaded(client, packageID, library.Name, fmt.Sprintf("加载管网依赖模型库 %s", library.Name)); err != nil {
			return err
		}
	}

	return verifyRequiredPipeNetLibrariesLoaded(client)
}

// requiredModelLibrary 描述接口测试启动前需要加载到工作空间的模型库。
type requiredModelLibrary struct {
	Name     string
	FileName string
}

// requiredPipeNetLibraries 是管网生成接口依赖的用户模型库。
var requiredPipeNetLibraries = []requiredModelLibrary{
	{Name: "FuelSystem", FileName: "FuelSystem.zip"},
	{Name: "SIMTEKPiping", FileName: "SIMTEKPiping.zip"},
	{Name: "USECASE", FileName: "USECASE.mo"},
}

// loadedUserModelLibraries 返回当前工作空间已加载的用户模型库名称集合。
func loadedUserModelLibraries(client *helpers.APIClient) (map[string]bool, error) {
	resp, err := client.GetUserPackage()
	if err != nil {
		return nil, fmt.Errorf("获取已加载用户模型库列表失败: %w", err)
	}
	if resp.Status != 0 {
		return nil, fmt.Errorf("获取已加载用户模型库列表返回非成功状态: status=%d msg=%s err=%s", resp.Status, resp.Msg, resp.Err)
	}

	result := make(map[string]bool)
	for _, item := range responseArray(resp.Data) {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if name, ok := itemMap["package_name"].(string); ok && name != "" {
			result[name] = true
		}
	}
	return result, nil
}

// packageFileIDsByName 返回当前用户空间中已上传模型文件的 package_id。
func packageFileIDsByName(client *helpers.APIClient) (map[string]string, error) {
	resp, err := client.GetPackageFileList()
	if err != nil {
		return nil, fmt.Errorf("获取模型文件列表失败: %w", err)
	}
	if resp.Status != 0 {
		return nil, fmt.Errorf("获取模型文件列表返回非成功状态: status=%d msg=%s err=%s", resp.Status, resp.Msg, resp.Err)
	}

	result := make(map[string]string)
	for _, item := range responseArray(resp.Data) {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		name, _ := itemMap["package_name"].(string)
		packageID, _ := itemMap["package_id"].(string)
		if name != "" && packageID != "" {
			result[name] = packageID
		}
	}
	return result, nil
}

// uploadRequiredModelLibrary 上传 TestMain 前置检查缺失的管网模型库文件。
func uploadRequiredModelLibrary(client *helpers.APIClient, library requiredModelLibrary) (string, error) {
	resourcePath := pipeNetResourcePath(library.FileName)
	content, err := os.ReadFile(resourcePath)
	if err != nil {
		return "", fmt.Errorf("读取管网模型库资源 %s 失败: %w", resourcePath, err)
	}

	resp, err := client.UploadModelPackage(library.FileName, content)
	if err != nil {
		return "", fmt.Errorf("上传管网模型库 %s 失败: %w", library.Name, err)
	}
	if resp.Status != 0 {
		packageIDs, lookupErr := packageFileIDsByName(client)
		if lookupErr == nil && packageIDs[library.Name] != "" {
			return packageIDs[library.Name], nil
		}
		return "", fmt.Errorf("上传管网模型库 %s 返回非成功状态: status=%d msg=%s err=%s", library.Name, resp.Status, resp.Msg, resp.Err)
	}

	packageIDs, err := packageFileIDsByName(client)
	if err != nil {
		return "", err
	}
	packageID := packageIDs[library.Name]
	if packageID == "" {
		return "", fmt.Errorf("上传管网模型库 %s 后未在模型文件列表中找到 package_id", library.Name)
	}
	return packageID, nil
}

// verifyRequiredPipeNetLibrariesLoaded 二次确认管网模型库均已加载。
func verifyRequiredPipeNetLibrariesLoaded(client *helpers.APIClient) error {
	loadedLibraries, err := loadedUserModelLibraries(client)
	if err != nil {
		return err
	}
	for _, library := range requiredPipeNetLibraries {
		if !loadedLibraries[library.Name] {
			return fmt.Errorf("管网模型库 %s 未加载，且自动加载后仍未出现在已加载列表中", library.Name)
		}
	}
	return nil
}

// ReadModelTestResource 读取 tests/resources 下的模型测试资源文件。
func ReadModelTestResource(pathParts ...string) ([]byte, error) {
	resourcePath := modelTestResourcePath(pathParts...)
	content, err := os.ReadFile(resourcePath)
	if err != nil {
		return nil, fmt.Errorf("读取模型测试资源 %s 失败: %w", resourcePath, err)
	}
	return content, nil
}

// modelTestResourcePath 返回 tests/resources 下模型测试资源文件的绝对路径。
func modelTestResourcePath(pathParts ...string) string {
	_, currentFile, _, ok := runtime.Caller(0)
	baseParts := []string{"tests", "resources"}
	if ok {
		baseParts = []string{filepath.Dir(currentFile), "..", "..", "resources"}
	}
	return filepath.Join(append(baseParts, pathParts...)...)
}

// pipeNetResourcePath 返回管网测试资源文件的绝对路径。
func pipeNetResourcePath(fileName string) string {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		return filepath.Join("tests", "resources", "pipenet", fileName)
	}
	return filepath.Join(filepath.Dir(currentFile), "..", "..", "resources", "pipenet", fileName)
}

// responseArray 将接口返回的数组 data 转成 []any；非数组时返回 nil。
func responseArray(data any) []any {
	items, ok := data.([]any)
	if !ok {
		return nil
	}
	return items
}
