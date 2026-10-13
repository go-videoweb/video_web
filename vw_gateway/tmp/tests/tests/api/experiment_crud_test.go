//go:build api_test

package api

import (
	"net/http"
	"testing"

	"yssim-go/tests/helpers"
	simulationhelpers "yssim-go/tests/helpers/simulation"
)

// 测试用常量
const (
	testPackageId = "2942f1a3-c5b3-45cd-9cdb-c951784e01ce"
	testModelName = "Modelica.Blocks.Examples.PID_Controller"
)

// extractExperimentList 解析 v2 实验列表接口返回的 data.experiment_list 数组。
// 不存在或类型不符返回 nil（用例侧据此判定空列表）。
func extractExperimentList(t *testing.T, resp *helpers.Response) []any {
	t.Helper()
	dataMap, ok := resp.Data.(map[string]any)
	if !ok {
		return nil
	}
	listVal, ok := dataMap["experiment_list"]
	if !ok || listVal == nil {
		return nil
	}
	list, ok := listVal.([]any)
	if !ok {
		t.Errorf("experiment_list 不是数组，实际类型 %T", listVal)
		return nil
	}
	return list
}

// experimentInList 判断指定实验 ID 是否在 v2 list 返回中。
func experimentInList(list []any, id string) bool {
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if m["id"] == id {
			return true
		}
	}
	return false
}

// ===== 第一批：Create / Get(List) / Delete =====

func TestExperimentCreate(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	// 正常创建与重复名称用例必须使用同一个实际名称，才能稳定命中后端重名校验。
	normalExperimentName := uniqueName("测试实验_创建正常")
	tests := []struct {
		name           string
		packageId      string
		modelName      string
		experimentName string
		simulateVar    map[string]string
		expectError    bool
		expectedCode   int
	}{
		{
			name:           "正常创建实验",
			packageId:      testPackageId,
			modelName:      testModelName,
			experimentName: normalExperimentName,
			simulateVar:    simulationhelpers.DefaultSimulateVarData(),
			expectError:    false,
		},
		{
			name:           "实验名称为默认实验应被拒绝",
			packageId:      testPackageId,
			modelName:      testModelName,
			experimentName: "实验1", // 默认实验名称为 实验1
			simulateVar:    simulationhelpers.DefaultSimulateVarData(),
			expectError:    true,
			expectedCode:   2,
		},
		{
			name:           "重复名称应被拒绝",
			packageId:      testPackageId,
			modelName:      testModelName,
			experimentName: normalExperimentName, // 与第一条同名
			simulateVar:    simulationhelpers.DefaultSimulateVarData(),
			expectError:    true,
			expectedCode:   2,
		},
	}

	var createdIds []string
	hook := helpers.TestHooks{
		Before: func(t *testing.T) {
			// 先获取实验列表, 保证有一个默认实验
			_, err := testClient.ExperimentGet(testPackageId, testModelName)
			if err != nil {
				t.Fatalf("获取列表失败: %s", err.Error())
			}
		},
		After: func(t *testing.T) {
			for _, id := range createdIds {
				simulationhelpers.DeleteExperiment(testClient, id)
			}
		}}
	helpers.RunWithHooks(t, hook,
		func(t *testing.T) {
			for _, tt := range tests {
				tt := tt
				t.Run(tt.name, func(t *testing.T) {
					resp, err := testClient.Primary().ExperimentCreate(tt.packageId, tt.modelName, tt.experimentName, tt.simulateVar)
					if err != nil {
						t.Fatalf("请求失败: %v", err)
					}

					if tt.expectError {
						assert.IsError(resp, tt.expectedCode, tt.name)
					} else {
						assert.IsSuccess(resp, tt.name)
						assert.DataIsNotEmpty(resp, tt.name)
						assert.HasField(resp, "id", tt.name)
						// 记录创建的实验 ID，由 After 钩子统一清理
						if data, ok := resp.Data.(map[string]any); ok {
							if id, ok := data["id"].(string); ok {
								createdIds = append(createdIds, id)
							}
						}
					}
				})
			}
		})
}

