//go:build api_test

package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"yssim-go/app/DataBase"
	"yssim-go/config"
	"yssim-go/tests/helpers"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// TestSnapshotCreate 验证 POST /simulation/snapshot/create。
// 覆盖：
//   - 使用完整 payload 创建成功
//   - SimulateResultObj 被写入结果文件，数据库中存储的是文件路径
//   - 缺少必填字段返回 HTTP 400
func TestSnapshotCreate(t *testing.T) {
	assert := helpers.NewAssertResponse(t)
	var snapshotId string

	tests := []struct {
		name               string
		buildPayload       func(t *testing.T) map[string]any
		useRaw             bool
		expectedHTTPStatus int
	}{
		{
			name: "使用完整 payload 创建成功",
			buildPayload: func(t *testing.T) map[string]any {
				return newSnapshotCreatePayload(t)
			},
		},
		{
			name: "缺少必填字段返回 HTTP 400",
			buildPayload: func(t *testing.T) map[string]any {
				return map[string]any{
					"snapshot_name": "only-name",
				}
			},
			useRaw:             true,
			expectedHTTPStatus: http.StatusBadRequest,
		},
	}

	hooks := helpers.TestHooks{
		After: func(t *testing.T) {
			cleanupSnapshotCreatedByAPI(t, snapshotId)
		},
	}
	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				payload := tt.buildPayload(t)
				if tt.useRaw {
					rawResp, err := testClient.PostRawResponse("/simulation/snapshot/create", payload)
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

				resp, err := testClient.Primary().SnapshotCreate(payload)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				assert.IsSuccess(resp, tt.name)

				snapshotId = snapshotIDFromCreateResponse(t, resp)
				storedPath := lookupSnapshotStoredResultPath(t, snapshotId)
				if storedPath == "" {
					t.Fatalf("%s: 数据库中的 simulate_result_obj 路径为空", tt.name)
				}
				if filepath.Ext(storedPath) != ".json" {
					t.Errorf("%s: simulate_result_obj 路径应指向 json 文件，实际=%q", tt.name, storedPath)
				}
				if filepath.IsAbs(storedPath) {
					t.Logf("%s: simulate_result_obj 存储为绝对路径 %q", tt.name, storedPath)
				}

				detailResp, err := testClient.SnapshotDetail(snapshotId)
				if err != nil {
					t.Fatalf("%s: 创建后查询快照详情失败: %v", tt.name, err)
				}
				assert.IsSuccess(detailResp, tt.name+": 创建后查询详情")
				detailData, ok := detailResp.Data.(map[string]any)
				if !ok {
					t.Fatalf("%s: 详情 data 不是 object: %T", tt.name, detailResp.Data)
				}
				helpers.AssertJSONEquivalent(t, detailData["simulateResult_obj"], payload["simulate_result_obj"], tt.name+": simulate_result_obj 详情回读")
			})
		}
	})
}

// TestSnapshotList 验证 GET /simulation/snapshot/list。
// 覆盖：
//   - 正常获取快照列表
//   - 不存在的模型过滤后返回空列表
func TestSnapshotList(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var snapshotId string
	var snapshotPayload map[string]any
	var snapshotName string

	tests := []struct {
		name        string
		modelName   string
		packageId   string
		expectEmpty bool
		mustContain string
	}{
		{
			name:        "正常获取快照列表",
			modelName:   "",
			packageId:   "",
			mustContain: "",
		},
		{
			name:        "不存在的模型返回空列表",
			modelName:   "Non.Existent.Snapshot.Model",
			packageId:   "",
			expectEmpty: true,
		},
	}

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			snapshotId, snapshotPayload = createSnapshotFixtureByAPI(t)
			snapshotName = snapshotPayloadString(t, snapshotPayload, "snapshot_name")
			tests[0].modelName = snapshotPayloadString(t, snapshotPayload, "model_name")
			tests[0].packageId = snapshotPayloadString(t, snapshotPayload, "package_id")
			tests[1].packageId = tests[0].packageId
			tests[0].mustContain = snapshotName
		},
		After: func(t *testing.T) {
			cleanupSnapshotCreatedByAPI(t, snapshotId)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				resp, err := testClient.Primary().SnapshotList(tt.modelName, tt.packageId)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				assert.IsSuccess(resp, tt.name)

				list := extractSnapshotList(t, resp)
				if tt.expectEmpty {
					if len(list) != 0 {
						t.Errorf("%s: 期望空列表，实际 %d 条", tt.name, len(list))
					}
					return
				}
				if len(list) == 0 {
					t.Fatalf("%s: 期望非空列表，实际为空", tt.name)
				}
				item := snapshotItemByName(list, tt.mustContain)
				if item == nil {
					t.Fatalf("%s: 列表中未找到快照 %q", tt.name, tt.mustContain)
				}
				for _, field := range []string{"id", "snapshot_name", "updated_time"} {
					if _, ok := item[field]; !ok {
						t.Errorf("%s: 快照列表项缺少字段 %q", tt.name, field)
					}
				}
			})
		}
	})
}

