//go:build api_test

package api

import (
	"encoding/json"
	"io"
	"net/http"
	"testing"

	"yssim-go/tests/helpers"
	modelhelpers "yssim-go/tests/helpers/model"
)

const (
	collectionTestPackageID      = modelicaStdLibId
	collectionCreateModelName    = "Modelica.Blocks.Examples.PID_Controller"
	collectionDuplicateModelName = "Modelica.Blocks.Continuous.PI"
	collectionReservedModelName  = "Modelica.Blocks.Math.Feedback"
)

// TestCreateCollection 验证 POST /model/collection/create。
// 覆盖：
//   - 正常收藏模型
//   - package 类型模型返回 status=2
//   - 重复收藏返回 status=2
//   - package_id / model_name 不存在时返回 HTTP 400 + JSON string body
func TestCreateCollection(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var duplicateID string

	tests := []struct {
		name               string
		packageID          string
		modelName          string
		expectError        bool
		expectedCode       int
		useRaw             bool
		expectedHTTPStatus int
		expectedBody       string
	}{
		{
			name:        "正常收藏模型",
			packageID:   collectionTestPackageID,
			modelName:   collectionCreateModelName,
			expectError: false,
		},
		{
			name:         "包类型模型不允许收藏",
			packageID:    collectionTestPackageID,
			modelName:    "Modelica",
			expectError:  true,
			expectedCode: 2,
		},
		{
			name:         "重复收藏同一模型应返回 status=2",
			packageID:    collectionTestPackageID,
			modelName:    collectionDuplicateModelName,
			expectError:  true,
			expectedCode: 2,
		},
		{
			name:               "不存在的 package_id 返回 HTTP 400",
			packageID:          "non-existent-package-id",
			modelName:          collectionReservedModelName,
			useRaw:             true,
			expectedHTTPStatus: http.StatusBadRequest,
			expectedBody:       "not found",
		},
		{
			name:               "不存在的模型返回 HTTP 400",
			packageID:          collectionTestPackageID,
			modelName:          "Non.Existent.Collection.Model",
			useRaw:             true,
			expectedHTTPStatus: http.StatusBadRequest,
			expectedBody:       "model not found",
		},
	}

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			if err := modelhelpers.EnsureSystemModelLoaded(testClient, collectionTestPackageID, "", "收藏创建前置加载 Modelica"); err != nil {
				t.Fatal(err)
			}
			cleanupCollectionByModelName(t, collectionCreateModelName)
			cleanupCollectionByModelName(t, collectionDuplicateModelName)
			cleanupCollectionByModelName(t, collectionReservedModelName)
			duplicateID = createCollectionFixture(t, collectionTestPackageID, collectionDuplicateModelName)
		},
		After: func(t *testing.T) {
			deleteCollectionByID(t, duplicateID)
			cleanupCollectionByModelName(t, collectionCreateModelName)
			cleanupCollectionByModelName(t, collectionDuplicateModelName)
			cleanupCollectionByModelName(t, collectionReservedModelName)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				if tt.useRaw {
					rawResp, err := testClient.PostRawResponse("/model/collection/create", map[string]any{
						"package_id": tt.packageID,
						"model_name": tt.modelName,
					})
					if err != nil {
						t.Fatalf("请求失败: %v", err)
					}
					body, err := helpers.ReadAndCloseRawBody(rawResp)
					if err != nil {
						t.Fatal(err)
					}
					if rawResp.StatusCode != tt.expectedHTTPStatus {
						t.Fatalf("%s: 期望 HTTP %d，实际 %d，body=%s", tt.name, tt.expectedHTTPStatus, rawResp.StatusCode, string(body))
					}
					assertRawJSONStringBody(t, tt.name, body, tt.expectedBody)
					return
				}

				resp, err := testClient.Primary().CreateCollection(tt.packageID, tt.modelName)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				if tt.expectError {
					assert.IsError(resp, tt.expectedCode, tt.name)
					return
				}

				assert.IsSuccess(resp, tt.name)
				listResp, err := testClient.GetCollection(tt.modelName)
				if err != nil {
					t.Fatalf("查询收藏列表失败: %v", err)
				}
				list := extractCollectionList(t, listResp)
				item := collectionItemByModelName(list, tt.modelName)
				if item == nil {
					t.Fatalf("%s: 收藏列表中未找到模型 %q", tt.name, tt.modelName)
				}
				for _, field := range []string{"id", "package_id", "model_name", "haschild", "type", "is_sys"} {
					if _, ok := item[field]; !ok {
						t.Errorf("%s: 收藏项缺少字段 %q", tt.name, field)
					}
				}
			})
		}
	})
}

