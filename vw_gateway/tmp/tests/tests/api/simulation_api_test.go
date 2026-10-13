//go:build api_test

package api

import (
	"bufio"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
	"yssim-go/app/DataBase"
	"yssim-go/config"
	"yssim-go/tests/helpers"
	modelhelpers "yssim-go/tests/helpers/model"
	simulationhelpers "yssim-go/tests/helpers/simulation"
	workspacehelpers "yssim-go/tests/helpers/workspace"
)

const (
	modelicaStdLibId           = "2942f1a3-c5b3-45cd-9cdb-c951784e01ce"
	workspaceLibId             = "3e9a9da4-b55f-4451-bb8e-46b71bc6a0d1"
	realtimeSimulationStopTime = 2.0
)

// simulationResultSmokeVariables 是 PID_Controller 中较稳定的结果变量候选，用于避免测试进程预读本地 MAT 或结果树。
var simulationResultSmokeVariables = []string{
	"PI.y",
	"PI.x",
	"inertia1.phi",
	"inertia1.w",
	"inertia2.phi",
	"speedSensor.w",
	"spring.phi_rel",
	"spring.tau",
}

func TestGetSimulationRecordListV2(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var expId string
	var recordItem map[string]any

	tests := []struct {
		name           string
		packageId      string
		modelName      string
		pageNum        int
		assertResponse func(t *testing.T, resp *helpers.Response)
	}{
		{
			name:      "正常获取仿真记录列表",
			packageId: testPackageId,
			modelName: testModelName,
			pageNum:   1,
			assertResponse: func(t *testing.T, resp *helpers.Response) {
				assert.IsSuccess(resp, "正常获取仿真记录列表")
				dataMap := extractSimulationRecordListData(t, resp)
				for _, field := range []string{"resultList", "pageCount", "total"} {
					if _, ok := dataMap[field]; !ok {
						t.Fatalf("正常获取仿真记录列表: data 缺少字段 %q", field)
					}
				}
				list := extractSimulationRecordListItems(t, resp)
				item := findSimulationRecordByExperimentID(list, expId)
				if item == nil {
					t.Fatalf("正常获取仿真记录列表: 未找到 experiment_id=%s 的记录", expId)
				}
				for _, field := range []string{"index", "id", "create_time", "simulate_status", "simulate_start_time", "simulate_end_time", "simulate_model_name", "simulate_run_time", "another_name", "simulate_percentage", "experiment_name", "pipe_net"} {
					if _, ok := item[field]; !ok {
						t.Errorf("正常获取仿真记录列表: 记录项缺少字段 %q", field)
					}
				}
				if item["id"] != expId {
					t.Errorf("正常获取仿真记录列表: 期望 id=%s，实际=%v", expId, item["id"])
				}
				if item["experiment_name"] != recordItem["experiment_name"] {
					t.Errorf("正常获取仿真记录列表: experiment_name 不匹配，actual=%v want=%v", item["experiment_name"], recordItem["experiment_name"])
				}
				if item["simulate_model_name"] != testModelName {
					t.Errorf("正常获取仿真记录列表: simulate_model_name 不匹配，actual=%v", item["simulate_model_name"])
				}
				if item["another_name"] != recordItem["experiment_name"].(string)+"的结果" {
					t.Errorf("正常获取仿真记录列表: another_name 不匹配，actual=%v", item["another_name"])
				}
				if item["simulate_status"] == "" {
					t.Errorf("正常获取仿真记录列表: simulate_status 为空")
				}
				for _, field := range []string{"simulate_start_time", "simulate_end_time", "simulate_run_time", "simulate_percentage"} {
					if item[field] == "" || item[field] == "-" {
						t.Errorf("正常获取仿真记录列表: 字段 %s 未填充，actual=%v", field, item[field])
					}
				}
			},
		},
		{
			name:      "不存在的 package_id 返回空列表",
			packageId: "non-existent-package-id",
			modelName: testModelName,
			pageNum:   1,
			assertResponse: func(t *testing.T, resp *helpers.Response) {
				assert.IsSuccess(resp, "不存在的 package_id 返回空列表")
				list := extractSimulationRecordListItems(t, resp)
				if len(list) != 0 {
					t.Errorf("不存在的 package_id 返回空列表: 期望空列表，实际 %d 条", len(list))
				}
			},
		},
		{
			name:      "不存在的 model_name 返回空列表",
			packageId: testPackageId,
			modelName: "Non.Existent.Model",
			pageNum:   1,
			assertResponse: func(t *testing.T, resp *helpers.Response) {
				assert.IsSuccess(resp, "不存在的 model_name 返回空列表")
				list := extractSimulationRecordListItems(t, resp)
				if len(list) != 0 {
					t.Errorf("不存在的 model_name 返回空列表: 期望空列表，实际 %d 条", len(list))
				}
			},
		},
		{
			name:      "model_name 为空时返回全空间分页结果",
			packageId: "",
			modelName: "",
			pageNum:   1,
			assertResponse: func(t *testing.T, resp *helpers.Response) {
				assert.IsSuccess(resp, "model_name 为空时返回全空间分页结果")
				dataMap := extractSimulationRecordListData(t, resp)
				list := extractSimulationRecordListItems(t, resp)
				if findSimulationRecordByExperimentID(list, expId) == nil {
					t.Fatalf("model_name 为空时返回全空间分页结果: 当前实验 %s 未出现在分页结果中", expId)
				}
				if _, ok := dataMap["pageCount"].(float64); !ok {
					t.Errorf("model_name 为空时返回全空间分页结果: pageCount 不是数字，actual=%T", dataMap["pageCount"])
				}
				if _, ok := dataMap["total"].(float64); !ok {
					t.Errorf("model_name 为空时返回全空间分页结果: total 不是数字，actual=%T", dataMap["total"])
				}
			},
		},
	}

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			if _, err := testClient.ExperimentGet(testPackageId, testModelName); err != nil {
				t.Fatalf("预热实验列表失败: %v", err)
			}
			{
				var err error
				expId, _, err = simulationhelpers.CreateExperiment(testClient, simulationhelpers.ExperimentOptions{PackageID: testPackageId, ModelName: testModelName, NamePrefix: "reclist_" + time.Now().Format("150405000")})
				if err != nil {
					t.Fatal(err)
				}
			}
			resp, err := testClient.ModelSimulateWithTranslate(expId)
			if err != nil {
				t.Fatalf("提交 v4 仿真任务失败: %v", err)
			}
			if resp.Status != 0 {
				t.Fatalf("提交 v4 仿真任务返回非零状态: status=%d err=%s", resp.Status, resp.Err)
			}
			// 已完成列表在实时仿真场景下可能包含进行中的记录；这里以 poll 接口返回本次任务结束作为完成信号。
			if err := simulationhelpers.WaitWithTranslateFinished(testClient, testPackageId, testModelName, expId, "仿真实验 "+expId); err != nil {
				t.Fatal(err)
			}
			recordItem = waitForSimulationRecordListVisible(t, testPackageId, testModelName, expId)
		},
		After: func(t *testing.T) {
			simulationhelpers.DeleteExperiment(testClient, expId)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				resp, err := testClient.Primary().GetSimulationRecordListV2(tt.packageId, tt.modelName, tt.pageNum)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				if tt.assertResponse != nil {
					tt.assertResponse(t, resp)
				}
			})
		}
	})
}

