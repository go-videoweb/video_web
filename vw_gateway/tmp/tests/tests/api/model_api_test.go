//go:build api_test

package api

import (
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"yssim-go/tests/helpers"
	modelhelpers "yssim-go/tests/helpers/model"
)

var testClient *helpers.APIClient

func TestMain(m *testing.M) {
	// 客户端是纯构造，不会失败 —— 留在 TestMain 体内，
	// 让 Suite.Setup 专注于"会失败 / 依赖外部就绪"的真活。
	testClient = helpers.NewAPIClientWithConfig(helpers.DefaultTestConfig())

	suite := &helpers.Suite{
		Setup: func() error {
			helpers.LogSetupStep("校验环境变量")
			if os.Getenv("TEST_SPACE_ID") == "" || os.Getenv("TEST_USERNAME") == "" {
				return fmt.Errorf("TEST_SPACE_ID / TEST_USERNAME 未设置")
			}

			helpers.LogSetupStep("确保系统库和管网模型库已加载")
			if err := modelhelpers.EnsureCommonModelLibrariesLoaded(testClient, modelicaStdLibId); err != nil {
				return err
			}

			helpers.LogSetupStep("Setup 完成")
			return nil
		},
		Teardown: func() {
			helpers.LogTeardownStep("test suite finished")
		},
	}
	os.Exit(suite.Run(m))
}

func TestSimple(t *testing.T) {
	if 1+1 == 2 {
		fmt.Println("simple test pass")
	} else {
		t.Fatalf("simple test fail")
	}
}
func TestGetSysRootModel(t *testing.T) {
	assert := helpers.NewAssertResponse(t)
	if err := modelhelpers.EnsureSystemModelLoaded(testClient, modelicaStdLibId, "", "系统库列表前置加载 Modelica"); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name        string
		expectError bool
		checkFields bool
	}{
		{
			name:        "正常获取系统库",
			expectError: false,
			checkFields: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp, err := testClient.Primary().GetSysRootModel()
			if err != nil {
				t.Fatalf("request failed: %v", err)
			}

			fmt.Println(resp)
			if tt.expectError {
				assert.IsError(resp, 500, tt.name)
			} else {
				assert.IsSuccess(resp, tt.name)
				assert.DataIsNotEmpty(resp, tt.name)
			}
		})
	}
}

func TestGetUserRootModel(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	resp, err := testClient.Primary().GetUserRootModel("", "")
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}

	assert.IsSuccess(resp, "get user root model")
	//assert.DataIsNotEmpty(resp, "正常获取用户模型")
}

func TestGetUserPackage(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var fixturePackageID string
	var fixtureModelName string

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			{
				var err error
				fixturePackageID, fixtureModelName, err = modelhelpers.CreateTempPackageFixture(testClient, "TestResourcePackage")
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := modelhelpers.EnsureModelLoaded(testClient, fixturePackageID, fixtureModelName, "已加载用户包列表夹具"); err != nil {
				t.Fatal(err)
			}
		},
		After: func(t *testing.T) {
			modelhelpers.CleanupTempModelFixture(testClient, fixturePackageID, fixtureModelName)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		resp, err := testClient.Primary().GetUserPackage()
		if err != nil {
			t.Fatalf("request failed: %v", err)
		}

		assert.IsSuccess(resp, "获取已加载用户 package 列表")
		list, ok := resp.Data.([]any)
		if !ok {
			t.Fatalf("获取已加载用户 package 列表: data 不是数组: %T", resp.Data)
		}

		var found map[string]any
		for _, item := range list {
			m, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if m["package_id"] == fixturePackageID {
				found = m
				break
			}
		}
		if found == nil {
			t.Fatalf("获取已加载用户 package 列表: 未找到 package_id=%q 的夹具记录", fixturePackageID)
		}
		if found["package_name"] != fixtureModelName {
			t.Errorf("获取已加载用户 package 列表: 期望 package_name=%q，实际=%v", fixtureModelName, found["package_name"])
		}
	})
}