func TestExperimentGet(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	tests := []struct {
		name        string
		packageId   string
		modelName   string
		expectError bool
		minLength   int
	}{
		{
			name:        "正常获取实验列表",
			packageId:   testPackageId,
			modelName:   testModelName,
			expectError: false,
			minLength:   2,
		},
		{
			name:        "不存在的 package_id 返回空列表",
			packageId:   "non-existent-package-id",
			modelName:   testModelName,
			expectError: false,
			minLength:   0,
		},
		{
			name:        "不存在的 model_name 返回空列表",
			packageId:   testPackageId,
			modelName:   "Non.Existent.Model",
			expectError: false,
			minLength:   0,
		},
	}

	var id1, id2 string
	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			// 前置：创建两个实验
			{
				var err error
				id1, _, err = simulationhelpers.CreateExperiment(testClient, simulationhelpers.ExperimentOptions{PackageID: testPackageId, ModelName: testModelName, NamePrefix: "测试实验_列表1"})
				if err != nil {
					t.Fatal(err)
				}
			}
			{
				var err error
				id2, _, err = simulationhelpers.CreateExperiment(testClient, simulationhelpers.ExperimentOptions{PackageID: testPackageId, ModelName: testModelName, NamePrefix: "测试实验_列表2"})
				if err != nil {
					t.Fatal(err)
				}
			}
		},
		After: func(t *testing.T) {
			// 删除创建的实验
			simulationhelpers.DeleteExperiment(testClient, id1)
			simulationhelpers.DeleteExperiment(testClient, id2)
		},
	}
	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				resp, err := testClient.Primary().ExperimentGet(tt.packageId, tt.modelName)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}

				assert.IsSuccess(resp, tt.name)

				// v2 接口返回 data: { experiment_list: [...], more: bool }
				assert.HasFields(resp, []string{"experiment_list", "more"}, tt.name)

				list := extractExperimentList(t, resp)
				if tt.minLength > 0 {
					if len(list) < tt.minLength {
						t.Errorf("%s: experiment_list 长度 %d 小于期望最小值 %d", tt.name, len(list), tt.minLength)
					}
				}
			})
		}
	},
	)
}

func TestExperimentDelete(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var expId string
	// v2 创建接口会把最新实验设为默认实验；再创建一个占位实验，让待删除目标退为普通实验。
	var guardExpId string

	tests := []struct {
		name                string
		experimentIdLiteral string // 空字符串表示用 hook 创建的 expId
		expectError         bool
		expectedCode        int
	}{
		{
			name:                "正常删除实验",
			experimentIdLiteral: "", // 由 expId 注入
			expectError:         false,
		},
		{
			name:                "删除不存在的实验（不报错，幂等）",
			experimentIdLiteral: "non-existent-experiment-id",
			expectError:         false,
		},
	}

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			{
				var err error
				expId, _, err = simulationhelpers.CreateExperiment(testClient, simulationhelpers.ExperimentOptions{PackageID: testPackageId, ModelName: testModelName, NamePrefix: "测试实验_待删除"})
				if err != nil {
					t.Fatal(err)
				}
			}
			// 删除接口拒绝默认实验，guardExpId 创建后会成为默认实验，从而允许 expId 走正常删除分支。
			{
				var err error
				guardExpId, _, err = simulationhelpers.CreateExperiment(testClient, simulationhelpers.ExperimentOptions{PackageID: testPackageId, ModelName: testModelName, NamePrefix: "测试实验_删除占位"})
				if err != nil {
					t.Fatal(err)
				}
			}
		},
		After: func(t *testing.T) {
			simulationhelpers.DeleteExperiment(testClient, expId)
			simulationhelpers.DeleteExperiment(testClient, guardExpId)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				targetId := tt.experimentIdLiteral
				if targetId == "" {
					targetId = expId
				}
				resp, err := testClient.Primary().ExperimentDelete(targetId)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}

				if tt.expectError {
					assert.IsError(resp, tt.expectedCode, tt.name)
				} else {
					assert.IsSuccess(resp, tt.name)
				}
			})
		}

		// 验证删除后列表中不再包含该实验
		t.Run("删除后验证列表不含已删除实验", func(t *testing.T) {
			resp, err := testClient.ExperimentGet(testPackageId, testModelName)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			list := extractExperimentList(t, resp)
			if experimentInList(list, expId) {
				t.Errorf("已删除的实验 %s 仍在列表中", expId)
			}
		})
	})
}

