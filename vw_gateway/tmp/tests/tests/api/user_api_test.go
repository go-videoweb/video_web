//go:build api_test

package api

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"yssim-go/app/DataBase"
	"yssim-go/config"
	"yssim-go/tests/helpers"
	resourcehelpers "yssim-go/tests/helpers/resource"

	"github.com/google/uuid"
)

// TestGetUserSpaceNew 验证 GET /user/userspace/get/new。
// 覆盖：
//   - 正常获取用户空间列表
//   - 返回数据包含 all_space / recent_space 两个分组
//   - 新创建的工作空间可在 all_space 中查到
func TestGetUserSpaceNew(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var spaceId string
	var spaceName string

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			spaceName = uniqueName("测试空间_列表")
			spaceId = createTestUserSpace(t, spaceName)
		},
		After: func(t *testing.T) {
			deleteTestUserSpace(t, spaceId)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		tests := []struct {
			name string
		}{
			{name: "正常获取用户空间列表"},
		}

		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				resp, err := testClient.Primary().GetUserSpaceNew("", "")
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				assert.IsSuccess(resp, tt.name)
				assert.HasFields(resp, []string{"all_space", "recent_space"}, tt.name)

				dataMap, err := helpers.ObjectData(resp, "解析用户空间列表")
				if err != nil {
					t.Fatal(err)
				}
				allSpaces := extractUserSpaceListField(t, dataMap, "all_space")
				recentSpaces := extractUserSpaceListField(t, dataMap, "recent_space")
				if len(allSpaces) == 0 {
					t.Fatalf("%s: 期望 all_space 非空", tt.name)
				}
				if recentSpaces == nil {
					t.Fatalf("%s: recent_space 不应为 nil", tt.name)
				}

				spaceItem := findUserSpaceItem(allSpaces, spaceId)
				if spaceItem == nil {
					t.Fatalf("%s: 未在 all_space 中找到工作空间 id=%q", tt.name, spaceId)
				}
				if spaceItem["name"] != spaceName {
					t.Errorf("%s: 期望 name=%q，实际=%v", tt.name, spaceName, spaceItem["name"])
				}
				for _, field := range []string{"id", "name", "description", "background", "icon", "icon_color", "collect", "edit_time"} {
					if _, ok := spaceItem[field]; !ok {
						t.Errorf("%s: 工作空间项缺少字段 %q", tt.name, field)
					}
				}
			})
		}
	})
}

// TestGetUserSpace 验证 GET /user/userspace/get。
// 覆盖：
//   - 正常获取旧版用户空间列表
//   - 返回数组项包含 id / name
//   - 新创建的工作空间可在列表中查到
func TestGetUserSpace(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var spaceId string
	var spaceName string

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			spaceName = uniqueName("测试空间_旧列表")
			spaceId = createTestUserSpace(t, spaceName)
		},
		After: func(t *testing.T) {
			deleteTestUserSpace(t, spaceId)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		resp, err := testClient.Primary().GetUserSpace()
		if err != nil {
			t.Fatalf("请求失败: %v", err)
		}
		assert.IsSuccess(resp, "正常获取旧版用户空间列表")

		list, err := helpers.ArrayData(resp, "正常获取旧版用户空间列表")
		if err != nil {
			t.Fatal(err)
		}
		if len(list) == 0 {
			t.Fatalf("正常获取旧版用户空间列表: 期望非空列表")
		}

		var found map[string]any
		for _, item := range list {
			spaceItem, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if spaceItem["id"] == spaceId {
				found = spaceItem
				break
			}
		}
		if found == nil {
			t.Fatalf("正常获取旧版用户空间列表: 未找到工作空间 id=%q", spaceId)
		}
		if found["name"] != spaceName {
			t.Errorf("正常获取旧版用户空间列表: 期望 name=%q，实际=%v", spaceName, found["name"])
		}
		for _, field := range []string{"id", "name"} {
			if _, ok := found[field]; !ok {
				t.Errorf("正常获取旧版用户空间列表: 工作空间项缺少字段 %q", field)
			}
		}
	})
}