// TestSnapshotDetail 验证 GET /simulation/snapshot/detail。
// 覆盖：
//   - 正常获取快照详情
//   - 缺少 snapshot_id 返回 HTTP 400
//   - 不存在的 snapshot_id 返回 HTTP 404
func TestSnapshotDetail(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var snapshotId string
	var snapshotPayload map[string]any

	tests := []struct {
		name               string
		snapshotIdLiteral  string
		useRaw             bool
		expectedHTTPStatus int
		expectNotFound     bool
	}{
		{
			name:              "正常获取快照详情",
			snapshotIdLiteral: "",
		},
		{
			name:               "缺少 snapshot_id 返回 HTTP 400",
			snapshotIdLiteral:  "",
			useRaw:             true,
			expectedHTTPStatus: http.StatusBadRequest,
		},
		{
			name:               "不存在的 snapshot_id 返回 HTTP 404",
			snapshotIdLiteral:  "non-existent-snapshot-id",
			useRaw:             true,
			expectedHTTPStatus: http.StatusNotFound,
			expectNotFound:     true,
		},
	}

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			snapshotId, snapshotPayload = createSnapshotFixtureByAPI(t)
		},
		After: func(t *testing.T) {
			cleanupSnapshotCreatedByAPI(t, snapshotId)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				targetId := tt.snapshotIdLiteral
				if targetId == "" {
					targetId = snapshotId
				}

				if tt.useRaw {
					params := map[string]string{}
					if !tt.expectNotFound {
						// 缺参场景保持 query 为空
					} else {
						params["snapshot_id"] = targetId
					}
					rawResp, err := testClient.GetRawResponse("/simulation/snapshot/detail", params)
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
					resp, err := helpers.UnmarshalResponse(body)
					if err != nil {
						t.Fatal(err)
					}
					if tt.expectNotFound {
						assert.IsError(resp, 2, tt.name)
					} else {
						assert.IsError(resp, 2, tt.name)
					}
					return
				}

				resp, err := testClient.Primary().SnapshotDetail(targetId)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				assert.IsSuccess(resp, tt.name)
				assert.HasFields(resp, []string{"id", "snapshot_name", "updated_time", "component_name", "modelVar_data", "experiment_id", "simulateVar_data", "simulateResult_id", "simulateResult_obj", "active_case_highlight", "check_experiment_list"}, tt.name)

				dataMap, ok := resp.Data.(map[string]any)
				if !ok {
					t.Fatalf("%s: data 不是 object: %T", tt.name, resp.Data)
				}
				if dataMap["id"] != snapshotId {
					t.Errorf("%s: 期望 id=%q，实际=%v", tt.name, snapshotId, dataMap["id"])
				}
				if dataMap["snapshot_name"] != snapshotPayloadString(t, snapshotPayload, "snapshot_name") {
					t.Errorf("%s: 期望 snapshot_name=%q，实际=%v", tt.name, snapshotPayloadString(t, snapshotPayload, "snapshot_name"), dataMap["snapshot_name"])
				}
				if dataMap["component_name"] != snapshotPayloadString(t, snapshotPayload, "component_name") {
					t.Errorf("%s: 期望 component_name=%q，实际=%v", tt.name, snapshotPayloadString(t, snapshotPayload, "component_name"), dataMap["component_name"])
				}
				if dataMap["experiment_id"] != snapshotPayloadString(t, snapshotPayload, "experiment_id") {
					t.Errorf("%s: 期望 experiment_id=%q，实际=%v", tt.name, snapshotPayloadString(t, snapshotPayload, "experiment_id"), dataMap["experiment_id"])
				}
				if dataMap["simulateResult_id"] != snapshotPayloadString(t, snapshotPayload, "simulate_result_id") {
					t.Errorf("%s: 期望 simulateResult_id=%q，实际=%v", tt.name, snapshotPayloadString(t, snapshotPayload, "simulate_result_id"), dataMap["simulateResult_id"])
				}
				helpers.AssertJSONEquivalent(t, dataMap["modelVar_data"], snapshotPayload["model_var_data"], tt.name+": modelVar_data")
				helpers.AssertJSONEquivalent(t, dataMap["simulateVar_data"], snapshotPayload["simulate_var_data"], tt.name+": simulateVar_data")
				helpers.AssertJSONEquivalent(t, dataMap["simulateResult_obj"], snapshotPayload["simulate_result_obj"], tt.name+": simulateResult_obj")
				helpers.AssertJSONEquivalent(t, dataMap["active_case_highlight"], snapshotPayload["active_case_highlight"], tt.name+": active_case_highlight")
				helpers.AssertJSONEquivalent(t, dataMap["check_experiment_list"], snapshotPayload["check_experiment_list"], tt.name+": check_experiment_list")
			})
		}
	})
}