// TestExperimentDefaultSet 验证 POST /simulation/experiment/default/set。
// 覆盖正常设置默认实验、不存在的实验 ID，以及缺少 experiment_id 的绑定错误。
func TestExperimentDefaultSet(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var expId string
	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			var err error
			expId, _, err = simulationhelpers.CreateExperiment(testClient, simulationhelpers.ExperimentOptions{
				PackageID:  testPackageId,
				ModelName:  testModelName,
				NamePrefix: "测试实验_设置默认",
			})
			if err != nil {
				t.Fatal(err)
			}
		},
		After: func(t *testing.T) {
			simulationhelpers.DeleteExperiment(testClient, expId)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		t.Run("正常设置默认实验", func(t *testing.T) {
			resp, err := testClient.Primary().SetDefaultExperiment(expId)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsSuccess(resp, "正常设置默认实验")
			isDefault, err := simulationhelpers.ExperimentIsDefault(expId)
			if err != nil {
				t.Fatal(err)
			}
			if !isDefault {
				t.Fatalf("正常设置默认实验: 数据库中实验 %q 未被标记为默认实验", expId)
			}
		})

		t.Run("不存在的实验 ID 返回业务错误", func(t *testing.T) {
			resp, err := testClient.Primary().SetDefaultExperiment("experiment-id-not-exist")
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsError(resp, 2, "不存在的实验 ID 返回业务错误")
		})

		t.Run("缺少 experiment_id 返回 HTTP 400", func(t *testing.T) {
			rawResp, err := testClient.Primary().SetDefaultExperimentRaw(map[string]any{})
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			body, err := helpers.ReadAndCloseRawBody(rawResp)
			if err != nil {
				t.Fatal(err)
			}
			if rawResp.StatusCode != http.StatusBadRequest {
				t.Fatalf("缺少 experiment_id 返回 HTTP 400: 期望 HTTP 400，实际=%d，body=%q", rawResp.StatusCode, string(body))
			}
		})
	})
}

// ===== 第二批：OptionsSet / NameEdit / Parameters =====

func TestExperimentOptionsSet(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var expId string

	tests := []struct {
		name                string
		experimentIdLiteral string // 空 → 用 hook 创建的 expId
		options             map[string]any
		expectError         bool
		expectedCode        int
	}{
		{
			name: "正常设置仿真选项",
			options: map[string]any{
				"start_time":          "0",
				"stop_time":           "10",
				"method":              "euler",
				"number_of_intervals": "1000",
				"tolerance":           "1e-8",
				"interval":            "0.01",
				"multiple_run_sim":    false,
			},
			expectError: false,
		},
		{
			name: "stop_time 为 0 应被拒绝",
			options: map[string]any{
				"start_time":          "0",
				"stop_time":           "0",
				"method":              "dassl",
				"number_of_intervals": "500",
				"tolerance":           "1e-6",
				"interval":            "0.002",
				"multiple_run_sim":    false,
			},
			expectError:  true,
			expectedCode: 2,
		},
		{
			name: "stop_time 小于等于 start_time 应被拒绝",
			options: map[string]any{
				"start_time":          "5",
				"stop_time":           "5",
				"method":              "dassl",
				"number_of_intervals": "500",
				"tolerance":           "1e-6",
				"interval":            "0.002",
				"multiple_run_sim":    false,
			},
			expectError:  true,
			expectedCode: 2,
		},
		{
			name: "tolerance 为 0 应被拒绝",
			options: map[string]any{
				"start_time":          "0",
				"stop_time":           "1",
				"method":              "dassl",
				"number_of_intervals": "500",
				"tolerance":           "0",
				"interval":            "0.002",
				"multiple_run_sim":    false,
			},
			expectError:  true,
			expectedCode: 2,
		},
		{
			name:                "不存在的实验 ID（接口不报错，RowsAffected=0）",
			experimentIdLiteral: "non-existent-id",
			options:             simulationhelpers.DefaultExperimentOptionsData(),
			expectError:         false,
		},
	}

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			{
				var err error
				expId, _, err = simulationhelpers.CreateExperiment(testClient, simulationhelpers.ExperimentOptions{PackageID: testPackageId, ModelName: testModelName, NamePrefix: "测试实验_设置仿真选项"})
				if err != nil {
					t.Fatal(err)
				}
			}
		},
		After: func(t *testing.T) {
			simulationhelpers.DeleteExperiment(testClient, expId)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				targetId := tt.experimentIdLiteral
				if targetId == "" {
					targetId = expId
				}
				resp, err := testClient.Primary().ExperimentOptionsSet(targetId, tt.options)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}

				if tt.expectError {
					assert.IsError(resp, tt.expectedCode, tt.name)
				} else {
					assert.IsSuccess(resp, tt.name)
				}
			})
		}

		// 验证设置结果：通过 /simulation/experiment/detail 查看参数是否更新
		// （v2 list 接口已不返回 stopTime / method 等字段，需走详情接口）
		t.Run("验证设置后仿真选项已更新", func(t *testing.T) {
			resp, err := testClient.ExperimentDetails(testPackageId, testModelName, expId)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			dataMap, ok := resp.Data.(map[string]any)
			if !ok {
				t.Fatalf("详情接口返回 data 不是 object: %T", resp.Data)
			}
			simVar, ok := dataMap["simulate_var_data"].(map[string]any)
			if !ok {
				t.Fatalf("详情接口缺少 simulate_var_data 字段: %v", dataMap)
			}
			if simVar["stopTime"] != "10" {
				t.Errorf("stopTime 未更新，期望 10，实际 %v", simVar["stopTime"])
			}
		})
	})
}

