//go:build api_test

package api

import (
	"bufio"
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"yssim-go/app/DataBase"
	"yssim-go/config"
	"yssim-go/tests/helpers"
	modelhelpers "yssim-go/tests/helpers/model"
	simulationhelpers "yssim-go/tests/helpers/simulation"
)

// TestCalibrationTranslateFmu 覆盖 POST /calibration/translate/fmu。
func TestCalibrationTranslateFmu(t *testing.T) {
	assert := helpers.NewAssertResponse(t)
	var fixture *parameterCalibrationFixture

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			fixture = createParameterCalibrationModelFixture(t)
		},
		After: func(t *testing.T) {
			cleanupParameterCalibrationFixture(t, fixture)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		t.Run("正常生成标定准备记录", func(t *testing.T) {
			resp, err := testClient.Primary().CalibrationTranslateFmu(fixture.PackageID, fixture.ModelName)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsSuccess(resp, "正常生成标定准备记录")
			dataMap, err := helpers.ObjectData(resp, "正常生成标定准备记录")
			if err != nil {
				t.Fatal(err)
			}
			preparationID, ok := dataMap["preparation_id"].(string)
			if !ok || preparationID == "" {
				t.Fatalf("正常生成标定准备记录: preparation_id 为空或类型错误: %v", dataMap["preparation_id"])
			}
			fixture.PreparationID = preparationID
			if err := modelhelpers.WaitCompileFinished(testClient, fixture.PackageID, fixture.ModelName, "正常生成标定准备记录"); err != nil {
				t.Fatal(err)
			}
		})

		t.Run("缺少 package_id 返回 HTTP 400", func(t *testing.T) {
			rawResp, err := testClient.Primary().CalibrationTranslateFmuRaw(map[string]any{"model_name": fixture.ModelName})
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			body, err := helpers.ReadAndCloseRawBody(rawResp)
			if err != nil {
				t.Fatal(err)
			}
			if rawResp.StatusCode != http.StatusBadRequest {
				t.Fatalf("缺少 package_id 返回 HTTP 400: 期望 HTTP 400，实际=%d body=%q", rawResp.StatusCode, string(body))
			}
			assertRawJSONStringBody(t, "缺少 package_id 返回 HTTP 400", body, "")
		})
	})
}

// TestCalibrationDataUpload 覆盖 POST /calibration/data/upload。
func TestCalibrationDataUpload(t *testing.T) {
	assert := helpers.NewAssertResponse(t)
	var fixture *parameterCalibrationFixture

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			fixture = createParameterCalibrationFixture(t)
		},
		After: func(t *testing.T) {
			cleanupParameterCalibrationFixture(t, fixture)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		t.Run("正常上传标定 CSV", func(t *testing.T) {
			resp, err := testClient.Primary().UploadCalibrationData(fixture.PreparationID, false, fixture.CSVFileName, fixture.CSVContent)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsSuccess(resp, "正常上传标定 CSV")
			dataMap, err := helpers.ObjectData(resp, "正常上传标定 CSV")
			if err != nil {
				t.Fatal(err)
			}
			assertCalibrationCSVColumns(t, "正常上传标定 CSV", dataMap, "columns")
		})

		t.Run("非法文件后缀返回业务错误", func(t *testing.T) {
			resp, err := testClient.Primary().UploadCalibrationData(fixture.PreparationID, false, "bad.txt", []byte("bad"))
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsError(resp, 2, "非法文件后缀返回业务错误")
			if !strings.Contains(resp.Err, "*.csv") {
				t.Fatalf("非法文件后缀返回业务错误: 期望 err 包含 *.csv，实际=%q", resp.Err)
			}
		})
	})
}