// TestGetUserSpaceUsage 验证 GET /api/v2/user/userspace/usage。
// 覆盖用户空间资源占用和任务状态查询，以及非法分页、非法 with_status 参数。
func TestGetUserSpaceUsage(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var spaceId string
	var spaceName string

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			spaceName = uniqueName("测试空间_用量")
			spaceId = createTestUserSpace(t, spaceName)
		},
		After: func(t *testing.T) {
			deleteTestUserSpace(t, spaceId)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		t.Run("正常获取用户空间用量和任务状态", func(t *testing.T) {
			resp, err := testClient.Primary().GetUserSpaceUsage(spaceName, "", 1, 10, "true")
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsSuccess(resp, "正常获取用户空间用量和任务状态")

			dataMap, err := helpers.ObjectData(resp, "正常获取用户空间用量和任务状态")
			if err != nil {
				t.Fatal(err)
			}
			for _, field := range []string{"space_list", "pagination", "disk_usage_elapsed", "item_build_elapsed"} {
				if _, ok := dataMap[field]; !ok {
					t.Errorf("正常获取用户空间用量和任务状态: data 缺少字段 %q", field)
				}
			}
			spaceList := extractUserSpaceListField(t, dataMap, "space_list")
			spaceItem := findUserSpaceItem(spaceList, spaceId)
			if spaceItem == nil {
				t.Fatalf("正常获取用户空间用量和任务状态: 未找到工作空间 id=%q", spaceId)
			}
			for _, field := range []string{"id", "name", "description", "collect", "edit_time", "task_status", "task_status_error"} {
				if _, ok := spaceItem[field]; !ok {
					t.Errorf("正常获取用户空间用量和任务状态: 工作空间项缺少字段 %q", field)
				}
			}
			if spaceItem["name"] != spaceName {
				t.Errorf("正常获取用户空间用量和任务状态: 期望 name=%q，实际=%v", spaceName, spaceItem["name"])
			}

			pagination, ok := dataMap["pagination"].(map[string]any)
			if !ok {
				t.Fatalf("正常获取用户空间用量和任务状态: pagination 不是 object，实际=%T", dataMap["pagination"])
			}
			for _, field := range []string{"page", "page_size", "total", "total_page"} {
				if _, ok := pagination[field]; !ok {
					t.Errorf("正常获取用户空间用量和任务状态: pagination 缺少字段 %q", field)
				}
			}
		})

		t.Run("非法分页参数返回业务错误", func(t *testing.T) {
			resp, err := testClient.Primary().GetUserSpaceUsageRaw(map[string]string{
				"page":      "0",
				"page_size": "10",
			})
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsError(resp, 2, "非法分页参数返回业务错误")
		})

		t.Run("非法 with_status 返回业务错误", func(t *testing.T) {
			resp, err := testClient.Primary().GetUserSpaceUsageRaw(map[string]string{
				"with_status": "bad-bool",
			})
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsError(resp, 2, "非法 with_status 返回业务错误")
		})
	})
}

// TestCreateUserSpace 验证 POST /user/userspace/create。
// 覆盖：
//   - 正常创建工作空间
//   - 重复名称返回 status=2
//   - 名称包含非法字符返回 status=2
func TestCreateUserSpace(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var existingSpaceId string
	var createdSpaceIds []string
	duplicateName := uniqueName("测试空间_重名")
	createdSpaceName := uniqueName("测试空间_创建正常")

	tests := []struct {
		name         string
		spaceName    string
		expectError  bool
		expectedCode int
		assertResp   func(t *testing.T, resp *helpers.Response)
	}{
		{
			name:      "正常创建工作空间",
			spaceName: createdSpaceName,
			assertResp: func(t *testing.T, resp *helpers.Response) {
				assert.IsSuccess(resp, "正常创建工作空间")
				dataMap, err := helpers.ObjectData(resp, "正常创建工作空间")
				if err != nil {
					t.Fatal(err)
				}
				id, ok := dataMap["id"].(string)
				if !ok || id == "" {
					t.Fatalf("正常创建工作空间: 返回 id 为空或类型错误: %v", dataMap["id"])
				}
				createdSpaceIds = append(createdSpaceIds, id)
				if dataMap["name"] != createdSpaceName {
					t.Errorf("正常创建工作空间: 期望 name=%q，实际=%v", createdSpaceName, dataMap["name"])
				}
				assertUserSpaceExistsInList(t, id, createdSpaceName, "正常创建工作空间")
			},
		},
		{
			name:         "工作空间名称重复返回 status=2",
			spaceName:    duplicateName,
			expectError:  true,
			expectedCode: 2,
		},
		{
			name:         "工作空间名称包含非法字符返回 status=2",
			spaceName:    "非法 空间!",
			expectError:  true,
			expectedCode: 2,
		},
	}

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			existingSpaceId = createTestUserSpace(t, duplicateName)
		},
		After: func(t *testing.T) {
			deleteTestUserSpace(t, existingSpaceId)
			for _, id := range createdSpaceIds {
				deleteTestUserSpace(t, id)
			}
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				resp, err := testClient.Primary().CreateUserSpace(tt.spaceName, "测试描述", "", "cube", "blue")
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				if tt.expectError {
					assert.IsError(resp, tt.expectedCode, tt.name)
					return
				}
				tt.assertResp(t, resp)
			})
		}
	})
}

