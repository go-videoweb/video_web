//go:build bug_regression_test

package bug2750

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"yssim-go/tests/helpers"
	modelhelpers "yssim-go/tests/helpers/model"
)

var testClient *helpers.APIClient

// TestMain 函数作用：初始化 BUG_2750 回归测试客户端并校验真实后端测试环境。
// 入参：m 是当前测试包的执行入口。
// 返回值：无直接返回值；函数通过 os.Exit 返回测试进程退出码。
// 示例：
//
//	go test -tags bug_regression_test ./tests/bugRegression/BUG_2750
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
			helpers.LogTeardownStep("BUG_2750 regression suite finished")
		},
	}
	os.Exit(suite.Run(m))
}

// TestBug2750TopLevelMultiMoMoveReload 验证多 mo 顶层 package 拖入 p1 后，卸载再加载仍可见且文件落入 p1 目录。
func TestBug2750TopLevelMultiMoMoveReload(t *testing.T) {
	assert := helpers.NewAssertResponse(t)
	var parentPackageID string
	var parentPackageName string
	var braytonPackageID string

	helpers.RunWithHooks(t, helpers.TestHooks{
		Before: func(t *testing.T) {
			parentPackageID, parentPackageName = createBug2750ParentPackage(t)
			braytonPackageID = createBug2750BraytonFixture(t)
		},
		After: func(t *testing.T) {
			modelhelpers.CleanupTempModelFixture(testClient, braytonPackageID, "Brayton")
			modelhelpers.CleanupTempModelFixture(testClient, parentPackageID, parentPackageName)
		},
	}, func(t *testing.T) {
		rootPath, skipFileCheck := bug2750PackageRootPath(t, parentPackageID)

		resp, err := testClient.MoveClassToPackage(braytonPackageID, parentPackageID, "Brayton", parentPackageName)
		if err != nil {
			t.Fatalf("移动 Brayton 到 %s 失败: %v", parentPackageName, err)
		}
		assert.IsSuccess(resp, "BUG_2750 顶层多 mo package 移动")
		assertBug2750MoveResponse(t, resp, "Brayton", parentPackageName+".Brayton", "BUG_2750 顶层多 mo package 移动")
		assertBug2750ModelChildren(t, parentPackageID, parentPackageName, "Brayton", true, "移动后 p1 子节点")

		modelhelpers.TryUnloadPackage(testClient, parentPackageID)
		if err := modelhelpers.EnsureModelLoaded(testClient, parentPackageID, parentPackageName, "BUG_2750 重新加载 p1"); err != nil {
			t.Fatal(err)
		}

		assertBug2750ModelChildren(t, parentPackageID, parentPackageName, "Brayton", true, "重新加载后 p1 子节点")
		assertBug2750ModelChildren(t, parentPackageID, parentPackageName+".Brayton", "Machines", true, "重新加载后 Brayton 内容")
		if !skipFileCheck {
			assertBug2750MovedFiles(t, rootPath)
		}
	})
}

// createBug2750ParentPackage 函数作用：创建 BUG_2750 复现步骤中的顶层 p1 package。
// 入参：t 提供测试失败与日志能力。
// 返回值：返回 p1 的 packageID 和唯一 packageName。
// 示例：
//
//	packageID, packageName := createBug2750ParentPackage(t)
func createBug2750ParentPackage(t *testing.T) (string, string) {
	t.Helper()
	packageID, packageName, err := modelhelpers.CreateTempPackageFixture(testClient, "Bug2750P1")
	if err != nil {
		t.Fatal(err)
	}
	if err := modelhelpers.EnsureModelLoaded(testClient, packageID, packageName, "BUG_2750 加载 p1"); err != nil {
		t.Fatal(err)
	}
	return packageID, packageName
}