// TestCalibrationParseCSVColumns 覆盖 POST /calibration/data/columns/parse。
func TestCalibrationParseCSVColumns(t *testing.T) {
	assert := helpers.NewAssertResponse(t)
	var fixture *parameterCalibrationFixture
	var calibrationID string

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			fixture = createParameterCalibrationFixtureWithCSV(t)
		},
		After: func(t *testing.T) {
			cleanupParameterCalibrationFixture(t, fixture)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		t.Run("按准备记录解析 CSV", func(t *testing.T) {
			resp, err := testClient.Primary().CalibrationParseCSVColumns(fixture.PreparationID, false)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsSuccess(resp, "按准备记录解析 CSV")
			dataMap, err := helpers.ObjectData(resp, "按准备记录解析 CSV")
			if err != nil {
				t.Fatal(err)
			}
			assertCalibrationCSVColumns(t, "按准备记录解析 CSV", dataMap, "columns")
			if dataMap["start_time"] != "0" || dataMap["stop_time"] != "6.24" {
				t.Fatalf("按准备记录解析 CSV: 期望 start_time=0 stop_time=6.24，实际 start=%v stop=%v", dataMap["start_time"], dataMap["stop_time"])
			}
		})

		t.Run("按标定记录解析 CSV", func(t *testing.T) {
			calibrationID = createParameterCalibrationRecord(t, fixture, uniqueName("pc_parse"))
			resp, err := testClient.Primary().CalibrationParseCSVColumns(calibrationID, true)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsSuccess(resp, "按标定记录解析 CSV")
			dataMap, err := helpers.ObjectData(resp, "按标定记录解析 CSV")
			if err != nil {
				t.Fatal(err)
			}
			assertCalibrationCSVColumns(t, "按标定记录解析 CSV", dataMap, "columns")
		})

		t.Run("使用旧字段 preparation_id 返回 HTTP 400", func(t *testing.T) {
			rawResp, err := testClient.Primary().CalibrationParseCSVColumnsRaw(map[string]any{"preparation_id": "non-existent"})
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			body, err := helpers.ReadAndCloseRawBody(rawResp)
			if err != nil {
				t.Fatal(err)
			}
			if rawResp.StatusCode != http.StatusBadRequest {
				t.Fatalf("使用旧字段 preparation_id 返回 HTTP 400: 期望 HTTP 400，实际=%d body=%q", rawResp.StatusCode, string(body))
			}
			assertRawJSONStringBody(t, "使用旧字段 preparation_id 返回 HTTP 400", body, "")
		})

		t.Run("不存在的 id 返回业务错误", func(t *testing.T) {
			resp, err := testClient.Primary().CalibrationParseCSVColumns("non-existent-preparation-id", false)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsError(resp, 2, "不存在的 id 返回业务错误")
			if !strings.Contains(resp.Err, "标定准备记录不存在") {
				t.Fatalf("不存在的 id 返回业务错误: 期望 err 包含 标定准备记录不存在，实际=%q", resp.Err)
			}
		})
	})
}

// TestCalibrationCreate 覆盖 POST /calibration/create。
func TestCalibrationCreate(t *testing.T) {
	assert := helpers.NewAssertResponse(t)
	var fixture *parameterCalibrationFixture
	calibrationName := uniqueName("pc_create")

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			fixture = createParameterCalibrationFixtureWithCSV(t)
		},
		After: func(t *testing.T) {
			cleanupParameterCalibrationFixture(t, fixture)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		t.Run("正常创建标定记录", func(t *testing.T) {
			resp, err := testClient.Primary().CalibrationCreate(parameterCalibrationCreatePayload(fixture.PreparationID, fixture.DefaultExperimentID, calibrationName))
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsSuccess(resp, "正常创建标定记录")
			dataMap, err := helpers.ObjectData(resp, "正常创建标定记录")
			if err != nil {
				t.Fatal(err)
			}
			id, ok := dataMap["id"].(string)
			if !ok || id == "" {
				t.Fatalf("正常创建标定记录: 返回 id 为空或类型错误: %v", dataMap["id"])
			}
			if dataMap["preparation_id"] != fixture.PreparationID {
				t.Fatalf("正常创建标定记录: 期望 preparation_id=%q，实际=%v", fixture.PreparationID, dataMap["preparation_id"])
			}
			fixture.CalibrationIDs = append(fixture.CalibrationIDs, id)
		})

		t.Run("同一实验下重名返回业务错误", func(t *testing.T) {
			duplicateFixture := createParameterCalibrationFixtureWithCSV(t)
			defer cleanupParameterCalibrationFixture(t, duplicateFixture)

			duplicateName := uniqueName("pc_duplicate")
			_ = createParameterCalibrationRecord(t, duplicateFixture, duplicateName)
			resp, err := testClient.Primary().CalibrationCreate(parameterCalibrationCreatePayload(duplicateFixture.PreparationID, duplicateFixture.DefaultExperimentID, duplicateName))
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsError(resp, 2, "同一实验下重名返回业务错误")
			if !strings.Contains(resp.Err, "当前实验下已存在同名标定记录") {
				t.Fatalf("同一实验下重名返回业务错误: 期望 err 包含重名提示，实际=%q", resp.Err)
			}
		})

		t.Run("缺少必填字段返回 HTTP 400", func(t *testing.T) {
			rawResp, err := testClient.Primary().CalibrationCreateRaw(map[string]any{
				"calibration_name": "pc",
				"experiment_id":    fixture.DefaultExperimentID,
			})
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			body, err := helpers.ReadAndCloseRawBody(rawResp)
			if err != nil {
				t.Fatal(err)
			}
			if rawResp.StatusCode != http.StatusBadRequest {
				t.Fatalf("缺少必填字段返回 HTTP 400: 期望 HTTP 400，实际=%d body=%q", rawResp.StatusCode, string(body))
			}
			assertRawJSONStringBody(t, "缺少必填字段返回 HTTP 400", body, "")
		})

		t.Run("不存在的实验返回业务错误", func(t *testing.T) {
			resp, err := testClient.Primary().CalibrationCreate(parameterCalibrationCreatePayload(fixture.PreparationID, "non-existent-experiment-id", uniqueName("pc_invalid_exp")))
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsError(resp, 2, "不存在的实验返回业务错误")
			if !strings.Contains(resp.Err, "实验不存在") {
				t.Fatalf("不存在的实验返回业务错误: 期望 err 包含 实验不存在，实际=%q", resp.Err)
			}
		})
	})
}