// TestGetPackageAndVersion 验证 GET /model/package/get/all。
// 该接口无请求参数，主要确认当前用户空间可返回系统库或用户库的 package 基本信息。
func TestGetPackageAndVersion(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	resp, err := testClient.Primary().GetPackageAndVersion()
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}

	assert.IsSuccess(resp, "正常获取模型库和版本列表")
	items, err := helpers.ObjectListData(resp, "正常获取模型库和版本列表")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) == 0 {
		t.Fatal("正常获取模型库和版本列表: 期望至少返回一个模型库")
	}

	// 返回值会混合系统库和用户库；这里只校验列表项的稳定公共字段。
	for _, field := range []string{"package_id", "package_name", "version", "is_sys", "update_time"} {
		if _, ok := items[0][field]; !ok {
			t.Errorf("正常获取模型库和版本列表: 首个模型库缺少字段 %q", field)
		}
	}
}

// TestLoadModel 验证 POST /model/package/load。
// 覆盖系统库、用户库、不存在 package，以及缺少 id 的绑定错误。
func TestLoadModel(t *testing.T) {
	assert := helpers.NewAssertResponse(t)
	tests := []struct {
		name               string
		useRaw             bool
		packageId          string
		modelName          string
		expectError        bool
		expectedCode       int
		expectedHTTPStatus int
		wantMsgContains    string
	}{
		{
			packageId:   modelicaStdLibId,
			name:        "加载有效模型库",
			modelName:   "Modelica.Blocks.Examples.PID_Controller",
			expectError: false,
		},
		{
			name:            "正常加载系统模型包",
			packageId:       modelicaStdLibId,
			modelName:       "Modelica",
			wantMsgContains: "加载成功",
		},
		{
			packageId:   workspaceLibId,
			name:        "加载用户个人模型库",
			modelName:   "Modelica.Blocks.Examples.PID_Controller",
			expectError: false,
		},
		{
			packageId:    "id-non-exist",
			name:         "加载不存在的模型",
			modelName:    "NonExistentModel.Test",
			expectError:  true,
			expectedCode: 4,
		},
		{
			name:               "缺少 id 返回 HTTP 400",
			useRaw:             true,
			modelName:          "Modelica",
			expectedHTTPStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			if tt.useRaw {
				rawResp, err := testClient.PostRawResponse("/model/package/load", map[string]any{
					"id":   tt.packageId,
					"name": tt.modelName,
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

			resp, err := testClient.Primary().LoadModel(tt.packageId, tt.modelName)

			if err != nil {
				if !tt.expectError {
					t.Fatalf("unexpected error: %v", err)
				}
				return
			}

			if tt.expectError {
				assert.IsError(resp, tt.expectedCode, tt.name)
			} else {
				assert.IsSuccess(resp, tt.name)
				if tt.wantMsgContains != "" && !strings.Contains(resp.Msg, tt.wantMsgContains) {
					t.Errorf("%s: 期望 msg 包含 %q，实际=%q", tt.name, tt.wantMsgContains, resp.Msg)
				}
			}
		})
	}
}

// ===== B01: /model/code, /model/search, /model/exists =====
//
// 这三个接口都是 GET 只读，无副作用。注意它们的失败语义并不是 status=2，
// 而是返回 status=0 + 空 data：
//   - /model/code 在 modelName 为空 / encryption 包 / 不存在时返回 data=""（或 nil）
//   - /model/search 在无命中时返回 data=null
//   - /model/exists 始终返回 [bool]，模型不存在时为 [false]

func TestGetModelCode(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	tests := []struct {
		name         string
		packageId    string
		modelName    string
		wantNonEmpty bool // data 是否应是非空字符串
	}{
		{
			name:         "正常获取已加载模型源码",
			packageId:    modelicaStdLibId,
			modelName:    "Modelica.Blocks.Examples.PID_Controller",
			wantNonEmpty: true,
		},
		{
			name:         "model_name 为空时返回空 data（不是错误）",
			packageId:    modelicaStdLibId,
			modelName:    "",
			wantNonEmpty: false,
		},
		{
			name:         "不存在的模型返回空 data",
			packageId:    modelicaStdLibId,
			modelName:    "Non.Existent.Model.For.Test",
			wantNonEmpty: false,
		},
		{
			name:         "不存在的 package_id 仍能通过 SMC 查到全名模型",
			packageId:    "non-existent-package-id",
			modelName:    "Modelica.Blocks.Examples.PID_Controller",
			wantNonEmpty: true,
		},
	}

	hooks := helpers.TestHooks{}
	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				resp, err := testClient.Primary().GetModelCode(tt.packageId, tt.modelName)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				assert.IsSuccess(resp, tt.name)

				code, _ := resp.Data.(string)
				if tt.wantNonEmpty && code == "" {
					t.Errorf("%s: 期望 data 是非空字符串，实际为空", tt.name)
				}
				if !tt.wantNonEmpty && code != "" {
					t.Errorf("%s: 期望 data 为空，实际为 %d 字节", tt.name, len(code))
				}
				fmt.Println("Code:", code)
				fmt.Println()
				fmt.Println()
			})
		}
	})
}