// createBug2750BraytonFixture 函数作用：上传并加载禅道附件 Brayton.zip。
// 入参：t 提供测试失败能力。
// 返回值：返回 Brayton 顶层模型库的 packageID。
// 示例：
//
//	braytonPackageID := createBug2750BraytonFixture(t)
func createBug2750BraytonFixture(t *testing.T) string {
	t.Helper()
	content, err := os.ReadFile(bug2750ResourcePath(t, "Brayton.zip"))
	if err != nil {
		t.Fatalf("读取 BUG_2750 Brayton.zip 失败: %v", err)
	}
	packageID, _, err := modelhelpers.ImportModelLibraryFixture(testClient, "Brayton", "Brayton.zip", content)
	if err != nil {
		t.Fatal(err)
	}
	assertBug2750ModelChildren(t, packageID, "Brayton", "Machines", true, "Brayton 上传加载后内容")
	return packageID
}

// bug2750ResourcePath 函数作用：定位 BUG_2750 专属资源文件。
// 入参：t 提供失败能力；fileName 是 resources 目录下的文件名。
// 返回值：返回资源文件绝对路径。
// 示例：
//
//	path := bug2750ResourcePath(t, "Brayton.zip")
func bug2750ResourcePath(t *testing.T, fileName string) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("获取当前测试文件路径失败")
	}
	return filepath.Join(filepath.Dir(currentFile), "resources", fileName)
}

// bug2750PackageRootPath 函数作用：获取 p1 的后端文件根目录，并按环境设置决定是否跳过文件断言。
// 入参：t 提供测试失败能力；packageID 是 p1 的 package ID。
// 返回值：返回根目录路径和是否跳过文件断言。
// 示例：
//
//	rootPath, skip := bug2750PackageRootPath(t, packageID)
func bug2750PackageRootPath(t *testing.T, packageID string) (string, bool) {
	t.Helper()
	skipReason, skipFileCheck := modelhelpers.FileExpectationSkipReason()
	if skipFileCheck {
		t.Logf("BUG_2750: 跳过远端文件校验: %s", skipReason)
		return "", true
	}
	rootPath, err := modelhelpers.ModelPackageRootPath(packageID)
	if err != nil {
		t.Fatal(err)
	}
	return rootPath, false
}

// assertBug2750MoveResponse 函数作用：校验顶层模型库移动接口返回的旧类名和新类名。
// 入参：t 提供断言能力；resp 是接口响应；oldClassName 与 newClassName 是期望回显；message 是场景说明。
// 返回值：无；不匹配时终止当前测试。
// 示例：
//
//	assertBug2750MoveResponse(t, resp, "Brayton", parentName+".Brayton", "移动 Brayton")
func assertBug2750MoveResponse(t *testing.T, resp *helpers.Response, oldClassName, newClassName, message string) {
	t.Helper()
	dataMap, err := helpers.ObjectData(resp, message)
	if err != nil {
		t.Fatal(err)
	}
	if dataMap["old_class_name"] != oldClassName {
		t.Fatalf("%s: old_class_name 期望=%q，实际=%v", message, oldClassName, dataMap["old_class_name"])
	}
	if dataMap["new_class_name"] != newClassName {
		t.Fatalf("%s: new_class_name 期望=%q，实际=%v", message, newClassName, dataMap["new_class_name"])
	}
}

// assertBug2750ModelChildren 函数作用：断言指定父模型的子节点是否包含目标短名称。
// 入参：t 提供断言能力；packageID 和 parentModelName 定位父节点；childName 是短名；wantPresent 表示期望存在性；message 是场景说明。
// 返回值：无；不满足期望时终止当前测试。
// 示例：
//
//	assertBug2750ModelChildren(t, packageID, parentName, "Brayton", true, "p1 子节点")
func assertBug2750ModelChildren(t *testing.T, packageID, parentModelName, childName string, wantPresent bool, message string) {
	t.Helper()
	names := bug2750ModelChildShortNames(t, packageID, parentModelName, message)
	found := false
	for _, name := range names {
		if name == childName {
			found = true
			break
		}
	}
	if found != wantPresent {
		t.Fatalf("%s: 子节点 %q 存在性期望=%v，实际=%v，全部子节点=%v", message, childName, wantPresent, found, names)
	}
}