// TestCalibrationList 覆盖 GET /calibration/list。
func TestCalibrationList(t *testing.T) {
	assert := helpers.NewAssertResponse(t)
	var fixture *parameterCalibrationFixture
	var calibrationID string

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			fixture, calibrationID = createParameterCalibrationRecordFixture(t, uniqueName("pc_list"))
		},
		After: func(t *testing.T) {
			cleanupParameterCalibrationFixture(t, fixture)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		t.Run("正常读取列表 SSE", func(t *testing.T) {
			rawResp, err := testClient.Primary().CalibrationListRaw(fixture.PackageID, fixture.ModelName, 1)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			defer func() { _ = rawResp.Body.Close() }()
			if rawResp.StatusCode != http.StatusOK {
				t.Fatalf("正常读取列表 SSE: 期望 HTTP 200，实际=%d", rawResp.StatusCode)
			}
			helpers.AssertSSEContentType(t, rawResp, "正常读取列表 SSE")

			// 列表接口会持续推送；这里仅读取第一条 data 事件来验证结构和目标记录可见性。
			eventName, payload, err := helpers.ReadSSEEvent(bufio.NewReader(rawResp.Body))
			if err != nil {
				t.Fatal(err)
			}
			if eventName != "data" {
				t.Fatalf("正常读取列表 SSE: 期望 event=data，实际=%q", eventName)
			}
			var envelope helpers.Response
			if err := json.Unmarshal(payload, &envelope); err != nil {
				t.Fatalf("正常读取列表 SSE: 解析事件失败: %v, payload=%s", err, string(payload))
			}
			assert.IsSuccess(&envelope, "正常读取列表 SSE")
			dataMap, err := helpers.ObjectData(&envelope, "正常读取列表 SSE")
			if err != nil {
				t.Fatal(err)
			}
			if !calibrationListContainsID(dataMap["list"], calibrationID) {
				t.Fatalf("正常读取列表 SSE: 列表中未找到标定记录 id=%q，data=%v", calibrationID, dataMap)
			}
		})

		t.Run("缺少 package_id 或 model_name 返回业务错误", func(t *testing.T) {
			resp, err := testClient.Primary().CalibrationList("", "", 1)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsError(resp, 2, "缺少 package_id 或 model_name 返回业务错误")
		})
	})
}

// TestCalibrationTree 覆盖 GET /calibration/tree。
func TestCalibrationTree(t *testing.T) {
	assert := helpers.NewAssertResponse(t)
	var fixture *parameterCalibrationFixture

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			fixture = createParameterCalibrationFixture(t)
		},
		After: func(t *testing.T) {
			cleanupParameterCalibrationFixture(t, fixture)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		t.Run("正常查询参数树", func(t *testing.T) {
			resp, err := testClient.Primary().CalibrationTree(fixture.PreparationID, false, "", "engine", "parameter")
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsSuccess(resp, "正常查询参数树")
			items, ok := resp.Data.([]any)
			if !ok || len(items) == 0 {
				t.Fatalf("正常查询参数树: data 不是非空数组: %T %v", resp.Data, resp.Data)
			}
			if !calibrationTreeContainsVariable(items, "engineTorque") {
				t.Fatalf("正常查询参数树: 未找到 engineTorque，data=%v", resp.Data)
			}
		})

		t.Run("缺少 id 返回业务错误", func(t *testing.T) {
			resp, err := testClient.Primary().CalibrationTree("", false, "", "", "parameter")
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsError(resp, 2, "缺少 id 返回业务错误")
			if !strings.Contains(resp.Err, "id不能为空") {
				t.Fatalf("缺少 id 返回业务错误: 期望 err 包含 id不能为空，实际=%q", resp.Err)
			}
		})
	})
}

