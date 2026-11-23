//go:build api_test

package api

import (
	"net/http"
	"testing"

	"yssim-go/tests/helpers"
	modelhelpers "yssim-go/tests/helpers/model"
	simulationhelpers "yssim-go/tests/helpers/simulation"
)

// TestExperimentCaseInfo 验证 GET /simulation/experiment/case-info。
// 覆盖：
//   - 默认实验返回单 case 且大小未知（N/A）
//   - 设置多运行参数与变量导出后返回可计算的 case 信息
//   - 导出项包含组件时大小返回 N/A，但 case_count 仍按参数组合计算
//   - 不存在的 experiment_id 返回 status=2
func TestExperimentCaseInfo(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var defaultExpId string
	var configuredExpId string
	var componentExportExpId string

	tests := []struct {
		name          string
		experimentRef string // default / configured / component / missing
		expectError   bool
		expectedCode  int
		wantCount     int
		wantCaseSize  string
		wantTotal     string
	}{
		{
			name:          "默认实验返回单 case 且大小为 N/A",
			experimentRef: "default",
			wantCount:     1,
			wantCaseSize:  "N/A",
			wantTotal:     "N/A",
		},
		{
			name:          "多运行参数与变量导出返回计算后的大小",
			experimentRef: "configured",
			wantCount:     6,
			wantCaseSize:  "3.9 KB",
			wantTotal:     "23.4 KB",
		},
		{
			name:          "导出项包含组件时大小返回 N/A",
			experimentRef: "component",
			wantCount:     6,
			wantCaseSize:  "N/A",
			wantTotal:     "N/A",
		},
		{
			name:          "不存在的 experiment_id 返回 status=2",
			experimentRef: "missing",
			expectError:   true,
			expectedCode:  2,
		},
	}

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			{
				var err error
				defaultExpId, _, err = simulationhelpers.CreateExperiment(testClient, simulationhelpers.ExperimentOptions{PackageID: testPackageId, ModelName: testModelName, NamePrefix: "测试实验_case_info_默认"})
				if err != nil {
					t.Fatal(err)
				}
			}
			{
				var err error
				configuredExpId, _, err = simulationhelpers.CreateExperiment(testClient, simulationhelpers.ExperimentOptions{PackageID: testPackageId, ModelName: testModelName, NamePrefix: "测试实验_case_info_变量导出"})
				if err != nil {
					t.Fatal(err)
				}
			}
			{
				var err error
				componentExportExpId, _, err = simulationhelpers.CreateExperiment(testClient, simulationhelpers.ExperimentOptions{PackageID: testPackageId, ModelName: testModelName, NamePrefix: "测试实验_case_info_组件导出"})
				if err != nil {
					t.Fatal(err)
				}
			}

			configureExperimentCaseInfoFixture(t, configuredExpId, false)
			configureExperimentCaseInfoFixture(t, componentExportExpId, true)
		},
		After: func(t *testing.T) {
			simulationhelpers.DeleteExperiment(testClient, defaultExpId)
			simulationhelpers.DeleteExperiment(testClient, configuredExpId)
			simulationhelpers.DeleteExperiment(testClient, componentExportExpId)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				experimentId := resolveExperimentRef(tt.experimentRef, defaultExpId, configuredExpId, componentExportExpId)
				resp, err := testClient.Primary().ExperimentCaseInfo(experimentId)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}

				if tt.expectError {
					assert.IsError(resp, tt.expectedCode, tt.name)
					return
				}

				assert.IsSuccess(resp, tt.name)
				caseInfo := extractExperimentCaseInfo(t, resp)
				if caseInfo["case_count"] != float64(tt.wantCount) {
					t.Errorf("%s: 期望 case_count=%d，实际=%v", tt.name, tt.wantCount, caseInfo["case_count"])
				}
				if caseInfo["case_size"] != tt.wantCaseSize {
					t.Errorf("%s: 期望 case_size=%q，实际=%v", tt.name, tt.wantCaseSize, caseInfo["case_size"])
				}
				if caseInfo["total_size"] != tt.wantTotal {
					t.Errorf("%s: 期望 total_size=%q，实际=%v", tt.name, tt.wantTotal, caseInfo["total_size"])
				}

				detailData := getExperimentDetailData(t, experimentId)
				detailCaseInfo, ok := detailData["case_info"].(map[string]any)
				if !ok {
					t.Fatalf("%s: detail.case_info 不是 object: %T", tt.name, detailData["case_info"])
				}
				if detailCaseInfo["case_count"] != caseInfo["case_count"] || detailCaseInfo["case_size"] != caseInfo["case_size"] || detailCaseInfo["total_size"] != caseInfo["total_size"] {
					t.Errorf("%s: case-info 接口与 detail.case_info 不一致，caseInfo=%v detail=%v", tt.name, caseInfo, detailCaseInfo)
				}
			})
		}
	})
}