// TestGetSimulationRecordFinishedV2 验证 GET /api/v2/simulation/record/get/finished。
// 该接口会根据任务中心状态筛选完成记录；基础场景不强依赖已有完成记录数量，并覆盖 case 运行模式快照优先级。
func TestGetSimulationRecordFinishedV2(t *testing.T) {
	var caseRunModeExpId string
	var caseRunModeOriginalSettings helpers.UserSettings
	var shouldRestoreCaseRunModeSettings bool

	tests := []struct {
		name           string
		packageID      string
		modelName      string
		hooks          helpers.TestHooks
		assertResponse func(t *testing.T, resp *helpers.Response)
	}{
		{
			name:      "正常获取指定模型已完成仿真记录",
			packageID: testPackageId,
			modelName: testModelName,
			assertResponse: func(t *testing.T, resp *helpers.Response) {
				assert := helpers.NewAssertResponse(t)
				assert.IsSuccess(resp, "正常获取指定模型已完成仿真记录")
				_, err := helpers.ObjectData(resp, "正常获取指定模型已完成仿真记录")
				if err != nil {
					t.Fatal(err)
				}
				// 后端无完成记录时会把 nil slice 编码为 null；这里统一按空列表处理。
				resultList := extractSimulationRecordListItems(t, resp)
				if len(resultList) == 0 {
					return
				}
				firstItem, ok := resultList[0].(map[string]any)
				if !ok {
					t.Fatalf("正常获取指定模型已完成仿真记录: resultList[0] 不是 object，实际=%T", resultList[0])
				}
				for _, field := range []string{"id", "simulate_model_name", "simulate_status", "simulate_status_msg"} {
					if _, ok := firstItem[field]; !ok {
						t.Errorf("正常获取指定模型已完成仿真记录: 首条记录缺少字段 %q", field)
					}
				}
			},
		},
		{
			name:      "不存在的模型返回空列表结构",
			packageID: testPackageId,
			modelName: "Non.Existent.Model.Finished",
			assertResponse: func(t *testing.T, resp *helpers.Response) {
				assert := helpers.NewAssertResponse(t)
				assert.IsSuccess(resp, "不存在的模型返回空列表结构")
				_, err := helpers.ObjectData(resp, "不存在的模型返回空列表结构")
				if err != nil {
					t.Fatal(err)
				}
				resultList := extractSimulationRecordListItems(t, resp)
				if len(resultList) != 0 {
					t.Fatalf("不存在的模型返回空列表结构: 期望空列表，实际 %d 条", len(resultList))
				}
			},
		},
		{
			name:      "case 运行模式优先于实验当前配置",
			packageID: testPackageId,
			modelName: testModelName,
			hooks: helpers.TestHooks{
				Before: func(t *testing.T) {
					caseRunModeExpId = ""
					shouldRestoreCaseRunModeSettings = false

					settingsResp, err := testClient.GetUserSettings()
					if err != nil {
						t.Fatalf("获取当前用户设置失败: %v", err)
					}
					settings, err := helpers.ParseUserSettings(settingsResp)
					if err != nil {
						t.Fatal(err)
					}
					caseRunModeOriginalSettings = settings
					shouldRestoreCaseRunModeSettings = true
					if settings.Interactive {
						settingsResp, err := testClient.SetUserSettingsFull(settings.GridDisplay, false)
						if err != nil {
							t.Fatalf("关闭实时仿真设置失败: %v", err)
						}
						if settingsResp.Status != 0 {
							t.Fatalf("关闭实时仿真设置返回非成功状态: status=%d msg=%s err=%s", settingsResp.Status, settingsResp.Msg, settingsResp.Err)
						}
					}

					// 准备一个已完成的单运行实验，再把本次 case 快照调整为多运行。
					caseRunModeExpId = createFinishedSimulationExperiment(t, "finished_case_mode_"+time.Now().Format("150405000"))

					var experiment DataBase.YssimExperimentRecord
					if err := config.DB.Select("id", "task_batch_id").Where("id = ?", caseRunModeExpId).First(&experiment).Error; err != nil {
						t.Fatalf("查询实验记录失败: %v", err)
					}

					var taskCases []DataBase.YssimExperimentCase
					if err := config.DB.Where("experiment_id = ? AND task_batch_id = ?", caseRunModeExpId, experiment.TaskBatchId).Find(&taskCases).Error; err != nil {
						t.Fatalf("查询实验 case 失败: %v", err)
					}
					if len(taskCases) == 0 {
						t.Fatalf("查询实验 case 为空: experiment_id=%s task_batch_id=%s", caseRunModeExpId, experiment.TaskBatchId)
					}
					for _, taskCase := range taskCases {
						if taskCase.MultipleRunSim {
							t.Fatalf("单运行仿真 case 的 multiple_run_sim 期望为 false，实际为 true: case_id=%s", taskCase.ID)
						}
					}

					caseUpdate := config.DB.Model(&DataBase.YssimExperimentCase{}).Where("experiment_id = ? AND task_batch_id = ?", caseRunModeExpId, experiment.TaskBatchId).Update("multiple_run_sim", true)
					if caseUpdate.Error != nil {
						t.Fatalf("设置 case 多运行快照失败: %v", caseUpdate.Error)
					}
					if caseUpdate.RowsAffected == 0 {
						t.Fatalf("设置 case 多运行快照未更新任何记录: experiment_id=%s task_batch_id=%s", caseRunModeExpId, experiment.TaskBatchId)
					}
					if err := config.DB.Model(&DataBase.YssimExperimentRecord{}).Where("id = ?", caseRunModeExpId).Updates(map[string]any{
						"multiple_run_sim": false,
						"interactive":      false,
					}).Error; err != nil {
						t.Fatalf("设置实验当前运行模式失败: %v", err)
					}
				},
				After: func(t *testing.T) {
					simulationhelpers.DeleteExperiment(testClient, caseRunModeExpId)
					if shouldRestoreCaseRunModeSettings {
						settingsResp, err := testClient.SetUserSettingsFull(caseRunModeOriginalSettings.GridDisplay, caseRunModeOriginalSettings.Interactive)
						if err != nil {
							t.Errorf("恢复用户设置失败: %v", err)
							return
						}
						if settingsResp.Status != 0 {
							t.Errorf("恢复用户设置返回非成功状态: status=%d msg=%s err=%s", settingsResp.Status, settingsResp.Msg, settingsResp.Err)
						}
					}
				},
			},
			assertResponse: func(t *testing.T, resp *helpers.Response) {
				assert := helpers.NewAssertResponse(t)
				assert.IsSuccess(resp, "case 运行模式优先于实验当前配置")

				items := extractSimulationRecordListItems(t, resp)
				item := findSimulationRecordByExperimentID(items, caseRunModeExpId)
				if item == nil {
					t.Fatalf("未找到 experiment_id=%s 的完成记录", caseRunModeExpId)
				}
				if multipleRun, ok := item["multiple_run"].(bool); !ok || !multipleRun {
					t.Fatalf("期望 multiple_run=true，实际=%v", item["multiple_run"])
				}
				taskList, ok := item["task_list"].([]any)
				if !ok || len(taskList) == 0 {
					t.Fatalf("期望 task_list 为非空数组，实际=%T %v", item["task_list"], item["task_list"])
				}
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			helpers.RunWithHooks(t, tt.hooks, func(t *testing.T) {
				resp, err := testClient.Primary().GetSimulationRecordFinishedV2(tt.packageID, tt.modelName)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				tt.assertResponse(t, resp)
			})
		})
	}
}

func TestWorkspaceChangePollSimulationState(t *testing.T) {
	assert := helpers.NewAssertResponse(t)
	missingModelName := "Non.Existent.Model.State." + time.Now().Format("20060102150405.000000000")

	tests := []struct {
		name      string
		packageID string
		modelName string
	}{
		{name: "正常获取工作空间 poll 仿真状态", packageID: testPackageId, modelName: testModelName},
		{name: "model_name 为空仍返回空间级状态", packageID: testPackageId, modelName: ""},
		{name: "不存在的 model_name 仍返回空间级状态", packageID: testPackageId, modelName: missingModelName},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			resp, err := testClient.Primary().WorkspaceChangePoll(tt.packageID, tt.modelName, "api-test-simulation-state")
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsSuccess(resp, tt.name)

			statuses, err := workspacehelpers.ExtractPollStatuses(resp)
			if err != nil {
				t.Fatal(err)
			}
			assertWorkspacePollStatus(t, tt.name+" translate_status_data", statuses.Translate)
			assertWorkspacePollStatus(t, tt.name+" simulate_status_data", statuses.Simulate)
		})
	}
}

func TestGetSimulationRecordTreeV2(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var expId string

	tests := []struct {
		name               string
		parentNode         string
		keyWords           string
		useRaw             bool
		rawBody            map[string]any
		expectedHTTPStatus int
		assertResponse     func(t *testing.T, resp *helpers.Response)
	}{
		{
			name:       "正常获取顶层仿真结果树",
			parentNode: "",
			keyWords:   "",
			assertResponse: func(t *testing.T, resp *helpers.Response) {
				assert.IsSuccess(resp, "正常获取顶层仿真结果树")
				nodes, err := helpers.ObjectListData(resp, "仿真结果树")
				if err != nil {
					t.Fatal(err)
				}
				if len(nodes) == 0 {
					t.Fatalf("正常获取顶层仿真结果树: 期望返回非空节点列表")
				}
				for _, field := range []string{"variables", "has_child", "id"} {
					if _, ok := nodes[0][field]; !ok {
						t.Errorf("正常获取顶层仿真结果树: 首项缺少字段 %q", field)
					}
				}
			},
		},
		{
			name:       "指定 parent_node 当前返回空 data",
			parentNode: "PID",
			keyWords:   "",
			assertResponse: func(t *testing.T, resp *helpers.Response) {
				assert.IsSuccess(resp, "指定 parent_node 当前返回空 data")
				assert.ValueIsStructurallyEmpty(resp.Data, "指定 parent_node 当前返回空 data")
			},
		},
		{
			name:       "关键字过滤未命中返回空数组",
			parentNode: "",
			keyWords:   "__not_found_keyword__",
			assertResponse: func(t *testing.T, resp *helpers.Response) {
				assert.IsSuccess(resp, "关键字过滤未命中返回空数组")
				assert.ValueIsStructurallyEmpty(resp.Data, "关键字过滤未命中返回空数组")
			},
		},
		{
			name:               "缺少 experiment_id_list 返回 HTTP 400",
			useRaw:             true,
			expectedHTTPStatus: http.StatusBadRequest,
			rawBody: map[string]any{
				"parent_node": "PID",
			},
		},
	}

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			expId = createFinishedSimulationExperiment(t, "treev2_"+time.Now().Format("150405000"))
		},
		After: func(t *testing.T) {
			simulationhelpers.DeleteExperiment(testClient, expId)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				if tt.useRaw {
					rawResp, err := testClient.Primary().GetSimulationRecordTreeV2Raw(tt.rawBody)
					if err != nil {
						t.Fatalf("请求失败: %v", err)
					}
					body, err := helpers.ReadAndCloseRawBody(rawResp)
					if err != nil {
						t.Fatal(err)
					}
					if rawResp.StatusCode != tt.expectedHTTPStatus {
						t.Fatalf("%s: 期望 HTTP %d，实际 %d，body=%q", tt.name, tt.expectedHTTPStatus, rawResp.StatusCode, string(body))
					}
					assertEmptyJSONStringBody(t, tt.name, body)
					return
				}

				resp, err := testClient.Primary().GetSimulationRecordTreeV2([]string{expId}, tt.parentNode, tt.keyWords)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				tt.assertResponse(t, resp)
			})
		}
	})
}

// TestGetSimulationResultFileV2 验证 POST /api/v2/file/result/all/get。
// 成功场景依赖已完成仿真结果；测试通过结果接口确认 MAT 可读后，再获取结果文件下载路径。
func TestGetSimulationResultFileV2(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var expId string

	tests := []struct {
		name               string
		experimentID       string
		useRaw             bool
		rawBody            any
		expectedHTTPStatus int
		expectError        bool
		expectedCode       int
	}{
		{name: "正常获取仿真结果文件下载路径", experimentID: "fixture"},
		{name: "不存在的实验返回业务错误", experimentID: "non-existent-experiment-id", expectError: true, expectedCode: 2},
		{
			name:               "缺少 experiment_id 返回 HTTP 400",
			useRaw:             true,
			rawBody:            map[string]any{},
			expectedHTTPStatus: http.StatusBadRequest,
		},
	}

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			expId = createFinishedSimulationExperiment(t, "resultfile_"+time.Now().Format("150405000"))
		},
		After: func(t *testing.T) {
			simulationhelpers.DeleteExperiment(testClient, expId)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				if tt.useRaw {
					rawResp, err := testClient.Primary().GetSimulationResultFileV2Raw(tt.rawBody)
					if err != nil {
						t.Fatalf("请求失败: %v", err)
					}
					body, err := helpers.ReadAndCloseRawBody(rawResp)
					if err != nil {
						t.Fatal(err)
					}
					if rawResp.StatusCode != tt.expectedHTTPStatus {
						t.Fatalf("%s: 期望 HTTP %d，实际 %d，body=%q", tt.name, tt.expectedHTTPStatus, rawResp.StatusCode, string(body))
					}
					assertEmptyJSONStringBody(t, tt.name, body)
					return
				}

				experimentID := tt.experimentID
				if experimentID == "fixture" {
					experimentID = expId
				}
				resp, err := testClient.Primary().GetSimulationResultFileV2(experimentID)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				if tt.expectError {
					assert.IsError(resp, tt.expectedCode, tt.name)
					return
				}
				assert.IsSuccess(resp, tt.name)
				dataMap, err := helpers.ObjectData(resp, tt.name)
				if err != nil {
					t.Fatal(err)
				}
				urlValue, ok := dataMap["url"].(string)
				if !ok || strings.TrimSpace(urlValue) == "" {
					t.Fatalf("%s: 返回 url 为空或类型错误: %T %v", tt.name, dataMap["url"], dataMap["url"])
				}
				if !strings.HasSuffix(urlValue, ".mat") && !strings.HasSuffix(urlValue, ".zip") {
					t.Errorf("%s: 期望 url 指向 .mat 或 .zip，实际=%q", tt.name, urlValue)
				}
			})
		}
	})
}