// TestExperimentParameters 验证通过 /simulation/experiment/detail 接口获取实验参数。
// 旧的 /simulation/experiment/parameters 接口已被废弃，参数列表位于 data.parameters。
func TestExperimentParameters(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var expId string

	tests := []struct {
		name                string
		packageId           string
		modelName           string
		experimentIdLiteral string // 空 → 用 hook 创建的 expId
		expectError         bool
		expectedCode        int
	}{
		{
			name:        "正常获取实验详情中的参数列表",
			packageId:   testPackageId,
			modelName:   testModelName,
			expectError: false,
		},
		{
			name:                "不存在的实验 ID 返回空 parameters",
			packageId:           testPackageId,
			modelName:           testModelName,
			experimentIdLiteral: "non-existent-id",
			expectError:         false, // 接口不报错，data.parameters 为 null
		},
	}

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			{
				var err error
				expId, _, err = simulationhelpers.CreateExperiment(testClient, simulationhelpers.ExperimentOptions{PackageID: testPackageId, ModelName: testModelName, NamePrefix: "测试实验_获取参数"})
				if err != nil {
					t.Fatal(err)
				}
			}
		},
		After: func(t *testing.T) {
			simulationhelpers.DeleteExperiment(testClient, expId)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				targetId := tt.experimentIdLiteral
				if targetId == "" {
					targetId = expId
				}
				resp, err := testClient.Primary().ExperimentDetails(tt.packageId, tt.modelName, targetId)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}

				if tt.expectError {
					assert.IsError(resp, tt.expectedCode, tt.name)
					return
				}

				assert.IsSuccess(resp, tt.name)

				dataMap, ok := resp.Data.(map[string]any)
				if !ok {
					t.Fatalf("%s: data 不是 object: %T", tt.name, resp.Data)
				}
				// detail 接口必须包含 parameters 字段（即便为空也应存在）
				if _, exists := dataMap["parameters"]; !exists {
					t.Errorf("%s: data 缺少 parameters 字段", tt.name)
				}
			})
		}
	})
}