// TestSnapshotEdit 验证 POST /simulation/snapshot/edit。
// 覆盖：
//   - 正常编辑快照并通过 detail 回读
//   - 编辑为已存在名称时保持现状返回 success + 提示文案
//   - 编辑不存在的 snapshot_id 当前幂等成功
//   - 缺少 snapshot_id 返回 HTTP 400
func TestSnapshotEdit(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var normalSnapshotId string
	var normalSnapshotPayload map[string]any
	var conflictTargetSnapshotId string
	var conflictTargetSnapshotPayload map[string]any
	var conflictExistingSnapshotId string
	var conflictExistingSnapshotPayload map[string]any

	var editedPayload map[string]any
	var duplicatePayload map[string]any
	var missingIDPayload map[string]any

	tests := []struct {
		name               string
		buildPayload       func(t *testing.T) map[string]any
		useRaw             bool
		expectedHTTPStatus int
		assertResponse     func(t *testing.T, resp *helpers.Response)
	}{
		{
			name: "正常编辑快照",
			buildPayload: func(t *testing.T) map[string]any {
				editedPayload = buildSnapshotEditPayload(t, normalSnapshotId, normalSnapshotPayload)
				editedPayload["snapshot_name"] = snapshotPayloadString(t, normalSnapshotPayload, "snapshot_name") + "_edited"
				editedPayload["component_name"] = "edited_component"
				editedPayload["simulate_result_id"] = "edited-result-id"
				editedPayload["simulate_var_data"] = map[string]any{
					"interval":          "0.004",
					"method":            "Lsodar",
					"multiple_run_sim":  false,
					"numberOfIntervals": "250",
					"simulate_type":     "DM",
					"startTime":         "1",
					"stopTime":          "6",
					"tolerance":         "1e-5",
				}
				editedPayload["model_var_data"] = map[string]any{
					"parameters": []any{
						map[string]any{"name": "gain", "value": "2.5"},
					},
					"properties": map[string]any{"annotation": "edited"},
				}
				editedPayload["simulate_result_obj"] = map[string]any{
					"chartMap": []any{
						map[string]any{"key": "curve_1", "visible": true},
					},
					"nodeData": []any{
						map[string]any{"key": "node_1"},
					},
					"checkedKeys": []any{"node_1"},
					"expandKeys":  []any{"TREE_ROOT", "node_1"},
					"currentKey":  "node_1",
					"displayUnitMap": map[string]any{
						"speed": "rpm",
					},
				}
				editedPayload["active_case_highlight"] = map[string]any{
					"recordId":    "edited-record-id",
					"caseId":      "edited-case-id",
					"caseName":    "edited_case",
					"displayName": "edited_case",
				}
				editedPayload["check_experiment_list"] = []any{"edited-record-id", "edited-record-id-2"}
				return editedPayload
			},
			assertResponse: func(t *testing.T, resp *helpers.Response) {
				assert.IsSuccess(resp, "正常编辑快照")
				if resp.Msg != "视图记录已更新" {
					t.Errorf("正常编辑快照: 期望 msg=%q，实际=%q", "视图记录已更新", resp.Msg)
				}
				assertSnapshotDetailMatchesPayload(t, normalSnapshotId, editedPayload, "正常编辑快照")
			},
		},
		{
			name: "编辑为已存在名称返回提示",
			buildPayload: func(t *testing.T) map[string]any {
				duplicatePayload = buildSnapshotEditPayload(t, conflictTargetSnapshotId, conflictTargetSnapshotPayload)
				duplicatePayload["snapshot_name"] = snapshotPayloadString(t, conflictExistingSnapshotPayload, "snapshot_name")
				return duplicatePayload
			},
			assertResponse: func(t *testing.T, resp *helpers.Response) {
				assert.IsSuccess(resp, "编辑为已存在名称返回提示")
				if resp.Msg != "视图记录名称已存在，请更换。" {
					t.Errorf("编辑为已存在名称返回提示: 期望 msg=%q，实际=%q", "视图记录名称已存在，请更换。", resp.Msg)
				}
				detailResp, err := testClient.SnapshotDetail(conflictTargetSnapshotId)
				if err != nil {
					t.Fatalf("编辑为已存在名称返回提示: 查询详情失败: %v", err)
				}
				assert.IsSuccess(detailResp, "编辑为已存在名称返回提示: 查询详情")
				dataMap, ok := detailResp.Data.(map[string]any)
				if !ok {
					t.Fatalf("编辑为已存在名称返回提示: data 不是 object: %T", detailResp.Data)
				}
				if dataMap["snapshot_name"] != snapshotPayloadString(t, conflictTargetSnapshotPayload, "snapshot_name") {
					t.Errorf("编辑为已存在名称返回提示: 期望 snapshot_name=%q，实际=%v", snapshotPayloadString(t, conflictTargetSnapshotPayload, "snapshot_name"), dataMap["snapshot_name"])
				}
				if dataMap["component_name"] != snapshotPayloadString(t, conflictTargetSnapshotPayload, "component_name") {
					t.Errorf("编辑为已存在名称返回提示: 期望 component_name=%q，实际=%v", snapshotPayloadString(t, conflictTargetSnapshotPayload, "component_name"), dataMap["component_name"])
				}
				if dataMap["experiment_id"] != snapshotPayloadString(t, conflictTargetSnapshotPayload, "experiment_id") {
					t.Errorf("编辑为已存在名称返回提示: 期望 experiment_id=%q，实际=%v", snapshotPayloadString(t, conflictTargetSnapshotPayload, "experiment_id"), dataMap["experiment_id"])
				}
				if dataMap["simulateResult_id"] != snapshotPayloadString(t, conflictTargetSnapshotPayload, "simulate_result_id") {
					t.Errorf("编辑为已存在名称返回提示: 期望 simulateResult_id=%q，实际=%v", snapshotPayloadString(t, conflictTargetSnapshotPayload, "simulate_result_id"), dataMap["simulateResult_id"])
				}
				helpers.AssertJSONEquivalent(t, dataMap["modelVar_data"], conflictTargetSnapshotPayload["model_var_data"], "编辑为已存在名称返回提示: modelVar_data")
				helpers.AssertJSONEquivalent(t, dataMap["simulateVar_data"], conflictTargetSnapshotPayload["simulate_var_data"], "编辑为已存在名称返回提示: simulateVar_data")
				helpers.AssertJSONEquivalent(t, dataMap["active_case_highlight"], conflictTargetSnapshotPayload["active_case_highlight"], "编辑为已存在名称返回提示: active_case_highlight")
				helpers.AssertJSONEquivalent(t, dataMap["check_experiment_list"], conflictTargetSnapshotPayload["check_experiment_list"], "编辑为已存在名称返回提示: check_experiment_list")
			},
		},
		{
			name: "编辑不存在的快照当前幂等成功",
			buildPayload: func(t *testing.T) map[string]any {
				missingIDPayload = buildSnapshotEditPayload(t, "non-existent-snapshot-id", normalSnapshotPayload)
				missingIDPayload["snapshot_name"] = "missing_snapshot_" + uuid.NewString()[:8]
				return missingIDPayload
			},
			assertResponse: func(t *testing.T, resp *helpers.Response) {
				assert.IsSuccess(resp, "编辑不存在的快照当前幂等成功")
				if resp.Msg != "视图记录已更新" {
					t.Errorf("编辑不存在的快照当前幂等成功: 期望 msg=%q，实际=%q", "视图记录已更新", resp.Msg)
				}
				rawResp, err := testClient.GetRawResponse("/simulation/snapshot/detail", map[string]string{"snapshot_id": "non-existent-snapshot-id"})
				if err != nil {
					t.Fatalf("编辑不存在的快照当前幂等成功: 查询详情失败: %v", err)
				}
				body, err := helpers.ReadAndCloseRawBody(rawResp)
				if err != nil {
					t.Fatal(err)
				}
				if rawResp.StatusCode != http.StatusNotFound {
					t.Fatalf("编辑不存在的快照当前幂等成功: 期望 HTTP 404，实际 %d，body=%q", rawResp.StatusCode, string(body))
				}
			},
		},
		{
			name: "缺少 snapshot_id 返回 HTTP 400",
			buildPayload: func(t *testing.T) map[string]any {
				payload := buildSnapshotEditPayload(t, normalSnapshotId, normalSnapshotPayload)
				delete(payload, "snapshot_id")
				return payload
			},
			useRaw:             true,
			expectedHTTPStatus: http.StatusBadRequest,
		},
	}

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			normalSnapshotId, normalSnapshotPayload = createSnapshotFixtureByAPI(t)
			conflictTargetSnapshotId, conflictTargetSnapshotPayload = createSnapshotFixtureByAPI(t)
			conflictExistingSnapshotId, conflictExistingSnapshotPayload = createSnapshotFixtureByAPI(t)
		},
		After: func(t *testing.T) {
			cleanupSnapshotCreatedByAPI(t, normalSnapshotId)
			cleanupSnapshotCreatedByAPI(t, conflictTargetSnapshotId)
			cleanupSnapshotCreatedByAPI(t, conflictExistingSnapshotId)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				payload := tt.buildPayload(t)
				if tt.useRaw {
					rawResp, err := testClient.PostRawResponse("/simulation/snapshot/edit", payload)
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

				resp, err := testClient.Primary().SnapshotEdit(payload)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				tt.assertResponse(t, resp)
			})
		}
	})
}