func TestGetSimulationResultV2(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var expId string
	variableName := simulationResultSmokeVariables[0]

	tests := []struct {
		name               string
		variable           string
		useRaw             bool
		rawBody            map[string]any
		expectedHTTPStatus int
		assertResponse     func(t *testing.T, resp *helpers.Response)
	}{
		{
			name:     "正常获取单变量结果",
			variable: "fixture",
			assertResponse: func(t *testing.T, resp *helpers.Response) {
				assert.IsSuccess(resp, "正常获取单变量结果")
				items, err := helpers.ObjectListData(resp, "单变量仿真结果")
				if err != nil {
					t.Fatal(err)
				}
				if len(items) != 1 {
					t.Fatalf("正常获取单变量结果: 期望 1 条结果，实际 %d 条", len(items))
				}
				item := items[0]
				if item["experiment_id"] != expId {
					t.Errorf("正常获取单变量结果: 期望 experiment_id=%s，实际=%v", expId, item["experiment_id"])
				}
				if item["experiment_name"] == "" {
					t.Errorf("正常获取单变量结果: experiment_name 为空")
				}
				if multipleRun, ok := item["multiple_run"].(bool); !ok || multipleRun {
					t.Errorf("正常获取单变量结果: 期望 multiple_run=false，实际=%v", item["multiple_run"])
				}
				assertSeriesPair(t, "正常获取单变量结果", item["abscissa"], item["ordinate"])
				if item["startTime"] == "" || item["stopTime"] == "" {
					t.Errorf("正常获取单变量结果: startTime/stopTime 不应为空，actual start=%v stop=%v", item["startTime"], item["stopTime"])
				}
			},
		},
		{
			name:     "不存在的变量返回空纵轴",
			variable: "Non.Existent.Result.Variable",
			assertResponse: func(t *testing.T, resp *helpers.Response) {
				assert.IsSuccess(resp, "不存在的变量返回空纵轴")
				items, err := helpers.ObjectListData(resp, "单变量仿真结果")
				if err != nil {
					t.Fatal(err)
				}
				if len(items) != 1 {
					t.Fatalf("不存在的变量返回空纵轴: 期望 1 条结果，实际 %d 条", len(items))
				}
				item := items[0]
				if item["experiment_id"] != expId {
					t.Errorf("不存在的变量返回空纵轴: 期望 experiment_id=%s，实际=%v", expId, item["experiment_id"])
				}
				abscissa, ok := item["abscissa"].([]any)
				if !ok || len(abscissa) == 0 {
					t.Fatalf("不存在的变量返回空纵轴: 期望 abscissa 为非空数组，实际=%T %v", item["abscissa"], item["abscissa"])
				}
				if !assert.ValueIsStructurallyEmpty(item["ordinate"], "不存在的变量返回空纵轴: ordinate 为空") {
					return
				}
			},
		},
		{
			name:               "缺少 id 返回 HTTP 400",
			useRaw:             true,
			expectedHTTPStatus: http.StatusBadRequest,
			rawBody: map[string]any{
				"variable": "time",
			},
		},
	}

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			expId = createFinishedSimulationExperiment(t, "resultv2_"+time.Now().Format("150405000"))
		},
		After: func(t *testing.T) {
			simulationhelpers.DeleteExperiment(testClient, expId)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				if tt.useRaw {
					rawResp, err := testClient.Primary().GetSimulationResultV2Raw(tt.rawBody)
					if err != nil {
						t.Fatalf("请求失败: %v", err)
					}
					body, err := helpers.ReadAndCloseRawBody(rawResp)
					if err != nil {
						t.Fatal(err)
					}
					if rawResp.StatusCode != tt.expectedHTTPStatus {
						t.Fatalf("%s: 期望 HTTP %d，实际 %d，body=%q", tt.name, tt.expectedHTTPStatus, rawResp.StatusCode, string(body))
					}
					assertEmptyJSONStringBody(t, tt.name, body)
					return
				}

				variable := tt.variable
				if variable == "fixture" {
					variable = variableName
				}
				resp, err := testClient.Primary().GetSimulationResultV2([]string{expId}, variable)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				tt.assertResponse(t, resp)
			})
		}
	})
}

func TestGetSimulationResultSingularV2(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var expId string
	variableName := simulationResultSmokeVariables[0]

	tests := []struct {
		name               string
		items              []map[string]any
		useRaw             bool
		rawBody            any
		expectedHTTPStatus int
		assertResponse     func(t *testing.T, resp *helpers.Response)
	}{
		{
			name: "正常获取单记录单变量结果",
			items: []map[string]any{{
				"id":       "fixture",
				"variable": "fixture",
			}},
			assertResponse: func(t *testing.T, resp *helpers.Response) {
				assert.IsSuccess(resp, "正常获取单记录单变量结果")
				items, err := helpers.ObjectListData(resp, "多变量仿真结果")
				if err != nil {
					t.Fatal(err)
				}
				if len(items) != 1 {
					t.Fatalf("正常获取单记录单变量结果: 期望 1 条实验结果，实际 %d 条", len(items))
				}
				item := items[0]
				if item["experiment_id"] != expId {
					t.Errorf("正常获取单记录单变量结果: 期望 experiment_id=%s，实际=%v", expId, item["experiment_id"])
				}
				vars := extractSimulationSingularVars(t, item, "正常获取单记录单变量结果")
				varMap, ok := vars[0].(map[string]any)
				if !ok {
					t.Fatalf("正常获取单记录单变量结果: 首个变量项不是 object: %T", vars[0])
				}
				if varMap["name"] != variableName {
					t.Errorf("正常获取单记录单变量结果: 期望变量名=%s，实际=%v", variableName, varMap["name"])
				}
				dataMap, ok := varMap["data"].(map[string]any)
				if !ok {
					t.Fatalf("正常获取单记录单变量结果: data 不是 object: %T", varMap["data"])
				}
				assertSeriesPair(t, "正常获取单记录单变量结果", dataMap["abscissa"], dataMap["ordinate"])
			},
		},
		{
			name: "变量不存在返回空纵轴",
			items: []map[string]any{{
				"id":       "fixture",
				"variable": "Non.Existent.Result.Variable",
			}},
			assertResponse: func(t *testing.T, resp *helpers.Response) {
				assert.IsSuccess(resp, "变量不存在返回空纵轴")
				items, err := helpers.ObjectListData(resp, "多变量仿真结果")
				if err != nil {
					t.Fatal(err)
				}
				if len(items) != 1 {
					t.Fatalf("变量不存在返回空纵轴: 期望 1 条实验结果，实际 %d 条", len(items))
				}
				vars := extractSimulationSingularVars(t, items[0], "变量不存在返回空纵轴")
				varMap, ok := vars[0].(map[string]any)
				if !ok {
					t.Fatalf("变量不存在返回空纵轴: 首个变量项不是 object: %T", vars[0])
				}
				if varMap["name"] != "Non.Existent.Result.Variable" {
					t.Errorf("变量不存在返回空纵轴: 期望变量名=Non.Existent.Result.Variable，实际=%v", varMap["name"])
				}
				dataMap, ok := varMap["data"].(map[string]any)
				if !ok {
					t.Fatalf("变量不存在返回空纵轴: data 不是 object: %T", varMap["data"])
				}
				abscissa, ok := dataMap["abscissa"].([]any)
				if !ok || len(abscissa) == 0 {
					t.Fatalf("变量不存在返回空纵轴: 期望 abscissa 为非空数组，实际=%T %v", dataMap["abscissa"], dataMap["abscissa"])
				}
				if !assert.ValueIsStructurallyEmpty(dataMap["ordinate"], "变量不存在返回空纵轴: ordinate 为空") {
					return
				}
			},
		},
		{
			name:               "空请求列表返回 HTTP 400",
			useRaw:             true,
			rawBody:            []map[string]any{},
			expectedHTTPStatus: http.StatusBadRequest,
		},
	}

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			expId = createFinishedSimulationExperiment(t, "singv2_"+time.Now().Format("150405000"))
		},
		After: func(t *testing.T) {
			simulationhelpers.DeleteExperiment(testClient, expId)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				if tt.useRaw {
					rawResp, err := testClient.Primary().GetSimulationResultSingularV2Raw(tt.rawBody)
					if err != nil {
						t.Fatalf("请求失败: %v", err)
					}
					body, err := helpers.ReadAndCloseRawBody(rawResp)
					if err != nil {
						t.Fatal(err)
					}
					if rawResp.StatusCode != tt.expectedHTTPStatus {
						t.Fatalf("%s: 期望 HTTP %d，实际 %d，body=%q", tt.name, tt.expectedHTTPStatus, rawResp.StatusCode, string(body))
					}
					assertJSONStringBodyEquals(t, tt.name, body, "empty record id")
					return
				}

				items := cloneSimulationResultSingularItems(tt.items, expId, variableName)
				resp, err := testClient.Primary().GetSimulationResultSingularV2(items)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				tt.assertResponse(t, resp)
			})
		}
	})
}

func TestModelSimulateWithTranslateV4(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var expId string

	tests := []struct {
		name               string
		experimentIdRef    string
		useRaw             bool
		rawBody            any
		expectedHTTPStatus int
		expectError        bool
		expectedCode       int
		assertResponse     func(t *testing.T, resp *helpers.Response)
	}{
		{
			name:            "正常发起带翻译 v4 仿真并返回编译与仿真状态",
			experimentIdRef: "fixture",
			assertResponse: func(t *testing.T, resp *helpers.Response) {
				name := "正常发起带翻译 v4 仿真并返回编译与仿真状态"
				assert.IsSuccess(resp, name)
				if resp.Msg != "仿真任务正在准备，请等待仿真完成" && resp.Msg != "模型正在编译中" {
					t.Errorf("%s: 期望 msg 为仿真准备或编译中提示，实际=%q", name, resp.Msg)
				}
				dataMap, err := helpers.ObjectData(resp, "带翻译 v4 仿真")
				if err != nil {
					t.Fatal(err)
				}
				if dataMap["experiment_id"] != expId {
					t.Errorf("%s: 期望 experiment_id=%q，实际=%v", name, expId, dataMap["experiment_id"])
				}
				assert.MapStringFieldIn(dataMap, "compile_status", []string{"compiling", "compiled"}, name)
				assert.MapStringFieldIn(dataMap, "simulate_status", []string{"compiling", "pending"}, name)
				// simulate-with-translate 返回时任务可能还在编译或排队，后续断言前必须等到 poll 接口进入结束态。
				if err := simulationhelpers.WaitWithTranslateFinished(testClient, testPackageId, testModelName, expId, "仿真实验 "+expId); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:            "experiment_id 为空返回 status=2",
			experimentIdRef: "empty",
			expectError:     true,
			expectedCode:    2,
		},
		{
			name:            "不存在的 experiment_id 返回 status=2",
			experimentIdRef: "non-existent-experiment-id",
			expectError:     true,
			expectedCode:    2,
		},
		{
			name:               "请求体类型错误返回 HTTP 400",
			useRaw:             true,
			rawBody:            []string{"unexpected"},
			expectedHTTPStatus: http.StatusBadRequest,
		},
	}

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			{
				var err error
				expId, _, err = simulationhelpers.CreateExperiment(testClient, simulationhelpers.ExperimentOptions{PackageID: testPackageId, ModelName: testModelName, NamePrefix: "simv4_" + time.Now().Format("150405000")})
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
				if tt.useRaw {
					rawResp, err := testClient.Primary().ModelSimulateWithTranslateRaw(tt.rawBody)
					if err != nil {
						t.Fatalf("请求失败: %v", err)
					}
					body, err := helpers.ReadAndCloseRawBody(rawResp)
					if err != nil {
						t.Fatal(err)
					}
					if rawResp.StatusCode != tt.expectedHTTPStatus {
						t.Fatalf("%s: 期望 HTTP %d，实际 %d，body=%q", tt.name, tt.expectedHTTPStatus, rawResp.StatusCode, string(body))
					}
					assertEmptyJSONStringBody(t, tt.name, body)
					return
				}

				experimentId := resolveSimulationExperimentRef(tt.experimentIdRef, expId)
				resp, err := testClient.Primary().ModelSimulateWithTranslate(experimentId)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				if tt.expectError {
					assert.IsError(resp, tt.expectedCode, tt.name)
					return
				}
				tt.assertResponse(t, resp)
			})
		}
	})
}