// TestExperimentNameEdit 验证 /simulation/experiment/name/edit 接口。
// 注：该接口曾被列入接口总览的废弃清单，但现已恢复使用，因此重新覆盖测试。
func TestExperimentNameEdit(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var expId1, expId2 string
	// expName2 保存带唯一后缀的真实名称，用于验证“改名为已存在名称”。
	var expName2 string
	// 改名目标也加唯一后缀，避免与上一次失败测试留下的数据冲突。
	newExpName := uniqueName("测试实验_改名A_新名称")

	tests := []struct {
		name              string
		packageId         string
		modelName         string
		experimentIdRef   string // "first"→expId1, "second"→expId2, 其它→字面量
		newExperimentName string
		// newNameRef 指向 hook 中动态生成的名称，避免在表驱动数据里写死会被 uniqueName 改写的值。
		newNameRef   string
		expectError  bool
		expectedCode int
	}{
		{
			name:              "正常修改实验名称",
			packageId:         testPackageId,
			modelName:         testModelName,
			experimentIdRef:   "first",
			newExperimentName: newExpName,
			expectError:       false,
		},
		{
			name:              "修改为 实验1 应被拒绝",
			packageId:         testPackageId,
			modelName:         testModelName,
			experimentIdRef:   "first",
			newExperimentName: "实验1",
			expectError:       true,
			expectedCode:      2,
		},
		{
			name:            "修改实验名称为已存在的名称应被拒绝",
			packageId:       testPackageId,
			modelName:       testModelName,
			experimentIdRef: "first",
			newNameRef:      "secondName",
			expectError:     true,
			expectedCode:    2,
		},
		{
			name:              "不存在的实验 ID 应被拒绝",
			packageId:         testPackageId,
			modelName:         testModelName,
			experimentIdRef:   "non-existent-id",
			newExperimentName: "任意名称",
			expectError:       true,
			expectedCode:      2,
		},
	}

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			{
				var err error
				expId1, _, err = simulationhelpers.CreateExperiment(testClient, simulationhelpers.ExperimentOptions{PackageID: testPackageId, ModelName: testModelName, NamePrefix: "测试实验_改名A"})
				if err != nil {
					t.Fatal(err)
				}
			}
			{
				var err error
				expId2, expName2, err = simulationhelpers.CreateExperiment(testClient, simulationhelpers.ExperimentOptions{PackageID: testPackageId, ModelName: testModelName, NamePrefix: "测试实验_改名B"})
				if err != nil {
					t.Fatal(err)
				}
			}
		},
		After: func(t *testing.T) {
			simulationhelpers.DeleteExperiment(testClient, expId1)
			simulationhelpers.DeleteExperiment(testClient, expId2)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				var targetId string
				switch tt.experimentIdRef {
				case "first":
					targetId = expId1
				case "second":
					targetId = expId2
				default:
					targetId = tt.experimentIdRef
				}
				newExperimentName := tt.newExperimentName
				if tt.newNameRef == "secondName" {
					// 使用第二个实验的真实名称，才能测试同模型下名称冲突。
					newExperimentName = expName2
				}
				resp, err := testClient.Primary().ExperimentNameEdit(tt.packageId, tt.modelName, targetId, newExperimentName)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				if tt.expectError {
					assert.IsError(resp, tt.expectedCode, tt.name)
				} else {
					assert.IsSuccess(resp, tt.name)
				}
			})
		}
	})
}

// ===== 第三批：ExperimentParametersSet / ExperimentParametersDelete =====

// parameterInList 在 detail 接口返回的 parameters []any 中查找指定 name 的项。
// 找到则返回该 map，否则返回 nil。
func parameterInList(params []any, name string) map[string]any {
	for _, p := range params {
		item, ok := p.(map[string]any)
		if !ok {
			continue
		}
		if item["name"] == name {
			return item
		}
	}
	return nil
}

// getDetailParameters 调用 ExperimentDetails，返回解析好的 parameters []any 和 export_variables []any。
// 任何步骤失败则调用 t.Fatalf。
func getDetailFields(t *testing.T, expId string) (parameters []any, exportVars []any) {
	t.Helper()
	resp, err := testClient.ExperimentDetails(testPackageId, testModelName, expId)
	if err != nil {
		t.Fatalf("获取详情请求失败: %v", err)
	}
	if resp.Status != 0 {
		t.Fatalf("获取详情返回非零状态: status=%d err=%s", resp.Status, resp.Err)
	}
	dataMap, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("detail data 不是 object: %T", resp.Data)
	}
	parameters, _ = dataMap["parameters"].([]any)
	exportVars, _ = dataMap["export_variables"].([]any)
	return
}