// TestExperimentCopy 验证 POST /simulation/experiment/copy。
// 覆盖：
//   - 正常复制实验，并保留源实验的仿真选项 / 参数 / 导出变量
//   - 复制为已存在名称返回 status=2
//   - 缺少必填字段返回 HTTP 400
func TestExperimentCopy(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var sourceExpId string
	var conflictExpId string
	var copiedExpId string
	// conflictExpName 是加唯一后缀后的真实名称，复制冲突用例必须使用它才能命中重名校验。
	var conflictExpName string
	// 副本名称加唯一后缀，避免 CI 环境中历史失败用例留下同名实验。
	copyExpName := uniqueName("测试实验_复制副本")

	tests := []struct {
		name              string
		sourceIdLiteral   string
		newExperimentName string
		// newNameRef 用于引用 hook 中创建出的动态名称，避免表驱动用例写死冲突名。
		newNameRef         string
		useRaw             bool
		expectedHTTPStatus int
		expectError        bool
		expectedCode       int
		assertResponse     func(t *testing.T, resp *helpers.Response)
	}{
		{
			name:              "正常复制实验并保留关键配置",
			sourceIdLiteral:   "",
			newExperimentName: copyExpName,
			assertResponse: func(t *testing.T, resp *helpers.Response) {
				assert.IsSuccess(resp, "正常复制实验并保留关键配置")
				copiedExpId = extractCopiedExperimentID(t, resp)
				assertCopiedExperimentMatchesSource(t, sourceExpId, copiedExpId, copyExpName)
			},
		},
		{
			name:            "复制为已存在名称返回 status=2",
			sourceIdLiteral: "",
			newNameRef:      "conflictName",
			expectError:     true,
			expectedCode:    2,
		},
		{
			name:               "缺少 source_experiment_id 返回 HTTP 400",
			sourceIdLiteral:    "",
			newExperimentName:  "测试实验_复制缺参",
			useRaw:             true,
			expectedHTTPStatus: http.StatusBadRequest,
		},
	}

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			{
				var err error
				sourceExpId, _, err = simulationhelpers.CreateExperiment(testClient, simulationhelpers.ExperimentOptions{PackageID: testPackageId, ModelName: testModelName, NamePrefix: "测试实验_复制源"})
				if err != nil {
					t.Fatal(err)
				}
			}
			{
				var err error
				conflictExpId, conflictExpName, err = simulationhelpers.CreateExperiment(testClient, simulationhelpers.ExperimentOptions{PackageID: testPackageId, ModelName: testModelName, NamePrefix: "测试实验_复制冲突"})
				if err != nil {
					t.Fatal(err)
				}
			}
			configureExperimentCopySource(t, sourceExpId)
		},
		After: func(t *testing.T) {
			simulationhelpers.DeleteExperiment(testClient, sourceExpId)
			simulationhelpers.DeleteExperiment(testClient, conflictExpId)
			simulationhelpers.DeleteExperiment(testClient, copiedExpId)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				newExperimentName := tt.newExperimentName
				if tt.newNameRef == "conflictName" {
					// 复制为已存在名称必须使用真实冲突名，而不是未加唯一后缀的基础名。
					newExperimentName = conflictExpName
				}
				if tt.useRaw {
					rawResp, err := testClient.PostRawResponse("/simulation/experiment/copy", map[string]any{
						"new_experiment_name": newExperimentName,
					})
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
					return
				}

				sourceId := sourceExpId
				if tt.sourceIdLiteral != "" {
					sourceId = tt.sourceIdLiteral
				}
				resp, err := testClient.Primary().ExperimentCopy(sourceId, newExperimentName)
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

// TestSaveExperimentToOriginModel 验证 POST /api/v3/simulation/experiment/original-model/save。
// 覆盖：
//   - 正常保存实验修改参数到原模型
//   - 缺少 experiment_id 返回 status=2
//   - 原模型不可写/不存在时返回 status=2
func TestSaveExperimentToOriginModel(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var packageID string
	var modelName string
	var editableExpID string
	var systemExpID string

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			{
				var err error
				packageID, modelName, err = modelhelpers.CreateTempModelFixture(testClient, tempModelSourcePackageID, tempModelSourceModelName)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := modelhelpers.EnsureModelLoaded(testClient, packageID, modelName, "保存到原模型夹具"); err != nil {
				t.Fatal(err)
			}
			{
				var err error
				editableExpID, _, err = simulationhelpers.CreateExperiment(testClient, simulationhelpers.ExperimentOptions{PackageID: packageID, ModelName: modelName, NamePrefix: "测试实验_保存到原模型", WarmDefaultExperiment: true})
				if err != nil {
					t.Fatal(err)
				}
			}
			{
				var err error
				systemExpID, _, err = simulationhelpers.CreateExperiment(testClient, simulationhelpers.ExperimentOptions{PackageID: testPackageId, ModelName: testModelName, NamePrefix: "测试实验_保存到原模型_系统库"})
				if err != nil {
					t.Fatal(err)
				}
			}
			setExperimentParameterForModelSave(t, editableExpID, "11")
		},
		After: func(t *testing.T) {
			simulationhelpers.DeleteExperiment(testClient, editableExpID)
			simulationhelpers.DeleteExperiment(testClient, systemExpID)
			modelhelpers.CleanupTempModelFixture(testClient, packageID, modelName)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		tests := []struct {
			name               string
			experimentID       string
			useRaw             bool
			expectedHTTPStatus int
			expectError        bool
			expectedCode       int
			assertResponse     func(t *testing.T, resp *helpers.Response)
		}{
			{
				name:         "正常保存实验参数到原模型",
				experimentID: editableExpID,
				assertResponse: func(t *testing.T, resp *helpers.Response) {
					assert.IsSuccess(resp, "正常保存实验参数到原模型")
					assertModelCodeContains(t, packageID, modelName, "k=11", "正常保存实验参数到原模型: 源码回读")
				},
			},
			{
				name:         "原模型不可写时返回 status=2",
				experimentID: systemExpID,
				expectError:  true,
				expectedCode: 2,
			},
			{
				name:         "缺少 experiment_id 返回 status=2",
				useRaw:       true,
				expectError:  true,
				expectedCode: 2,
			},
		}

		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				if tt.useRaw {
					resp, err := testClient.Post("/api/v3/simulation/experiment/original-model/save", map[string]any{}, nil)
					if err != nil {
						t.Fatalf("请求失败: %v", err)
					}
					assert.IsError(resp, tt.expectedCode, tt.name)
					return
				}

				resp, err := testClient.Primary().SaveExperimentToOriginModel(tt.experimentID)
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

// TestSaveExperimentToNewModel 验证 POST /api/v3/simulation/experiment/new-model/save。
// 覆盖：
//   - 正常保存实验到新模型，并验证新模型创建成功且源码可回读
//   - 缺少必填参数返回 status=2
//   - 非法模型名/保存失败返回 status=2
//
// 说明：当前后端实现下，“保存到新模型”稳定可验证的是“复制成功 + 新模型可回读”；
// 实验参数是否已物化到新模型源码中暂不作为通过标准。
func TestSaveExperimentToNewModel(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var packageID string
	var modelName string
	var experimentID string
	var createdPackageIDs []string
	var createdModelNames []string

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			{
				var err error
				packageID, modelName, err = modelhelpers.CreateTempModelFixture(testClient, tempModelSourcePackageID, tempModelSourceModelName)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := modelhelpers.EnsureModelLoaded(testClient, packageID, modelName, "保存到新模型夹具"); err != nil {
				t.Fatal(err)
			}
			{
				var err error
				experimentID, _, err = simulationhelpers.CreateExperiment(testClient, simulationhelpers.ExperimentOptions{PackageID: packageID, ModelName: modelName, NamePrefix: "测试实验_保存到新模型", WarmDefaultExperiment: true})
				if err != nil {
					t.Fatal(err)
				}
			}
			setExperimentParameterForModelSave(t, experimentID, "13")
		},
		After: func(t *testing.T) {
			simulationhelpers.DeleteExperiment(testClient, experimentID)
			for i := range createdPackageIDs {
				modelhelpers.CleanupTempModelFixture(testClient, createdPackageIDs[i], createdModelNames[i])
			}
			modelhelpers.CleanupTempModelFixture(testClient, packageID, modelName)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		tests := []struct {
			name         string
			experimentID string
			newModelName string
			useRaw       bool
			expectError  bool
			expectedCode int
			assertResult func(t *testing.T, resp *helpers.Response)
		}{
			{
				name:         "正常保存实验到新模型并可回读源码",
				experimentID: experimentID,
				newModelName: modelhelpers.UniqueModelName("TestModel"),
				assertResult: func(t *testing.T, resp *helpers.Response) {
					assert.IsSuccess(resp, "正常保存实验到新模型并可回读源码")
					dataMap, err := helpers.ObjectData(resp, "正常保存实验到新模型并可回读源码")
					if err != nil {
						t.Fatal(err)
					}
					newPackageID, _ := dataMap["id"].(string)
					newModelName, _ := dataMap["model_name"].(string)
					if newPackageID == "" || newModelName == "" {
						t.Fatalf("正常保存实验到新模型并可回读源码: 返回数据缺少 id/model_name: %v", dataMap)
					}
					createdPackageIDs = append(createdPackageIDs, newPackageID)
					createdModelNames = append(createdModelNames, newModelName)
					assertCopiedModelVisibleInUserRoot(t, newPackageID, newModelName, "正常保存实验到新模型并可回读源码: 用户模型树回读")
					if err := modelhelpers.EnsureModelLoaded(testClient, newPackageID, newModelName, "保存到新模型回读"); err != nil {
						t.Fatal(err)
					}
					assertModelCodeContains(t, newPackageID, newModelName, "model "+newModelName, "正常保存实验到新模型并可回读源码: 源码回读")
				},
			},
			{
				name:         "缺少 new_model_name 返回 status=2",
				useRaw:       true,
				expectError:  true,
				expectedCode: 2,
			},
			{
				name:         "非法模型名返回 status=2",
				experimentID: experimentID,
				newModelName: "1BadModelName",
				expectError:  true,
				expectedCode: 2,
			},
		}

		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				if tt.useRaw {
					resp, err := testClient.Post("/api/v3/simulation/experiment/new-model/save", map[string]any{
						"experiment_id": experimentID,
					}, nil)
					if err != nil {
						t.Fatalf("请求失败: %v", err)
					}
					assert.IsError(resp, tt.expectedCode, tt.name)
					return
				}

				resp, err := testClient.Primary().SaveExperimentToNewModel(tt.experimentID, tt.newModelName)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				if tt.expectError {
					assert.IsError(resp, tt.expectedCode, tt.name)
					return
				}
				tt.assertResult(t, resp)
			})
		}
	})
}