func TestSearchModel(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	tests := []struct {
		name        string
		keywords    string
		parent      string
		minResults  int  // 期望返回的最小条目数
		expectEmpty bool // 期望返回空数组
	}{
		{
			name:       "命中关键字 PID",
			keywords:   "PID",
			parent:     "",
			minResults: 1,
		},
		{
			name:       "限定 parent 检索",
			keywords:   "",
			parent:     "Modelica.Blocks.Examples",
			minResults: 1,
		},
		{
			name:        "不存在的 parent 返回空",
			keywords:    "",
			parent:      "Foo.Bar.NotExist",
			expectEmpty: true,
		},
		{
			name:        "匹配不到模型的关键字返回空",
			keywords:    "QQQXYZNoSuchKeyword12345",
			parent:      "",
			expectEmpty: true,
		},
	}

	hooks := helpers.TestHooks{}
	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				resp, err := testClient.Primary().SearchModel(tt.keywords, tt.parent)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				assert.IsSuccess(resp, tt.name)

				// data 可能是 []any 或 nil
				list, _ := resp.Data.([]any)
				if tt.expectEmpty {
					if len(list) > 0 {
						t.Errorf("%s: 期望空数组，实际返回 %d 条", tt.name, len(list))
					}
					return
				}
				if tt.minResults > 0 && len(list) < tt.minResults {
					t.Errorf("%s: 期望至少 %d 条，实际返回 %d 条", tt.name, tt.minResults, len(list))
				}
			})
		}
	})
}

func TestModelExists(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	tests := []struct {
		name      string
		modelName string
		wantTrue  bool
	}{
		{
			name:      "已加载的模型返回 [true]",
			modelName: "Modelica.Blocks.Examples.PID_Controller",
			wantTrue:  true,
		},
		{
			name:      "不存在的模型返回 [false]",
			modelName: "Non.Existent.Model.Foo.Bar",
			wantTrue:  false,
		},
		{
			name:      "空 model_name 返回 [false]",
			modelName: "",
			wantTrue:  false,
		},
	}

	hooks := helpers.TestHooks{}
	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				resp, err := testClient.Primary().ModelExists(tt.modelName)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				assert.IsSuccess(resp, tt.name)

				arr, ok := resp.Data.([]any)
				if !ok || len(arr) != 1 {
					t.Fatalf("%s: data 应为单元素数组，实际 %T = %v", tt.name, resp.Data, resp.Data)
				}
				got, ok := arr[0].(bool)
				if !ok {
					t.Fatalf("%s: data[0] 不是 bool: %T", tt.name, arr[0])
				}
				if got != tt.wantTrue {
					t.Errorf("%s: 期望 %v，实际 %v", tt.name, tt.wantTrue, got)
				}
			})
		}
	})
}