func TestSimulationRecordTerminateV3(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var expId string

	tests := []struct {
		name                  string
		experimentIdRef       string
		waitTerminateFinished bool
		useRaw                bool
		rawBody               any
		expectedHTTPStatus    int
		assertResponse        func(t *testing.T, resp *helpers.Response)
	}{
		{
			name:                  "正常终止仿真任务返回 success",
			experimentIdRef:       "fixture",
			waitTerminateFinished: true,
			assertResponse: func(t *testing.T, resp *helpers.Response) {
				assert.IsSuccess(resp, "正常终止仿真任务返回 success")
				assertTerminateResponseMessage(t, "正常终止仿真任务返回 success", resp.Msg)
			},
		},
		{
			name:            "不存在的 experiment_id 当前仍返回 success",
			experimentIdRef: "non-existent-experiment-id",
			assertResponse: func(t *testing.T, resp *helpers.Response) {
				assert.IsSuccess(resp, "不存在的 experiment_id 当前仍返回 success")
				assertTerminateResponseMessage(t, "不存在的 experiment_id 当前仍返回 success", resp.Msg)
			},
		},
		{
			name:               "缺少 experiment_id 返回 HTTP 400",
			useRaw:             true,
			rawBody:            map[string]any{},
			expectedHTTPStatus: http.StatusBadRequest,
		},
	}

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			{
				var err error
				expId, _, err = simulationhelpers.CreateExperiment(testClient, simulationhelpers.ExperimentOptions{PackageID: testPackageId, ModelName: testModelName, NamePrefix: "termv3_" + time.Now().Format("150405000")})
				if err != nil {
					t.Fatal(err)
				}
			}
			resp, err := testClient.ModelSimulateWithTranslate(expId)
			if err != nil {
				t.Fatalf("提交 v4 仿真任务失败: %v", err)
			}
			if resp.Status != 0 {
				t.Fatalf("提交 v4 仿真任务返回非零状态: status=%d err=%s", resp.Status, resp.Err)
			}
			waitForExperimentTaskBatchCreated(t, expId, 180*time.Second)
		},
		After: func(t *testing.T) {
			simulationhelpers.DeleteExperiment(testClient, expId)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				if tt.useRaw {
					rawResp, err := testClient.Primary().SimulationRecordTerminateV3Raw(tt.rawBody)
					if err != nil {
						t.Fatalf("请求失败: %v", err)
					}
					body, err := helpers.ReadAndCloseRawBody(rawResp)
					if err != nil {
						t.Fatal(err)
					}
					if rawResp.StatusCode != tt.expectedHTTPStatus {
						t.Fatalf("%s: 期望 HTTP %d，实际 %d，body=%q", tt.name, tt.expectedHTTPStatus, rawResp.StatusCode, string(body))
					}
					assertEmptyJSONStringBody(t, tt.name, body)
					return
				}

				experimentId := resolveSimulationExperimentRef(tt.experimentIdRef, expId)
				resp, err := testClient.Primary().SimulationRecordTerminateV3(experimentId)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				tt.assertResponse(t, resp)
				if tt.waitTerminateFinished && resp.Msg == "停止中" {
					// 终止接口异步下发停止请求，返回“停止中”后必须继续通过 poll 等到状态 4。
					if err := simulationhelpers.WaitTerminateFinished(testClient, testPackageId, testModelName, experimentId, tt.name); err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	})
}

func TestSetRealtimeFmuVariables(t *testing.T) {
	assert := helpers.NewAssertResponse(t)
	var fixture realtimeSimulationFixture

	tests := []struct {
		name               string
		experimentId       string
		variables          []string
		useRaw             bool
		rawBody            any
		expectedHTTPStatus int
		assertResponse     func(t *testing.T, resp *helpers.Response)
	}{
		{
			name:         "正常设置实时曲线变量 driveAngle",
			experimentId: "fixture",
			variables:    []string{"time", "driveAngle"},
			assertResponse: func(t *testing.T, resp *helpers.Response) {
				assert.IsSuccess(resp, "正常设置实时曲线变量 driveAngle")
				if resp.Msg != "实时曲线变量设置成功" {
					t.Errorf("正常设置实时曲线变量 driveAngle: 期望 msg=%q，实际=%q", "实时曲线变量设置成功", resp.Msg)
				}
				dataMap, err := helpers.ObjectData(resp, "正常设置实时曲线变量 driveAngle")
				if err != nil {
					t.Fatal(err)
				}
				if dataMap["experiment_id"] != fixture.ExperimentID {
					t.Errorf("正常设置实时曲线变量 driveAngle: experiment_id 不匹配，actual=%v want=%s", dataMap["experiment_id"], fixture.ExperimentID)
				}
				assertStringListContains(t, "正常设置实时曲线变量 driveAngle", dataMap["subscribed_variables"], "driveAngle")
			},
		},
		{
			name:               "缺少 variables 返回 HTTP 400",
			useRaw:             true,
			rawBody:            map[string]any{"experiment_id": "non-existent-experiment-id"},
			expectedHTTPStatus: http.StatusBadRequest,
		},
		{
			name:         "实验不存在返回 status=2",
			experimentId: "non-existent-experiment-id",
			variables:    []string{"time"},
			assertResponse: func(t *testing.T, resp *helpers.Response) {
				assert.IsError(resp, 2, "实验不存在返回 status=2")
				if resp.Err != "实验记录不存在" {
					t.Errorf("实验不存在返回 status=2: 期望 err=%q，实际=%q", "实验记录不存在", resp.Err)
				}
			},
		},
		{
			name:               "请求体类型错误返回 HTTP 400",
			useRaw:             true,
			rawBody:            []string{"unexpected"},
			expectedHTTPStatus: http.StatusBadRequest,
		},
		{
			name:               "缺少 experiment_id 返回 HTTP 400",
			useRaw:             true,
			rawBody:            map[string]any{"variables": []string{"time"}},
			expectedHTTPStatus: http.StatusBadRequest,
		},
	}

	helpers.RunWithHooks(t, helpers.TestHooks{
		Before: func(t *testing.T) {
			fixture = createRealtimeSimulationFixture(t, "rt_vars")
			if err := simulationhelpers.WaitSimulationRunning(testClient, testPackageId, testModelName, fixture.ExperimentID, "api-test-realtime", "实时曲线变量夹具", 45*time.Second); err != nil {
				t.Fatal(err)
			}
		},
		After: func(t *testing.T) {
			cleanupRealtimeSimulationFixture(t, fixture)
		},
	}, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				if tt.useRaw {
					rawResp, err := testClient.Primary().SetRealtimeFmuVariablesRaw(tt.rawBody)
					if err != nil {
						t.Fatalf("请求失败: %v", err)
					}
					body, err := helpers.ReadAndCloseRawBody(rawResp)
					if err != nil {
						t.Fatal(err)
					}
					if rawResp.StatusCode != tt.expectedHTTPStatus {
						t.Fatalf("%s: 期望 HTTP %d，实际 %d，body=%q", tt.name, tt.expectedHTTPStatus, rawResp.StatusCode, string(body))
					}
					assertEmptyJSONStringBody(t, tt.name, body)
					return
				}

				experimentID := resolveRealtimeExperimentRef(tt.experimentId, fixture.ExperimentID)
				resp, err := testClient.Primary().SetRealtimeFmuVariables(experimentID, tt.variables)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				tt.assertResponse(t, resp)
			})
		}
	})
}