// TestExperimentParametersSet 验证 /api/v2/simulation/experiment/parameters/set
// 参数有三种类型：
//   - 单值：parameter_value 为普通数值字符串，如 "20"
//   - 范围：parameter_value 形如 range(start, end, count)
//   - 离散：parameter_value 形如 choices(v1, v2, ...)
//
// 行为说明（来自 ExperimentParameterSetView）：
//   - experiment_id 不存在 → status=2 "未找到实验记录"
//   - 成功 → status=0，msg="参数设置成功"
//   - 存储 key = element_name（无 parameter_name 时）或 element_name.parameter_name
func TestExperimentParametersSet(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var expId string

	tests := []struct {
		name                string
		experimentIdLiteral string
		parameters          []map[string]any
		// wantParamKey: 期望在 detail.parameters 中存在的 name（空则跳过验证）
		wantParamKey string
		// wantParamValue: 期望的 value 字符串（空则只检查存在性）
		wantParamValue string
		expectError    bool
		expectedCode   int
	}{
		{
			// 单值：element_name.parameter_name → "PI.k"
			name: "单值参数：PI.k=2.0",
			parameters: []map[string]any{
				{
					"extend_name":         "Modelica.Units.SI.Real",
					"is_extend":           false,
					"element_name":        "PI",
					"parameter_name":      "k",
					"parameter_value":     "2.0",
					"redeclare":           false,
					"class_prefixes":      "Real",
					"replaced_class_type": "",
					"unit":                "",
					"displayUnit":         "",
				},
			},
			wantParamKey:   "PI.k",
			wantParamValue: "2.0",
			expectError:    false,
		},
		{
			// 范围：无 parameter_name，key = element_name → "PI"
			// parameter_value 形如 range(start, end, count)
			name: "范围参数：PI.k=range(1.0,3.0,3)",
			parameters: []map[string]any{
				{
					"extend_name":         "Modelica.Units.SI.Real",
					"is_extend":           false,
					"element_name":        "PI",
					"parameter_name":      "k",
					"parameter_value":     "range(1.0, 3.0, 3)",
					"redeclare":           false,
					"class_prefixes":      "Real",
					"replaced_class_type": "",
					"unit":                "",
					"displayUnit":         "",
				},
			},
			wantParamKey: "PI.k",
			// 范围值经 ParseParameterValue 处理后原样存储，不校验具体值
			expectError: false,
		},
		{
			// 离散：parameter_value 形如 choices(v1,v2,...)
			// key = element_name.parameter_name → "PI.Ti"
			name: "离散参数：PI.Ti=choices(0.5,1.0,2.0)",
			parameters: []map[string]any{
				{
					"extend_name":         "Modelica.Units.SI.Time",
					"is_extend":           false,
					"element_name":        "PI",
					"parameter_name":      "Ti",
					"parameter_value":     "choices(0.5, 1.0, 2.0)",
					"redeclare":           false,
					"class_prefixes":      "Real",
					"replaced_class_type": "",
					"unit":                "s",
					"displayUnit":         "s",
				},
			},
			wantParamKey: "PI.Ti",
			expectError:  false,
		},
		{
			// 同时写入多个参数（单值 + 离散）
			name: "同时设置多个参数（PI.k 单值 + PI.Ti 离散）",
			parameters: []map[string]any{
				{
					"extend_name":         "Modelica.Units.SI.Real",
					"is_extend":           false,
					"element_name":        "PI",
					"parameter_name":      "k",
					"parameter_value":     "2.0",
					"redeclare":           false,
					"class_prefixes":      "Real",
					"replaced_class_type": "",
					"unit":                "",
					"displayUnit":         "",
				},
				{
					"extend_name":         "Modelica.Units.SI.Time",
					"is_extend":           false,
					"element_name":        "PI",
					"parameter_name":      "Ti",
					"parameter_value":     "choices(0.5, 1.0, 2.0)",
					"redeclare":           false,
					"class_prefixes":      "Real",
					"replaced_class_type": "",
					"unit":                "s",
					"displayUnit":         "s",
				},
			},
			wantParamKey: "PI.k",
			expectError:  false,
		},
		{
			name:                "不存在的实验 ID 应返回 status=2",
			experimentIdLiteral: "non-existent-id",
			parameters: []map[string]any{
				{
					"extend_name":         "Modelica.Units.SI.Real",
					"is_extend":           false,
					"element_name":        "PI",
					"parameter_name":      "k",
					"parameter_value":     "2.0",
					"redeclare":           false,
					"class_prefixes":      "Real",
					"replaced_class_type": "",
					"unit":                "",
					"displayUnit":         "",
				},
			},
			expectError:  true,
			expectedCode: 2,
		},
	}

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			{
				var err error
				expId, _, err = simulationhelpers.CreateExperiment(testClient, simulationhelpers.ExperimentOptions{PackageID: testPackageId, ModelName: testModelName, NamePrefix: "测试实验_设置参数"})
				if err != nil {
					t.Fatal(err)
				}
			}
		},
		After: func(t *testing.T) {
			simulationhelpers.DeleteExperiment(testClient, expId)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				targetId := tt.experimentIdLiteral
				if targetId == "" {
					targetId = expId
				}
				resp, err := testClient.Primary().ExperimentParametersSet(targetId, tt.parameters)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				if tt.expectError {
					assert.IsError(resp, tt.expectedCode, tt.name)
					return
				}
				assert.IsSuccess(resp, tt.name)

				// 精确验证：通过 detail 接口检查指定参数 key 存在
				if tt.wantParamKey != "" {
					params, _ := getDetailFields(t, expId)
					item := parameterInList(params, tt.wantParamKey)
					if item == nil {
						t.Errorf("%s: detail.parameters 中未找到 name=%q", tt.name, tt.wantParamKey)
						return
					}
					if tt.wantParamValue != "" && item["value"] != tt.wantParamValue {
						t.Errorf("%s: 期望 %q value=%q，实际=%q",
							tt.name, tt.wantParamKey, tt.wantParamValue, item["value"])
					}
				}
			})
		}
	})
}

