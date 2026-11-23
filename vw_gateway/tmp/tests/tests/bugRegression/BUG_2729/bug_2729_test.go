//go:build bug_regression_test

package bug2729

import (
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"yssim-go/tests/helpers"
	modelhelpers "yssim-go/tests/helpers/model"
	simulationhelpers "yssim-go/tests/helpers/simulation"
)

var testClient *helpers.APIClient

// TestMain 函数作用：初始化 BUG_2729 回归测试客户端并校验真实后端测试环境。
// 入参：m 是当前测试包的执行入口。
// 返回值：无直接返回值；函数通过 os.Exit 返回测试进程退出码。
// 示例：
//
//	go test -tags bug_regression_test ./tests/bugRegression/BUG_2729
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
			helpers.LogTeardownStep("BUG_2729 regression suite finished")
		},
	}
	os.Exit(suite.Run(m))
}

// TestBug2729RenamedModelKeepsSimulationResult 验证模型已有仿真结果后重命名，历史仿真记录和结果树仍可查看。
func TestBug2729RenamedModelKeepsSimulationResult(t *testing.T) {
	assert := helpers.NewAssertResponse(t)
	originalPackageName := modelhelpers.UniqueModelName("Bug2729Model")
	renamedPackageName := originalPackageName + "Renamed"
	originalModelName := originalPackageName + ".SimpleCar"
	renamedModelName := renamedPackageName + ".SimpleCar"

	var packageID string
	var cleanupName string
	var experimentID string

	helpers.RunWithHooks(t, helpers.TestHooks{
		After: func(t *testing.T) {
			simulationhelpers.DeleteExperiment(testClient, experimentID)
			modelhelpers.CleanupTempModelFixture(testClient, packageID, cleanupName)
		},
	}, func(t *testing.T) {
		// 准备可仿真的临时模型 A，并记录清理名称。
		packageID = createBug2729SimulationModel(t, originalPackageName)
		cleanupName = originalPackageName

		// 对模型 A 执行一次真实仿真，生成原 Bug 中无法查看的历史结果。
		experimentID = createAndRunBug2729Simulation(t, packageID, originalModelName, "bug2729_before_rename")
		assertBug2729SimulationRecordAvailable(t, packageID, originalModelName, experimentID)
		assertBug2729SimulationTreeAvailable(t, experimentID)

		// 将模型 A 重命名为模型 B，触发 BUG_2729 的关键路径。
		renameResp, err := testClient.RenameModel(packageID, originalPackageName, renamedPackageName)
		if err != nil {
			t.Fatalf("BUG_2729 重命名模型失败: %v", err)
		}
		assert.IsSuccess(renameResp, "BUG_2729 重命名已有仿真结果的模型")
		cleanupName = renamedPackageName
		if err := modelhelpers.EnsureModelLoaded(testClient, packageID, renamedModelName, "BUG_2729 加载重命名后的模型"); err != nil {
			t.Fatal(err)
		}

		// 验证重命名后的模型 B 仍能列出并打开原模型 A 生成的仿真结果。
		assertBug2729SimulationRecordAvailable(t, packageID, renamedModelName, experimentID)
		assertBug2729SimulationTreeAvailable(t, experimentID)
	})
}

// createBug2729SimulationModel 函数作用：上传并加载用于 BUG_2729 复现的可仿真模型包。
// 入参：t 提供测试失败能力；packageName 是唯一顶层 package 名。
// 返回值：返回上传后模型包的 packageID。
// 示例：
//
//	packageID := createBug2729SimulationModel(t, "Bug2729Model_abcd1234")
func createBug2729SimulationModel(t *testing.T, packageName string) string {
	t.Helper()
	content := bug2729ModelSource(t, packageName)
	packageID, _, err := modelhelpers.ImportModelLibraryFixture(testClient, packageName, packageName+".mo", []byte(content))
	if err != nil {
		t.Fatal(err)
	}
	modelName := packageName + ".SimpleCar"
	if err := modelhelpers.EnsureModelLoaded(testClient, packageID, modelName, "BUG_2729 加载仿真模型"); err != nil {
		t.Fatal(err)
	}
	return packageID
}

// bug2729ModelSource 函数作用：读取固定参数标定模型并替换为当前测试唯一 package 名。
// 入参：t 提供失败能力；packageName 是目标顶层 package 名。
// 返回值：返回替换后的 Modelica 源码。
// 示例：
//
//	source := bug2729ModelSource(t, "Bug2729Model_abcd1234")
func bug2729ModelSource(t *testing.T, packageName string) string {
	t.Helper()
	contentBytes, err := modelhelpers.ReadModelTestResource("ParameterCalibration", "ParameterCalibration.mo")
	if err != nil {
		t.Fatal(err)
	}
	content := string(contentBytes)
	content = strings.Replace(content, "package ParameterCalibration ", "package "+packageName+" ", 1)
	content = strings.Replace(content, "end ParameterCalibration;", "end "+packageName+";", 1)
	return content
}