// TestCalibrationEdit 覆盖 POST /calibration/edit。
func TestCalibrationEdit(t *testing.T) {
	assert := helpers.NewAssertResponse(t)
	var fixture *parameterCalibrationFixture
	var calibrationID string

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			fixture, calibrationID = createParameterCalibrationRecordFixture(t, uniqueName("pc_edit"))
		},
		After: func(t *testing.T) {
			cleanupParameterCalibrationFixture(t, fixture)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		t.Run("正常编辑并重置状态", func(t *testing.T) {
			editedName := uniqueName("pc_edited")
			resp, err := testClient.Primary().CalibrationEdit(map[string]any{
				"id":                calibrationID,
				"calibration_name":  editedName,
				"tuner_parameter":   []map[string]any{{"name": "engine.tau_0", "min": 90, "max": 220, "start": 120}},
				"result_couplings":  []map[string]any{{"csv_column": "acc", "model_variable": "carBody.a"}},
				"cases":             []map[string]any{{"name": "edited", "type": "calibrate", "start_time": 0, "stop_time": 6.24}},
				"unused_test_field": "ignored",
			})
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsSuccess(resp, "正常编辑并重置状态")
			assertCalibrationRecordState(t, calibrationID, editedName, "not_started", "")
		})

		t.Run("无变更编辑返回业务错误", func(t *testing.T) {
			record := getParameterCalibrationRecord(t, calibrationID)
			resp, err := testClient.Primary().CalibrationEdit(map[string]any{
				"id":               calibrationID,
				"calibration_name": record.CalibrationName,
			})
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsError(resp, 2, "无变更编辑返回业务错误")
		})

		t.Run("不存在的 id 返回业务错误", func(t *testing.T) {
			resp, err := testClient.Primary().CalibrationEdit(map[string]any{"id": "non-existent-calibration-id", "calibration_name": "pc"})
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsError(resp, 2, "不存在的 id 返回业务错误")
		})
	})
}

// TestCalibrationDelete 覆盖 POST /calibration/delete。
func TestCalibrationDelete(t *testing.T) {
	assert := helpers.NewAssertResponse(t)
	var fixture *parameterCalibrationFixture
	var calibrationID string

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			fixture, calibrationID = createParameterCalibrationRecordFixture(t, uniqueName("pc_delete"))
		},
		After: func(t *testing.T) {
			cleanupParameterCalibrationFixture(t, fixture)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		t.Run("正常删除标定记录并验证重复删除返回错误", func(t *testing.T) {
			resp, err := testClient.Primary().CalibrationDelete(calibrationID)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsSuccess(resp, "正常删除标定记录")

			resp, err = testClient.Primary().CalibrationDelete(calibrationID)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsError(resp, 2, "删除后重复删除返回业务错误")
		})
	})
}

// TestCalibrationStart 覆盖 POST /calibration/start。
func TestCalibrationStart(t *testing.T) {
	assert := helpers.NewAssertResponse(t)
	var fixture *parameterCalibrationFixture
	var calibrationID string

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			fixture, calibrationID = createParameterCalibrationRecordFixture(t, uniqueName("pc_start"))
		},
		After: func(t *testing.T) {
			cleanupParameterCalibrationFixture(t, fixture)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		t.Run("正常启动标定任务", func(t *testing.T) {
			resp := startParameterCalibrationTask(t, calibrationID)
			assert.IsSuccess(resp, "正常启动标定任务")
			taskID := extractCalibrationTaskID(t, resp, "正常启动标定任务")
			assertCalibrationRecordState(t, calibrationID, "", "running", taskID)
		})

		t.Run("不存在的 id 返回业务错误", func(t *testing.T) {
			resp, err := testClient.Primary().CalibrationStart("non-existent-calibration-id")
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsError(resp, 2, "不存在的 id 返回业务错误")
		})

		t.Run("缺少 id 返回 HTTP 400", func(t *testing.T) {
			rawResp, err := testClient.Primary().CalibrationStartRaw(map[string]any{})
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			body, err := helpers.ReadAndCloseRawBody(rawResp)
			if err != nil {
				t.Fatal(err)
			}
			if rawResp.StatusCode != http.StatusBadRequest {
				t.Fatalf("缺少 id 返回 HTTP 400: 期望 HTTP 400，实际=%d body=%q", rawResp.StatusCode, string(body))
			}
			assertRawJSONStringBody(t, "缺少 id 返回 HTTP 400", body, "")
		})
	})
}

// TestCalibrationStop 覆盖 POST /calibration/stop。
func TestCalibrationStop(t *testing.T) {
	assert := helpers.NewAssertResponse(t)
	var fixture *parameterCalibrationFixture
	var calibrationID string

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			fixture, calibrationID = createParameterCalibrationRecordFixture(t, uniqueName("pc_stop"))
		},
		After: func(t *testing.T) {
			cleanupParameterCalibrationFixture(t, fixture)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		t.Run("停止未启动标定记录成功", func(t *testing.T) {
			resp, err := testClient.Primary().CalibrationStop(calibrationID)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsSuccess(resp, "停止未启动标定记录成功")
			assertCalibrationRecordState(t, calibrationID, "", "stopped", "")
		})

		t.Run("不存在的 id 返回业务错误", func(t *testing.T) {
			resp, err := testClient.Primary().CalibrationStop("non-existent-calibration-id")
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsError(resp, 2, "不存在的 id 返回业务错误")
		})
	})
}