// TestDeleteUserSpace 验证 POST /user/userspace/delete。
// 覆盖：
//   - 正常删除工作空间
//   - 删除不存在的工作空间当前幂等成功
//   - 删除后列表中不再包含该工作空间
func TestDeleteUserSpace(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var spaceId string
	var deleted bool

	tests := []struct {
		name     string
		spaceIDs []string
	}{
		{
			name:     "正常删除工作空间",
			spaceIDs: nil,
		},
		{
			name:     "删除不存在的工作空间当前幂等成功",
			spaceIDs: []string{"non-existent-space-id"},
		},
	}

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			spaceId = createTestUserSpace(t, uniqueName("测试空间_删除"))
		},
		After: func(t *testing.T) {
			if !deleted {
				deleteTestUserSpace(t, spaceId)
			}
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				targetIDs := tt.spaceIDs
				if targetIDs == nil {
					targetIDs = []string{spaceId}
				}
				resp, err := testClient.Primary().DeleteUserSpace(targetIDs)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				assert.IsSuccess(resp, tt.name)
				if len(targetIDs) == 1 && targetIDs[0] == spaceId {
					deleted = true
				}
			})
		}

		t.Run("删除后列表中不再包含该工作空间", func(t *testing.T) {
			// 删除接口当前通过 CleanupSpaceResources 软删空间；使用 v2 空间列表回查最终可见列表。
			resp, err := testClient.GetUserSpaceV2("", "", 0, 0)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsSuccess(resp, "删除后列表中不再包含该工作空间")
			dataMap, err := helpers.ObjectData(resp, "删除后列表中不再包含该工作空间")
			if err != nil {
				t.Fatal(err)
			}
			spaceList := extractUserSpaceListField(t, dataMap, "space_list")
			if findUserSpaceItem(spaceList, spaceId) != nil {
				t.Errorf("删除后列表中仍找到工作空间 id=%q", spaceId)
			}
		})
	})
}

// TestClearUserSpaceTaskLogs 验证 POST /user/userspace/task-log/clear。
// 覆盖正常清除任务日志游标、缺少必填模型信息和非法日志类型。
func TestClearUserSpaceTaskLogs(t *testing.T) {
	assert := helpers.NewAssertResponse(t)
	tabID := "api-test-task-log-clear"

	tests := []struct {
		name         string
		packageID    string
		modelName    string
		logTypes     []string
		expectError  bool
		expectedCode int
	}{
		{
			name:      "正常清除指定模型任务日志",
			packageID: testPackageId,
			modelName: testModelName,
			logTypes:  []string{"check", "translate", "compile", "simulate"},
		},
		{
			name:         "缺少 package_id 返回业务错误",
			modelName:    testModelName,
			logTypes:     []string{"compile"},
			expectError:  true,
			expectedCode: 2,
		},
		{
			name:         "非法日志类型返回业务错误",
			packageID:    testPackageId,
			modelName:    testModelName,
			logTypes:     []string{"unknown"},
			expectError:  true,
			expectedCode: 2,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			resp, err := testClient.Primary().ClearUserSpaceTaskLogs(tt.packageID, tt.modelName, tt.logTypes, tabID)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			if tt.expectError {
				assert.IsError(resp, tt.expectedCode, tt.name)
				return
			}
			assert.IsSuccess(resp, tt.name)
			if resp.Msg != "清除任务日志成功" {
				t.Errorf("%s: 期望 msg=%q，实际=%q", tt.name, "清除任务日志成功", resp.Msg)
			}
			dataMap, err := helpers.ObjectData(resp, tt.name)
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := dataMap["cleared"].(float64); !ok {
				t.Errorf("%s: cleared 不是数字，实际=%T", tt.name, dataMap["cleared"])
			}
		})
	}
}

// TestTerminateAllUserSpaceTasks 验证 POST /user/userspace/task/terminate/all。
// 当前场景不额外创建长任务，只验证无任务时接口可幂等进入“停止中”状态。
func TestTerminateAllUserSpaceTasks(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	tests := []struct {
		name    string
		spaceID string
	}{
		{name: "使用请求头工作空间停止所有任务", spaceID: ""},
		{name: "使用 body 指定工作空间停止所有任务", spaceID: helpers.DefaultTestConfig().SpaceID},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			resp, err := testClient.Primary().TerminateAllUserSpaceTasks(tt.spaceID)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsSuccess(resp, tt.name)
			if resp.Msg != "停止中" {
				t.Errorf("%s: 期望 msg=%q，实际=%q", tt.name, "停止中", resp.Msg)
			}
		})
	}
}

