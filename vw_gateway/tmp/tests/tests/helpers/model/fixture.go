package model

import (
	"fmt"

	"github.com/google/uuid"
	"yssim-go/tests/helpers"
)

// CreateTempModelFixture 从指定源模型复制一个临时模型；复制失败时退回到新建模型再写入源码。
func CreateTempModelFixture(client *helpers.APIClient, sourcePackageID, sourceModelName string) (string, string, error) {
	newModelName := UniqueModelName("TestModel")
	resp, err := client.CopyClass(sourcePackageID, "", "", newModelName, sourceModelName)
	if err == nil && resp != nil && resp.Status == 0 {
		dataMap, ok := resp.Data.(map[string]any)
		if !ok {
			return "", "", fmt.Errorf("复制临时模型返回的 data 不是 object: %T", resp.Data)
		}
		packageID, ok := dataMap["id"].(string)
		if !ok || packageID == "" {
			return "", "", fmt.Errorf("复制临时模型返回的 id 为空或类型错误: %v", dataMap["id"])
		}
		modelName, ok := dataMap["model_name"].(string)
		if !ok || modelName == "" {
			return "", "", fmt.Errorf("复制临时模型返回的 model_name 为空或类型错误: %v", dataMap["model_name"])
		}
		return packageID, modelName, nil
	}

	fallbackModelName := UniqueModelName("TestModel")
	createResp, createErr := client.CreateModelPackage(fallbackModelName, "model", map[string]any{
		"expand":       "",
		"insert_to":    "",
		"partial":      false,
		"encapsulated": false,
		"state":        false,
	})
	if createErr != nil {
		return "", "", fmt.Errorf("创建临时模型失败（copy fallback）: %w; copyErr=%v", createErr, err)
	}
	if createResp.Status != 0 {
		return "", "", fmt.Errorf("创建临时模型返回非成功状态（copy fallback）: status=%d, msg=%s, err=%s; copyStatus=%v copyMsg=%v copyErr=%v",
			createResp.Status, createResp.Msg, createResp.Err, responseStatus(resp), responseMsg(resp), err)
	}

	dataMap, ok := createResp.Data.(map[string]any)
	if !ok {
		return "", "", fmt.Errorf("创建临时模型返回的 data 不是 object: %T", createResp.Data)
	}
	packageID, ok := dataMap["id"].(string)
	if !ok || packageID == "" {
		return "", "", fmt.Errorf("创建临时模型返回的 id 为空或类型错误: %v", dataMap["id"])
	}
	modelName, ok := dataMap["model_name"].(string)
	if !ok || modelName == "" {
		return "", "", fmt.Errorf("创建临时模型返回的 model_name 为空或类型错误: %v", dataMap["model_name"])
	}

	updateResp, updateErr := client.UpdateModelPackage(packageID, modelName, DefaultModelSource(modelName))
	if updateErr != nil {
		CleanupTempModelFixture(client, packageID, modelName)
		return "", "", fmt.Errorf("写入临时模型源码失败（copy fallback）: %w", updateErr)
	}
	if updateResp.Status != 0 {
		CleanupTempModelFixture(client, packageID, modelName)
		return "", "", fmt.Errorf("写入临时模型源码返回非成功状态（copy fallback）: status=%d, msg=%s, err=%s", updateResp.Status, updateResp.Msg, updateResp.Err)
	}
	return packageID, modelName, nil
}

// ImportModelLibraryFixture 上传并加载指定测试模型库，上传前会清理同名残留模型库。
func ImportModelLibraryFixture(client *helpers.APIClient, libraryName, fileName string, content []byte) (string, string, error) {
	if libraryName == "" {
		return "", "", fmt.Errorf("libraryName 不能为空")
	}
	if fileName == "" {
		return "", "", fmt.Errorf("fileName 不能为空")
	}
	if len(content) == 0 {
		return "", "", fmt.Errorf("模型库资源 %s 内容为空", fileName)
	}

	packageIDs, err := packageFileIDsByName(client)
	if err != nil {
		return "", "", err
	}
	if stalePackageID := packageIDs[libraryName]; stalePackageID != "" {
		CleanupTempModelFixture(client, stalePackageID, libraryName)
	}

	resp, err := client.UploadModelPackage(fileName, content)
	if err != nil {
		return "", "", fmt.Errorf("上传测试模型库 %s 失败: %w", libraryName, err)
	}
	if resp.Status != 0 {
		return "", "", fmt.Errorf("上传测试模型库 %s 返回非成功状态: status=%d, msg=%s, err=%s", libraryName, resp.Status, resp.Msg, resp.Err)
	}

	packageIDs, err = packageFileIDsByName(client)
	if err != nil {
		return "", "", err
	}
	packageID := packageIDs[libraryName]
	if packageID == "" {
		return "", "", fmt.Errorf("上传测试模型库 %s 后未在模型文件列表中找到 package_id", libraryName)
	}
	if err := EnsureModelLoaded(client, packageID, libraryName, "加载测试模型库 "+libraryName); err != nil {
		CleanupTempModelFixture(client, packageID, libraryName)
		return "", "", err
	}
	return packageID, libraryName, nil
}