// TestSnapshotLifecycle 验证创建→编辑→删除全链路。
func TestSnapshotLifecycle(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var snapshotId string
	var snapshotPayload map[string]any
	var editedPayload map[string]any

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			snapshotId, snapshotPayload = createSnapshotFixtureByAPI(t)
		},
		After: func(t *testing.T) {
			cleanupSnapshotCreatedByAPI(t, snapshotId)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		t.Run("创建后编辑再删除全链路", func(t *testing.T) {
			editedPayload = buildSnapshotEditPayload(t, snapshotId, snapshotPayload)
			editedPayload["snapshot_name"] = snapshotPayloadString(t, snapshotPayload, "snapshot_name") + "_lifecycle"
			editedPayload["component_name"] = "lifecycle_component"
			editedPayload["simulate_result_obj"] = map[string]any{
				"chartMap": []any{
					map[string]any{"key": "life_curve"},
				},
				"nodeData":    []any{},
				"checkedKeys": []any{"life_curve"},
				"expandKeys":  []any{"TREE_ROOT", "life_curve"},
				"currentKey":  "life_curve",
				"displayUnitMap": map[string]any{
					"flow": "kg/s",
				},
			}

			resp, err := testClient.SnapshotEdit(editedPayload)
			if err != nil {
				t.Fatalf("编辑快照失败: %v", err)
			}
			assert.IsSuccess(resp, "创建后编辑再删除全链路")
			assertSnapshotDetailMatchesPayload(t, snapshotId, editedPayload, "创建后编辑再删除全链路")

			deleteResp, err := testClient.SnapshotDelete(snapshotId)
			if err != nil {
				t.Fatalf("删除快照失败: %v", err)
			}
			assert.IsSuccess(deleteResp, "创建后编辑再删除全链路: 删除")

			rawResp, err := testClient.GetRawResponse("/simulation/snapshot/detail", map[string]string{"snapshot_id": snapshotId})
			if err != nil {
				t.Fatalf("删除后查询详情失败: %v", err)
			}
			body, err := helpers.ReadAndCloseRawBody(rawResp)
			if err != nil {
				t.Fatal(err)
			}
			if rawResp.StatusCode != http.StatusNotFound {
				t.Fatalf("创建后编辑再删除全链路: 期望 HTTP 404，实际 %d，body=%q", rawResp.StatusCode, string(body))
			}
			resp404, err := helpers.UnmarshalResponse(body)
			if err != nil {
				t.Fatal(err)
			}
			assert.IsError(resp404, 2, "创建后编辑再删除全链路: 删除后详情返回 404")
		})
	})
}

