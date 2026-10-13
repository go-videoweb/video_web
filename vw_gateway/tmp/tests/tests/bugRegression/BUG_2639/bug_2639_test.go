//go:build bug_regression_test

package bug2639

import (
	"fmt"
	"os"
	"testing"

	"yssim-go/tests/helpers"
	modelhelpers "yssim-go/tests/helpers/model"
)

const (
	bug2639ModelicaStdLibID       = "2942f1a3-c5b3-45cd-9cdb-c951784e01ce"
	bug2639SystemModelName        = "Modelica.Blocks.Examples.PID_Controller"
	bug2639ComponentName          = "PI"
	bug2639ComponentClassName     = "Modelica.Blocks.Continuous.PI"
	bug2639MovedComponentOrigin   = "{30,40}"
	bug2639MovedComponentExtent   = "{{-20,-20},{20,20}}"
	bug2639MovedComponentRotate   = "45"
	bug2639MovedComponentOriginIn = "30,40"
)

var testClient *helpers.APIClient

// TestMain 函数作用：初始化 BUG_2639 回归测试客户端并校验真实后端测试环境。
// 入参：m 是当前测试包的执行入口。
// 返回值：无直接返回值；函数通过 os.Exit 返回测试进程退出码。
// 示例：
//
//	go test -tags bug_regression_test ./tests/bugRegression/BUG_2639
func TestMain(m *testing.M) {
	suite := &helpers.Suite{
		Setup: func() error {
			if os.Getenv("TEST_SPACE_ID") == "" || os.Getenv("TEST_USERNAME") == "" {
				return fmt.Errorf("TEST_SPACE_ID / TEST_USERNAME 未设置")
			}
			testClient = helpers.NewAPIClientWithConfig(helpers.DefaultTestConfig())
			return nil
		},
		Teardown: func() {
			helpers.LogTeardownStep("BUG_2639 regression suite finished")
		},
	}
	os.Exit(suite.Run(m))
}