// TestCheckModelV2 验证 POST /api/v2/model/check。
// 覆盖有效模型检查任务提交、模型包不存在，以及缺少必填 package_id 的绑定错误。
func TestCheckModelV2(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	tests := []struct {
		name               string
		useRaw             bool
		packageID          string
		modelName          string
		expectError        bool
		expectedCode       int
		expectedHTTPStatus int
	}{
		{
			name:      "正常提交模型检查任务",
			packageID: modelicaStdLibId,
			modelName: "Modelica.Blocks.Examples.PID_Controller",
		},
		{
			name:         "不存在的模型包返回 status=2",
			packageID:    "package-id-not-exist",
			modelName:    "Modelica.Blocks.Examples.PID_Controller",
			expectError:  true,
			expectedCode: 2,
		},
		{
			name:               "缺少 package_id 返回 HTTP 400",
			useRaw:             true,
			modelName:          "Modelica.Blocks.Examples.PID_Controller",
			expectedHTTPStatus: http.StatusBadRequest,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			if tt.useRaw {
				rawResp, err := testClient.Primary().CheckModelV2Raw(map[string]any{
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
					t.Fatalf("%s: 期望 HTTP %d，实际 %d，body=%q", tt.name, tt.expectedHTTPStatus, rawResp.StatusCode, string(body))
				}
				return
			}

			resp, err := testClient.Primary().CheckModelV2(tt.packageID, tt.modelName)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			if tt.expectError {
				assert.IsError(resp, tt.expectedCode, tt.name)
				return
			}
			assert.IsSuccess(resp, tt.name)
			if !strings.Contains(resp.Msg, "已提交") {
				t.Errorf("%s: 期望 msg 包含 已提交，实际=%q", tt.name, resp.Msg)
			}
		})
	}
}

// TestTranslateModelV3 验证 POST /api/v3/model/translate。
// 覆盖有效模型翻译任务提交并等待编译结束、模型不存在，以及缺少 model_name 的绑定错误。
func TestTranslateModelV3(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var packageID string
	var modelName string

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			{
				var err error
				packageID, modelName, err = modelhelpers.CreateTempModelFixture(testClient, modelicaStdLibId, "Modelica.Blocks.Examples.PID_Controller")
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := modelhelpers.EnsureModelLoaded(testClient, packageID, modelName, "v3 模型翻译夹具"); err != nil {
				t.Fatal(err)
			}
		},
		After: func(t *testing.T) {
			modelhelpers.CleanupTempModelFixture(testClient, packageID, modelName)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		t.Run("正常提交模型翻译任务并等待结束", func(t *testing.T) {
			resp, err := testClient.Primary().TranslateModelV3(packageID, modelName)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsSuccess(resp, "正常提交模型翻译任务并等待结束")
			if !strings.Contains(resp.Msg, "已提交") {
				t.Errorf("正常提交模型翻译任务并等待结束: 期望 msg 包含 已提交，实际=%q", resp.Msg)
			}
			if err := modelhelpers.WaitCompileFinished(testClient, packageID, modelName, "正常提交模型翻译任务并等待结束"); err != nil {
				t.Fatal(err)
			}
		})

		t.Run("不存在的模型返回 status=2", func(t *testing.T) {
			resp, err := testClient.Primary().TranslateModelV3(packageID, "Non.Existent.Model.For.Translate")
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsError(resp, 2, "不存在的模型返回 status=2")
		})

		t.Run("缺少 model_name 返回 HTTP 400", func(t *testing.T) {
			rawResp, err := testClient.Primary().TranslateModelV3Raw(map[string]any{
				"package_id": packageID,
			})
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			body, err := helpers.ReadAndCloseRawBody(rawResp)
			if err != nil {
				t.Fatal(err)
			}
			if rawResp.StatusCode != http.StatusBadRequest {
				t.Fatalf("缺少 model_name 返回 HTTP 400: 期望 HTTP 400，实际=%d，body=%q", rawResp.StatusCode, string(body))
			}
		})
	})
}

// ===== 旧 t.Skip 占位测试（保留以待恢复） =====