func TestSetRealtimeFmuParameters(t *testing.T) {
	assert := helpers.NewAssertResponse(t)
	var fixture realtimeSimulationFixture

	tests := []struct {
		name               string
		experimentId       string
		parameters         []map[string]any
		useRaw             bool
		rawBody            any
		expectedHTTPStatus int
		assertResponse     func(t *testing.T, resp *helpers.Response)
	}{
		{
			name:         "正常设置 driveAngle 参数",
			experimentId: "fixture",
			parameters: []map[string]any{
				{"name": "driveAngle", "value": "1.0"},
			},
			assertResponse: func(t *testing.T, resp *helpers.Response) {
				assert.IsSuccess(resp, "正常设置 driveAngle 参数")
				if resp.Msg != "参数修改指令已提交" {
					t.Errorf("正常设置 driveAngle 参数: 期望 msg=%q，实际=%q", "参数修改指令已提交", resp.Msg)
				}
				dataMap, err := helpers.ObjectData(resp, "正常设置 driveAngle 参数")
				if err != nil {
					t.Fatal(err)
				}
				if dataMap["experiment_id"] != fixture.ExperimentID {
					t.Errorf("正常设置 driveAngle 参数: experiment_id 不匹配，actual=%v want=%s", dataMap["experiment_id"], fixture.ExperimentID)
				}
				assertStringListContains(t, "正常设置 driveAngle 参数", dataMap["accepted"], "driveAngle")

				rawResp, err := testClient.GetRealtimeFmuStreamRaw(fixture.ExperimentID, fixture.TaskID, "")
				if err != nil {
					t.Fatalf("正常设置 driveAngle 参数: 建立实时 SSE 连接失败: %v", err)
				}
				defer func() { _ = rawResp.Body.Close() }()
				if rawResp.StatusCode != http.StatusOK {
					t.Fatalf("正常设置 driveAngle 参数: SSE 期望 HTTP 200，实际=%d", rawResp.StatusCode)
				}
				helpers.AssertSSEContentType(t, rawResp, "正常设置 driveAngle 参数")
				sample := readRealtimeSampleUntil(t, rawResp, fixture.ExperimentID, realtimeSimulationStopTime, func(sample realtimeSampleEvent) bool {
					_, ok := sample.Values["driveAngle"]
					return ok
				})
				if _, ok := helpers.NumberAsFloat64(sample.Values["driveAngle"]); !ok {
					t.Fatalf("正常设置 driveAngle 参数: SSE values.driveAngle 不是数字，actual=%T %v", sample.Values["driveAngle"], sample.Values["driveAngle"])
				}
			},
		},
		{
			name:         "实验不存在返回 status=2",
			experimentId: "non-existent-experiment-id",
			parameters: []map[string]any{
				{"name": "k", "value": "1"},
			},
			assertResponse: func(t *testing.T, resp *helpers.Response) {
				assert.IsError(resp, 2, "实验不存在返回 status=2")
				if resp.Err != "实验记录不存在" {
					t.Errorf("实验不存在返回 status=2: 期望 err=%q，实际=%q", "实验记录不存在", resp.Err)
				}
			},
		},
		{
			name:               "请求体类型错误返回 HTTP 400",
			useRaw:             true,
			rawBody:            []string{"unexpected"},
			expectedHTTPStatus: http.StatusBadRequest,
		},
		{
			name:               "缺少 experiment_id 返回 HTTP 400",
			useRaw:             true,
			rawBody:            map[string]any{"parameters": []map[string]any{{"name": "k", "value": "1"}}},
			expectedHTTPStatus: http.StatusBadRequest,
		},
		{
			name:               "parameters 字段类型错误返回 HTTP 400",
			useRaw:             true,
			rawBody:            map[string]any{"experiment_id": "non-existent-experiment-id", "parameters": "unexpected"},
			expectedHTTPStatus: http.StatusBadRequest,
		},
	}

	helpers.RunWithHooks(t, helpers.TestHooks{
		Before: func(t *testing.T) {
			fixture = createRealtimeSimulationFixture(t, "rt_params")
			if err := simulationhelpers.WaitSimulationRunning(testClient, testPackageId, testModelName, fixture.ExperimentID, "api-test-realtime", "实时参数夹具", 45*time.Second); err != nil {
				t.Fatal(err)
			}
			var err error
			fixture.TaskID, err = simulationhelpers.SubscribeRealtimeVariables(testClient, fixture.ExperimentID, []string{"time", "driveAngle"}, "实时参数夹具", 20*time.Second)
			if err != nil {
				t.Fatal(err)
			}
			if err := simulationhelpers.WaitRealtimeSampleStreamAvailable(fixture.TaskID, "实时参数夹具", 20*time.Second); err != nil {
				t.Fatal(err)
			}
		},
		After: func(t *testing.T) {
			cleanupRealtimeSimulationFixture(t, fixture)
		},
	}, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				if tt.useRaw {
					rawResp, err := testClient.Primary().SetRealtimeFmuParametersRaw(tt.rawBody)
					if err != nil {
						t.Fatalf("请求失败: %v", err)
					}
					body, err := helpers.ReadAndCloseRawBody(rawResp)
					if err != nil {
						t.Fatal(err)
					}
					if rawResp.StatusCode != tt.expectedHTTPStatus {
						t.Fatalf("%s: 期望 HTTP %d，实际 %d，body=%q", tt.name, tt.expectedHTTPStatus, rawResp.StatusCode, string(body))
					}
					assertEmptyJSONStringBody(t, tt.name, body)
					return
				}

				experimentID := resolveRealtimeExperimentRef(tt.experimentId, fixture.ExperimentID)
				resp, err := testClient.Primary().SetRealtimeFmuParameters(experimentID, tt.parameters)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				tt.assertResponse(t, resp)
			})
		}
	})
}

func TestGetRealtimeFmuStream(t *testing.T) {
	assert := helpers.NewAssertResponse(t)
	var fixture realtimeSimulationFixture

	tests := []struct {
		name              string
		experimentID      string
		wantSuccess       bool
		wantErrContaining string
	}{
		{name: "正常读取实时仿真 SSE 至结束时间", experimentID: "fixture", wantSuccess: true},
		{name: "experiment_id 为空返回 status=2", experimentID: "", wantErrContaining: "experiment_id"},
		{name: "不存在的 experiment_id 返回 status=2", experimentID: "non-existent-experiment-id", wantErrContaining: "实验记录不存在"},
	}

	helpers.RunWithHooks(t, helpers.TestHooks{
		Before: func(t *testing.T) {
			fixture = createRealtimeSimulationFixture(t, "rt_stream")
			if err := simulationhelpers.WaitSimulationRunning(testClient, testPackageId, testModelName, fixture.ExperimentID, "api-test-realtime", "实时 SSE 夹具", 45*time.Second); err != nil {
				t.Fatal(err)
			}
			var err error
			fixture.TaskID, err = simulationhelpers.SubscribeRealtimeVariables(testClient, fixture.ExperimentID, []string{"time", "inertia1.a"}, "实时 SSE 夹具", 20*time.Second)
			if err != nil {
				t.Fatal(err)
			}
		},
		After: func(t *testing.T) {
			cleanupRealtimeSimulationFixture(t, fixture)
		},
	}, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				experimentID := resolveRealtimeExperimentRef(tt.experimentID, fixture.ExperimentID)
				if tt.wantSuccess {
					if err := simulationhelpers.WaitRealtimeSampleStreamAvailable(fixture.TaskID, "实时 SSE 夹具", 20*time.Second); err != nil {
						t.Fatal(err)
					}
					rawResp, err := testClient.Primary().GetRealtimeFmuStreamRaw(experimentID, fixture.TaskID, "")
					if err != nil {
						t.Fatalf("请求失败: %v", err)
					}
					defer func() { _ = rawResp.Body.Close() }()
					if rawResp.StatusCode != http.StatusOK {
						t.Fatalf("%s: 期望 HTTP 200，实际=%d", tt.name, rawResp.StatusCode)
					}
					helpers.AssertSSEContentType(t, rawResp, tt.name)
					readRealtimeSampleUntil(t, rawResp, experimentID, realtimeSimulationStopTime, func(sample realtimeSampleEvent) bool {
						valuesTime, ok := helpers.NumberAsFloat64(sample.Values["time"])
						return ok && helpers.FloatEqual(valuesTime, realtimeSimulationStopTime, 1e-9)
					})
					return
				}

				resp, err := testClient.Primary().GetRealtimeFmuStream(experimentID, "", "")
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				assert.IsError(resp, 2, tt.name)
				if tt.wantErrContaining != "" && !strings.Contains(resp.Err, tt.wantErrContaining) {
					t.Fatalf("%s: 期望 err 包含 %q，实际=%q", tt.name, tt.wantErrContaining, resp.Err)
				}
			})
		}
	})
}

func TestSimulationExperimentExists(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	tests := []struct {
		name               string
		packageId          string
		modelName          string
		simulateVarData    map[string]string
		useRaw             bool
		rawBody            any
		expectedHTTPStatus int
		assertResponse     func(t *testing.T, resp *helpers.Response)
	}{
		{
			name:            "匹配现有实验配置当前返回空 data",
			packageId:       testPackageId,
			modelName:       testModelName,
			simulateVarData: simulationhelpers.DefaultSimulateVarData(),
			assertResponse: func(t *testing.T, resp *helpers.Response) {
				assert.IsSuccess(resp, "匹配现有实验配置当前返回空 data")
				assert.ValueIsStructurallyEmpty(resp.Data, "匹配现有实验配置当前返回空 data")
			},
		},
		{
			name:            "不存在的模型当前返回空 data",
			packageId:       "non-existent-package-id",
			modelName:       "Non.Existent.Model",
			simulateVarData: simulationhelpers.DefaultSimulateVarData(),
			assertResponse: func(t *testing.T, resp *helpers.Response) {
				assert.IsSuccess(resp, "不存在的模型当前返回空 data")
				assert.ValueIsStructurallyEmpty(resp.Data, "不存在的模型当前返回空 data")
			},
		},
		{
			name:               "缺少 package_id 返回 HTTP 400",
			useRaw:             true,
			rawBody:            map[string]any{"model_name": testModelName, "simulate_var_data": simulationhelpers.DefaultSimulateVarData()},
			expectedHTTPStatus: http.StatusBadRequest,
		},
	}

	helpers.RunWithHooks(t, helpers.TestHooks{}, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				if tt.useRaw {
					rawResp, err := testClient.Primary().ExperimentExistsRaw(tt.rawBody)
					if err != nil {
						t.Fatalf("请求失败: %v", err)
					}
					body, err := helpers.ReadAndCloseRawBody(rawResp)
					if err != nil {
						t.Fatal(err)
					}
					if rawResp.StatusCode != tt.expectedHTTPStatus {
						t.Fatalf("%s: 期望 HTTP %d，实际 %d，body=%q", tt.name, tt.expectedHTTPStatus, rawResp.StatusCode, string(body))
					}
					assertEmptyJSONStringBody(t, tt.name, body)
					return
				}

				resp, err := testClient.Primary().ExperimentExists(tt.packageId, tt.modelName, tt.simulateVarData, nil)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				tt.assertResponse(t, resp)
			})
		}
	})
}

func TestSimulationRecordDetails(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var expId string
	var expectedRecord DataBase.YssimExperimentRecord
	var err error

	tests := []struct {
		name           string
		recordId       string
		assertResponse func(t *testing.T, resp *helpers.Response)
	}{
		{
			name:     "正常获取仿真结果详情",
			recordId: "fixture",
			assertResponse: func(t *testing.T, resp *helpers.Response) {
				assert.IsSuccess(resp, "正常获取仿真结果详情")
				dataMap, err := helpers.ObjectData(resp, "仿真结果详情")
				if err != nil {
					t.Fatal(err)
				}
				if dataMap["start_time"] != expectedRecord.StartTime {
					t.Errorf("正常获取仿真结果详情: 期望 start_time=%s，实际=%v", expectedRecord.StartTime, dataMap["start_time"])
				}
				if dataMap["stop_time"] != expectedRecord.StopTime {
					t.Errorf("正常获取仿真结果详情: 期望 stop_time=%s，实际=%v", expectedRecord.StopTime, dataMap["stop_time"])
				}
				for _, field := range []string{"step_size", "tolerance", "solver", "method", "number_intervals", "model_var_data"} {
					if _, ok := dataMap[field]; !ok {
						t.Errorf("正常获取仿真结果详情: 缺少字段 %q", field)
					}
				}
			},
		},
		{
			name:     "不存在的 id 返回空详情对象",
			recordId: "non-existent-record-id",
			assertResponse: func(t *testing.T, resp *helpers.Response) {
				assert.IsSuccess(resp, "不存在的 id 返回空详情对象")
				dataMap, err := helpers.ObjectData(resp, "仿真结果详情")
				if err != nil {
					t.Fatal(err)
				}
				assertSimulationRecordDetailsEmpty(t, "不存在的 id 返回空详情对象", dataMap)
			},
		},
	}

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			expId = createFinishedSimulationExperiment(t, "detail_"+time.Now().Format("150405000"))
			expectedRecord, err = simulationhelpers.GetExperimentRecord(expId)
			if err != nil {
				t.Fatal(err)
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
				recordId := tt.recordId
				if recordId == "fixture" {
					recordId = expId
				}
				resp, err := testClient.Primary().GetSimulationResult(recordId)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				tt.assertResponse(t, resp)
			})
		}
	})
}