// TestGetUserSettings 验证 GET /user/settings/get。
// 覆盖：
//   - 正常获取用户设置
//   - 返回包含 grid_display 与 interactive 字段
func TestGetUserSettings(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	hooks := helpers.TestHooks{}
	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		tests := []struct {
			name string
		}{
			{name: "正常获取用户设置"},
		}

		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				resp, err := testClient.Primary().GetUserSettings()
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				assert.IsSuccess(resp, tt.name)
				assert.HasField(resp, "grid_display", tt.name)
				assert.HasField(resp, "interactive", tt.name)

				dataMap, err := helpers.ObjectData(resp, tt.name)
				if err != nil {
					t.Fatal(err)
				}
				if _, ok := dataMap["grid_display"].(bool); !ok {
					t.Fatalf("%s: grid_display 不是 bool: %T", tt.name, dataMap["grid_display"])
				}
				if _, ok := dataMap["interactive"].(bool); !ok {
					t.Fatalf("%s: interactive 不是 bool: %T", tt.name, dataMap["interactive"])
				}
			})
		}
	})
}

// TestGetUserExamples 验证 GET /user/examples。
// 覆盖：
//   - 当前接口可正常返回 status=0
//   - 当前实现返回空 data（保守校验，不对示例内容做假设）
func TestGetUserExamples(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	hooks := helpers.TestHooks{}
	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		resp, err := testClient.Primary().GetUserExamples()
		if err != nil {
			t.Fatalf("请求失败: %v", err)
		}
		assert.IsSuccess(resp, "正常获取示例数据")
		if resp.Data != nil && !helpers.IsStructurallyEmpty(resp.Data) {
			t.Errorf("正常获取示例数据: 当前实现预期空 data，实际=%#v", resp.Data)
		}
	})
}

// TestUploadUserBackground 验证 POST /user/background/upload。
// 覆盖：
//   - 正常上传 png 背景图
//   - 非法文件后缀返回 status=2
//   - 超过大小限制返回 status=2
//   - 缺少文件当前返回 HTTP 500
func TestUploadUserBackground(t *testing.T) {
	assert := helpers.NewAssertResponse(t)
	pngData, err := resourcehelpers.ReadTestPicturePNG()
	if err != nil {
		t.Fatal(err)
	}
	var uploadedPaths []string

	helpers.RunWithHooks(t, helpers.TestHooks{
		After: func(t *testing.T) {
			for _, path := range uploadedPaths {
				_ = os.Remove(path)
			}
		},
	}, func(t *testing.T) {
		t.Run("正常上传 png 背景图", func(t *testing.T) {
			resp, err := testClient.Primary().UploadUserBackground("test_picture.png", pngData)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsSuccess(resp, "正常上传 png 背景图")
			if resp.Msg != "上传成功" {
				t.Errorf("正常上传 png 背景图: 期望 msg=%q，实际=%q", "上传成功", resp.Msg)
			}

			dataMap, err := helpers.ObjectData(resp, "正常上传 png 背景图")
			if err != nil {
				t.Fatal(err)
			}
			pathValue, _ := dataMap["path"].(string)
			if strings.TrimSpace(pathValue) == "" {
				t.Fatalf("正常上传 png 背景图: 返回 path 为空")
			}
			if !strings.HasSuffix(pathValue, "test_picture.png") {
				t.Errorf("正常上传 png 背景图: 期望 path 以 test_picture.png 结尾，实际=%q", pathValue)
			}
			uploadedPaths = append(uploadedPaths, pathValue)
		})

		t.Run("非法文件后缀返回 status=2", func(t *testing.T) {
			resp, err := testClient.Primary().UploadUserBackground("background.txt", []byte("plain text"))
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsError(resp, 2, "非法文件后缀返回 status=2")
		})

		t.Run("超过大小限制返回 status=2", func(t *testing.T) {
			largePNG := make([]byte, 3500001)
			resp, err := testClient.Primary().UploadUserBackground("too_large.png", largePNG)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsError(resp, 2, "超过大小限制返回 status=2")
		})

		t.Run("缺少文件当前返回 HTTP 500", func(t *testing.T) {
			rawResp, body, err := resourcehelpers.PostMultipartRaw("/user/background/upload", nil, false, "file", "", nil)
			if err != nil {
				t.Fatal(err)
			}
			if rawResp.StatusCode != http.StatusInternalServerError {
				t.Fatalf("缺少文件当前返回 HTTP 500: 期望 HTTP 500，实际=%d，body=%q", rawResp.StatusCode, string(body))
			}
		})
	})
}

