//go:build bug_regression_test

package bug2752

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"yssim-go/tests/helpers"
	modelhelpers "yssim-go/tests/helpers/model"
	simulationhelpers "yssim-go/tests/helpers/simulation"
)

const bug2752CalibrationCSVFile = "Acceleration measurements.csv"

var testClient *helpers.APIClient

// TestMain 函数作用：初始化 BUG_2752 回归测试客户端并校验真实后端测试环境。
// 入参：m 是当前测试包的执行入口。
// 返回值：无直接返回值；函数通过 os.Exit 返回测试进程退出码。
// 示例：
//
//	go test -tags bug_regression_test ./tests/bugRegression/BUG_2752
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
			helpers.LogTeardownStep("BUG_2752 regression suite finished")
		},
	}
	os.Exit(suite.Run(m))
}

// TestBug2752RenamedModelKeepsHistoryAfterSameNameDelete 验证模型 A 改名为 B 后，新建同名 A 并删除不会破坏 B 的仿真结果和参数标定记录。
func TestBug2752RenamedModelKeepsHistoryAfterSameNameDelete(t *testing.T) {
	assert := helpers.NewAssertResponse(t)
	originalPackageName := modelhelpers.UniqueModelName("Bug2752Model")
	renamedPackageName := originalPackageName + "Renamed"
	originalModelName := originalPackageName + ".SimpleCar"
	renamedModelName := renamedPackageName + ".SimpleCar"

	var originalPackageID string
	var originalCleanupName string
	var newPackageID string
	var oldExperimentID string
	var oldCalibrationID string
	var newExperimentID string
	var newCalibrationID string

	helpers.RunWithHooks(t, helpers.TestHooks{
		After: func(t *testing.T) {
			cleanupBug2752CalibrationAndExperiment(oldCalibrationID, oldExperimentID)
			cleanupBug2752CalibrationAndExperiment(newCalibrationID, newExperimentID)
			modelhelpers.CleanupTempModelFixture(testClient, newPackageID, originalPackageName)
			modelhelpers.CleanupTempModelFixture(testClient, originalPackageID, originalCleanupName)
		},
	}, func(t *testing.T) {
		originalPackageID = createBug2752CalibrationCapableModel(t, originalPackageName)
		originalCleanupName = originalPackageName
		oldExperimentID = createAndRunBug2752Simulation(t, originalPackageID, originalModelName, "bug2752_old")
		oldCalibrationID = createBug2752CalibrationRecord(t, originalPackageID, originalModelName, oldExperimentID, "bug2752_old_calibration")

		renameResp, err := testClient.RenameModel(originalPackageID, originalPackageName, renamedPackageName)
		if err != nil {
			t.Fatalf("BUG_2752 重命名旧模型失败: %v", err)
		}
		assert.IsSuccess(renameResp, "BUG_2752 重命名旧模型 A 为 B")
		originalCleanupName = renamedPackageName
		if err := modelhelpers.EnsureModelLoaded(testClient, originalPackageID, renamedPackageName, "BUG_2752 加载重命名后的模型 B"); err != nil {
			t.Fatal(err)
		}

		newPackageID = createBug2752CalibrationCapableModel(t, originalPackageName)
		newExperimentID = createAndRunBug2752Simulation(t, newPackageID, originalModelName, "bug2752_new")
		newCalibrationID = createBug2752CalibrationRecord(t, newPackageID, originalModelName, newExperimentID, "bug2752_new_calibration")

		deleteResp, err := testClient.DeletePackage(newPackageID, "", originalPackageName)
		if err != nil {
			t.Fatalf("BUG_2752 删除新同名模型 A 失败: %v", err)
		}
		assert.IsSuccess(deleteResp, "BUG_2752 删除新同名模型 A")

		assertBug2752SimulationRecordAvailable(t, originalPackageID, renamedModelName, oldExperimentID)
		assertBug2752SimulationTreeAvailable(t, oldExperimentID)
		assertBug2752CalibrationListContains(t, originalPackageID, renamedModelName, oldCalibrationID)
	})
}