func TestDeleteSimulationRecord(t *testing.T) {
	t.Skip("跳过旧版 /simulation/record/delete 接口；仿真相关测试统一使用 v3 接口，v3 结果清理由 TestExperimentCleanup 覆盖")
}

func TestSimulationLogGetV2(t *testing.T) {
	helpers.RunWithHooks(t, helpers.TestHooks{}, func(t *testing.T) {
		rawResp, err := testClient.Primary().GetRawResponse("/api/v2/simulation/log/get", nil)
		if err != nil {
			t.Fatalf("请求失败: %v", err)
		}
		body, err := helpers.ReadAndCloseRawBody(rawResp)
		if err != nil {
			t.Fatal(err)
		}
		if rawResp.StatusCode != http.StatusNotFound {
			t.Fatalf("当前未挂载轮巡日志路由: 期望 HTTP 404，实际 %d，body=%q", rawResp.StatusCode, string(body))
		}
	})
}

func TestGetSimulationOptions(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	tests := []struct {
		name               string
		modelName          string
		wantDefaultOptions bool
	}{
		{
			name:      "正常获取已加载模型的仿真选项",
			modelName: testModelName,
		},
		{
			name:               "model_name 为空时返回默认仿真参数",
			modelName:          "",
			wantDefaultOptions: true,
		},
		{
			name:               "不存在的模型当前返回默认仿真参数",
			modelName:          "NonExistent.Model",
			wantDefaultOptions: true,
		},
	}

	helpers.RunWithHooks(t, helpers.TestHooks{
		Before: func(t *testing.T) {
			if err := modelhelpers.EnsureSystemModelLoaded(testClient, modelicaStdLibId, "", "仿真参数获取前置加载 Modelica"); err != nil {
				t.Fatal(err)
			}
		},
	}, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				resp, err := testClient.Primary().GetSimulationOptions(tt.modelName)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}

				assert.IsSuccess(resp, tt.name)
				dataMap, err := helpers.ObjectData(resp, "仿真参数")
				if err != nil {
					t.Fatal(err)
				}
				for _, field := range []string{"startTime", "stopTime", "tolerance", "numberOfIntervals", "interval", "method", "simulate_type"} {
					if _, ok := dataMap[field]; !ok {
						t.Fatalf("%s: 返回结果缺少字段 %q", tt.name, field)
					}
				}
				if tt.wantDefaultOptions {
					assertSimulationOptionsDefaultValues(t, tt.name, dataMap)
					return
				}
				if dataMap["startTime"] == "" || dataMap["stopTime"] == "" {
					t.Errorf("%s: startTime/stopTime 不应为空，actual start=%v stop=%v", tt.name, dataMap["startTime"], dataMap["stopTime"])
				}
			})
		}
	})
}

func TestSetSimulationOptions(t *testing.T) {
	tests := []struct {
		name               string
		body               map[string]any
		expectedHTTPStatus int
		wantEmptyBody      bool
	}{
		{
			name:               "有效请求当前返回空响应体",
			expectedHTTPStatus: http.StatusOK,
			wantEmptyBody:      true,
			body: map[string]any{
				"package_id":        modelicaStdLibId,
				"model_name":        testModelName,
				"startTime":         "0",
				"stopTime":          "2",
				"tolerance":         "1e-6",
				"numberOfIntervals": "500",
				"interval":          "0.004",
				"method":            "Dassl",
				"simulate_type":     "DM",
			},
		},
		{
			name:               "缺少 package_id 返回 HTTP 400",
			expectedHTTPStatus: http.StatusBadRequest,
			body: map[string]any{
				"model_name":        testModelName,
				"startTime":         "0",
				"stopTime":          "2",
				"tolerance":         "1e-6",
				"numberOfIntervals": "500",
				"interval":          "0.004",
				"method":            "Dassl",
				"simulate_type":     "DM",
			},
		},
		{
			name:               "缺少 model_name 返回 HTTP 400",
			expectedHTTPStatus: http.StatusBadRequest,
			body: map[string]any{
				"package_id":        modelicaStdLibId,
				"startTime":         "0",
				"stopTime":          "2",
				"tolerance":         "1e-6",
				"numberOfIntervals": "500",
				"interval":          "0.004",
				"method":            "Dassl",
				"simulate_type":     "DM",
			},
		},
	}

	helpers.RunWithHooks(t, helpers.TestHooks{}, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				rawResp, err := testClient.Primary().SetSimulationOptionsRaw(tt.body)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				body, err := helpers.ReadAndCloseRawBody(rawResp)
				if err != nil {
					t.Fatal(err)
				}
				if rawResp.StatusCode != tt.expectedHTTPStatus {
					t.Fatalf("%s: 期望 HTTP %d，实际 %d，body=%q", tt.name, tt.expectedHTTPStatus, rawResp.StatusCode, string(body))
				}
				if tt.wantEmptyBody {
					assertRawBodyEmpty(t, tt.name, body)
					return
				}
				assertEmptyJSONStringBody(t, tt.name, body)
			})
		}
	})
}

func TestGetAllSimulationStateV2(t *testing.T) {
	t.Skip("/api/v2/simulation/state/get/all 已弃用；全空间仿真状态改由 poll 接口覆盖")
}

func TestGetExperimentCleanupStats(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var expId string

	tests := []struct {
		name               string
		soonerThan         string
		useRaw             bool
		expectedHTTPStatus int
		assertResponse     func(t *testing.T, resp *helpers.Response)
		assertRawBody      func(t *testing.T, body []byte)
	}{
		{
			name:       "正常获取待删除结果记录统计",
			soonerThan: "36500",
			assertResponse: func(t *testing.T, resp *helpers.Response) {
				assert.IsSuccess(resp, "正常获取待删除结果记录统计")
				dataMap, err := helpers.ObjectData(resp, "待删除结果统计")
				if err != nil {
					t.Fatal(err)
				}
				deleteCount, ok := dataMap["delete_count"].(float64)
				if !ok {
					t.Fatalf("正常获取待删除结果记录统计: delete_count 不是数字: %T", dataMap["delete_count"])
				}
				totalCount, ok := dataMap["total_count"].(float64)
				if !ok {
					t.Fatalf("正常获取待删除结果记录统计: total_count 不是数字: %T", dataMap["total_count"])
				}
				if deleteCount != 0 {
					t.Errorf("正常获取待删除结果记录统计: 期望 delete_count=0，实际=%v", deleteCount)
				}
				if totalCount < 1 {
					t.Errorf("正常获取待删除结果记录统计: 期望 total_count>=1，实际=%v", totalCount)
				}
			},
		},
		{
			name:               "缺少 sooner_than 返回 HTTP 400",
			useRaw:             true,
			expectedHTTPStatus: http.StatusBadRequest,
			assertRawBody: func(t *testing.T, body []byte) {
				assertJSONStringBodyEquals(t, "缺少 sooner_than 返回 HTTP 400", body, "时间格式错误")
			},
		},
		{
			name:               "sooner_than 非数字返回 HTTP 400",
			soonerThan:         "not-a-number",
			useRaw:             true,
			expectedHTTPStatus: http.StatusBadRequest,
			assertRawBody: func(t *testing.T, body []byte) {
				assertJSONStringBodyEquals(t, "sooner_than 非数字返回 HTTP 400", body, "时间格式错误")
			},
		},
	}

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			expId = createFinishedSimulationExperiment(t, "cleanupstats_"+time.Now().Format("150405000"))
		},
		After: func(t *testing.T) {
			simulationhelpers.DeleteExperiment(testClient, expId)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				if tt.useRaw {
					rawResp, err := testClient.Primary().GetExperimentCleanupStatsRaw(map[string]string{"sooner_than": tt.soonerThan})
					if err != nil {
						t.Fatalf("请求失败: %v", err)
					}
					body, err := helpers.ReadAndCloseRawBody(rawResp)
					if err != nil {
						t.Fatal(err)
					}
					if rawResp.StatusCode != tt.expectedHTTPStatus {
						t.Fatalf("%s: 期望 HTTP %d，实际 %d，body=%q", tt.name, tt.expectedHTTPStatus, rawResp.StatusCode, string(body))
					}
					if tt.assertRawBody != nil {
						tt.assertRawBody(t, body)
					}
					return
				}

				resp, err := testClient.Primary().GetExperimentCleanupStats(tt.soonerThan)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				tt.assertResponse(t, resp)
			})
		}
	})
}

func TestExperimentCleanup(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var expId string
	var batchId string
	var caseCount int64

	tests := []struct {
		name               string
		body               any
		useRaw             bool
		expectedHTTPStatus int
		assertResponse     func(t *testing.T, resp *helpers.Response)
		assertRawBody      func(t *testing.T, body []byte)
	}{
		{
			name: "无匹配结果时删除成功且不影响当前实验记录",
			body: map[string]any{
				"sooner_than": "36500",
			},
			assertResponse: func(t *testing.T, resp *helpers.Response) {
				assert.IsSuccess(resp, "无匹配结果时删除成功且不影响当前实验记录")
				if resp.Msg != "删除成功" {
					t.Errorf("无匹配结果时删除成功且不影响当前实验记录: 期望 msg=删除成功，实际=%q", resp.Msg)
				}
				if gotBatchID := getExperimentRecordTaskBatchID(t, expId); gotBatchID != batchId {
					t.Fatalf("无匹配结果时删除成功且不影响当前实验记录: task_batch_id 被意外修改，actual=%q want=%q", gotBatchID, batchId)
				}
				if gotCaseCount := countExperimentCasesByBatch(t, expId, batchId); gotCaseCount != caseCount {
					t.Fatalf("无匹配结果时删除成功且不影响当前实验记录: case 数量发生变化，actual=%d want=%d", gotCaseCount, caseCount)
				}
			},
		},
		{
			name:               "缺少 sooner_than 返回 HTTP 400",
			useRaw:             true,
			body:               map[string]any{},
			expectedHTTPStatus: http.StatusBadRequest,
			assertRawBody: func(t *testing.T, body []byte) {
				assertEmptyJSONStringBody(t, "缺少 sooner_than 返回 HTTP 400", body)
			},
		},
		{
			name:               "sooner_than 非数字返回 HTTP 400",
			useRaw:             true,
			body:               map[string]any{"sooner_than": "not-a-number"},
			expectedHTTPStatus: http.StatusBadRequest,
			assertRawBody: func(t *testing.T, body []byte) {
				assertJSONStringBodyEquals(t, "sooner_than 非数字返回 HTTP 400", body, "时间格式错误")
			},
		},
	}

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			expId = createFinishedSimulationExperiment(t, "cleanup_"+time.Now().Format("150405000"))
			batchId = getExperimentRecordTaskBatchID(t, expId)
			if batchId == "" {
				t.Fatalf("清理结果测试前置失败: task_batch_id 为空")
			}
			caseCount = countExperimentCasesByBatch(t, expId, batchId)
			if caseCount < 1 {
				t.Fatalf("清理结果测试前置失败: case 数量为空")
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
				if tt.useRaw {
					rawResp, err := testClient.Primary().ExperimentCleanupRaw(tt.body)
					if err != nil {
						t.Fatalf("请求失败: %v", err)
					}
					body, err := helpers.ReadAndCloseRawBody(rawResp)
					if err != nil {
						t.Fatal(err)
					}
					if rawResp.StatusCode != tt.expectedHTTPStatus {
						t.Fatalf("%s: 期望 HTTP %d，实际 %d，body=%q", tt.name, tt.expectedHTTPStatus, rawResp.StatusCode, string(body))
					}
					if tt.assertRawBody != nil {
						tt.assertRawBody(t, body)
					}
					return
				}

				resp, err := testClient.Primary().ExperimentCleanup(tt.body.(map[string]any)["sooner_than"].(string))
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				tt.assertResponse(t, resp)
			})
		}
	})
}