// TestBug2639CopiedSystemModelDiagramMoveDoesNotMutateSystemModel 验证复制系统库模型到我的模型后，移动复制品组件不会污染系统库原模型。
func TestBug2639CopiedSystemModelDiagramMoveDoesNotMutateSystemModel(t *testing.T) {
	assert := helpers.NewAssertResponse(t)
	copiedModelName := modelhelpers.UniqueModelName("Bug2639PIDController")

	var copiedPackageID string
	var actualCopiedModelName string

	helpers.RunWithHooks(t, helpers.TestHooks{
		After: func(t *testing.T) {
			modelhelpers.CleanupTempModelFixture(testClient, copiedPackageID, actualCopiedModelName)
		},
	}, func(t *testing.T) {
		// 复现步骤 1：加载系统库模型，并记录系统库中 PI 组件的初始画布信息。
		if err := modelhelpers.EnsureSystemModelLoaded(testClient, bug2639ModelicaStdLibID, "", "BUG_2639 加载 Modelica 系统库"); err != nil {
			t.Fatal(err)
		}
		systemBefore := bug2639ComponentInInstance(t, bug2639ModelicaStdLibID, bug2639SystemModelName, bug2639ComponentName, "BUG_2639 系统库修改前")

		// 复现步骤 2：将系统库 PID_Controller 复制到我的模型下。
		copyResp, err := testClient.CopyClass(bug2639ModelicaStdLibID, "", "", copiedModelName, bug2639SystemModelName)
		if err != nil {
			t.Fatalf("BUG_2639 复制系统库模型失败: %v", err)
		}
		assert.IsSuccess(copyResp, "BUG_2639 复制系统库模型到我的模型")
		copiedPackageID, actualCopiedModelName = bug2639CopiedModelIdentity(t, copyResp)
		if err := modelhelpers.EnsureModelLoaded(testClient, copiedPackageID, actualCopiedModelName, "BUG_2639 加载复制后的用户模型"); err != nil {
			t.Fatal(err)
		}

		// 复现步骤 3：在复制出来的用户模型画布上移动 PI 组件。
		updateResp, err := testClient.UpdateModelComponent(copiedPackageID, actualCopiedModelName, []map[string]any{{
			"component_name":       bug2639ComponentName,
			"component_class_name": bug2639ComponentClassName,
			"origin":               bug2639MovedComponentOriginIn,
			"extent":               []string{"-20,-20", "20,20"},
			"rotation":             bug2639MovedComponentRotate,
		}})
		if err != nil {
			t.Fatalf("BUG_2639 移动复制品组件失败: %v", err)
		}
		assert.IsSuccess(updateResp, "BUG_2639 移动复制品 PI 组件")
		copiedAfter := bug2639ComponentInInstance(t, copiedPackageID, actualCopiedModelName, bug2639ComponentName, "BUG_2639 复制品移动后")
		bug2639AssertStringField(t, copiedAfter, "origin", bug2639MovedComponentOrigin, "BUG_2639 复制品 PI origin")
		bug2639AssertStringField(t, copiedAfter, "extent", bug2639MovedComponentExtent, "BUG_2639 复制品 PI extent")
		bug2639AssertStringField(t, copiedAfter, "rotation", bug2639MovedComponentRotate, "BUG_2639 复制品 PI rotation")

		// 复现步骤 4：回查系统库同一模型的 PI 组件，确认系统库未被复制品编辑污染。
		systemAfter := bug2639ComponentInInstance(t, bug2639ModelicaStdLibID, bug2639SystemModelName, bug2639ComponentName, "BUG_2639 系统库修改后")
		bug2639AssertComponentFieldUnchanged(t, systemBefore, systemAfter, "origin")
		bug2639AssertComponentFieldUnchanged(t, systemBefore, systemAfter, "extent")
		bug2639AssertComponentFieldUnchanged(t, systemBefore, systemAfter, "rotation")
		if systemAfter["origin"] == bug2639MovedComponentOrigin && systemAfter["extent"] == bug2639MovedComponentExtent {
			t.Fatalf("BUG_2639 系统库 PI 组件被复制品移动污染: system_after=%v copied_after=%v", systemAfter, copiedAfter)
		}
	})
}

// bug2639CopiedModelIdentity 函数作用：从复制模型接口响应中解析新用户模型的 packageID 和 modelName。
// 入参：t 提供失败能力；resp 是 CopyClass 返回的统一响应。
// 返回值：返回复制品 packageID 和 modelName；字段缺失或类型错误时终止当前测试。
// 示例：
//
//	packageID, modelName := bug2639CopiedModelIdentity(t, copyResp)
func bug2639CopiedModelIdentity(t *testing.T, resp *helpers.Response) (string, string) {
	t.Helper()
	dataMap, err := helpers.ObjectData(resp, "BUG_2639 复制系统库模型响应")
	if err != nil {
		t.Fatal(err)
	}
	packageID, ok := dataMap["id"].(string)
	if !ok || packageID == "" {
		t.Fatalf("BUG_2639 复制响应 id 为空或类型错误: %v", dataMap["id"])
	}
	modelName, ok := dataMap["model_name"].(string)
	if !ok || modelName == "" {
		t.Fatalf("BUG_2639 复制响应 model_name 为空或类型错误: %v", dataMap["model_name"])
	}
	return packageID, modelName
}

// bug2639ComponentInInstance 函数作用：读取模型实例并查找指定组件图形数据。
// 入参：t 提供失败能力；packageID 和 modelName 定位模型；componentName 是待查找组件；message 是场景说明。
// 返回值：返回组件对象；未找到组件或响应结构异常时终止当前测试。
// 示例：
//
//	component := bug2639ComponentInInstance(t, packageID, modelName, "PI", "回查组件")
func bug2639ComponentInInstance(t *testing.T, packageID, modelName, componentName, message string) map[string]any {
	t.Helper()
	modelData := bug2639ModelInstanceData(t, packageID, modelName, message)
	components := bug2639ModelInstanceElements(t, modelData, message)
	for _, item := range components {
		if item["name"] == componentName {
			return item
		}
	}
	t.Fatalf("%s: 未找到组件 %q，全部组件=%v", message, componentName, components)
	return nil
}