// createBug2752CalibrationCapableModel 函数作用：上传并加载用于仿真和参数标定的唯一模型包。
// 入参：t 提供测试失败能力；packageName 是需要写入资源源码的顶层 package 名。
// 返回值：返回上传后模型包的 packageID。
// 示例：
//
//	packageID := createBug2752CalibrationCapableModel(t, "Bug2752Model_abcd1234")
func createBug2752CalibrationCapableModel(t *testing.T, packageName string) string {
	t.Helper()
	content := bug2752ModelSource(t, packageName)
	packageID, _, err := modelhelpers.ImportModelLibraryFixture(testClient, packageName, packageName+".mo", []byte(content))
	if err != nil {
		t.Fatal(err)
	}
	modelName := packageName + ".SimpleCar"
	if err := modelhelpers.EnsureModelLoaded(testClient, packageID, modelName, "BUG_2752 加载标定模型"); err != nil {
		t.Fatal(err)
	}
	return packageID
}

// bug2752ModelSource 函数作用：读取固定参数标定模型并替换为当前测试唯一 package 名。
// 入参：t 提供失败能力；packageName 是目标顶层 package 名。
// 返回值：返回替换后的 Modelica 源码。
// 示例：
//
//	source := bug2752ModelSource(t, "Bug2752Model_abcd1234")
func bug2752ModelSource(t *testing.T, packageName string) string {
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

// createAndRunBug2752Simulation 函数作用：创建并完成一次仿真，生成可回查的仿真结果记录。
// 入参：t 提供失败能力；packageID 和 modelName 定位模型；namePrefix 用于生成唯一实验名。
// 返回值：返回已完成仿真实验 ID。
// 示例：
//
//	experimentID := createAndRunBug2752Simulation(t, packageID, modelName, "bug2752_old")
func createAndRunBug2752Simulation(t *testing.T, packageID, modelName, namePrefix string) string {
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
		t.Fatalf("BUG_2752 提交仿真失败: %v", err)
	}
	if resp.Status != 0 {
		t.Fatalf("BUG_2752 提交仿真返回非成功状态: status=%d msg=%s err=%s", resp.Status, resp.Msg, resp.Err)
	}
	if err := simulationhelpers.WaitWithTranslateFinished(testClient, packageID, modelName, experimentID, "BUG_2752 仿真 "+experimentID); err != nil {
		t.Fatal(err)
	}
	return experimentID
}