// TestSetUserSettings 验证 POST /user/settings/set。
// 覆盖：
//   - 设置 grid_display 与 interactive 成功
//   - 设置后再次读取返回最新值
func TestSetUserSettings(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var original helpers.UserSettings

	tests := []struct {
		name            string
		gridDisplay     bool
		interactive     bool
		expectedMessage string
	}{
		{
			name:            "设置 grid_display 和 interactive 为 true 成功",
			gridDisplay:     true,
			interactive:     true,
			expectedMessage: "修改成功。",
		},
		{
			name:            "设置 grid_display 和 interactive 为 false 成功",
			gridDisplay:     false,
			interactive:     false,
			expectedMessage: "修改成功。",
		},
	}

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			// 用户设置是持久化配置，测试前记录完整原值，避免只恢复 grid_display 时改动 interactive。
			original = getCurrentUserSettings(t)
			if _, err := testClient.SetUserSettingsFull(original.GridDisplay, original.Interactive); err != nil {
				t.Fatalf("初始化用户设置记录失败: %v", err)
			}
		},
		After: func(t *testing.T) {
			// 测试会反复切换两个字段，结束后恢复用户原始偏好。
			_, _ = testClient.SetUserSettingsFull(original.GridDisplay, original.Interactive)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				resp, err := testClient.Primary().SetUserSettingsFull(tt.gridDisplay, tt.interactive)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				assert.IsSuccess(resp, tt.name)
				if resp.Msg != tt.expectedMessage {
					t.Errorf("%s: 期望 msg=%q，实际=%q", tt.name, tt.expectedMessage, resp.Msg)
				}
				if resp.Data != true {
					t.Errorf("%s: 期望 data=true，实际=%v", tt.name, resp.Data)
				}

				getResp, err := testClient.GetUserSettings()
				if err != nil {
					t.Fatalf("%s: 再次读取用户设置失败: %v", tt.name, err)
				}
				assert.IsSuccess(getResp, tt.name+": 再次读取")
				dataMap, err := helpers.ObjectData(getResp, tt.name+": 再次读取")
				if err != nil {
					t.Fatal(err)
				}
				if dataMap["grid_display"] != tt.gridDisplay {
					t.Errorf("%s: 期望 grid_display=%v，实际=%v", tt.name, tt.gridDisplay, dataMap["grid_display"])
				}
				if dataMap["interactive"] != tt.interactive {
					t.Errorf("%s: 期望 interactive=%v，实际=%v", tt.name, tt.interactive, dataMap["interactive"])
				}
			})
		}
	})
}

// TestLoadStatus 验证 /user/userspace/model/loadstatus/get 与 /user/userspace/model/loadstatus/set。
// 覆盖：
//   - 正常获取工作空间加载状态
//   - 设置加载状态成功
//   - 设置后再次获取状态为 Ready(3)
func TestLoadStatus(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	hooks := helpers.TestHooks{}
	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		t.Run("正常获取工作空间加载状态", func(t *testing.T) {
			resp, err := testClient.Primary().GetLoadStatus()
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsSuccess(resp, "正常获取工作空间加载状态")
			assert.HasFields(resp, []string{"space_id", "workspace_status"}, "正常获取工作空间加载状态")
		})

		t.Run("设置工作空间加载状态成功", func(t *testing.T) {
			resp, err := testClient.Primary().LoadstatusSet()
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsSuccess(resp, "设置工作空间加载状态成功")
		})

		t.Run("设置后再次获取状态为 Ready(3)", func(t *testing.T) {
			resp, err := testClient.Primary().GetLoadStatus()
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsSuccess(resp, "设置后再次获取状态为 Ready(3)")
			dataMap, err := helpers.ObjectData(resp, "设置后再次获取状态为 Ready(3)")
			if err != nil {
				t.Fatal(err)
			}
			if dataMap["workspace_status"] != float64(helpers.WorkspaceStatusReady) {
				t.Errorf("设置后再次获取状态为 Ready(3): 期望 workspace_status=%d，实际=%v", helpers.WorkspaceStatusReady, dataMap["workspace_status"])
			}
		})
	})
}