// TestGetCollection 验证 GET /model/collection/get。
// 覆盖：
//   - 正常获取全部收藏列表
//   - 关键字过滤命中单条记录
//   - 不存在关键字返回空列表
func TestGetCollection(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var firstID, secondID string

	tests := []struct {
		name        string
		keywords    string
		minLength   int
		expectEmpty bool
		mustContain []string
	}{
		{
			name:        "正常获取全部收藏列表",
			keywords:    "",
			minLength:   2,
			mustContain: []string{collectionCreateModelName, collectionReservedModelName},
		},
		{
			name:        "关键字命中单个收藏模型",
			keywords:    "PID_Controller",
			minLength:   1,
			mustContain: []string{collectionCreateModelName},
		},
		{
			name:        "不存在的关键字返回空列表",
			keywords:    "QQQCollectionNoSuchKeyword12345",
			expectEmpty: true,
		},
	}

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			if err := modelhelpers.EnsureSystemModelLoaded(testClient, collectionTestPackageID, "", "收藏查询前置加载 Modelica"); err != nil {
				t.Fatal(err)
			}
			cleanupCollectionByModelName(t, collectionCreateModelName)
			cleanupCollectionByModelName(t, collectionReservedModelName)
			firstID = createCollectionFixture(t, collectionTestPackageID, collectionCreateModelName)
			secondID = createCollectionFixture(t, collectionTestPackageID, collectionReservedModelName)
		},
		After: func(t *testing.T) {
			deleteCollectionByID(t, firstID)
			deleteCollectionByID(t, secondID)
			cleanupCollectionByModelName(t, collectionCreateModelName)
			cleanupCollectionByModelName(t, collectionReservedModelName)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				resp, err := testClient.Primary().GetCollection(tt.keywords)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				assert.IsSuccess(resp, tt.name)

				list := extractCollectionList(t, resp)
				if tt.expectEmpty {
					if len(list) != 0 {
						t.Errorf("%s: 期望空列表，实际 %d 条", tt.name, len(list))
					}
					return
				}
				if len(list) < tt.minLength {
					t.Fatalf("%s: 期望至少 %d 条，实际 %d 条", tt.name, tt.minLength, len(list))
				}
				for _, modelName := range tt.mustContain {
					if collectionItemByModelName(list, modelName) == nil {
						t.Errorf("%s: 收藏列表中未找到模型 %q", tt.name, modelName)
					}
				}
			})
		}
	})
}

// TestDeleteCollection 验证 GET /model/collection/delete。
// 覆盖：
//   - 正常删除收藏记录
//   - 删除不存在 id 返回 HTTP 400 + JSON string body
//   - 删除后列表校验：目标记录消失，其余记录保留
func TestDeleteCollection(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var deleteTargetID, remainID string

	tests := []struct {
		name               string
		collectionID       string
		useRaw             bool
		expectError        bool
		expectedCode       int
		expectedHTTPStatus int
		expectedBody       string
	}{
		{
			name:        "正常删除收藏模型",
			expectError: false,
		},
		{
			name:               "删除不存在的收藏 ID 返回 HTTP 400",
			collectionID:       "non-existent-collection-id",
			useRaw:             true,
			expectedHTTPStatus: http.StatusBadRequest,
			expectedBody:       "not found",
		},
	}

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			if err := modelhelpers.EnsureSystemModelLoaded(testClient, collectionTestPackageID, "", "收藏删除前置加载 Modelica"); err != nil {
				t.Fatal(err)
			}
			cleanupCollectionByModelName(t, collectionCreateModelName)
			cleanupCollectionByModelName(t, collectionReservedModelName)
			deleteTargetID = createCollectionFixture(t, collectionTestPackageID, collectionCreateModelName)
			remainID = createCollectionFixture(t, collectionTestPackageID, collectionReservedModelName)
		},
		After: func(t *testing.T) {
			deleteCollectionByID(t, deleteTargetID)
			deleteCollectionByID(t, remainID)
			cleanupCollectionByModelName(t, collectionCreateModelName)
			cleanupCollectionByModelName(t, collectionReservedModelName)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				targetID := tt.collectionID
				if targetID == "" {
					targetID = deleteTargetID
				}

				if tt.useRaw {
					rawResp, err := testClient.GetRawResponse("/model/collection/delete", map[string]string{"id": targetID})
					if err != nil {
						t.Fatalf("请求失败: %v", err)
					}
					body, err := helpers.ReadAndCloseRawBody(rawResp)
					if err != nil {
						t.Fatal(err)
					}
					if rawResp.StatusCode != tt.expectedHTTPStatus {
						t.Fatalf("%s: 期望 HTTP %d，实际 %d，body=%s", tt.name, tt.expectedHTTPStatus, rawResp.StatusCode, string(body))
					}
					assertRawJSONStringBody(t, tt.name, body, tt.expectedBody)
					return
				}

				resp, err := testClient.Primary().DeleteCollection(targetID)
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

		t.Run("删除后验证目标收藏已移除且其余收藏仍存在", func(t *testing.T) {
			resp, err := testClient.GetCollection("")
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			list := extractCollectionList(t, resp)
			if collectionItemByModelName(list, collectionCreateModelName) != nil {
				t.Errorf("收藏模型 %q 删除后仍然存在", collectionCreateModelName)
			}
			if collectionItemByModelName(list, collectionReservedModelName) == nil {
				t.Errorf("保留的收藏模型 %q 不存在", collectionReservedModelName)
			}
		})
	})
}