// TestSnapshotDelete 验证 POST /simulation/snapshot/delete。
// 覆盖：
//   - 正常删除快照
//   - 删除不存在的快照（幂等成功）
//   - 删除后详情返回 HTTP 404
func TestSnapshotDelete(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var snapshotId string

	tests := []struct {
		name              string
		snapshotIdLiteral string
		expectError       bool
		expectedCode      int
	}{
		{
			name:              "正常删除快照",
			snapshotIdLiteral: "",
		},
		{
			name:              "删除不存在的快照幂等成功",
			snapshotIdLiteral: "non-existent-snapshot-id",
		},
	}

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			snapshotId, _ = createSnapshotFixtureByAPI(t)
		},
		After: func(t *testing.T) {
			cleanupSnapshotCreatedByAPI(t, snapshotId)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				targetId := tt.snapshotIdLiteral
				if targetId == "" {
					targetId = snapshotId
				}
				resp, err := testClient.Primary().SnapshotDelete(targetId)
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

		t.Run("删除后详情返回 HTTP 404", func(t *testing.T) {
			rawResp, err := testClient.GetRawResponse("/simulation/snapshot/detail", map[string]string{"snapshot_id": snapshotId})
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			body, err := helpers.ReadAndCloseRawBody(rawResp)
			if err != nil {
				t.Fatal(err)
			}
			if rawResp.StatusCode != http.StatusNotFound {
				t.Fatalf("期望 HTTP 404，实际 %d，body=%q", rawResp.StatusCode, string(body))
			}
			resp, err := helpers.UnmarshalResponse(body)
			if err != nil {
				t.Fatal(err)
			}
			assert.IsError(resp, 2, "删除后详情返回 HTTP 404")
		})
	})
}