// CreateTempPackageFixture 创建一个临时 package 类型模型，适合资源目录等需要包容器的测试。
func CreateTempPackageFixture(client *helpers.APIClient, namePrefix string) (string, string, error) {
	if namePrefix == "" {
		namePrefix = "TestPackage"
	}

	packageName := UniqueModelName(namePrefix)
	resp, err := client.CreateModelPackage(packageName, "package", map[string]any{
		"expand":       "",
		"insert_to":    "",
		"partial":      false,
		"encapsulated": false,
		"state":        false,
	})
	if err != nil {
		return "", "", fmt.Errorf("创建临时 package 失败: %w", err)
	}
	if resp.Status != 0 {
		return "", "", fmt.Errorf("创建临时 package 返回非成功状态: status=%d, msg=%s, err=%s", resp.Status, resp.Msg, resp.Err)
	}

	dataMap, ok := resp.Data.(map[string]any)
	if !ok {
		return "", "", fmt.Errorf("创建临时 package 返回的 data 不是 object: %T", resp.Data)
	}
	packageID, ok := dataMap["id"].(string)
	if !ok || packageID == "" {
		return "", "", fmt.Errorf("创建临时 package 返回的 id 为空或类型错误: %v", dataMap["id"])
	}
	modelName, ok := dataMap["model_name"].(string)
	if !ok || modelName == "" {
		return "", "", fmt.Errorf("创建临时 package 返回的 model_name 为空或类型错误: %v", dataMap["model_name"])
	}
	return packageID, modelName, nil
}

// CleanupTempModelFixture 删除测试创建的临时模型；空 ID 或空模型名会被忽略。
func CleanupTempModelFixture(client *helpers.APIClient, packageID, modelName string) {
	if packageID == "" || modelName == "" {
		return
	}
	// 清理阶段只需要尽力删除模型；如果模型加载失败，后续删除接口也可能依旧能清理残留数据。
	_ = EnsureModelLoaded(client, packageID, modelName, "清理临时模型夹具")
	_, _ = client.DeletePackage(packageID, "", modelName)
}

// TryUnloadPackage 尝试卸载模型包；清理路径使用，失败不影响主测试断言。
func TryUnloadPackage(client *helpers.APIClient, packageID string) {
	if packageID == "" {
		return
	}
	_, _ = client.UnloadPackageByID(packageID)
}

// EnsureModelLoaded 确保用户模型已加载到工作空间。
func EnsureModelLoaded(client *helpers.APIClient, packageID, modelName, message string) error {
	resp, err := client.LoadUserspaceModel(packageID, modelName)
	if err != nil {
		return fmt.Errorf("%s: 加载用户模型失败: %w", message, err)
	}
	if resp.Status != 0 {
		return fmt.Errorf("%s: 加载用户模型返回非成功状态: status=%d, msg=%s, err=%s", message, resp.Status, resp.Msg, resp.Err)
	}
	return nil
}

// EnsureSystemModelLoaded 确保系统库模型已加载到工作空间。
func EnsureSystemModelLoaded(client *helpers.APIClient, packageID, modelName, message string) error {
	resp, err := client.LoadModel(packageID, modelName)
	if err != nil {
		return fmt.Errorf("%s: 加载系统库失败: %w", message, err)
	}
	if resp.Status != 0 {
		return fmt.Errorf("%s: 加载系统库返回非成功状态: status=%d, msg=%s, err=%s", message, resp.Status, resp.Msg, resp.Err)
	}
	return nil
}

// UniqueModelName 为临时模型生成唯一名称，避免测试环境残留数据导致名称冲突。
func UniqueModelName(prefix string) string {
	return fmt.Sprintf("%s_%s", prefix, uuid.NewString()[:8])
}

// DefaultModelSource 返回带输入、输出、PI 组件和基础图标的测试模型源码。
func DefaultModelSource(modelName string) string {
	return fmt.Sprintf(`model %s
  Modelica.Blocks.Interfaces.RealInput u annotation(Placement(transformation(extent={{-120,-20},{-80,20}}), iconTransformation(extent={{-120,-20},{-80,20}})));
  Modelica.Blocks.Interfaces.RealOutput y annotation(Placement(transformation(extent={{80,-10},{100,10}}), iconTransformation(extent={{80,-10},{100,10}})));
  Modelica.Blocks.Continuous.PI PI(k=1, Ti=0.5) annotation(Placement(transformation(extent={{-20,-20},{20,20}})));
equation
  connect(u, PI.u) annotation(Line(points={{-100,0},{-20,0}}, color={0,0,127}));
  connect(PI.y, y) annotation(Line(points={{20,0},{90,0}}, color={0,0,127}));
annotation(
  Icon(graphics={
    Rectangle(extent={{-100,-100},{100,100}}, lineColor={0,0,255}),
    Text(extent={{-100,60},{100,100}}, textString="%%name")
  })
);
end %s;`, modelName, modelName)
}

func responseStatus(resp *helpers.Response) any {
	if resp == nil {
		return nil
	}
	return resp.Status
}

func responseMsg(resp *helpers.Response) any {
	if resp == nil {
		return nil
	}
	return resp.Msg
}