// TestEditUserSpace 验证 POST /user/userspace/edit。
// 覆盖：
//   - 正常编辑工作空间名称与展示字段
//   - 编辑为已存在名称返回 status=2
//   - 编辑不存在的工作空间当前静默成功
func TestEditUserSpace(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var editableSpaceId string
	var editableOriginalName string
	var conflictSpaceId string
	var conflictSpaceName string
	editedSpaceName := uniqueName("测试空间_编辑后")

	tests := []struct {
		name            string
		spaceIdLiteral  string
		spaceName       string
		description     string
		background      string
		icon            string
		iconColor       string
		expectError     bool
		expectedCode    int
		expectListCheck bool
	}{
		{
			name:            "正常编辑工作空间",
			spaceIdLiteral:  "",
			spaceName:       editedSpaceName,
			description:     "编辑后的描述",
			background:      "bg.png",
			icon:            "folder",
			iconColor:       "orange",
			expectListCheck: true,
		},
		{
			name:           "编辑为已存在名称返回 status=2",
			spaceIdLiteral: "",
			spaceName:      "",
			description:    "重复名称描述",
			background:     "",
			icon:           "cube",
			iconColor:      "blue",
			expectError:    true,
			expectedCode:   2,
		},
		{
			name:           "编辑不存在的工作空间当前静默成功",
			spaceIdLiteral: "non-existent-space-id",
			spaceName:      uniqueName("测试空间_不存在编辑"),
			description:    "不存在空间描述",
			background:     "ghost.png",
			icon:           "ghost",
			iconColor:      "gray",
		},
	}

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			editableOriginalName = uniqueName("测试空间_待编辑")
			editableSpaceId = createTestUserSpace(t, editableOriginalName)
			conflictSpaceName = uniqueName("测试空间_编辑冲突")
			conflictSpaceId = createTestUserSpace(t, conflictSpaceName)
		},
		After: func(t *testing.T) {
			deleteTestUserSpace(t, editableSpaceId)
			deleteTestUserSpace(t, conflictSpaceId)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				targetId := tt.spaceIdLiteral
				if targetId == "" {
					targetId = editableSpaceId
				}
				spaceName := tt.spaceName
				if tt.name == "编辑为已存在名称返回 status=2" {
					spaceName = conflictSpaceName
				}

				resp, err := testClient.Primary().EditUserSpace(targetId, spaceName, tt.description, tt.background, tt.icon, tt.iconColor)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				if tt.expectError {
					assert.IsError(resp, tt.expectedCode, tt.name)
					return
				}
				assert.IsSuccess(resp, tt.name)

				if !tt.expectListCheck {
					return
				}

				item := getUserSpaceItemFromAllSpaces(t, editableSpaceId, tt.name)
				if item["name"] != editedSpaceName {
					t.Errorf("%s: 期望 name=%q，实际=%v", tt.name, editedSpaceName, item["name"])
				}
				if item["description"] != tt.description {
					t.Errorf("%s: 期望 description=%q，实际=%v", tt.name, tt.description, item["description"])
				}
				if item["background"] != tt.background {
					t.Errorf("%s: 期望 background=%q，实际=%v", tt.name, tt.background, item["background"])
				}
				if item["icon"] != tt.icon {
					t.Errorf("%s: 期望 icon=%q，实际=%v", tt.name, tt.icon, item["icon"])
				}
				if item["icon_color"] != tt.iconColor {
					t.Errorf("%s: 期望 icon_color=%q，实际=%v", tt.name, tt.iconColor, item["icon_color"])
				}
			})
		}
	})
}

// TestCollectUserSpace 验证 POST /user/userspace/collect。
// 覆盖：
//   - 正常收藏工作空间
//   - 正常取消收藏工作空间
//   - 收藏不存在的工作空间当前静默成功
func TestCollectUserSpace(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var spaceId string

	tests := []struct {
		name            string
		spaceIDs        []string
		collect         bool
		expectedMsg     string
		expectedCollect bool
		checkExisting   bool
	}{
		{
			name:            "正常收藏工作空间",
			spaceIDs:        nil,
			collect:         true,
			expectedMsg:     "收藏成功",
			expectedCollect: true,
			checkExisting:   true,
		},
		{
			name:            "正常取消收藏工作空间",
			spaceIDs:        nil,
			collect:         false,
			expectedMsg:     "取消收藏成功",
			expectedCollect: false,
			checkExisting:   true,
		},
		{
			name:            "收藏不存在的工作空间当前静默成功",
			spaceIDs:        []string{"non-existent-space-id"},
			collect:         true,
			expectedMsg:     "收藏成功",
			expectedCollect: false,
			checkExisting:   true,
		},
	}

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			spaceId = createTestUserSpace(t, uniqueName("测试空间_收藏"))
		},
		After: func(t *testing.T) {
			deleteTestUserSpace(t, spaceId)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				targetIDs := tt.spaceIDs
				if targetIDs == nil {
					targetIDs = []string{spaceId}
				}

				resp, err := testClient.Primary().CollectUserSpace(targetIDs, tt.collect)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				assert.IsSuccess(resp, tt.name)
				if resp.Msg != tt.expectedMsg {
					t.Errorf("%s: 期望 msg=%q，实际=%q", tt.name, tt.expectedMsg, resp.Msg)
				}

				if tt.checkExisting {
					item := getUserSpaceItemFromAllSpaces(t, spaceId, tt.name)
					if item["collect"] != tt.expectedCollect {
						t.Errorf("%s: 期望 collect=%v，实际=%v", tt.name, tt.expectedCollect, item["collect"])
					}
				}
			})
		}
	})
}