// TestCalibrationDetail 覆盖 GET /calibration/detail。
func TestCalibrationDetail(t *testing.T) {
	assert := helpers.NewAssertResponse(t)
	var fixture *parameterCalibrationFixture
	var calibrationID string

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			fixture, calibrationID = createParameterCalibrationRecordFixture(t, uniqueName("pc_detail"))
		},
		After: func(t *testing.T) {
			cleanupParameterCalibrationFixture(t, fixture)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		t.Run("已启动记录正常返回详情 SSE", func(t *testing.T) {
			startedFixture, startedCalibrationID := createParameterCalibrationRecordFixture(t, uniqueName("pc_detail_started"))
			defer cleanupParameterCalibrationFixture(t, startedFixture)
			startParameterCalibrationTask(t, startedCalibrationID)

			rawResp, err := testClient.Primary().CalibrationDetailRaw(startedCalibrationID)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			defer func() { _ = rawResp.Body.Close() }()
			if rawResp.StatusCode != http.StatusOK {
				t.Fatalf("已启动记录正常返回详情 SSE: 期望 HTTP 200，实际=%d", rawResp.StatusCode)
			}
			helpers.AssertSSEContentType(t, rawResp, "已启动记录正常返回详情 SSE")
			assertCalibrationDetailDataEvent(t, rawResp, "已启动记录正常返回详情 SSE")
		})

		t.Run("未启动记录返回 task_id 为空", func(t *testing.T) {
			resp, err := testClient.Primary().CalibrationDetail(calibrationID)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsError(resp, 2, "未启动记录返回 task_id 为空")
			if !strings.Contains(resp.Err, "task_id为空") {
				t.Fatalf("未启动记录返回 task_id 为空: 期望 err 包含 task_id为空，实际=%q", resp.Err)
			}
		})

		t.Run("不存在的 id 返回业务错误", func(t *testing.T) {
			resp, err := testClient.Primary().CalibrationDetail("non-existent-calibration-id")
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsError(resp, 2, "不存在的 id 返回业务错误")
		})
	})
}

// TestCalibrationSaveExperimentParameter 覆盖 POST /calibration/experiment/parameter/save。
// TODO: 正向保存依赖已完成的标定结果，待 completed 标定结果夹具稳定后补充成功用例。
func TestCalibrationSaveExperimentParameter(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	t.Run("不存在的 id 返回业务错误", func(t *testing.T) {
		resp, err := testClient.Primary().CalibrationSaveExperimentParameter("non-existent-calibration-id")
		if err != nil {
			t.Fatalf("请求失败: %v", err)
		}
		assert.IsError(resp, 2, "不存在的 id 返回业务错误")
	})

	t.Run("缺少 id 返回 HTTP 400", func(t *testing.T) {
		rawResp, err := testClient.Primary().CalibrationSaveExperimentParameterRaw(map[string]any{})
		if err != nil {
			t.Fatalf("请求失败: %v", err)
		}
		body, err := helpers.ReadAndCloseRawBody(rawResp)
		if err != nil {
			t.Fatal(err)
		}
		if rawResp.StatusCode != http.StatusBadRequest {
			t.Fatalf("缺少 id 返回 HTTP 400: 期望 HTTP 400，实际=%d body=%q", rawResp.StatusCode, string(body))
		}
		assertRawJSONStringBody(t, "缺少 id 返回 HTTP 400", body, "")
	})
}

// TestCalibrationSaveNewExperimentParameter 覆盖 POST /calibration/experiment/parameter/save/new。
// TODO: 正向保存依赖已完成的标定结果，待 completed 标定结果夹具稳定后补充成功用例。
func TestCalibrationSaveNewExperimentParameter(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	t.Run("不存在的 id 返回业务错误", func(t *testing.T) {
		resp, err := testClient.Primary().CalibrationSaveNewExperimentParameter("non-existent-calibration-id")
		if err != nil {
			t.Fatalf("请求失败: %v", err)
		}
		assert.IsError(resp, 2, "不存在的 id 返回业务错误")
	})

	t.Run("缺少 id 返回 HTTP 400", func(t *testing.T) {
		rawResp, err := testClient.Primary().CalibrationSaveNewExperimentParameterRaw(map[string]any{})
		if err != nil {
			t.Fatalf("请求失败: %v", err)
		}
		body, err := helpers.ReadAndCloseRawBody(rawResp)
		if err != nil {
			t.Fatal(err)
		}
		if rawResp.StatusCode != http.StatusBadRequest {
			t.Fatalf("缺少 id 返回 HTTP 400: 期望 HTTP 400，实际=%d body=%q", rawResp.StatusCode, string(body))
		}
		assertRawJSONStringBody(t, "缺少 id 返回 HTTP 400", body, "")
	})
}

// parameterCalibrationFixture 保存参数标定接口用例所需的临时模型、默认实验、准备记录和 CSV 数据。
type parameterCalibrationFixture struct {
	PackageID           string
	PackageName         string
	ModelName           string
	DefaultExperimentID string
	PreparationID       string
	CSVFileName         string
	CSVContent          []byte
	CalibrationIDs      []string
}