// createAndRunBug2729Simulation 函数作用：创建并完成一次仿真，生成重命名前的历史结果记录。
// 入参：t 提供失败能力；packageID 和 modelName 定位模型；namePrefix 用于生成唯一实验名。
// 返回值：返回已完成仿真实验 ID。
// 示例：
//
//	experimentID := createAndRunBug2729Simulation(t, packageID, modelName, "bug2729")
func createAndRunBug2729Simulation(t *testing.T, packageID, modelName, namePrefix string) string {
	t.Helper()
	simulateVarData := simulationhelpers.DefaultSimulateVarData()
	simulateVarData["stopTime"] = "0.2"
	simulateVarData["numberOfIntervals"] = "40"
	simulateVarData["interval"] = "0.005"

	experimentID, _, err := simulationhelpers.CreateExperiment(testClient, simulationhelpers.ExperimentOptions{
		PackageID:       packageID,
		ModelName:       modelName,
		NamePrefix:      namePrefix,
		SimulateVarData: simulateVarData,
	})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := testClient.ModelSimulateWithTranslate(experimentID)
	if err != nil {
		t.Fatalf("BUG_2729 提交仿真失败: %v", err)
	}
	if resp.Status != 0 {
		t.Fatalf("BUG_2729 提交仿真返回非成功状态: status=%d msg=%s err=%s", resp.Status, resp.Msg, resp.Err)
	}
	if err := simulationhelpers.WaitWithTranslateFinished(testClient, packageID, modelName, experimentID, "BUG_2729 仿真 "+experimentID); err != nil {
		t.Fatal(err)
	}
	return experimentID
}

// assertBug2729SimulationRecordAvailable 函数作用：断言指定模型仍能查到目标仿真实验记录。
// 入参：t 提供断言能力；packageID 和 modelName 定位模型；experimentID 是目标仿真实验 ID。
// 返回值：无；记录不可见时终止当前测试。
// 示例：
//
//	assertBug2729SimulationRecordAvailable(t, packageID, renamedModelName, experimentID)
func assertBug2729SimulationRecordAvailable(t *testing.T, packageID, modelName, experimentID string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Minute)
	var lastList []any
	for time.Now().Before(deadline) {
		resp, err := testClient.GetSimulationRecordListV2(packageID, modelName, 1)
		if err != nil {
			t.Fatalf("BUG_2729 查询仿真记录列表失败: %v", err)
		}
		if resp.Status != 0 {
			t.Fatalf("BUG_2729 查询仿真记录列表返回非成功状态: status=%d msg=%s err=%s", resp.Status, resp.Msg, resp.Err)
		}
		lastList = bug2729SimulationRecordItems(t, resp)
		if item := bug2729FindSimulationRecord(lastList, experimentID); item != nil {
			if item["simulate_model_name"] != modelName {
				t.Fatalf("BUG_2729 仿真记录模型名未随重命名更新: 期望=%q 实际=%v", modelName, item["simulate_model_name"])
			}
			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("BUG_2729 未在模型 %s 下找到实验 %s，最后列表=%v", modelName, experimentID, lastList)
}

// bug2729SimulationRecordItems 函数作用：解析仿真记录列表响应中的 resultList。
// 入参：t 提供失败能力；resp 是仿真记录列表接口响应。
// 返回值：返回 resultList 数组；列表为空时返回空数组。
// 示例：
//
//	items := bug2729SimulationRecordItems(t, resp)
func bug2729SimulationRecordItems(t *testing.T, resp *helpers.Response) []any {
	t.Helper()
	dataMap, err := helpers.ObjectData(resp, "BUG_2729 仿真记录列表")
	if err != nil {
		t.Fatal(err)
	}
	listValue := dataMap["resultList"]
	if listValue == nil {
		return []any{}
	}
	items, ok := listValue.([]any)
	if !ok {
		t.Fatalf("BUG_2729 仿真记录 resultList 不是数组: %T", listValue)
	}
	return items
}

// bug2729FindSimulationRecord 函数作用：按实验 ID 从仿真记录列表中查找记录项。
// 入参：list 是 resultList 数组；experimentID 是目标实验 ID。
// 返回值：找到时返回记录 map；未找到时返回 nil。
// 示例：
//
//	item := bug2729FindSimulationRecord(items, experimentID)
func bug2729FindSimulationRecord(list []any, experimentID string) map[string]any {
	for _, item := range list {
		data, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if data["id"] == experimentID {
			return data
		}
	}
	return nil
}

// assertBug2729SimulationTreeAvailable 函数作用：断言目标实验的仿真结果树仍可读取且非空。
// 入参：t 提供断言能力；experimentID 是目标仿真实验 ID。
// 返回值：无；结果树不可读或为空时终止当前测试。
// 示例：
//
//	assertBug2729SimulationTreeAvailable(t, experimentID)
func assertBug2729SimulationTreeAvailable(t *testing.T, experimentID string) {
	t.Helper()
	resp, err := testClient.GetSimulationRecordTreeV2([]string{experimentID}, "", "")
	if err != nil {
		t.Fatalf("BUG_2729 查询仿真结果树失败: %v", err)
	}
	if resp.Status != 0 {
		t.Fatalf("BUG_2729 查询仿真结果树返回非成功状态: status=%d msg=%s err=%s", resp.Status, resp.Msg, resp.Err)
	}
	if helpers.IsStructurallyEmpty(resp.Data) {
		t.Fatalf("BUG_2729 仿真结果树为空")
	}
}