// TestExperimentParametersDelete 验证 /api/v2/simulation/experiment/parameters/delete
// 行为说明（来自 ExperimentParameterDelete）：
//   - experiment_id 不存在 → status=2 "未找到实验记录"
//   - 参数名不存在于记录中 → 静默通过（delete map key 幂等），status=0
//   - 成功 → status=0，msg="删除参数成功"
func TestExperimentParametersDelete(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var expId string

	tests := []struct {
		name                string
		experimentIdLiteral string
		parameterNames      []string
		expectError         bool
		expectedCode        int
	}{
		{
			name:           "正常删除已存在的参数 PI.k",
			parameterNames: []string{"PI.k"},
			expectError:    false,
		},
		{
			name:           "删除不存在的参数名（幂等，不报错）",
			parameterNames: []string{"non.existent.param"},
			expectError:    false,
		},
		{
			name:                "不存在的实验 ID 应返回 status=2",
			experimentIdLiteral: "non-existent-id",
			parameterNames:      []string{"PI.k"},
			expectError:         true,
			expectedCode:        2,
		},
	}

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			{
				var err error
				expId, _, err = simulationhelpers.CreateExperiment(testClient, simulationhelpers.ExperimentOptions{PackageID: testPackageId, ModelName: testModelName, NamePrefix: "测试实验_删除参数"})
				if err != nil {
					t.Fatal(err)
				}
			}
			// 先写入 PI.k 和 PI.Ti 两个参数，供后续删除验证
			if _, err := testClient.ExperimentParametersSet(expId, []map[string]any{
				{
					"element_name":    "PI",
					"parameter_name":  "k",
					"parameter_value": "2.0",
					"class_prefixes":  "Real",
				},
				{
					"element_name":    "PI",
					"parameter_name":  "Ti",
					"parameter_value": "1.0",
					"class_prefixes":  "Real",
				},
			}); err != nil {
				t.Fatalf("前置设置参数失败: %v", err)
			}
		},
		After: func(t *testing.T) {
			simulationhelpers.DeleteExperiment(testClient, expId)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				targetId := tt.experimentIdLiteral
				if targetId == "" {
					targetId = expId
				}
				resp, err := testClient.Primary().ExperimentParametersDelete(targetId, tt.parameterNames)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				if tt.expectError {
					assert.IsError(resp, tt.expectedCode, tt.name)
				} else {
					assert.IsSuccess(resp, tt.name)
				}
			})
		}

		// 精确验证：PI.k 已删除，PI.Ti 仍存在
		t.Run("删除后验证 PI.k 已移除、PI.Ti 仍存在", func(t *testing.T) {
			params, _ := getDetailFields(t, expId)
			if parameterInList(params, "PI.k") != nil {
				t.Errorf("'PI.k' 仍存在于 parameters 中，期望已删除")
			}
			if parameterInList(params, "PI.Ti") == nil {
				t.Errorf("'PI.Ti' 应仍存在于 parameters 中，但未找到")
			}
		})
	})
}

// ===== 第四批：ExperimentExportVariableSet / ExperimentExportVariableUnset =====