// createParameterCalibrationModelFixture 上传 SimpleCar 模型并触发默认实验创建。
// 该 helper 不生成标定准备记录，供 translate/fmu 接口的正向用例自行调用目标接口。
func createParameterCalibrationModelFixture(t *testing.T) *parameterCalibrationFixture {
	t.Helper()

	packageName := uniqueName("ParameterCalibration")
	modelName := packageName + ".SimpleCar"
	modelContent := readUniqueParameterCalibrationModel(t, packageName)

	resp, err := testClient.UploadModelPackage(packageName+".mo", []byte(modelContent))
	if err != nil {
		t.Fatalf("上传参数标定模型失败: %v", err)
	}
	if resp.Status != 0 {
		t.Fatalf("上传参数标定模型返回非成功状态: status=%d msg=%s err=%s", resp.Status, resp.Msg, resp.Err)
	}

	packageID := findPackageIDByName(t, packageName, "上传参数标定模型")
	if packageID == "" {
		t.Fatalf("上传参数标定模型: 未找到 package_id")
	}
	if err := modelhelpers.EnsureModelLoaded(testClient, packageID, modelName, "参数标定模型加载"); err != nil {
		t.Fatal(err)
	}

	// 参数标定创建逻辑依赖实验记录；ExperimentListView 会为新模型懒创建默认实验。
	experimentResp, err := testClient.ExperimentGet(packageID, modelName)
	if err != nil {
		t.Fatalf("触发参数标定模型默认实验创建失败: %v", err)
	}
	if experimentResp.Status != 0 {
		t.Fatalf("触发参数标定模型默认实验创建返回非成功状态: status=%d msg=%s err=%s", experimentResp.Status, experimentResp.Msg, experimentResp.Err)
	}

	csvPath := parameterCalibrationResourcePath(t, "Acceleration measurements.csv")
	csvContent, err := os.ReadFile(csvPath)
	if err != nil {
		t.Fatalf("读取参数标定 CSV 失败: %v", err)
	}

	return &parameterCalibrationFixture{
		PackageID:           packageID,
		PackageName:         packageName,
		ModelName:           modelName,
		DefaultExperimentID: findDefaultExperimentID(t, experimentResp, "参数标定模型默认实验"),
		CSVFileName:         filepath.Base(csvPath),
		CSVContent:          csvContent,
	}
}

// createParameterCalibrationFixture 上传 SimpleCar 模型并生成参数标定准备记录。
func createParameterCalibrationFixture(t *testing.T) *parameterCalibrationFixture {
	t.Helper()

	fixture := createParameterCalibrationModelFixture(t)
	translateResp, err := testClient.CalibrationTranslateFmu(fixture.PackageID, fixture.ModelName)
	if err != nil {
		t.Fatalf("生成标定准备记录失败: %v", err)
	}
	if translateResp.Status != 0 {
		t.Fatalf("生成标定准备记录返回非成功状态: status=%d msg=%s err=%s", translateResp.Status, translateResp.Msg, translateResp.Err)
	}
	preparationData, err := helpers.ObjectData(translateResp, "生成标定准备记录")
	if err != nil {
		t.Fatal(err)
	}
	preparationID, ok := preparationData["preparation_id"].(string)
	if !ok || preparationID == "" {
		t.Fatalf("生成标定准备记录: preparation_id 为空或类型错误: %v", preparationData["preparation_id"])
	}
	fixture.PreparationID = preparationID
	if err := modelhelpers.WaitCompileFinished(testClient, fixture.PackageID, fixture.ModelName, "生成标定准备记录"); err != nil {
		t.Fatal(err)
	}
	return fixture
}

// createParameterCalibrationFixtureWithCSV 生成标定准备记录并上传测试 CSV。
func createParameterCalibrationFixtureWithCSV(t *testing.T) *parameterCalibrationFixture {
	t.Helper()

	fixture := createParameterCalibrationFixture(t)
	resp, err := testClient.UploadCalibrationData(fixture.PreparationID, false, fixture.CSVFileName, fixture.CSVContent)
	if err != nil {
		t.Fatalf("上传参数标定 CSV 失败: %v", err)
	}
	if resp.Status != 0 {
		t.Fatalf("上传参数标定 CSV 返回非成功状态: status=%d msg=%s err=%s", resp.Status, resp.Msg, resp.Err)
	}
	return fixture
}

// createParameterCalibrationRecordFixture 创建一条可用于查询、编辑、删除的标定记录夹具。
func createParameterCalibrationRecordFixture(t *testing.T, calibrationName string) (*parameterCalibrationFixture, string) {
	t.Helper()

	fixture := createParameterCalibrationFixtureWithCSV(t)
	calibrationID := createParameterCalibrationRecord(t, fixture, calibrationName)
	return fixture, calibrationID
}