// createBug2752CalibrationRecord 函数作用：为指定模型和实验创建一条参数标定记录。
// 入参：t 提供失败能力；packageID、modelName 和 experimentID 绑定标定对象；namePrefix 生成唯一标定名。
// 返回值：返回新建参数标定记录 ID。
// 示例：
//
//	calibrationID := createBug2752CalibrationRecord(t, packageID, modelName, experimentID, "bug2752_calibration")
func createBug2752CalibrationRecord(t *testing.T, packageID, modelName, experimentID, namePrefix string) string {
	t.Helper()
	translateResp, err := testClient.CalibrationTranslateFmu(packageID, modelName)
	if err != nil {
		t.Fatalf("BUG_2752 生成标定准备记录失败: %v", err)
	}
	if translateResp.Status != 0 {
		t.Fatalf("BUG_2752 生成标定准备记录返回非成功状态: status=%d msg=%s err=%s", translateResp.Status, translateResp.Msg, translateResp.Err)
	}
	dataMap, err := helpers.ObjectData(translateResp, "BUG_2752 生成标定准备记录")
	if err != nil {
		t.Fatal(err)
	}
	preparationID, _ := dataMap["preparation_id"].(string)
	if preparationID == "" {
		t.Fatalf("BUG_2752 生成标定准备记录: preparation_id 为空或类型错误: %v", dataMap["preparation_id"])
	}
	uploadBug2752CalibrationCSV(t, preparationID)

	parameterName := firstBug2752TreeVariable(t, preparationID, "parameter", nil)
	resultName := firstBug2752TreeVariable(t, preparationID, "all", map[string]bool{parameterName: true})
	payload := map[string]any{
		"preparation_id":   preparationID,
		"calibration_name": fmt.Sprintf("%s_%d", namePrefix, time.Now().UnixNano()),
		"experiment_id":    experimentID,
		"tuner_parameter": []map[string]any{{
			"name":  parameterName,
			"min":   0,
			"max":   200,
			"start": 1,
		}},
		"result_couplings": []map[string]any{{
			"csv_column":     "speed",
			"model_variable": resultName,
		}},
		"cases": []map[string]any{{
			"name":       "calibrate",
			"type":       "calibrate",
			"start_time": 0,
			"stop_time":  0.2,
		}},
	}
	createResp, err := testClient.CalibrationCreate(payload)
	if err != nil {
		t.Fatalf("BUG_2752 创建标定记录失败: %v", err)
	}
	if createResp.Status != 0 {
		t.Fatalf("BUG_2752 创建标定记录返回非成功状态: status=%d msg=%s err=%s", createResp.Status, createResp.Msg, createResp.Err)
	}
	createdData, err := helpers.ObjectData(createResp, "BUG_2752 创建标定记录")
	if err != nil {
		t.Fatal(err)
	}
	calibrationID, _ := createdData["id"].(string)
	if calibrationID == "" {
		t.Fatalf("BUG_2752 创建标定记录: id 为空或类型错误: %v", createdData["id"])
	}
	return calibrationID
}

// uploadBug2752CalibrationCSV 函数作用：把固定 CSV 数据上传到指定标定准备记录。
// 入参：t 提供失败能力；preparationID 是标定准备记录 ID。
// 返回值：无；上传失败时终止当前测试。
// 示例：
//
//	uploadBug2752CalibrationCSV(t, preparationID)
func uploadBug2752CalibrationCSV(t *testing.T, preparationID string) {
	t.Helper()
	content, err := modelhelpers.ReadModelTestResource("ParameterCalibration", bug2752CalibrationCSVFile)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := testClient.UploadCalibrationData(preparationID, false, bug2752CalibrationCSVFile, content)
	if err != nil {
		t.Fatalf("BUG_2752 上传标定 CSV 失败: %v", err)
	}
	if resp.Status != 0 {
		t.Fatalf("BUG_2752 上传标定 CSV 返回非成功状态: status=%d msg=%s err=%s", resp.Status, resp.Msg, resp.Err)
	}
}

// firstBug2752TreeVariable 函数作用：轮询标定变量树并选择第一个可用于创建标定记录的变量名。
// 入参：t 提供失败能力；preparationID 定位准备记录；modelFilter 是 tree 接口过滤类型；excluded 是需跳过的变量名集合。
// 返回值：返回变量树中的 variables 字段值。
// 示例：
//
//	name := firstBug2752TreeVariable(t, preparationID, "parameter", nil)
func firstBug2752TreeVariable(t *testing.T, preparationID, modelFilter string, excluded map[string]bool) string {
	t.Helper()

	deadline := time.Now().Add(2 * time.Minute)
	var lastData any
	var lastErr error
	for time.Now().Before(deadline) {
		resp, err := testClient.CalibrationTree(preparationID, false, "", "", modelFilter)
		if err != nil {
			lastErr = err
			time.Sleep(time.Second)
			continue
		}
		lastData = resp.Data
		if resp.Status != 0 {
			lastErr = fmt.Errorf("status=%d msg=%s err=%s", resp.Status, resp.Msg, resp.Err)
			time.Sleep(time.Second)
			continue
		}
		items, err := helpers.ArrayData(resp, "BUG_2752 查询标定变量树")
		if err != nil {
			lastErr = err
			time.Sleep(time.Second)
			continue
		}
		for _, item := range items {
			data, ok := item.(map[string]any)
			if !ok {
				continue
			}
			name, _ := data["variables"].(string)
			if strings.TrimSpace(name) == "" || excluded[name] {
				continue
			}
			return name
		}
		time.Sleep(time.Second)
	}
	t.Fatalf("BUG_2752 标定变量树中未找到可用变量，model=%s last_data=%v last_err=%v", modelFilter, lastData, lastErr)
	return ""
}