// bug2750ModelChildShortNames 函数作用：读取父模型子节点并提取短名称列表。
// 入参：t 提供失败能力；packageID 和 parentModelName 定位父节点；message 是场景说明。
// 返回值：返回过滤 Resources 后的子节点短名称列表。
// 示例：
//
//	names := bug2750ModelChildShortNames(t, packageID, parentName, "回查模型树")
func bug2750ModelChildShortNames(t *testing.T, packageID, parentModelName, message string) []string {
	t.Helper()
	resp, err := testClient.GetListModel(packageID, parentModelName)
	if err != nil {
		t.Fatalf("%s: 回查模型子节点失败: %v", message, err)
	}
	if resp.Status != 0 {
		t.Fatalf("%s: 回查模型子节点返回非成功状态: status=%d msg=%s err=%s", message, resp.Status, resp.Msg, resp.Err)
	}
	if resp.Data == nil {
		return []string{}
	}
	items, err := helpers.ArrayData(resp, message)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		node, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("%s: 子节点不是 object: %T", message, item)
		}
		name := bug2750NodeShortName(t, node, message)
		nodeType, _ := node["type"].(string)
		if name == "Resources" && nodeType == "static" {
			continue
		}
		names = append(names, name)
	}
	return names
}

// bug2750NodeShortName 函数作用：从模型树节点中解析可比较的短名称。
// 入参：t 提供失败能力；node 是接口返回的模型树节点；message 是场景说明。
// 返回值：返回模型节点短名称。
// 示例：
//
//	shortName := bug2750NodeShortName(t, node, "回查模型树")
func bug2750NodeShortName(t *testing.T, node map[string]any, message string) string {
	t.Helper()
	for _, field := range []string{"name", "model_name", "class_name", "full_name"} {
		value, _ := node[field].(string)
		if strings.TrimSpace(value) != "" {
			return bug2750ShortModelName(value)
		}
	}
	t.Fatalf("%s: 子节点缺少可识别名称字段: %v", message, node)
	return ""
}

// bug2750ShortModelName 函数作用：提取完整 Modelica 名称的最后一段。
// 入参：modelName 是完整或短模型名。
// 返回值：返回最后一个点号后的短名称。
// 示例：
//
//	name := bug2750ShortModelName("A.B.C")
func bug2750ShortModelName(modelName string) string {
	modelName = strings.TrimSpace(modelName)
	if idx := strings.LastIndex(modelName, "."); idx >= 0 {
		return modelName[idx+1:]
	}
	return modelName
}

// assertBug2750MovedFiles 函数作用：断言 Brayton 多 mo 文件已移动到 p1 目录下。
// 入参：t 提供断言能力；rootPath 是 p1 模型文件根目录。
// 返回值：无；文件状态不满足时终止当前测试。
// 示例：
//
//	assertBug2750MovedFiles(t, rootPath)
func assertBug2750MovedFiles(t *testing.T, rootPath string) {
	t.Helper()
	expectations := []modelhelpers.FileExpectation{
		{Path: filepath.Join(rootPath, "Brayton", "package.mo"), ShouldExist: true, Contains: []string{"package Brayton"}},
		{Path: filepath.Join(rootPath, "Brayton", "Machines", "Compressor.mo"), ShouldExist: true, Contains: []string{"model Compressor"}},
	}
	if err := modelhelpers.WaitForFileExpectations(expectations, modelhelpers.FileWaitOptions{Timeout: time.Minute, InitialDelay: 200 * time.Millisecond, MaxDelay: 5 * time.Second}); err != nil {
		t.Fatalf("BUG_2750 移动后文件状态不符合预期: %v", err)
	}
}