func resolveSimulationExperimentRef(ref, expId string) string {
	switch ref {
	case "", "fixture":
		return expId
	case "empty":
		return ""
	default:
		return ref
	}
}

func assertWorkspacePollStatus(t *testing.T, name string, status workspacehelpers.TaskStatus) {
	t.Helper()

	if status.Status != 2 && status.Status != 3 && status.Status != 4 {
		t.Fatalf("%s: status 非法，实际=%d raw=%v", name, status.Status, status.Raw)
	}
	if _, ok := status.Raw["task_id"]; !ok {
		t.Fatalf("%s: 缺少 task_id 字段: %v", name, status.Raw)
	}
	if _, ok := status.Raw["package_id"]; !ok {
		t.Fatalf("%s: 缺少 package_id 字段: %v", name, status.Raw)
	}
	if _, ok := status.Raw["model"]; !ok {
		t.Fatalf("%s: 缺少 model 字段: %v", name, status.Raw)
	}
}

func waitForSimulationRecordListVisible(t *testing.T, packageId, modelName, experimentId string) map[string]any {
	t.Helper()

	deadline := time.Now().Add(120 * time.Second)
	t.Logf("开始轮询结果接口，等待服务端读取 MAT: experiment_id=%s candidates=%v", experimentId, simulationResultSmokeVariables)
	var lastList []any

	for time.Now().Before(deadline) {
		resp, err := testClient.GetSimulationRecordListV2(packageId, modelName, 1)
		if err != nil {
			t.Fatalf("查询仿真记录列表失败: %v", err)
		}
		if resp.Status != 0 {
			t.Fatalf("查询仿真记录列表返回非零状态: status=%d err=%s", resp.Status, resp.Err)
		}
		list := extractSimulationRecordListItems(t, resp)
		lastList = list
		if item := findSimulationRecordByExperimentID(list, experimentId); item != nil {
			return item
		}
		time.Sleep(2 * time.Second)
	}

	t.Fatalf("在限定时间内未等到实验 %q 出现在仿真记录列表中，最后列表=%v", experimentId, lastList)
	return nil
}

func extractSimulationRecordListData(t *testing.T, resp *helpers.Response) map[string]any {
	t.Helper()

	dataMap, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("仿真记录列表接口返回 data 不是 object: %T", resp.Data)
	}
	return dataMap
}

func extractSimulationRecordListItems(t *testing.T, resp *helpers.Response) []any {
	t.Helper()

	dataMap := extractSimulationRecordListData(t, resp)
	listVal, ok := dataMap["resultList"]
	if !ok || listVal == nil {
		return []any{}
	}
	list, ok := listVal.([]any)
	if !ok {
		t.Fatalf("仿真记录列表中的 resultList 不是数组: %T", listVal)
	}
	return list
}

func findSimulationRecordByExperimentID(list []any, experimentId string) map[string]any {
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if m["id"] == experimentId {
			return m
		}
	}
	return nil
}

func extractSimulationStateData(t *testing.T, resp *helpers.Response) map[string]any {
	t.Helper()

	dataMap, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("仿真状态接口返回 data 不是 object: %T", resp.Data)
	}
	return dataMap
}

func assertTerminateResponseMessage(t *testing.T, name, msg string) {
	t.Helper()

	if msg == "停止中" || msg == "仿真已终止" {
		return
	}
	t.Errorf("%s: 期望 msg 为停止中或仿真已终止，实际=%q", name, msg)
}
func assertEmptyJSONStringBody(t *testing.T, name string, body []byte) {
	t.Helper()

	var got string
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("%s: 响应体不是 JSON string: %q, err=%v", name, string(body), err)
	}
	if got != "" {
		t.Errorf("%s: 期望空字符串响应体，实际=%q", name, got)
	}
}

func createFinishedSimulationExperiment(t *testing.T, experimentName string) string {
	t.Helper()

	if _, err := testClient.ExperimentGet(testPackageId, testModelName); err != nil {
		t.Fatalf("预热实验列表失败: %v", err)
	}

	expId, _, err := simulationhelpers.CreateExperiment(testClient, simulationhelpers.ExperimentOptions{PackageID: testPackageId, ModelName: testModelName, NamePrefix: experimentName})
	if err != nil {
		t.Fatal(err)
	}
	resp, err := testClient.ModelSimulateWithTranslate(expId)
	if err != nil {
		t.Fatalf("提交 v4 仿真任务失败: %v", err)
	}
	if resp.Status != 0 {
		t.Fatalf("提交 v4 仿真任务返回非零状态: status=%d err=%s", resp.Status, resp.Err)
	}
	// 统一使用 poll 接口等待完成，避免旧 finished-list 接口把实时仿真中的记录误判为完成。
	if err := simulationhelpers.WaitWithTranslateFinished(testClient, testPackageId, testModelName, expId, "仿真实验 "+expId); err != nil {
		t.Fatal(err)
	}
	return expId
}

// getSimulationResultCasePaths 返回当前实验 case 记录中的结果目录，便于定位服务端结果接口为何读不到 MAT。
func getSimulationResultCasePaths(t *testing.T, experimentId string) []string {
	t.Helper()

	var experiment DataBase.YssimExperimentRecord
	if err := config.DB.Select("id", "task_batch_id", "simulate_model_result_path").Where("id = ?", experimentId).First(&experiment).Error; err != nil {
		return nil
	}

	var taskCases []DataBase.YssimExperimentCase
	if err := config.DB.Select("simulate_model_result_path").Where("experiment_id = ? AND task_batch_id = ?", experiment.ID, experiment.TaskBatchId).Find(&taskCases).Error; err != nil {
		return []string{experiment.SimulateModelResultPath}
	}

	paths := make([]string, 0, len(taskCases)+1)
	for _, taskCase := range taskCases {
		if taskCase.SimulateModelResultPath != "" {
			paths = append(paths, taskCase.SimulateModelResultPath)
		}
	}
	if len(paths) == 0 && experiment.SimulateModelResultPath != "" {
		paths = append(paths, experiment.SimulateModelResultPath)
	}
	return paths
}

func assertEmptySimulationState(t *testing.T, name string, stateData map[string]any) {
	t.Helper()

	if stateData["id"] != "" {
		t.Errorf("%s: 期望 id 为空，实际=%v", name, stateData["id"])
	}
	if status, ok := stateData["status"].(float64); !ok || status != 4 {
		t.Errorf("%s: 期望 status=4，实际=%v", name, stateData["status"])
	}
	if stateData["simulate_status_msg"] != "结束" {
		t.Errorf("%s: 期望 simulate_status_msg=结束，实际=%v", name, stateData["simulate_status_msg"])
	}
}

// extractSimulationSingularVars 读取 SingularV2 单运行/实时仿真的顶层 vars。
// 在 git 提交 7b74286d 之后，SingularV2 接口会区分实验是多运行仿真还是单运行/实时仿真。
// 单运行结果不再返回 cases/case_list 包装结构。
func extractSimulationSingularVars(t *testing.T, item map[string]any, name string) []any {
	t.Helper()

	vars, ok := item["vars"].([]any)
	if !ok || len(vars) == 0 {
		t.Fatalf("%s: 期望顶层 vars 为非空数组，实际=%T %v", name, item["vars"], item["vars"])
	}
	return vars
}

func assertSeriesPair(t *testing.T, name string, abscissaVal, ordinateVal any) {
	t.Helper()

	abscissa, ok := abscissaVal.([]any)
	if !ok || len(abscissa) == 0 {
		t.Fatalf("%s: abscissa 不是非空数组: %T %v", name, abscissaVal, abscissaVal)
	}
	ordinate, ok := ordinateVal.([]any)
	if !ok || len(ordinate) == 0 {
		t.Fatalf("%s: ordinate 不是非空数组: %T %v", name, ordinateVal, ordinateVal)
	}
	if len(abscissa) != len(ordinate) {
		t.Errorf("%s: abscissa 与 ordinate 长度不一致，actual=%d/%d", name, len(abscissa), len(ordinate))
	}
}

func cloneSimulationResultSingularItems(items []map[string]any, expId, variableName string) []map[string]any {
	cloned := make([]map[string]any, 0, len(items))
	for _, item := range items {
		copied := make(map[string]any, len(item))
		for k, v := range item {
			copied[k] = v
		}
		if copied["id"] == "fixture" {
			copied["id"] = expId
		}
		if copied["variable"] == "fixture" {
			copied["variable"] = variableName
		}
		cloned = append(cloned, copied)
	}
	return cloned
}

func assertJSONStringBodyEquals(t *testing.T, name string, body []byte, want string) {
	t.Helper()

	var got string
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("%s: 响应体不是 JSON string: %q, err=%v", name, string(body), err)
	}
	if got != want {
		t.Errorf("%s: 期望响应体=%q，实际=%q", name, want, got)
	}
}