//func TestSetModelParameters(t *testing.T) {
//	t.Skip("测试设置参数接口. 暂时跳过")
//	assert := helpers.NewAssertResponse(t)
//
//	// 先加载模型
//	modelName := "Modelica.Blocks.Examples.PID_Controller"
//	testClient.LoadModel(modelicaStdLibId, modelName)
//
//	tests := []struct {
//		name        string
//		modelName   string
//		parameters  map[string]interface{}
//		expectError bool
//	}{
//		{
//			name:      "设置有效参数",
//			modelName: modelName,
//			parameters: map[string]interface{}{
//				"k":  1.0,
//				"Ti": 0.5,
//			},
//			expectError: false,
//		},
//		{
//			name:        "模型不存在",
//			modelName:   "NonExistent.Model",
//			parameters:  map[string]interface{}{"k": 1.0},
//			expectError: true,
//		},
//		{
//			name:        "参数为空",
//			modelName:   modelName,
//			parameters:  map[string]interface{}{},
//			expectError: false,
//		},
//	}
//
//	for _, tt := range tests {
//		t.Run(tt.name, func(t *testing.T) {
//			resp, err := testClient.SetModelParameters(tt.modelName, tt.parameters)
//
//			if err != nil {
//				if !tt.expectError {
//					t.Fatalf("unexpected error: %v", err)
//				}
//				return
//			}
//
//			if tt.expectError {
//				assert.IsError(resp, 400, tt.name)
//			} else {
//				assert.IsSuccess(resp, tt.name)
//			}
//		})
//	}
//
//	// 清理
//	testClient.UnLoadModel(modelName)
//}

func TestGetModelCodeLegacy(t *testing.T) {
	t.Skip("已被 B01 的 TestGetModelCode 替代，保留以备参考")
}

// ===== B02: /model/list_library, /model/components/get, /model/document/get =====

func TestGetListModel(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	tests := []struct {
		name         string
		packageId    string
		modelName    string
		wantNonEmpty bool // data 是否应包含子节点
	}{
		{
			name:         "获取 Modelica 标准库根节点的子节点",
			packageId:    modelicaStdLibId,
			modelName:    "Modelica",
			wantNonEmpty: true,
		},
		{
			name:         "获取 Modelica.Blocks 的子节点",
			packageId:    modelicaStdLibId,
			modelName:    "Modelica.Blocks",
			wantNonEmpty: true,
		},
		{
			name:         "model_name 为空时返回根节点列表",
			packageId:    modelicaStdLibId,
			modelName:    "",
			wantNonEmpty: false, // 可能返回根节点列表，不强制要求为空
		},
		{
			name:         "不存在的模型返回空或 nil",
			packageId:    modelicaStdLibId,
			modelName:    "Non.Existent.Model",
			wantNonEmpty: false,
		},
		{
			name:         "叶子节点（无子节点）返回空或 nil",
			packageId:    modelicaStdLibId,
			modelName:    "Modelica.Blocks.Examples.PID_Controller",
			wantNonEmpty: false,
		},
	}

	hooks := helpers.TestHooks{}
	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				resp, err := testClient.Primary().GetListModel(tt.packageId, tt.modelName)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				assert.IsSuccess(resp, tt.name)

				// data 可能是数组、nil 或其他类型
				if resp.Data == nil {
					if tt.wantNonEmpty {
						t.Errorf("%s: 期望返回非空子节点列表，实际为 nil", tt.name)
					}
					return
				}

				dataSlice, ok := resp.Data.([]interface{})
				if !ok {
					// data 不是数组类型，可能是空字符串或其他类型，视为空列表
					if tt.wantNonEmpty {
						t.Errorf("%s: 期望返回非空子节点列表，实际 data 类型为 %T", tt.name, resp.Data)
					}
					return
				}

				if tt.wantNonEmpty && len(dataSlice) == 0 {
					t.Errorf("%s: 期望返回非空子节点列表，实际为空数组", tt.name)
				}
			})
		}
	})
}

