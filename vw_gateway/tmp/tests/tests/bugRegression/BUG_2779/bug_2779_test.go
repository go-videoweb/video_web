//go:build bug_regression_test

package bug2779

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"yssim-go/tests/helpers"
	modelhelpers "yssim-go/tests/helpers/model"
)

var testClient *helpers.APIClient

// TestMain 函数作用：初始化 BUG_2779 回归测试客户端并校验真实后端测试环境。
// 入参：m 是当前测试包的执行入口。
// 返回值：无直接返回值；函数通过 os.Exit 返回测试进程退出码。
// 示例：
//
//	go test -tags bug_regression_test ./tests/bugRegression/BUG_2779
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
			helpers.LogTeardownStep("BUG_2779 regression suite finished")
		},
	}
	os.Exit(suite.Run(m))
}

// TestBug2779SingleMoSubPackageMove 验证单 mo package 内子 package 跨父级拖拽后，模型树和后端单 mo 文件不会丢失。
func TestBug2779SingleMoSubPackageMove(t *testing.T) {
	assert := helpers.NewAssertResponse(t)
	var packageID string
	var packageName string

	helpers.RunWithHooks(t, helpers.TestHooks{
		Before: func(t *testing.T) {
			packageID, packageName = createBug2779SingleMoFixture(t)
		},
		After: func(t *testing.T) {
			modelhelpers.CleanupTempModelFixture(testClient, packageID, packageName)
		},
	}, func(t *testing.T) {
		sourceClassName := packageName + ".SourcePkg.ChildPkg"
		targetPackageName := packageName + ".TargetPkg"
		newClassName := targetPackageName + ".ChildPkg"

		rootPath, skipFileCheck := bug2779PackageRootPath(t, packageID)
		assertBug2779ModelChildren(t, packageID, packageName+".SourcePkg", "ChildPkg", true, "移动前源 package")
		assertBug2779ModelChildren(t, packageID, targetPackageName, "ChildPkg", false, "移动前目标 package")

		resp, err := testClient.MoveClassToPackage(packageID, packageID, sourceClassName, targetPackageName)
		if err != nil {
			t.Fatalf("移动子 package 请求失败: %v", err)
		}
		assert.IsSuccess(resp, "BUG_2779 子 package 移动")
		assertBug2779MoveResponse(t, resp, sourceClassName, newClassName, "BUG_2779 子 package 移动")

		assertBug2779ModelChildren(t, packageID, packageName+".SourcePkg", "ChildPkg", false, "移动后源 package")
		assertBug2779ModelChildren(t, packageID, targetPackageName, "ChildPkg", true, "移动后目标 package")
		assertBug2779ModelChildren(t, packageID, newClassName, "InnerModel", true, "移动后子 package 内容")
		if !skipFileCheck {
			assertBug2779SingleMoFile(t, rootPath, packageName)
		}
	})
}

// createBug2779SingleMoFixture 函数作用：上传一个包含 SourcePkg 与 TargetPkg 的单 mo package。
// 入参：t 提供测试失败与日志能力。
// 返回值：返回 packageID 和 packageName；失败时直接终止当前测试。
// 示例：
//
//	packageID, packageName := createBug2779SingleMoFixture(t)
func createBug2779SingleMoFixture(t *testing.T) (string, string) {
	t.Helper()
	packageName := modelhelpers.UniqueModelName("Bug2779SingleMo")
	resp, err := testClient.UploadModelPackage(packageName+".mo", []byte(bug2779SingleMoSource(packageName)))
	if err != nil {
		t.Fatalf("上传 BUG_2779 单 mo package 失败: %v", err)
	}
	if resp.Status != 0 {
		t.Fatalf("上传 BUG_2779 单 mo package 返回非成功状态: status=%d msg=%s err=%s", resp.Status, resp.Msg, resp.Err)
	}

	packageID := bug2779PackageIDByName(t, packageName)
	if err := modelhelpers.EnsureModelLoaded(testClient, packageID, packageName, "BUG_2779 加载单 mo package"); err != nil {
		t.Fatal(err)
	}
	return packageID, packageName
}

// bug2779SingleMoSource 函数作用：生成 BUG_2779 复现用的单文件 Modelica package 源码。
// 入参：packageName 是唯一顶层 package 名。
// 返回值：返回可上传的 Modelica 源码字符串。
// 示例：
//
//	source := bug2779SingleMoSource("Bug2779SingleMo_abcd1234")
func bug2779SingleMoSource(packageName string) string {
	return fmt.Sprintf(`package %s
  package SourcePkg
    package ChildPkg
      model InnerModel
        Real x;
      equation
        x = time;
      end InnerModel;
    end ChildPkg;
  end SourcePkg;

  package TargetPkg
    model Marker
      Real y;
    equation
      y = time;
    end Marker;
  end TargetPkg;
end %s;
`, packageName, packageName)
}

// bug2779PackageIDByName 函数作用：从当前用户空间模型文件列表中查找指定 package 的 ID。
// 入参：t 提供测试失败能力；packageName 是待查找的模型包名称。
// 返回值：返回匹配到的 package_id；未找到时终止当前测试。
// 示例：
//
//	packageID := bug2779PackageIDByName(t, packageName)
func bug2779PackageIDByName(t *testing.T, packageName string) string {
	t.Helper()
	resp, err := testClient.GetPackageFileList()
	if err != nil {
		t.Fatalf("获取模型文件列表失败: %v", err)
	}
	if resp.Status != 0 {
		t.Fatalf("获取模型文件列表返回非成功状态: status=%d msg=%s err=%s", resp.Status, resp.Msg, resp.Err)
	}
	items, err := helpers.ArrayData(resp, "BUG_2779 获取模型文件列表")
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range items {
		data, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if data["package_name"] == packageName {
			packageID, _ := data["package_id"].(string)
			if packageID != "" {
				return packageID
			}
		}
	}
	t.Fatalf("未在模型文件列表中找到 package_name=%q", packageName)
	return ""
}