// createParameterCalibrationRecord 基于已有准备记录和默认实验创建标定记录。
func createParameterCalibrationRecord(t *testing.T, fixture *parameterCalibrationFixture, calibrationName string) string {
	t.Helper()

	resp, err := testClient.CalibrationCreate(parameterCalibrationCreatePayload(fixture.PreparationID, fixture.DefaultExperimentID, calibrationName))
	if err != nil {
		t.Fatalf("创建标定记录失败: %v", err)
	}
	if resp.Status != 0 {
		t.Fatalf("创建标定记录返回非成功状态: status=%d msg=%s err=%s", resp.Status, resp.Msg, resp.Err)
	}
	dataMap, err := helpers.ObjectData(resp, "创建标定记录")
	if err != nil {
		t.Fatal(err)
	}
	id, ok := dataMap["id"].(string)
	if !ok || id == "" {
		t.Fatalf("创建标定记录返回 id 为空或类型错误: %v", dataMap["id"])
	}
	fixture.CalibrationIDs = append(fixture.CalibrationIDs, id)
	return id
}

// startParameterCalibrationTask 启动一条标定任务，并在失败时输出完整响应信息。
func startParameterCalibrationTask(t *testing.T, calibrationID string) *helpers.Response {
	t.Helper()
	resp, err := testClient.CalibrationStart(calibrationID)
	if err != nil {
		t.Fatalf("启动标定任务失败: %v", err)
	}
	if resp.Status != 0 {
		t.Fatalf("启动标定任务返回非成功状态: status=%d msg=%s err=%s data=%v", resp.Status, resp.Msg, resp.Err, resp.Data)
	}
	return resp
}

// cleanupParameterCalibrationFixture 清理参数标定用例创建的标定记录、默认实验和临时模型包。
func cleanupParameterCalibrationFixture(t *testing.T, fixture *parameterCalibrationFixture) {
	t.Helper()
	if fixture == nil {
		return
	}
	for _, id := range fixture.CalibrationIDs {
		// 启动成功的标定记录可能处于 running；清理阶段先尝试停止，避免删除接口拒绝运行中任务。
		_, _ = testClient.CalibrationStop(id)
		_, _ = testClient.CalibrationDelete(id)
	}

	var experiments []DataBase.YssimExperimentRecord
	_ = config.DB.Where("package_id = ? AND model_name = ?", fixture.PackageID, fixture.ModelName).Find(&experiments).Error
	for _, experiment := range experiments {
		// 子用例中途失败时，已创建的标定记录可能没有写入 fixture.CalibrationIDs；按实验反查可以兜底清理。
		var calibrations []DataBase.ParameterCalibration
		_ = config.DB.Where("experiment_id = ?", experiment.ID).Find(&calibrations).Error
		for _, calibration := range calibrations {
			_, _ = testClient.CalibrationStop(calibration.ID)
			_, _ = testClient.CalibrationDelete(calibration.ID)
		}
		simulationhelpers.DeleteExperiment(testClient, experiment.ID)
	}

	modelhelpers.CleanupTempModelFixture(testClient, fixture.PackageID, fixture.PackageName)
}

// readUniqueParameterCalibrationModel 读取测试资源中的 SimpleCar 模型并替换为唯一包名。
func readUniqueParameterCalibrationModel(t *testing.T, packageName string) string {
	t.Helper()
	modelPath := parameterCalibrationResourcePath(t, "ParameterCalibration.mo")
	contentBytes, err := os.ReadFile(modelPath)
	if err != nil {
		t.Fatalf("读取参数标定模型失败: %v", err)
	}
	content := string(contentBytes)
	// 不改动测试资源文件本身，只在内存中替换顶层 package/end 名称，保证上传包可重复创建。
	content = strings.Replace(content, "package ParameterCalibration ", "package "+packageName+" ", 1)
	content = strings.Replace(content, "end ParameterCalibration;", "end "+packageName+";", 1)
	return content
}

// parameterCalibrationResourcePath 返回参数标定测试资源文件的绝对路径。
// 通过 runtime.Caller 定位当前测试文件，避免 go test 工作目录变化导致相对路径失效。
func parameterCalibrationResourcePath(t *testing.T, fileName string) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("获取当前测试文件路径失败")
	}
	return filepath.Join(filepath.Dir(currentFile), "..", "resources", "ParameterCalibration", fileName)
}

// parameterCalibrationCreatePayload 构造一份可创建标定记录的基础 payload。
func parameterCalibrationCreatePayload(preparationID, experimentID, calibrationName string) map[string]any {
	return map[string]any{
		"preparation_id":   preparationID,
		"calibration_name": calibrationName,
		"experiment_id":    experimentID,
		"tuner_parameter": []map[string]any{{
			"name":  "engine.tau_0",
			"min":   80,
			"max":   200,
			"start": 100,
		}},
		"result_couplings": []map[string]any{{
			"csv_column":     "speed",
			"model_variable": "carBody.v",
		}},
		"cases": []map[string]any{{
			"name":       "calibrate",
			"type":       "calibrate",
			"start_time": 0,
			"stop_time":  6.24,
		}},
	}
}