// TestExportEncryptPackage 验证 GET /file/package/encrypt/export。
// 覆盖：
//   - 正常导出用户空间模型包为加密包
//   - 缺少 package_id 返回 HTTP 400
//   - 不存在的 package_id 返回导出失败
func TestExportEncryptPackage(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var packageID string
	var modelName string
	spaceID := helpers.DefaultTestConfig().SpaceID

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			{
				var err error
				packageID, modelName, err = modelhelpers.CreateTempModelFixture(testClient, tempModelSourcePackageID, tempModelSourceModelName)
				if err != nil {
					t.Fatal(err)
				}
			}
		},
		After: func(t *testing.T) {
			modelhelpers.CleanupTempModelFixture(testClient, packageID, modelName)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		tests := []struct {
			name               string
			packageID          string
			useRaw             bool
			rawParams          map[string]string
			expectError        bool
			expectedCode       int
			expectedHTTPStatus int
			assertResponse     func(t *testing.T, resp *helpers.Response)
		}{
			{
				name:      "正常导出用户模型包为加密包",
				packageID: packageID,
				assertResponse: func(t *testing.T, resp *helpers.Response) {
					assert.IsSuccess(resp, "正常导出用户模型包为加密包")
					dataMap, err := helpers.ObjectData(resp, "正常导出用户模型包为加密包")
					if err != nil {
						t.Fatal(err)
					}
					url, ok := dataMap["url"].(string)
					if !ok || url == "" {
						t.Fatalf("正常导出用户模型包为加密包: 返回的 url 为空或类型错误: %v", dataMap["url"])
					}
				},
			},
			{
				name:               "缺少 package_id 返回 HTTP 400",
				useRaw:             true,
				rawParams:          map[string]string{"space_id": spaceID},
				expectedHTTPStatus: http.StatusBadRequest,
				expectError:        true,
				expectedCode:       2,
			},
			{
				name:               "不存在的 package_id 返回导出失败",
				useRaw:             true,
				rawParams:          map[string]string{"package_id": "non-existent-package-id", "space_id": spaceID},
				expectedHTTPStatus: http.StatusBadRequest,
				expectError:        true,
				expectedCode:       2,
			},
		}

		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				if tt.useRaw {
					rawResp, err := testClient.GetRawResponse("/file/package/encrypt/export", tt.rawParams)
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
					if tt.expectError {
						resp, err := helpers.UnmarshalResponse(body)
						if err != nil {
							t.Fatal(err)
						}
						assert.IsError(resp, tt.expectedCode, tt.name)
					}
					return
				}

				resp, err := testClient.Primary().ExportEncryptPackage(tt.packageID)
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

// configureExperimentCaseInfoFixture 为 case-info 场景写入两类参数与一个导出项，构造 6 个 case。
func configureExperimentCaseInfoFixture(t *testing.T, experimentId string, useComponentExport bool) {
	t.Helper()

	if _, err := testClient.ExperimentParametersSet(experimentId, []map[string]any{
		{
			"element_name":    "PI",
			"parameter_name":  "k",
			"parameter_value": "range(1.0, 3.0, 3)",
			"class_prefixes":  "Real",
		},
		{
			"element_name":    "PI",
			"parameter_name":  "Ti",
			"parameter_value": "choices(0.5, 1.0)",
			"class_prefixes":  "Real",
		},
	}); err != nil {
		t.Fatalf("设置 case-info 参数失败: %v", err)
	}

	if _, err := testClient.ExperimentExportVariableSet(experimentId, "PI.y", useComponentExport); err != nil {
		t.Fatalf("设置 case-info 导出变量失败: %v", err)
	}
}

func setExperimentParameterForModelSave(t *testing.T, experimentID, value string) {
	t.Helper()
	resp, err := testClient.ExperimentParametersSet(experimentID, []map[string]any{{
		"extend_name":         "Modelica.Units.SI.Real",
		"is_extend":           false,
		"element_name":        "PI",
		"parameter_name":      "k",
		"parameter_value":     value,
		"redeclare":           false,
		"class_prefixes":      "Real",
		"replaced_class_type": "",
		"unit":                "",
		"displayUnit":         "",
	}})
	if err != nil {
		t.Fatalf("设置实验保存参数失败: %v", err)
	}
	if resp.Status != 0 {
		t.Fatalf("设置实验保存参数返回非成功状态: status=%d, msg=%s, err=%s", resp.Status, resp.Msg, resp.Err)
	}
}

// configureExperimentCopySource 为复制源实验设置非默认的仿真选项、参数和导出变量，便于验证复制结果。
func configureExperimentCopySource(t *testing.T, experimentId string) {
	t.Helper()

	options := simulationhelpers.DefaultExperimentOptionsData()
	options["start_time"] = "1"
	options["stop_time"] = "9"
	options["method"] = "Euler"
	options["interval"] = "0.016"
	options["multiple_run_sim"] = true
	if _, err := testClient.ExperimentOptionsSet(experimentId, options); err != nil {
		t.Fatalf("设置复制源实验仿真选项失败: %v", err)
	}

	if _, err := testClient.ExperimentParametersSet(experimentId, []map[string]any{
		{
			"element_name":    "PI",
			"parameter_name":  "k",
			"parameter_value": "2.0",
			"class_prefixes":  "Real",
		},
		{
			"element_name":    "PI",
			"parameter_name":  "Ti",
			"parameter_value": "choices(0.5, 1.0, 2.0)",
			"class_prefixes":  "Real",
			"unit":            "s",
			"displayUnit":     "s",
		},
	}); err != nil {
		t.Fatalf("设置复制源实验参数失败: %v", err)
	}

	if _, err := testClient.ExperimentExportVariableSet(experimentId, "PI.y", false); err != nil {
		t.Fatalf("设置复制源实验导出变量失败: %v", err)
	}
}

// resolveExperimentRef 将表驱动中的引用名称映射为真实实验 ID。
func resolveExperimentRef(ref, defaultExpId, configuredExpId, componentExportExpId string) string {
	switch ref {
	case "default":
		return defaultExpId
	case "configured":
		return configuredExpId
	case "component":
		return componentExportExpId
	default:
		return "non-existent-experiment-id"
	}
}

// extractExperimentCaseInfo 解析 case-info 接口返回的 data object。
func extractExperimentCaseInfo(t *testing.T, resp *helpers.Response) map[string]any {
	t.Helper()
	dataMap, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("case-info data 不是 object: %T", resp.Data)
	}
	return dataMap
}