// bug2779PackageRootPath 函数作用：获取后端模型文件根目录，并按环境设置决定是否跳过文件断言。
// 入参：t 提供测试失败能力；packageID 是待查询模型包 ID。
// 返回值：返回根目录路径和是否跳过文件断言。
// 示例：
//
//	rootPath, skip := bug2779PackageRootPath(t, packageID)
func bug2779PackageRootPath(t *testing.T, packageID string) (string, bool) {
	t.Helper()
	skipReason, skipFileCheck := modelhelpers.FileExpectationSkipReason()
	if skipFileCheck {
		t.Logf("BUG_2779: 跳过远端文件校验: %s", skipReason)
		return "", true
	}
	rootPath, err := modelhelpers.ModelPackageRootPath(packageID)
	if err != nil {
		t.Fatal(err)
	}
	return rootPath, false
}

// assertBug2779MoveResponse 函数作用：校验拖拽移动接口返回的旧类名和新类名。
// 入参：t 提供断言能力；resp 是接口响应；oldClassName 与 newClassName 是期望回显；message 是场景说明。
// 返回值：无；不匹配时终止或标记当前测试失败。
// 示例：
//
//	assertBug2779MoveResponse(t, resp, oldName, newName, "移动子 package")
func assertBug2779MoveResponse(t *testing.T, resp *helpers.Response, oldClassName, newClassName, message string) {
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

// assertBug2779ModelChildren 函数作用：断言指定父模型的子节点是否包含目标短名称。
// 入参：t 提供断言能力；packageID 和 parentModelName 定位父节点；childName 是短名；wantPresent 表示期望存在性；message 是场景说明。
// 返回值：无；不满足期望时终止当前测试。
// 示例：
//
//	assertBug2779ModelChildren(t, packageID, parentName, "ChildPkg", true, "移动后目标 package")
func assertBug2779ModelChildren(t *testing.T, packageID, parentModelName, childName string, wantPresent bool, message string) {
	t.Helper()
	names := bug2779ModelChildShortNames(t, packageID, parentModelName, message)
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

// bug2779ModelChildShortNames 函数作用：读取父模型子节点并提取短名称列表。
// 入参：t 提供失败能力；packageID 和 parentModelName 定位父节点；message 是场景说明。
// 返回值：返回过滤 Resources 后的子节点短名称列表。
// 示例：
//
//	names := bug2779ModelChildShortNames(t, packageID, packageName, "回查模型树")
func bug2779ModelChildShortNames(t *testing.T, packageID, parentModelName, message string) []string {
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
		name := bug2779NodeShortName(t, node, message)
		nodeType, _ := node["type"].(string)
		if name == "Resources" && nodeType == "static" {
			continue
		}
		names = append(names, name)
	}
	return names
}

// bug2779NodeShortName 函数作用：从模型树节点中解析可比较的短名称。
// 入参：t 提供失败能力；node 是接口返回的模型树节点；message 是场景说明。
// 返回值：返回模型节点短名称。
// 示例：
//
//	shortName := bug2779NodeShortName(t, node, "回查模型树")
func bug2779NodeShortName(t *testing.T, node map[string]any, message string) string {
	t.Helper()
	for _, field := range []string{"name", "model_name", "class_name", "full_name"} {
		value, _ := node[field].(string)
		if strings.TrimSpace(value) != "" {
			return bug2779ShortModelName(value)
		}
	}
	t.Fatalf("%s: 子节点缺少可识别名称字段: %v", message, node)
	return ""
}

// bug2779ShortModelName 函数作用：提取完整 Modelica 名称的最后一段。
// 入参：modelName 是完整或短模型名。
// 返回值：返回最后一个点号后的短名称。
// 示例：
//
//	name := bug2779ShortModelName("A.B.C")
func bug2779ShortModelName(modelName string) string {
	modelName = strings.TrimSpace(modelName)
	if idx := strings.LastIndex(modelName, "."); idx >= 0 {
		return modelName[idx+1:]
	}
	return modelName
}

// assertBug2779SingleMoFile 函数作用：断言移动后单 mo package 文件仍存在且包含移动后的子包结构。
// 入参：t 提供断言能力；rootPath 是模型文件根目录；packageName 是顶层 package 名。
// 返回值：无；文件状态不满足时终止当前测试。
// 示例：
//
//	assertBug2779SingleMoFile(t, rootPath, packageName)
func assertBug2779SingleMoFile(t *testing.T, rootPath, packageName string) {
	t.Helper()
	path := filepath.Join(rootPath, packageName+".mo")
	err := modelhelpers.WaitForFileExpectations([]modelhelpers.FileExpectation{{
		Path:        path,
		ShouldExist: true,
		Contains:    []string{"package TargetPkg", "package ChildPkg", "model InnerModel", "end " + packageName + ";"},
	}}, modelhelpers.FileWaitOptions{Timeout: time.Minute, InitialDelay: 200 * time.Millisecond, MaxDelay: 5 * time.Second})
	if err != nil {
		t.Fatalf("BUG_2779 单 mo 文件状态不符合预期: %v", err)
	}
}