// TestGetUserRecentlyOpened 验证 GET /user/userspace/recent。
// 覆盖：
//   - 正常获取最近打开列表
//   - 列表包含最近访问过的工作空间
//   - 列表按 last_login_time 倒序返回
func TestGetUserRecentlyOpened(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var newerSpaceId string
	var newerSpaceName string
	var olderSpaceId string
	var olderSpaceName string

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			newerSpaceName = uniqueName("测试空间_最近打开新")
			olderSpaceName = uniqueName("测试空间_最近打开旧")
			newerSpaceId = createTestUserSpace(t, newerSpaceName)
			olderSpaceId = createTestUserSpace(t, olderSpaceName)

			setUserSpaceLastLoginTime(t, olderSpaceId, 4102444800)
			setUserSpaceLastLoginTime(t, newerSpaceId, 4102445800)
		},
		After: func(t *testing.T) {
			deleteTestUserSpace(t, newerSpaceId)
			deleteTestUserSpace(t, olderSpaceId)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		t.Run("正常获取最近打开列表", func(t *testing.T) {
			resp, err := testClient.Primary().GetUserRecentlyOpened()
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsSuccess(resp, "正常获取最近打开列表")
			recentList, err := helpers.ArrayData(resp, "正常获取最近打开列表")
			if err != nil {
				t.Fatal(err)
			}
			if len(recentList) == 0 {
				t.Fatalf("正常获取最近打开列表: 期望列表非空")
			}
		})

		t.Run("最近打开列表包含最近访问的工作空间", func(t *testing.T) {
			resp, err := testClient.Primary().GetUserRecentlyOpened()
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsSuccess(resp, "最近打开列表包含最近访问的工作空间")
			recentList, err := helpers.ArrayData(resp, "最近打开列表包含最近访问的工作空间")
			if err != nil {
				t.Fatal(err)
			}

			newerItem := findUserSpaceItem(recentList, newerSpaceId)
			olderItem := findUserSpaceItem(recentList, olderSpaceId)
			if newerItem == nil {
				t.Fatalf("最近打开列表中未找到工作空间 id=%q", newerSpaceId)
			}
			if olderItem == nil {
				t.Fatalf("最近打开列表中未找到工作空间 id=%q", olderSpaceId)
			}
			if newerItem["name"] != newerSpaceName {
				t.Errorf("期望 newer name=%q，实际=%v", newerSpaceName, newerItem["name"])
			}
			if olderItem["name"] != olderSpaceName {
				t.Errorf("期望 older name=%q，实际=%v", olderSpaceName, olderItem["name"])
			}
		})

		t.Run("最近打开列表按 last_login_time 倒序返回", func(t *testing.T) {
			resp, err := testClient.Primary().GetUserRecentlyOpened()
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsSuccess(resp, "最近打开列表按 last_login_time 倒序返回")
			recentList, err := helpers.ArrayData(resp, "最近打开列表按 last_login_time 倒序返回")
			if err != nil {
				t.Fatal(err)
			}
			newerIndex := indexOfUserSpaceItem(recentList, newerSpaceId)
			olderIndex := indexOfUserSpaceItem(recentList, olderSpaceId)
			if newerIndex == -1 || olderIndex == -1 {
				t.Fatalf("最近打开列表中未找到用于排序断言的工作空间: newerIndex=%d olderIndex=%d", newerIndex, olderIndex)
			}
			if newerIndex >= olderIndex {
				t.Errorf("期望最近访问的工作空间排在更前面，实际 newerIndex=%d olderIndex=%d", newerIndex, olderIndex)
			}
		})
	})
}

// TestRestartSMC 验证 POST /user/service/restart。
// 覆盖：
//   - 正常重启工作空间成功
//   - 重启后工作空间状态进入 NeedLoad(2)
//   - 连续重复重启仍成功
func TestRestartSMC(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			_, _ = testClient.LoadstatusSet()
		},
		After: func(t *testing.T) {
			_, _ = testClient.LoadstatusSet()
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		t.Run("正常重启工作空间成功", func(t *testing.T) {
			resp, err := testClient.Primary().RestartSMC()
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsSuccess(resp, "正常重启工作空间成功")
			if resp.Msg != "服务重启成功" {
				t.Errorf("正常重启工作空间成功: 期望 msg=%q，实际=%q", "服务重启成功", resp.Msg)
			}
		})

		t.Run("重启后工作空间状态进入 NeedLoad(2)", func(t *testing.T) {
			resp, err := testClient.GetLoadStatus()
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsSuccess(resp, "重启后工作空间状态进入 NeedLoad(2)")
			dataMap, err := helpers.ObjectData(resp, "重启后工作空间状态进入 NeedLoad(2)")
			if err != nil {
				t.Fatal(err)
			}
			if dataMap["workspace_status"] != float64(helpers.WorkspaceStatusNeedLoad) {
				t.Errorf("重启后工作空间状态进入 NeedLoad(2): 期望 workspace_status=%d，实际=%v", helpers.WorkspaceStatusNeedLoad, dataMap["workspace_status"])
			}
		})

		t.Run("连续重复重启仍成功", func(t *testing.T) {
			resp, err := testClient.Primary().RestartSMC()
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsSuccess(resp, "连续重复重启仍成功")
		})

		t.Run("重复重启后仍可将状态设置回 Ready(3)", func(t *testing.T) {
			resp, err := testClient.LoadstatusSet()
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsSuccess(resp, "重复重启后仍可将状态设置回 Ready(3)")

			statusResp, err := testClient.GetLoadStatus()
			if err != nil {
				t.Fatalf("查询工作空间状态失败: %v", err)
			}
			assert.IsSuccess(statusResp, "重复重启后仍可将状态设置回 Ready(3): 查询状态")
			dataMap, err := helpers.ObjectData(statusResp, "重复重启后仍可将状态设置回 Ready(3): 查询状态")
			if err != nil {
				t.Fatal(err)
			}
			if dataMap["workspace_status"] != float64(helpers.WorkspaceStatusReady) {
				t.Errorf("重复重启后仍可将状态设置回 Ready(3): 期望 workspace_status=%d，实际=%v", helpers.WorkspaceStatusReady, dataMap["workspace_status"])
			}
		})
	})
}