// bug2639ModelInstanceData 函数作用：调用模型实例接口并解析 data.model。
// 入参：t 提供失败能力；packageID 和 modelName 定位模型；message 是场景说明。
// 返回值：返回 data.model 对象；接口失败或结构异常时终止当前测试。
// 示例：
//
//	modelData := bug2639ModelInstanceData(t, packageID, modelName, "读取模型实例")
func bug2639ModelInstanceData(t *testing.T, packageID, modelName, message string) map[string]any {
	t.Helper()
	resp, err := testClient.GetModelInstance(packageID, modelName, "")
	if err != nil {
		t.Fatalf("%s: 获取模型实例失败: %v", message, err)
	}
	if resp.Status != 0 {
		t.Fatalf("%s: 获取模型实例返回非成功状态: status=%d msg=%s err=%s", message, resp.Status, resp.Msg, resp.Err)
	}
	dataMap, err := helpers.ObjectData(resp, message)
	if err != nil {
		t.Fatal(err)
	}
	modelData, ok := dataMap["model"].(map[string]any)
	if !ok {
		t.Fatalf("%s: data.model 不是 object: %T", message, dataMap["model"])
	}
	return modelData
}

// bug2639ModelInstanceElements 函数作用：从模型实例 data.model 中提取画布组件列表。
// 入参：t 提供失败能力；modelData 是 data.model 对象；message 是场景说明。
// 返回值：返回组件对象数组；字段缺失或类型异常时终止当前测试。
// 示例：
//
//	components := bug2639ModelInstanceElements(t, modelData, "解析组件")
func bug2639ModelInstanceElements(t *testing.T, modelData map[string]any, message string) []map[string]any {
	t.Helper()
	graphics, ok := modelData["graphics"].(map[string]any)
	if !ok {
		t.Fatalf("%s: model.graphics 不是 object: %T", message, modelData["graphics"])
	}
	elements, ok := graphics["elements"].(map[string]any)
	if !ok {
		t.Fatalf("%s: model.graphics.elements 不是 object: %T", message, graphics["elements"])
	}
	list, ok := elements["model"].([]any)
	if !ok {
		t.Fatalf("%s: model.graphics.elements.model 不是数组: %T", message, elements["model"])
	}
	components := make([]map[string]any, 0, len(list))
	for _, item := range list {
		dataMap, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("%s: 组件项不是 object: %T", message, item)
		}
		components = append(components, dataMap)
	}
	return components
}

// bug2639AssertStringField 函数作用：断言对象中的字符串字段等于期望值。
// 入参：t 提供失败能力；dataMap 是待检查对象；field 是字段名；want 是期望值；message 是场景说明。
// 返回值：无；字段缺失、类型错误或值不一致时终止当前测试。
// 示例：
//
//	bug2639AssertStringField(t, component, "origin", "{30,40}", "组件位置")
func bug2639AssertStringField(t *testing.T, dataMap map[string]any, field, want, message string) {
	t.Helper()
	got, ok := dataMap[field].(string)
	if !ok {
		t.Fatalf("%s: 字段 %q 不是 string: %T", message, field, dataMap[field])
	}
	if got != want {
		t.Fatalf("%s: 字段 %q 期望=%q 实际=%q", message, field, want, got)
	}
}

// bug2639AssertComponentFieldUnchanged 函数作用：断言系统库组件在复制品编辑前后字段保持不变。
// 入参：t 提供失败能力；before 和 after 是编辑前后的系统库组件对象；field 是待比较字段。
// 返回值：无；字段值发生变化时终止当前测试。
// 示例：
//
//	bug2639AssertComponentFieldUnchanged(t, before, after, "origin")
func bug2639AssertComponentFieldUnchanged(t *testing.T, before, after map[string]any, field string) {
	t.Helper()
	if before[field] != after[field] {
		t.Fatalf("BUG_2639 系统库组件字段 %q 被修改: before=%v after=%v", field, before[field], after[field])
	}
}