// TestExperimentExportVariableSet 验证 /api/v2/simulation/experiment/export_variables/set
// 行为说明（来自 ExperimentExportVariableSet）：
//   - experiment_id 不存在 → status=2 "未找到实验记录"
//   - 变量已存在 → 幂等，不报错，status=0
//   - 成功 → status=0，msg="设置导出变量 xxx 成功"
//   - 存储结构 {name: variableName, type: "variable"|"component"}
func TestExperimentExportVariableSet(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var expId string

	tests := []struct {
		name                string
		experimentIdLiteral string
		variableName        string
		isComponent         bool
		expectError         bool
		expectedCode        int
	}{
		{
			name:         "正常设置导出变量（非组件）PI.y",
			variableName: "PI.y",
			isComponent:  false,
			expectError:  false,
		},
		{
			name:         "正常设置导出变量（组件类型）PI",
			variableName: "PI",
			isComponent:  true,
			expectError:  false,
		},
		{
			name:         "重复设置同一变量（幂等，不报错）",
			variableName: "PI.y",
			isComponent:  false,
			expectError:  false,
		},
		{
			name:                "不存在的实验 ID 应返回 status=2",
			experimentIdLiteral: "non-existent-id",
			variableName:        "PI.y",
			isComponent:         false,
			expectError:         true,
			expectedCode:        2,
		},
	}

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			{
				var err error
				expId, _, err = simulationhelpers.CreateExperiment(testClient, simulationhelpers.ExperimentOptions{PackageID: testPackageId, ModelName: testModelName, NamePrefix: "测试实验_设置导出变量"})
				if err != nil {
					t.Fatal(err)
				}
			}
		},
		After: func(t *testing.T) {
			simulationhelpers.DeleteExperiment(testClient, expId)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				targetId := tt.experimentIdLiteral
				if targetId == "" {
					targetId = expId
				}
				resp, err := testClient.Primary().ExperimentExportVariableSet(targetId, tt.variableName, tt.isComponent)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				if tt.expectError {
					assert.IsError(resp, tt.expectedCode, tt.name)
					return
				}
				assert.IsSuccess(resp, tt.name)

				// 精确验证：检查指定变量名已出现在 export_variables 中
				_, exportVars := getDetailFields(t, expId)
				if !simulationhelpers.ExportVariableInList(exportVars, tt.variableName) {
					t.Errorf("%s: detail.export_variables 中未找到 name=%q", tt.name, tt.variableName)
				}
			})
		}
	})
}

// TestExperimentExportVariableUnset 验证 /api/v2/simulation/experiment/export_variables/unset
// 行为说明（来自 ExperimentExportVariableUnset）：
//   - experiment_id 不存在 → status=2 "未找到实验记录"
//   - variable_name 仅包含空白字符 → status=2 "导出变量名不能为空"
//   - variable_name 为空字符串会先触发 binding 失败并返回 HTTP 400，因此这里覆盖 trim 后为空的业务分支
//   - 变量不在列表中 → 幂等，不报错，status=0
//   - 成功 → status=0，msg="取消设置导出变量 xxx"
func TestExperimentExportVariableUnset(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var expId string

	tests := []struct {
		name                string
		experimentIdLiteral string
		variableName        string
		expectError         bool
		expectedCode        int
	}{
		{
			name:         "正常取消已设置的导出变量 PI.y",
			variableName: "PI.y",
			expectError:  false,
		},
		{
			name:         "取消不在列表中的变量（幂等，不报错）",
			variableName: "non.existent.variable",
			expectError:  false,
		},
		{
			name:         "variable_name 仅空白字符应返回 status=2",
			variableName: "   ",
			expectError:  true,
			expectedCode: 2,
		},
		{
			name:                "不存在的实验 ID 应返回 status=2",
			experimentIdLiteral: "non-existent-id",
			variableName:        "PI.y",
			expectError:         true,
			expectedCode:        2,
		},
	}

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			{
				var err error
				expId, _, err = simulationhelpers.CreateExperiment(testClient, simulationhelpers.ExperimentOptions{PackageID: testPackageId, ModelName: testModelName, NamePrefix: "测试实验_取消导出变量"})
				if err != nil {
					t.Fatal(err)
				}
			}
			// 写入两个导出变量：PI.y（待取消）和 PI.u_s（保留），以便精确对比
			for _, v := range []string{"PI.y", "PI.u_s"} {
				if _, err := testClient.ExperimentExportVariableSet(expId, v, false); err != nil {
					t.Fatalf("前置设置导出变量 %s 失败: %v", v, err)
				}
			}
		},
		After: func(t *testing.T) {
			simulationhelpers.DeleteExperiment(testClient, expId)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				targetId := tt.experimentIdLiteral
				if targetId == "" {
					targetId = expId
				}
				resp, err := testClient.Primary().ExperimentExportVariableUnset(targetId, tt.variableName)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				if tt.expectError {
					assert.IsError(resp, tt.expectedCode, tt.name)
				} else {
					assert.IsSuccess(resp, tt.name)
				}
			})
		}

		// 精确验证：PI.y 已移除，PID.u_s 仍存在
		t.Run("取消后验证 PI.y 已移除、PI.u_s 仍存在", func(t *testing.T) {
			_, exportVars := getDetailFields(t, expId)
			if simulationhelpers.ExportVariableInList(exportVars, "PI.y") {
				t.Errorf("'PI.y' 仍存在于 export_variables 中，期望已移除")
			}
			if !simulationhelpers.ExportVariableInList(exportVars, "PI.u_s") {
				t.Errorf("'PI.u_s' 应仍存在于 export_variables 中，但未找到")
			}
		})
	})
}