func waitForExperimentTaskBatchCreated(t *testing.T, experimentId string, timeout ...time.Duration) string {
	t.Helper()

	waitTimeout := 30 * time.Second
	if len(timeout) > 0 && timeout[0] > 0 {
		// simulate-with-translate 可能先编译再创建批次；终止类用例会显式传入更长等待时间。
		waitTimeout = timeout[0]
	}
	deadline := time.Now().Add(waitTimeout)
	var lastBatchId string
	var lastCaseCount int64

	for time.Now().Before(deadline) {
		var experiment DataBase.YssimExperimentRecord
		if err := config.DB.Select("id", "task_batch_id").Where("id = ?", experimentId).First(&experiment).Error; err != nil {
			t.Fatalf("查询实验记录失败: %v", err)
		}
		lastBatchId = experiment.TaskBatchId
		if experiment.TaskBatchId != "" {
			var count int64
			if err := config.DB.Model(&DataBase.YssimExperimentCase{}).Where("experiment_id = ? AND task_batch_id = ?", experimentId, experiment.TaskBatchId).Count(&count).Error; err != nil {
				t.Fatalf("查询实验 case 数量失败: %v", err)
			}
			lastCaseCount = count
			if count > 0 {
				return experiment.TaskBatchId
			}
		}
		time.Sleep(1 * time.Second)
	}

	t.Fatalf("在限定时间内未等到 experiment_id=%s 生成 task_batch_id/case，last_batch_id=%q last_case_count=%d", experimentId, lastBatchId, lastCaseCount)
	return ""
}

func getExperimentRecordTaskID(t *testing.T, experimentId string) string {
	t.Helper()

	var experiment DataBase.YssimExperimentRecord
	if err := config.DB.Select("id", "task_id").Where("id = ?", experimentId).First(&experiment).Error; err != nil {
		t.Fatalf("查询实验 task_id 失败: %v", err)
	}
	return experiment.TaskId
}

func getExperimentRecordTaskBatchID(t *testing.T, experimentId string) string {
	t.Helper()

	var experiment DataBase.YssimExperimentRecord
	if err := config.DB.Select("id", "task_batch_id").Where("id = ?", experimentId).First(&experiment).Error; err != nil {
		t.Fatalf("查询实验 task_batch_id 失败: %v", err)
	}
	return experiment.TaskBatchId
}

func countExperimentCasesByBatch(t *testing.T, experimentId, batchId string) int64 {
	t.Helper()

	var count int64
	if err := config.DB.Model(&DataBase.YssimExperimentCase{}).Where("experiment_id = ? AND task_batch_id = ?", experimentId, batchId).Count(&count).Error; err != nil {
		t.Fatalf("统计实验 case 数量失败: %v", err)
	}
	return count
}

func assertSimulationRecordDetailsEmpty(t *testing.T, name string, dataMap map[string]any) {
	t.Helper()

	for _, field := range []string{"start_time", "stop_time", "step_size", "tolerance", "solver", "method", "number_intervals", "model_var_data"} {
		if !helpers.IsStructurallyEmpty(dataMap[field]) {
			t.Errorf("%s: 期望字段 %s 为空，实际=%v", name, field, dataMap[field])
		}
	}
}

func assertSimulationOptionsDefaultValues(t *testing.T, name string, dataMap map[string]any) {
	t.Helper()

	if dataMap["startTime"] != "0" {
		t.Errorf("%s: 期望 startTime=0，实际=%v", name, dataMap["startTime"])
	}
	if dataMap["stopTime"] != "0.1" {
		t.Errorf("%s: 期望 stopTime=0.1，实际=%v", name, dataMap["stopTime"])
	}
	if dataMap["tolerance"] != "1e-6" {
		t.Errorf("%s: 期望 tolerance=1e-6，实际=%v", name, dataMap["tolerance"])
	}
	if dataMap["numberOfIntervals"] != "500" {
		t.Errorf("%s: 期望 numberOfIntervals=500，实际=%v", name, dataMap["numberOfIntervals"])
	}
	if dataMap["interval"] != "0.0002" {
		t.Errorf("%s: 期望 interval=0.0002，实际=%v", name, dataMap["interval"])
	}
	if dataMap["method"] != "dassl" {
		t.Errorf("%s: 期望 method=dassl，实际=%v", name, dataMap["method"])
	}
	if dataMap["simulate_type"] != "OM" {
		t.Errorf("%s: 期望 simulate_type=OM，实际=%v", name, dataMap["simulate_type"])
	}
}

func assertRawBodyEmpty(t *testing.T, name string, body []byte) {
	t.Helper()

	if strings.TrimSpace(string(body)) != "" {
		t.Fatalf("%s: 期望空响应体，实际=%q", name, string(body))
	}
}

type realtimeSimulationFixture struct {
	ExperimentID     string
	TaskID           string
	OriginalSettings helpers.UserSettings
}

type realtimeSampleEvent struct {
	ID           string         `json:"id"`
	ExperimentID string         `json:"experiment_id"`
	Time         float64        `json:"time"`
	Values       map[string]any `json:"values"`
}

func createRealtimeSimulationFixture(t *testing.T, namePrefix string) realtimeSimulationFixture {
	t.Helper()
	settingsResp, err := testClient.GetUserSettings()
	if err != nil {
		t.Fatalf("实时仿真夹具: 获取用户设置失败: %v", err)
	}
	settings, err := helpers.ParseUserSettings(settingsResp)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := testClient.SetUserSettingsFull(settings.GridDisplay, true); err != nil {
		t.Fatalf("实时仿真夹具: 开启 interactive 失败: %v", err)
	}

	simulateVarData := simulationhelpers.DefaultSimulateVarData()
	simulateVarData["stopTime"] = "2"
	simulateVarData["interval"] = "0.004"
	simulateVarData["interval"] = "0.004"
	experimentID, _, err := simulationhelpers.CreateExperiment(testClient, simulationhelpers.ExperimentOptions{
		PackageID:       testPackageId,
		ModelName:       testModelName,
		NamePrefix:      namePrefix,
		SimulateVarData: simulateVarData,
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, variableName := range []string{"Time", "driveAngle"} {
		resp, err := testClient.ExperimentExportVariableSet(experimentID, variableName, false)
		if err != nil {
			t.Fatalf("实时仿真夹具: 设置导出变量 %s 失败: %v", variableName, err)
		}
		if resp.Status != 0 {
			t.Fatalf("实时仿真夹具: 设置导出变量 %s 返回非成功状态: status=%d msg=%s err=%s", variableName, resp.Status, resp.Msg, resp.Err)
		}
	}

	options := simulationhelpers.DefaultExperimentOptionsData()
	options["stop_time"] = "2"
	options["interval"] = "0.004"
	optionsResp, err := testClient.ExperimentOptionsSet(experimentID, options)
	if err != nil {
		t.Fatalf("实时仿真夹具: 设置 2s 仿真选项失败: %v", err)
	}
	if optionsResp.Status != 0 {
		t.Fatalf("实时仿真夹具: 设置 2s 仿真选项返回非成功状态: status=%d msg=%s err=%s", optionsResp.Status, optionsResp.Msg, optionsResp.Err)
	}

	startResp, err := testClient.ModelSimulateWithTranslate(experimentID)
	if err != nil {
		t.Fatalf("实时仿真夹具: 启动实时仿真失败: %v", err)
	}
	if startResp.Status != 0 {
		t.Fatalf("实时仿真夹具: 启动实时仿真返回非成功状态: status=%d msg=%s err=%s", startResp.Status, startResp.Msg, startResp.Err)
	}

	return realtimeSimulationFixture{ExperimentID: experimentID, OriginalSettings: settings}
}

func cleanupRealtimeSimulationFixture(t *testing.T, fixture realtimeSimulationFixture) {
	t.Helper()
	if fixture.ExperimentID != "" {
		if err := simulationhelpers.WaitSimulationFinished(testClient, testPackageId, testModelName, fixture.ExperimentID, "api-test-realtime", "实时仿真夹具清理", 30*time.Second); err != nil {
			t.Logf("实时仿真夹具清理: 等待自然结束失败: %v", err)
		}
		simulationhelpers.DeleteExperiment(testClient, fixture.ExperimentID)
	}
	_, _ = testClient.SetUserSettingsFull(fixture.OriginalSettings.GridDisplay, false)
}

func readRealtimeSampleUntil(t *testing.T, rawResp *http.Response, experimentID string, stopTime float64, done func(realtimeSampleEvent) bool) realtimeSampleEvent {
	t.Helper()
	reader := bufio.NewReader(rawResp.Body)
	var previousTime float64 = -1
	var lastSample realtimeSampleEvent
	deadline := time.Now().Add(25 * time.Second)

	for time.Now().Before(deadline) {
		eventName, payload, err := helpers.ReadSSEEvent(reader)
		if err != nil {
			t.Fatalf("读取实时仿真 SSE 失败: %v", err)
		}
		switch eventName {
		case "heartbeat":
			continue
		case "realtime_error":
			t.Fatalf("实时仿真 SSE 返回错误事件: %s", string(payload))
		case "realtime_sample":
			var sample realtimeSampleEvent
			if err := json.Unmarshal(payload, &sample); err != nil {
				t.Fatalf("解析实时仿真 SSE 样本失败: %v, payload=%s", err, string(payload))
			}
			if sample.ExperimentID != experimentID {
				t.Fatalf("实时仿真 SSE experiment_id 不匹配: actual=%s want=%s", sample.ExperimentID, experimentID)
			}
			if sample.ID == "" {
				t.Fatalf("实时仿真 SSE 样本 id 为空: %+v", sample)
			}
			valuesTime, ok := helpers.NumberAsFloat64(sample.Values["time"])
			if !ok {
				t.Fatalf("实时仿真 SSE values.time 不是数字: %T %v", sample.Values["time"], sample.Values["time"])
			}
			if sample.Time != 0 && !helpers.FloatEqual(sample.Time, valuesTime, 1e-9) {
				t.Fatalf("实时仿真 SSE 顶层 time 与 values.time 不一致: top=%v values=%v", sample.Time, valuesTime)
			}
			if previousTime > valuesTime {
				t.Fatalf("实时仿真 SSE time 非单调递增: previous=%v current=%v", previousTime, valuesTime)
			}
			previousTime = valuesTime
			lastSample = sample
			if done(sample) {
				return sample
			}
			if helpers.FloatEqual(valuesTime, stopTime, 1e-9) {
				t.Fatalf("实时仿真 SSE 已到结束时间但目标条件未满足: stop=%v sample=%+v", stopTime, sample)
			}
			if valuesTime > stopTime && !helpers.FloatEqual(valuesTime, stopTime, 1e-9) {
				t.Fatalf("实时仿真 SSE 已越过结束时间但未观察到结束点: stop=%v current=%v", stopTime, valuesTime)
			}
		default:
			t.Fatalf("实时仿真 SSE 收到未知事件 %q: %s", eventName, string(payload))
		}
	}
	t.Fatalf("实时仿真 SSE 在超时前未读到目标样本，最后样本=%+v", lastSample)
	return realtimeSampleEvent{}
}

func resolveRealtimeExperimentRef(ref, fixtureID string) string {
	if ref == "fixture" {
		return fixtureID
	}
	return ref
}

func assertStringListContains(t *testing.T, contextName string, value any, expected string) {
	t.Helper()
	items, ok := value.([]any)
	if !ok {
		t.Fatalf("%s: 字段不是数组: %T %v", contextName, value, value)
	}
	for _, item := range items {
		if item == expected {
			return
		}
	}
	t.Fatalf("%s: 数组中未找到 %q，actual=%v", contextName, expected, items)
}