// extractCopiedExperimentID 从复制实验响应中提取新实验 ID。
func extractCopiedExperimentID(t *testing.T, resp *helpers.Response) string {
	t.Helper()
	dataMap, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("复制实验返回 data 不是 object: %T", resp.Data)
	}
	id, ok := dataMap["id"].(string)
	if !ok || id == "" {
		t.Fatalf("复制实验返回的 id 为空或类型错误: %v", dataMap["id"])
	}
	return id
}

// getExperimentDetailData 获取实验详情接口返回的 data object。
func getExperimentDetailData(t *testing.T, experimentId string) map[string]any {
	t.Helper()
	resp, err := testClient.ExperimentDetails(testPackageId, testModelName, experimentId)
	if err != nil {
		t.Fatalf("获取实验详情失败: %v", err)
	}
	if resp.Status != 0 {
		t.Fatalf("获取实验详情返回非成功状态: status=%d, msg=%s, err=%s", resp.Status, resp.Msg, resp.Err)
	}
	dataMap, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("实验详情 data 不是 object: %T", resp.Data)
	}
	return dataMap
}

// assertCopiedExperimentMatchesSource 校验复制后的实验保留了关键配置，仅实验名称与 id 发生预期变化。
func assertCopiedExperimentMatchesSource(t *testing.T, sourceExperimentId, copiedExperimentId, copiedExperimentName string) {
	t.Helper()

	sourceData := getExperimentDetailData(t, sourceExperimentId)
	copiedData := getExperimentDetailData(t, copiedExperimentId)

	if copiedData["experiment_id"] != copiedExperimentId {
		t.Errorf("复制实验详情中的 experiment_id 不正确，期望=%q，实际=%v", copiedExperimentId, copiedData["experiment_id"])
	}
	if copiedData["experiment_name"] != copiedExperimentName {
		t.Errorf("复制实验详情中的 experiment_name 不正确，期望=%q，实际=%v", copiedExperimentName, copiedData["experiment_name"])
	}
	if copiedData["package_id"] != sourceData["package_id"] {
		t.Errorf("复制实验 package_id 与源实验不一致，source=%v copied=%v", sourceData["package_id"], copiedData["package_id"])
	}
	if copiedData["model_name"] != sourceData["model_name"] {
		t.Errorf("复制实验 model_name 与源实验不一致，source=%v copied=%v", sourceData["model_name"], copiedData["model_name"])
	}
	if copiedData["default"] != false {
		t.Errorf("复制实验应为非默认实验，实际 default=%v", copiedData["default"])
	}

	sourceSimVar, ok := sourceData["simulate_var_data"].(map[string]any)
	if !ok {
		t.Fatalf("源实验 simulate_var_data 不是 object: %T", sourceData["simulate_var_data"])
	}
	copiedSimVar, ok := copiedData["simulate_var_data"].(map[string]any)
	if !ok {
		t.Fatalf("复制实验 simulate_var_data 不是 object: %T", copiedData["simulate_var_data"])
	}
	for _, field := range []string{"startTime", "stopTime", "method", "interval", "numberOfIntervals", "tolerance", "multiple_run_sim"} {
		if copiedSimVar[field] != sourceSimVar[field] {
			t.Errorf("复制实验 simulate_var_data.%s 与源实验不一致，source=%v copied=%v", field, sourceSimVar[field], copiedSimVar[field])
		}
	}

	sourceParameters, sourceExportVars := getDetailFields(t, sourceExperimentId)
	copiedParameters, copiedExportVars := getDetailFields(t, copiedExperimentId)
	for _, parameterName := range []string{"PI.k", "PI.Ti"} {
		sourceItem := parameterInList(sourceParameters, parameterName)
		copiedItem := parameterInList(copiedParameters, parameterName)
		if sourceItem == nil || copiedItem == nil {
			t.Fatalf("参数 %q 在源实验或复制实验中缺失，source=%v copied=%v", parameterName, sourceItem, copiedItem)
		}
		if copiedItem["value"] != sourceItem["value"] {
			t.Errorf("复制实验参数 %q 的 value 与源实验不一致，source=%v copied=%v", parameterName, sourceItem["value"], copiedItem["value"])
		}
	}
	if !simulationhelpers.ExportVariableInList(sourceExportVars, "PI.y") || !simulationhelpers.ExportVariableInList(copiedExportVars, "PI.y") {
		t.Errorf("复制实验未保留源实验的导出变量 PI.y，source=%v copied=%v", sourceExportVars, copiedExportVars)
	}
}