// findDefaultExperimentID 从实验列表响应中提取默认实验 ID。
func findDefaultExperimentID(t *testing.T, resp *helpers.Response, message string) string {
	t.Helper()
	list := extractExperimentList(t, resp)
	for _, item := range list {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if itemMap["default"] == true {
			id, ok := itemMap["id"].(string)
			if ok && id != "" {
				return id
			}
		}
	}
	t.Fatalf("%s: 未找到默认实验，resp=%v", message, resp.Data)
	return ""
}

// extractCalibrationTaskID 从 start 接口响应中提取 data.task_id。
func extractCalibrationTaskID(t *testing.T, resp *helpers.Response, message string) string {
	t.Helper()
	dataMap, err := helpers.ObjectData(resp, message)
	if err != nil {
		t.Fatal(err)
	}
	taskID, ok := dataMap["task_id"].(string)
	if !ok || taskID == "" {
		t.Fatalf("%s: task_id 为空或类型错误: %v", message, dataMap["task_id"])
	}
	return taskID
}

// getParameterCalibrationRecord 从数据库读取指定标定记录。
func getParameterCalibrationRecord(t *testing.T, id string) DataBase.ParameterCalibration {
	t.Helper()
	var record DataBase.ParameterCalibration
	if err := config.DB.Where("id = ?", id).First(&record).Error; err != nil {
		t.Fatalf("查询标定记录失败: %v", err)
	}
	return record
}

// assertCalibrationDetailDataEvent 读取标定详情 SSE，直到拿到 data 事件或达到尝试上限。
func assertCalibrationDetailDataEvent(t *testing.T, rawResp *http.Response, message string) {
	t.Helper()
	reader := bufio.NewReader(rawResp.Body)
	for i := 0; i < 20; i++ {
		eventName, payload, err := helpers.ReadSSEEvent(reader)
		if err != nil {
			t.Fatal(err)
		}
		if eventName != "data" {
			continue
		}
		var data map[string]any
		if err := json.Unmarshal(payload, &data); err != nil {
			t.Fatalf("%s: 解析详情 SSE data 失败: %v, payload=%s", message, err, string(payload))
		}
		if len(data) == 0 {
			t.Fatalf("%s: 详情 SSE data 为空", message)
		}
		return
	}
	t.Fatalf("%s: 未在限定次数内读取到 data 事件", message)
}

// assertCalibrationCSVColumns 断言标定 CSV 列信息包含测试资源中的核心列。
func assertCalibrationCSVColumns(t *testing.T, message string, dataMap map[string]any, field string) {
	t.Helper()
	columns, ok := dataMap[field].([]any)
	if !ok {
		t.Fatalf("%s: %s 不是数组，实际=%T %v", message, field, dataMap[field], dataMap[field])
	}
	for _, want := range []string{"time", "speed", "dist", "acc"} {
		if !stringSliceAnyContains(columns, want) {
			t.Fatalf("%s: %s 未包含 %q，实际=%v", message, field, want, columns)
		}
	}
}

// assertCalibrationRecordState 通过数据库回查标定记录名称、状态和 task_id。
func assertCalibrationRecordState(t *testing.T, id, expectedName, expectedStatus, expectedTaskID string) {
	t.Helper()
	record := getParameterCalibrationRecord(t, id)
	if expectedName != "" && record.CalibrationName != expectedName {
		t.Fatalf("期望 calibration_name=%q，实际=%q", expectedName, record.CalibrationName)
	}
	if expectedStatus != "" && record.Status != expectedStatus {
		t.Fatalf("期望 calibration_status=%q，实际=%q", expectedStatus, record.Status)
	}
	if record.TaskId != expectedTaskID {
		t.Fatalf("期望 task_id=%q，实际=%q", expectedTaskID, record.TaskId)
	}
}

// calibrationTreeContainsVariable 判断标定变量树中是否包含指定变量名。
func calibrationTreeContainsVariable(items []any, want string) bool {
	for _, item := range items {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if itemMap["variables"] == want {
			return true
		}
	}
	return false
}

// calibrationListContainsID 判断标定列表响应中的 list 字段是否包含指定记录 ID。
func calibrationListContainsID(listValue any, id string) bool {
	list, ok := listValue.([]any)
	if !ok {
		return false
	}
	for _, item := range list {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if itemMap["id"] == id {
			return true
		}
	}
	return false
}

// stringSliceAnyContains 判断 []any 中是否包含指定字符串。
func stringSliceAnyContains(values []any, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}