// extractSnapshotList 解析快照列表接口返回的 data 数组。
func extractSnapshotList(t *testing.T, resp *helpers.Response) []any {
	t.Helper()
	if resp.Data == nil {
		return nil
	}
	list, ok := resp.Data.([]any)
	if !ok {
		t.Fatalf("快照列表 data 不是数组: %T", resp.Data)
	}
	return list
}

// snapshotItemByName 按快照名称从列表中查找对应条目。
func snapshotItemByName(list []any, snapshotName string) map[string]any {
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if m["snapshot_name"] == snapshotName {
			return m
		}
	}
	return nil
}

// baseSnapshotCreatePayload 返回快照创建接口的基础有效入参模板。
func baseSnapshotCreatePayload() map[string]any {
	return map[string]any{
		"model_var_data": map[string]any{
			"parameters": []any{},
			"properties": map[string]any{},
		},
		"experiment_id": "afb16ff6-08f9-40cc-a2a2-5dc70ee03689",
		"simulate_var_data": map[string]any{
			"interval":          "0.008",
			"method":            "Dassl",
			"multiple_run_sim":  true,
			"numberOfIntervals": "500",
			"simulate_type":     "DM",
			"startTime":         "0",
			"stopTime":          "4",
			"tolerance":         "1e-6",
		},
		"simulate_result_id": "afb16ff6-08f9-40cc-a2a2-5dc70ee03689",
		"active_case_highlight": map[string]any{
			"recordId":    "afb16ff6-08f9-40cc-a2a2-5dc70ee03689",
			"caseId":      "7c7d5fef-7135-4f56-a8ce-f8581da1feee",
			"caseName":    "case_1",
			"displayName": "case_1",
		},
		"check_experiment_list": []any{
			"afb16ff6-08f9-40cc-a2a2-5dc70ee03689",
		},
		"model_name":     "PID_Controller",
		"package_id":     "d6abfc18-08c6-41fb-add1-1816879c76b7",
		"component_name": "",
		"simulate_result_obj": map[string]any{
			"chartMap":    []any{},
			"nodeData":    []any{},
			"checkedKeys": []any{},
			"expandKeys": []any{
				"TREE_ROOT",
			},
			"currentKey":     0,
			"displayUnitMap": map[string]any{},
		},
		"snapshot_name": "快照1",
	}
}