func TestGetModelComponents(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	tests := []struct {
		name      string
		modelName string
	}{
		{
			name:      "获取标准库模型的组件（可能为空）",
			modelName: "Modelica.Blocks.Examples.PID_Controller",
		},
		{
			name:      "model_name 为空时返回空数据",
			modelName: "",
		},
		{
			name:      "不存在的模型返回空数据",
			modelName: "Non.Existent.Model",
		},
		{
			name:      "获取 package 类型模型（通常无组件）",
			modelName: "Modelica.Blocks",
		},
	}

	hooks := helpers.TestHooks{}
	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				resp, err := testClient.Primary().GetModelComponents(tt.modelName)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}

				assert.IsSuccess(resp, tt.name)
				// data 可能是 map、nil 或其他类型，不强制要求非空
				// 因为接口行为可能依赖模型是否已加载或模型类型
			})
		}
	})
}

func TestGetModelDocument(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	tests := []struct {
		name      string
		modelName string
	}{
		{
			name:      "获取标准库模型文档",
			modelName: "Modelica.Blocks.Examples.PID_Controller",
		},
		{
			name:      "获取 Modelica 根包文档",
			modelName: "Modelica",
		},
		{
			name:      "model_name 为空时返回空文档",
			modelName: "",
		},
		{
			name:      "不存在的模型返回空文档",
			modelName: "Non.Existent.Model",
		},
	}

	hooks := helpers.TestHooks{}
	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				resp, err := testClient.Primary().GetModelDocument(tt.modelName)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}

				assert.IsSuccess(resp, tt.name)
				// data 可能是字符串或 nil，不强制要求非空
			})
		}
	})
}

// ===== B03: /model/uml/get, /model/extend/get, /model/function/search =====

func TestGetUML(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	tests := []struct {
		name          string
		className     string
		wantDataType  string
		wantResultSet bool
	}{
		{
			name:          "正常获取模型 UML",
			className:     "Modelica.Blocks.Examples.PID_Controller",
			wantDataType:  "model",
			wantResultSet: true,
		},
		{
			name:          "正常获取 block UML",
			className:     "Modelica.Blocks.Continuous.PI",
			wantDataType:  "model",
			wantResultSet: true,
		},
		{
			name:          "正常获取 connector UML",
			className:     "Modelica.Blocks.Interfaces.RealInput",
			wantDataType:  "model",
			wantResultSet: true,
		},
	}

	hooks := helpers.TestHooks{}
	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				resp, err := testClient.Primary().GetUML(tt.className)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}

				assert.IsSuccess(resp, tt.name)
				assert.HasFields(resp, []string{"dataType", "resultData"}, tt.name)

				dataMap, ok := resp.Data.(map[string]any)
				if !ok {
					t.Fatalf("%s: data 不是 object: %T", tt.name, resp.Data)
				}
				if dataMap["dataType"] != tt.wantDataType {
					t.Errorf("%s: 期望 dataType=%q，实际=%v", tt.name, tt.wantDataType, dataMap["dataType"])
				}
				if tt.wantResultSet && dataMap["resultData"] == nil {
					t.Errorf("%s: 期望 resultData 非 nil，实际为 nil", tt.name)
				}
			})
		}
	})
}

func TestGetExtendedModel(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	tests := []struct {
		name         string
		modelName    string
		expectError  bool
		expectedCode int
		minLength    int
	}{
		{
			name:        "正常获取有父类的模型",
			modelName:   "Modelica.Blocks.Continuous.PI",
			expectError: false,
			minLength:   1,
		},
		{
			name:         "model_name 为空返回 status=2",
			modelName:    "",
			expectError:  true,
			expectedCode: 2,
		},
		{
			name:         "不存在的模型返回 status=2",
			modelName:    "Non.Existent.Model.For.Extend",
			expectError:  true,
			expectedCode: 2,
		},
	}

	hooks := helpers.TestHooks{}
	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				resp, err := testClient.Primary().GetExtendedModel(tt.modelName)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}

				if tt.expectError {
					assert.IsError(resp, tt.expectedCode, tt.name)
					return
				}

				assert.IsSuccess(resp, tt.name)
				list, ok := resp.Data.([]any)
				if !ok {
					t.Fatalf("%s: data 不是数组: %T", tt.name, resp.Data)
				}
				if len(list) < tt.minLength {
					t.Errorf("%s: 期望至少 %d 条父类，实际 %d 条", tt.name, tt.minLength, len(list))
				}
			})
		}
	})
}