// assertBug2752SimulationRecordAvailable 函数作用：断言重命名后的模型仍能查到旧仿真实验记录。
// 入参：t 提供断言能力；packageID 和 modelName 定位重命名后的模型；experimentID 是旧仿真实验 ID。
// 返回值：无；记录不可见时终止当前测试。
// 示例：
//
//	assertBug2752SimulationRecordAvailable(t, packageID, renamedModelName, experimentID)
func assertBug2752SimulationRecordAvailable(t *testing.T, packageID, modelName, experimentID string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Minute)
	var lastList []any
	for time.Now().Before(deadline) {
		resp, err := testClient.GetSimulationRecordListV2(packageID, modelName, 1)
		if err != nil {
			t.Fatalf("BUG_2752 查询仿真记录列表失败: %v", err)
		}
		if resp.Status != 0 {
			t.Fatalf("BUG_2752 查询仿真记录列表返回非成功状态: status=%d msg=%s err=%s", resp.Status, resp.Msg, resp.Err)
		}
		lastList = bug2752SimulationRecordItems(t, resp)
		if item := bug2752FindSimulationRecord(lastList, experimentID); item != nil {
			if item["simulate_model_name"] != modelName {
				t.Fatalf("BUG_2752 仿真记录模型名未随旧模型重命名更新: 期望=%q 实际=%v", modelName, item["simulate_model_name"])
			}
			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("BUG_2752 未在重命名后的模型 %s 下找到旧实验 %s，最后列表=%v", modelName, experimentID, lastList)
}

// bug2752SimulationRecordItems 函数作用：解析仿真记录列表响应中的 resultList。
// 入参：t 提供失败能力；resp 是仿真记录列表接口响应。
// 返回值：返回 resultList 数组；列表为空时返回空数组。
// 示例：
//
//	items := bug2752SimulationRecordItems(t, resp)
func bug2752SimulationRecordItems(t *testing.T, resp *helpers.Response) []any {
	t.Helper()
	dataMap, err := helpers.ObjectData(resp, "BUG_2752 仿真记录列表")
	if err != nil {
		t.Fatal(err)
	}
	listValue := dataMap["resultList"]
	if listValue == nil {
		return []any{}
	}
	items, ok := listValue.([]any)
	if !ok {
		t.Fatalf("BUG_2752 仿真记录 resultList 不是数组: %T", listValue)
	}
	return items
}

// bug2752FindSimulationRecord 函数作用：按实验 ID 从仿真记录列表中查找记录项。
// 入参：list 是 resultList 数组；experimentID 是目标实验 ID。
// 返回值：找到时返回记录 map；未找到时返回 nil。
// 示例：
//
//	item := bug2752FindSimulationRecord(items, experimentID)
func bug2752FindSimulationRecord(list []any, experimentID string) map[string]any {
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

// assertBug2752SimulationTreeAvailable 函数作用：断言旧实验的仿真结果树仍可读取。
// 入参：t 提供断言能力；experimentID 是旧仿真实验 ID。
// 返回值：无；结果树不可读或为空时终止当前测试。
// 示例：
//
//	assertBug2752SimulationTreeAvailable(t, experimentID)
func assertBug2752SimulationTreeAvailable(t *testing.T, experimentID string) {
	t.Helper()
	resp, err := testClient.GetSimulationRecordTreeV2([]string{experimentID}, "", "")
	if err != nil {
		t.Fatalf("BUG_2752 查询旧实验仿真结果树失败: %v", err)
	}
	if resp.Status != 0 {
		t.Fatalf("BUG_2752 查询旧实验仿真结果树返回非成功状态: status=%d msg=%s err=%s", resp.Status, resp.Msg, resp.Err)
	}
	if helpers.IsStructurallyEmpty(resp.Data) {
		t.Fatalf("BUG_2752 旧实验仿真结果树为空")
	}
}

// assertBug2752CalibrationListContains 函数作用：断言重命名后的模型仍能在标定列表中查到旧标定记录。
// 入参：t 提供断言能力；packageID 和 modelName 定位重命名后的模型；calibrationID 是旧标定记录 ID。
// 返回值：无；标定记录不可见时终止当前测试。
// 示例：
//
//	assertBug2752CalibrationListContains(t, packageID, renamedModelName, calibrationID)
func assertBug2752CalibrationListContains(t *testing.T, packageID, modelName, calibrationID string) {
	t.Helper()
	rawResp, err := testClient.CalibrationListRaw(packageID, modelName, 1)
	if err != nil {
		t.Fatalf("BUG_2752 查询标定列表失败: %v", err)
	}
	dataEvent := bug2752ReadSSEDataObject(t, rawResp.Body, "BUG_2752 标定列表")
	if status, ok := dataEvent["status"].(float64); ok && status != 0 {
		t.Fatalf("BUG_2752 标定列表返回非成功状态: %v", dataEvent)
	}
	dataMap, ok := dataEvent["data"].(map[string]any)
	if !ok {
		t.Fatalf("BUG_2752 标定列表 data 不是 object: %T %v", dataEvent["data"], dataEvent["data"])
	}
	list, ok := dataMap["list"].([]any)
	if !ok {
		t.Fatalf("BUG_2752 标定列表 list 不是数组: %T %v", dataMap["list"], dataMap["list"])
	}
	for _, item := range list {
		record, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if record["id"] == calibrationID {
			return
		}
	}
	t.Fatalf("BUG_2752 未在重命名后的模型 %s 下找到旧标定记录 %s，list=%v", modelName, calibrationID, list)
}

// bug2752ReadSSEDataObject 函数作用：读取 SSE 响应中的第一个 data 事件并解析为对象。
// 入参：t 提供失败能力；body 是 SSE 响应体；message 是场景说明。
// 返回值：返回 data 事件的 JSON 对象。
// 示例：
//
//	data := bug2752ReadSSEDataObject(t, rawResp.Body, "标定列表")
func bug2752ReadSSEDataObject(t *testing.T, body io.ReadCloser, message string) map[string]any {
	t.Helper()
	defer body.Close()
	reader := bufio.NewReader(body)
	for i := 0; i < 5; i++ {
		eventName, payload, err := helpers.ReadSSEEvent(reader)
		if err != nil {
			t.Fatalf("%s: 读取 SSE 事件失败: %v", message, err)
		}
		if eventName != "data" {
			continue
		}
		var data map[string]any
		if err := json.Unmarshal(payload, &data); err != nil {
			t.Fatalf("%s: 解析 SSE data 失败: %v payload=%s", message, err, string(payload))
		}
		return data
	}
	t.Fatalf("%s: 未读取到 data SSE 事件", message)
	return nil
}

// cleanupBug2752CalibrationAndExperiment 函数作用：清理 BUG_2752 测试创建的标定记录和实验。
// 入参：calibrationID 是待删除标定记录；experimentID 是待删除实验。
// 返回值：无；清理失败会被忽略以保护主断言结果。
// 示例：
//
//	cleanupBug2752CalibrationAndExperiment(calibrationID, experimentID)
func cleanupBug2752CalibrationAndExperiment(calibrationID, experimentID string) {
	if calibrationID != "" {
		_, _ = testClient.CalibrationStop(calibrationID)
		_, _ = testClient.CalibrationDelete(calibrationID)
	}
	simulationhelpers.DeleteExperiment(testClient, experimentID)
}