// newSnapshotCreatePayload 基于基础模板生成带唯一 snapshot_name 的创建 payload。
func newSnapshotCreatePayload(t *testing.T) map[string]any {
	t.Helper()
	payload := baseSnapshotCreatePayload()
	baseName := snapshotPayloadString(t, payload, "snapshot_name")
	payload["snapshot_name"] = fmt.Sprintf("%s_%s", baseName, uuid.NewString()[:8])
	return payload
}

// createSnapshotFixtureByAPI 通过创建接口生成测试快照，并返回快照 ID 与创建 payload。
func createSnapshotFixtureByAPI(t *testing.T) (string, map[string]any) {
	t.Helper()
	payload := newSnapshotCreatePayload(t)
	resp, err := testClient.SnapshotCreate(payload)
	if err != nil {
		t.Fatalf("创建快照失败: %v", err)
	}
	if resp.Status != 0 {
		t.Fatalf("创建快照返回非零状态: status=%d, msg=%s, err=%s", resp.Status, resp.Msg, resp.Err)
	}
	return snapshotIDFromCreateResponse(t, resp), payload
}

// buildSnapshotEditPayload 基于已有快照 payload 复制出可用于编辑接口的请求体。
func buildSnapshotEditPayload(t *testing.T, snapshotId string, payload map[string]any) map[string]any {
	t.Helper()
	cloned := cloneSnapshotPayload(t, payload)
	cloned["snapshot_id"] = snapshotId
	return cloned
}

// cloneSnapshotPayload 深拷贝快照 payload，避免测试用例之间共享可变 map。
func cloneSnapshotPayload(t *testing.T, payload map[string]any) map[string]any {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("复制快照 payload 失败: %v", err)
	}
	var cloned map[string]any
	if err := json.Unmarshal(raw, &cloned); err != nil {
		t.Fatalf("反序列化快照 payload 失败: %v", err)
	}
	return cloned
}

// snapshotIDFromCreateResponse 从创建快照响应中提取生成的快照 ID。
func snapshotIDFromCreateResponse(t *testing.T, resp *helpers.Response) string {
	t.Helper()
	id, ok := resp.Data.(string)
	if !ok || id == "" {
		t.Fatalf("创建快照返回的 data 不是非空字符串: %T (%v)", resp.Data, resp.Data)
	}
	return id
}

// cleanupSnapshotCreatedByAPI 清理通过接口创建的快照记录及其结果文件。
// 若快照已在测试中被显式删除，则跳过第二次 delete API 调用，只做硬删除与文件清理。
func cleanupSnapshotCreatedByAPI(t *testing.T, snapshotId string) {
	t.Helper()
	if snapshotId == "" {
		return
	}
	path := lookupSnapshotStoredResultPath(t, snapshotId)
	if snapshotRecordExists(t, snapshotId) {
		_, _ = testClient.SnapshotDelete(snapshotId)
	}
	_ = config.DB.Unscoped().Where("id = ?", snapshotId).Delete(&DataBase.YssimSnapshots{}).Error
	if path != "" {
		_ = os.Remove(path)
		_ = os.RemoveAll(filepath.Dir(path))
	}
}