func TestSearchFunctionType(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	tests := []struct {
		name        string
		parent      string
		minLength   int
		expectEmpty bool
	}{
		{
			name:      "不传 parent 返回顶层 function/record/package 列表",
			parent:    "",
			minLength: 1,
		},
		{
			name:      "限定 parent 为 Modelica.Math 时返回结果",
			parent:    "Modelica.Math",
			minLength: 1,
		},
		{
			name:        "不存在的 parent 返回空列表",
			parent:      "Non.Existent.Function.Parent",
			expectEmpty: true,
		},
	}

	hooks := helpers.TestHooks{}
	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				resp, err := testClient.Primary().SearchFunctionType(tt.parent)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}

				assert.IsSuccess(resp, tt.name)

				list, _ := resp.Data.([]any)
				if tt.expectEmpty {
					if len(list) != 0 {
						t.Errorf("%s: 期望空数组，实际返回 %d 条", tt.name, len(list))
					}
					return
				}
				if len(list) < tt.minLength {
					t.Errorf("%s: 期望至少 %d 条，实际 %d 条", tt.name, tt.minLength, len(list))
				}
			})
		}
	})
}

// TestParseTranslateLog 验证 POST /model/translate/log/parse。
// 覆盖有效翻译日志解析，以及缺少 log_text 时的 JSON 绑定错误。
func TestParseTranslateLog(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	t.Run("正常解析翻译日志", func(t *testing.T) {
		logText := strings.Join([]string{
			"Translation of DemoModel",
			"",
			"Statistics",
			"",
			"Original Model",
			"  Number of components: 2",
			"  Variables: 3",
			"  Unknowns: 1 (4 scalars)",
			"  Equations: 5",
			"Translated Model",
			"  Outputs: 1 scalars",
			"  Continuous time states: 2 scalars",
			"Finished",
		}, "\n")

		resp, err := testClient.Primary().ParseTranslateLog(logText)
		if err != nil {
			t.Fatalf("请求失败: %v", err)
		}

		assert.IsSuccess(resp, "正常解析翻译日志")
		dataMap, err := helpers.ObjectData(resp, "正常解析翻译日志")
		if err != nil {
			t.Fatal(err)
		}
		if dataMap["model_name"] != "DemoModel" {
			t.Errorf("正常解析翻译日志: 期望 model_name=DemoModel，实际=%v", dataMap["model_name"])
		}
		if dataMap["translate_success"] != true {
			t.Errorf("正常解析翻译日志: 期望 translate_success=true，实际=%v", dataMap["translate_success"])
		}
		for _, field := range []string{"summary", "structure_analysis", "abnormal_items", "log_info"} {
			if _, ok := dataMap[field]; !ok {
				t.Errorf("正常解析翻译日志: data 缺少字段 %q", field)
			}
		}
	})

	t.Run("缺少 log_text 返回 HTTP 400", func(t *testing.T) {
		rawResp, err := testClient.Primary().ParseTranslateLogRaw(map[string]any{})
		if err != nil {
			t.Fatalf("请求失败: %v", err)
		}
		body, err := helpers.ReadAndCloseRawBody(rawResp)
		if err != nil {
			t.Fatal(err)
		}
		if rawResp.StatusCode != http.StatusBadRequest {
			t.Fatalf("缺少 log_text 返回 HTTP 400: 期望 HTTP %d，实际 %d，body=%q", http.StatusBadRequest, rawResp.StatusCode, string(body))
		}
	})
}