// extractCollectionList 解析收藏列表接口返回的 data 数组。
// 若 data 为 null，则返回 nil；若类型不是数组则直接失败。
func extractCollectionList(t *testing.T, resp *helpers.Response) []any {
	t.Helper()
	if resp.Data == nil {
		return nil
	}
	list, ok := resp.Data.([]any)
	if !ok {
		t.Fatalf("收藏列表 data 不是数组: %T", resp.Data)
	}
	return list
}

// collectionItemByModelName 在收藏列表中按 model_name 查找条目。
// 找到返回该 object，否则返回 nil。
func collectionItemByModelName(list []any, modelName string) map[string]any {
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if m["model_name"] == modelName {
			return m
		}
	}
	return nil
}

// collectionIDByModelName 从收藏列表中提取指定模型对应的收藏 id。
// 若不存在则返回空字符串。
func collectionIDByModelName(list []any, modelName string) string {
	item := collectionItemByModelName(list, modelName)
	if item == nil {
		return ""
	}
	id, _ := item["id"].(string)
	return id
}

// assertRawJSONStringBody 断言 HTTP 400 分支返回的是 JSON string body，
// 并校验其实际字符串值。
func assertRawJSONStringBody(t *testing.T, name string, body []byte, expected string) {
	t.Helper()
	var got string
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("%s: 响应体不是 JSON string: %q, err=%v", name, string(body), err)
	}
	if got != expected {
		t.Errorf("%s: 期望 body=%q，实际=%q", name, expected, got)
	}
}

// deleteCollectionByID 通过原始 HTTP 请求幂等删除收藏记录。
// 该 helper 主要用于 After 钩子清理，忽略业务体结构与删除失败细节。
func deleteCollectionByID(t *testing.T, id string) {
	t.Helper()
	if id == "" {
		return
	}
	resp, err := testClient.GetRawResponse("/model/collection/delete", map[string]string{"id": id})
	if err == nil && resp != nil {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}
}

// cleanupCollectionByModelName 根据 model_name 查询并清理已有收藏记录。
// 供测试前置使用，避免历史脏数据影响“重复创建”或列表断言。
func cleanupCollectionByModelName(t *testing.T, modelName string) {
	t.Helper()
	resp, err := testClient.GetCollection(modelName)
	if err != nil {
		t.Fatalf("查询收藏列表失败: %v", err)
	}
	if resp.Status != 0 {
		t.Fatalf("查询收藏列表返回非零状态: status=%d err=%s", resp.Status, resp.Err)
	}
	for _, item := range extractCollectionList(t, resp) {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if m["model_name"] == modelName {
			if id, ok := m["id"].(string); ok {
				deleteCollectionByID(t, id)
			}
		}
	}
}

// createCollectionFixture 创建一条收藏 fixture，并回查列表拿到其收藏 id。
// 若任一步骤失败则直接 Fatal。
func createCollectionFixture(t *testing.T, packageID, modelName string) string {
	t.Helper()
	cleanupCollectionByModelName(t, modelName)
	resp, err := testClient.CreateCollection(packageID, modelName)
	if err != nil {
		t.Fatalf("创建收藏失败: %v", err)
	}
	if resp.Status != 0 {
		t.Fatalf("创建收藏返回非零状态: status=%d err=%s", resp.Status, resp.Err)
	}
	listResp, err := testClient.GetCollection(modelName)
	if err != nil {
		t.Fatalf("查询新建收藏失败: %v", err)
	}
	id := collectionIDByModelName(extractCollectionList(t, listResp), modelName)
	if id == "" {
		t.Fatalf("未能在收藏列表中找到模型 %s 的收藏记录", modelName)
	}
	return id
}