// snapshotRecordExists 判断快照记录当前是否仍处于未软删除状态。
func snapshotRecordExists(t *testing.T, snapshotId string) bool {
	t.Helper()
	if snapshotId == "" {
		return false
	}
	var count int64
	if err := config.DB.Model(&DataBase.YssimSnapshots{}).Where("id = ?", snapshotId).Count(&count).Error; err != nil {
		t.Fatalf("查询快照是否存在失败: %v", err)
	}
	return count > 0
}

// lookupSnapshotStoredResultPath 查询快照表中 simulate_result_obj 存储的文件路径。
func lookupSnapshotStoredResultPath(t *testing.T, snapshotId string) string {
	t.Helper()
	if snapshotId == "" {
		return ""
	}
	var snapshot DataBase.YssimSnapshots
	if err := config.DB.Unscoped().Select("simulate_result_obj").Where("id = ?", snapshotId).First(&snapshot).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return ""
		}
		t.Fatalf("查询快照 simulate_result_obj 路径失败: %v", err)
	}
	return snapshot.SimulateResultObj
}

// assertSnapshotDetailMatchesPayload 通过 detail 接口回读并断言关键字段与预期 payload 一致。
func assertSnapshotDetailMatchesPayload(t *testing.T, snapshotId string, expectedPayload map[string]any, message string) {
	t.Helper()
	resp, err := testClient.SnapshotDetail(snapshotId)
	if err != nil {
		t.Fatalf("%s: 查询快照详情失败: %v", message, err)
	}
	if resp.Status != 0 {
		t.Fatalf("%s: 快照详情返回非成功状态: status=%d, msg=%s, err=%s", message, resp.Status, resp.Msg, resp.Err)
	}

	dataMap, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("%s: detail data 不是 object: %T", message, resp.Data)
	}
	if dataMap["id"] != snapshotId {
		t.Errorf("%s: 期望 id=%q，实际=%v", message, snapshotId, dataMap["id"])
	}
	if dataMap["snapshot_name"] != snapshotPayloadString(t, expectedPayload, "snapshot_name") {
		t.Errorf("%s: 期望 snapshot_name=%q，实际=%v", message, snapshotPayloadString(t, expectedPayload, "snapshot_name"), dataMap["snapshot_name"])
	}
	if dataMap["component_name"] != snapshotPayloadString(t, expectedPayload, "component_name") {
		t.Errorf("%s: 期望 component_name=%q，实际=%v", message, snapshotPayloadString(t, expectedPayload, "component_name"), dataMap["component_name"])
	}
	if dataMap["experiment_id"] != snapshotPayloadString(t, expectedPayload, "experiment_id") {
		t.Errorf("%s: 期望 experiment_id=%q，实际=%v", message, snapshotPayloadString(t, expectedPayload, "experiment_id"), dataMap["experiment_id"])
	}
	if dataMap["simulateResult_id"] != snapshotPayloadString(t, expectedPayload, "simulate_result_id") {
		t.Errorf("%s: 期望 simulateResult_id=%q，实际=%v", message, snapshotPayloadString(t, expectedPayload, "simulate_result_id"), dataMap["simulateResult_id"])
	}
	helpers.AssertJSONEquivalent(t, dataMap["modelVar_data"], expectedPayload["model_var_data"], message+": modelVar_data")
	helpers.AssertJSONEquivalent(t, dataMap["simulateVar_data"], expectedPayload["simulate_var_data"], message+": simulateVar_data")
	helpers.AssertJSONEquivalent(t, dataMap["simulateResult_obj"], expectedPayload["simulate_result_obj"], message+": simulateResult_obj")
	helpers.AssertJSONEquivalent(t, dataMap["active_case_highlight"], expectedPayload["active_case_highlight"], message+": active_case_highlight")
	helpers.AssertJSONEquivalent(t, dataMap["check_experiment_list"], expectedPayload["check_experiment_list"], message+": check_experiment_list")
}

// snapshotPayloadString 读取 payload 中的字符串字段，不符合预期时直接失败。
func snapshotPayloadString(t *testing.T, payload map[string]any, key string) string {
	t.Helper()
	value, ok := payload[key].(string)
	if !ok {
		t.Fatalf("payload[%q] 不是 string: %T", key, payload[key])
	}
	return value
}