func createTestUserSpace(t *testing.T, spaceName string) string {
	t.Helper()
	resp, err := testClient.CreateUserSpace(spaceName, "测试描述", "", "cube", "blue")
	if err != nil {
		t.Fatalf("创建测试工作空间失败: %v", err)
	}
	if resp.Status != 0 {
		t.Fatalf("创建测试工作空间返回非零状态: status=%d, msg=%s, err=%s", resp.Status, resp.Msg, resp.Err)
	}
	dataMap, err := helpers.ObjectData(resp, "创建测试工作空间")
	if err != nil {
		t.Fatal(err)
	}
	id, ok := dataMap["id"].(string)
	if !ok || id == "" {
		t.Fatalf("创建测试工作空间返回的 id 为空或类型错误: %v", dataMap["id"])
	}
	return id
}

func deleteTestUserSpace(t *testing.T, spaceId string) {
	t.Helper()
	if spaceId == "" {
		return
	}
	_, _ = testClient.DeleteUserSpace([]string{spaceId})
}

func uniqueName(prefix string) string {
	return fmt.Sprintf("%s_%s", prefix, uuid.NewString()[:8])
}

func assertUserSpaceExistsInList(t *testing.T, spaceId, spaceName, message string) {
	t.Helper()
	item := getUserSpaceItemFromAllSpaces(t, spaceId, message)
	if item == nil {
		t.Fatalf("%s: 未在 all_space 中找到工作空间 id=%q", message, spaceId)
	}
	if item["name"] != spaceName {
		t.Errorf("%s: 期望工作空间名称=%q，实际=%v", message, spaceName, item["name"])
	}
}

// getCurrentUserSettings 读取完整设置快照，供会修改持久化用户偏好的用例恢复现场。
func getCurrentUserSettings(t *testing.T) helpers.UserSettings {
	t.Helper()
	resp, err := testClient.GetUserSettings()
	if err != nil {
		t.Fatalf("获取当前用户设置失败: %v", err)
	}
	settings, err := helpers.ParseUserSettings(resp)
	if err != nil {
		t.Fatal(err)
	}
	return settings
}

func getUserSpaceItemFromAllSpaces(t *testing.T, spaceId, message string) map[string]any {
	t.Helper()
	resp, err := testClient.GetUserSpaceNew("", "")
	if err != nil {
		t.Fatalf("%s: 获取用户空间列表失败: %v", message, err)
	}
	if resp.Status != 0 {
		t.Fatalf("%s: 获取用户空间列表返回非成功状态: status=%d, msg=%s, err=%s", message, resp.Status, resp.Msg, resp.Err)
	}
	dataMap, err := helpers.ObjectData(resp, "解析用户空间列表")
	if err != nil {
		t.Fatal(err)
	}
	allSpaces := extractUserSpaceListField(t, dataMap, "all_space")
	return findUserSpaceItem(allSpaces, spaceId)
}

func extractUserSpaceListField(t *testing.T, dataMap map[string]any, key string) []any {
	t.Helper()
	listValue, ok := dataMap[key]
	if !ok {
		t.Fatalf("用户空间列表缺少字段 %q", key)
	}
	list, ok := listValue.([]any)
	if !ok {
		t.Fatalf("用户空间列表字段 %q 不是数组: %T", key, listValue)
	}
	return list
}

func findUserSpaceItem(list []any, spaceId string) map[string]any {
	for _, item := range list {
		spaceItem, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if spaceItem["id"] == spaceId {
			return spaceItem
		}
	}
	return nil
}

func indexOfUserSpaceItem(list []any, spaceId string) int {
	for i, item := range list {
		spaceItem, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if spaceItem["id"] == spaceId {
			return i
		}
	}
	return -1
}

func setUserSpaceLastLoginTime(t *testing.T, spaceId string, lastLoginTime int64) {
	t.Helper()
	if err := config.DB.Model(&DataBase.YssimUserSpace{}).Where("id = ?", spaceId).UpdateColumn("last_login_time", lastLoginTime).Error; err != nil {
		t.Fatalf("更新工作空间 last_login_time 失败: %v", err)
	}
}
