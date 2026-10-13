//go:build api_test

package api

import (
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"yssim-go/tests/helpers"
	modelhelpers "yssim-go/tests/helpers/model"
)

const (
	tempModelSourcePackageID = modelicaStdLibId
	tempModelSourceModelName = "Modelica.Blocks.Examples.PID_Controller"
	tempModelComponentClass  = "Modelica.Blocks.Math.Gain"
)

// TestCopyClass 验证 POST /model/class/copy。
// 覆盖：
//   - 正常复制标准库模型到当前用户空间
//   - 复制 package 类型返回 status=2
//   - 缺少 copied_class_name 返回 HTTP 400
func TestCopyClass(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var createdPackageIDs []string
	var createdModelNames []string

	uniqueModelName := modelhelpers.UniqueModelName("TestModel")

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			if err := modelhelpers.EnsureSystemModelLoaded(testClient, tempModelSourcePackageID, "", "复制模型前置加载 Modelica"); err != nil {
				t.Fatal(err)
			}
		},
		After: func(t *testing.T) {
			for i := range createdPackageIDs {
				modelhelpers.CleanupTempModelFixture(testClient, createdPackageIDs[i], createdModelNames[i])
			}
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		tests := []struct {
			name               string
			useRaw             bool
			fromPackageID      string
			toPackageID        string
			parentName         string
			modelName          string
			copiedClassName    string
			expectError        bool
			expectedCode       int
			expectedHTTPStatus int
		}{
			{
				name:            "正常复制标准库模型到当前用户空间",
				fromPackageID:   tempModelSourcePackageID,
				toPackageID:     "",
				parentName:      "",
				modelName:       uniqueModelName,
				copiedClassName: tempModelSourceModelName,
			},
			{
				name:            "复制 package 类型返回 status=2",
				fromPackageID:   tempModelSourcePackageID,
				toPackageID:     "",
				parentName:      "",
				modelName:       modelhelpers.UniqueModelName("TestModel"),
				copiedClassName: "Modelica",
				expectError:     true,
				expectedCode:    2,
			},
			{
				name:               "缺少 copied_class_name 返回 HTTP 400",
				useRaw:             true,
				fromPackageID:      tempModelSourcePackageID,
				toPackageID:        "",
				parentName:         "",
				modelName:          modelhelpers.UniqueModelName("TestModel"),
				copiedClassName:    "",
				expectedHTTPStatus: 400,
			},
		}

		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				if tt.useRaw {
					rawResp, err := testClient.PostRawResponse("/model/class/copy", map[string]any{
						"from_package_id":   tt.fromPackageID,
						"to_package_id":     tt.toPackageID,
						"parent_name":       tt.parentName,
						"model_name":        tt.modelName,
						"copied_class_name": tt.copiedClassName,
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

				resp, err := testClient.Primary().CopyClass(tt.fromPackageID, tt.toPackageID, tt.parentName, tt.modelName, tt.copiedClassName)
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
				packageID, ok := dataMap["id"].(string)
				if !ok || packageID == "" {
					t.Fatalf("%s: 返回 id 为空或类型错误: %v", tt.name, dataMap["id"])
				}
				modelName, ok := dataMap["model_name"].(string)
				if !ok || modelName == "" {
					t.Fatalf("%s: 返回 model_name 为空或类型错误: %v", tt.name, dataMap["model_name"])
				}
				createdPackageIDs = append(createdPackageIDs, packageID)
				createdModelNames = append(createdModelNames, modelName)
				assertCopiedModelVisibleInUserRoot(t, packageID, modelName, tt.name)
			})
		}
	})
}

// TestMoveClass 验证 POST /model/class/move。
// 覆盖：
//   - model 节点上移、下移
//   - package 节点置底、置顶
//   - 同位置移动幂等成功
//   - 参数、模型归属和模型存在性等错误场景
func TestMoveClass(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	const (
		moveClassPackageName     = "MoveClassPackage"
		moveClassPackageFileName = moveClassPackageName + ".mo"
	)

	var packageID string
	packageName := moveClassPackageName
	subPackageName := packageName + ".SubPkg"

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			packageID = createMoveClassFixture(t, moveClassPackageName, moveClassPackageFileName)
			assertModelChildOrder(t, packageID, packageName, []string{"A", "SubPkg", "B", "C"}, "移动前顶层子节点顺序")
			assertModelChildOrder(t, packageID, subPackageName, []string{"SubA", "SubB", "SubC"}, "移动前 SubPkg 子节点顺序")
		},
		After: func(t *testing.T) {
			modelhelpers.CleanupTempModelFixture(testClient, packageID, packageName)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		successTests := []struct {
			name             string
			modelName        string
			targetIndex      int
			expectedTopOrder []string
		}{
			{
				name:             "模型节点上移",
				modelName:        packageName + ".B",
				targetIndex:      1,
				expectedTopOrder: []string{"A", "B", "SubPkg", "C"},
			},
			{
				name:             "模型节点下移",
				modelName:        packageName + ".B",
				targetIndex:      2,
				expectedTopOrder: []string{"A", "SubPkg", "B", "C"},
			},
			{
				name:             "package 节点置底",
				modelName:        subPackageName,
				targetIndex:      3,
				expectedTopOrder: []string{"A", "B", "C", "SubPkg"},
			},
			{
				name:             "package 节点置顶",
				modelName:        subPackageName,
				targetIndex:      0,
				expectedTopOrder: []string{"SubPkg", "A", "B", "C"},
			},
			{
				name:             "同位置移动幂等成功",
				modelName:        subPackageName,
				targetIndex:      0,
				expectedTopOrder: []string{"SubPkg", "A", "B", "C"},
			},
		}

		for _, tt := range successTests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				resp, err := testClient.Primary().MoveClass(packageID, tt.modelName, tt.targetIndex)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				assert.IsSuccess(resp, tt.name)
				assertMoveClassData(t, resp, packageID, tt.modelName, tt.targetIndex, tt.name)
				assertModelChildOrder(t, packageID, packageName, tt.expectedTopOrder, tt.name+": 顶层子节点顺序")
				// 移动 package 本身不能破坏其子树，模型节点移动时也不应影响旁边 package 的内部顺序。
				assertModelChildOrder(t, packageID, subPackageName, []string{"SubA", "SubB", "SubC"}, tt.name+": SubPkg 子节点顺序")
			})
		}

		errorTests := []struct {
			name         string
			packageID    string
			modelName    string
			targetIndex  int
			expectedErr  string
			expectedCode int
		}{
			{
				name:         "target_index 小于 0 返回业务错误",
				packageID:    packageID,
				modelName:    packageName + ".A",
				targetIndex:  -1,
				expectedErr:  "target_index不能小于0",
				expectedCode: 2,
			},
			{
				name:         "顶层模型库不支持排序",
				packageID:    packageID,
				modelName:    packageName,
				targetIndex:  0,
				expectedErr:  "第一层级模型或模型库不支持排序",
				expectedCode: 2,
			},
			{
				name:         "不存在的 package_id 返回业务错误",
				packageID:    "not-exist-package-id",
				modelName:    packageName + ".A",
				targetIndex:  0,
				expectedErr:  "模型库不存在",
				expectedCode: 2,
			},
			{
				name:         "model_name 不属于当前 package 返回业务错误",
				packageID:    packageID,
				modelName:    "OtherPackage.A",
				targetIndex:  0,
				expectedErr:  "模型不属于当前package",
				expectedCode: 2,
			},
			{
				name:         "不存在的 model_name 返回业务错误",
				packageID:    packageID,
				modelName:    packageName + ".NotExist",
				targetIndex:  0,
				expectedErr:  "模型不存在",
				expectedCode: 2,
			},
		}

		for _, tt := range errorTests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				resp, err := testClient.Primary().MoveClass(tt.packageID, tt.modelName, tt.targetIndex)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				assert.IsError(resp, tt.expectedCode, tt.name)
				if !strings.Contains(resp.Err, tt.expectedErr) {
					t.Fatalf("%s: 期望 err 包含 %q，实际=%q", tt.name, tt.expectedErr, resp.Err)
				}
			})
		}

		t.Run("缺少 target_index 返回 HTTP 400", func(t *testing.T) {
			rawResp, err := testClient.Primary().PostRawResponse("/model/class/move", map[string]any{
				"package_id": packageID,
				"model_name": packageName + ".A",
			})
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			body, err := helpers.ReadAndCloseRawBody(rawResp)
			if err != nil {
				t.Fatal(err)
			}
			if rawResp.StatusCode != http.StatusBadRequest {
				t.Fatalf("缺少 target_index 返回 HTTP 400: 期望 HTTP 400，实际=%d，body=%q", rawResp.StatusCode, string(body))
			}
		})
	})
}

// TestMoveClassToPackage 验证 POST /model/class/move-to-package。
// 覆盖跨父级移动、跨存储形态移动、顶层模型库移动和负向校验。
func TestMoveClassToPackage(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	const (
		dragMoveLibName        = "DragMoveLib"
		dragMoveLibFileName    = dragMoveLibName + ".zip"
		dragMoveRootSourceName = "DragMoveRootSource"
		dragMoveRootSourceFile = dragMoveRootSourceName + ".zip"
	)

	fileWaitOptions := modelhelpers.FileWaitOptions{
		Timeout:      time.Minute,
		InitialDelay: 200 * time.Millisecond,
		MaxDelay:     5 * time.Second,
	}

	t.Run("缺少 target_package_name 返回 HTTP 400", func(t *testing.T) {
		runWithDragMoveLibFixture(t, dragMoveLibName, dragMoveLibFileName, func(t *testing.T, packageID string) {
			rawResp, err := testClient.Primary().MoveClassToPackageRaw(map[string]any{
				"source_package_id": packageID,
				"target_package_id": packageID,
				"source_class_name": dragMoveLibName + ".SourcePkg.Motor",
			})
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			body, err := helpers.ReadAndCloseRawBody(rawResp)
			if err != nil {
				t.Fatal(err)
			}
			if rawResp.StatusCode != http.StatusBadRequest {
				t.Fatalf("缺少 target_package_name 返回 HTTP 400: 期望 HTTP 400，实际=%d，body=%q", rawResp.StatusCode, string(body))
			}
		})
	})

	t.Run("源模型库不存在返回 status=2", func(t *testing.T) {
		runWithDragMoveLibFixture(t, dragMoveLibName, dragMoveLibFileName, func(t *testing.T, packageID string) {
			resp, err := testClient.Primary().MoveClassToPackage("not-exist-source-package-id", packageID, dragMoveLibName+".SourcePkg.Motor", dragMoveLibName+".TargetPkg")
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsError(resp, 2, "源模型库不存在返回 status=2")
			if !strings.Contains(resp.Err, "源模型库不存在") {
				t.Fatalf("源模型库不存在返回 status=2: 期望 err 包含源模型库不存在，实际=%q", resp.Err)
			}
		})
	})

	// 正确场景测试, 会验证模型在 AST 中是否移动到了正确位置, 以及对应文件是否成功更新
	successTests := []struct {
		name              string
		sourceClassName   string
		targetPackageName string
		assertAfterMove   func(t *testing.T, packageID string)
		fileBefore        func(rootPath string) []modelhelpers.FileExpectation
		fileAfter         func(rootPath string) []modelhelpers.FileExpectation
	}{
		{
			name:              "独立 mo 模型移动到目录型 package",
			sourceClassName:   dragMoveLibName + ".SourcePkg.Motor",
			targetPackageName: dragMoveLibName + ".TargetPkg",
			assertAfterMove: func(t *testing.T, packageID string) {
				assertModelChildrenNotContain(t, packageID, dragMoveLibName+".SourcePkg", "Motor", "独立 mo 模型移动到目录型 package: 源 package")
				assertModelChildrenContain(t, packageID, dragMoveLibName+".TargetPkg", "Motor", "独立 mo 模型移动到目录型 package: 目标 package")
			},
			fileBefore: func(rootPath string) []modelhelpers.FileExpectation {
				return []modelhelpers.FileExpectation{
					{Path: filepath.Join(rootPath, "SourcePkg", "Motor.mo"), ShouldExist: true, Contains: []string{"model Motor", "end Motor;"}},
					{Path: filepath.Join(rootPath, "SourcePkg", "package.order"), ShouldExist: true, ContainsLines: []string{"Motor"}},
					{Path: filepath.Join(rootPath, "TargetPkg", "Motor.mo"), ShouldExist: false},
					{Path: filepath.Join(rootPath, "TargetPkg", "package.order"), ShouldExist: true, NotContainsLines: []string{"Motor"}},
				}
			},
			fileAfter: func(rootPath string) []modelhelpers.FileExpectation {
				return []modelhelpers.FileExpectation{
					{Path: filepath.Join(rootPath, "TargetPkg", "Motor.mo"), ShouldExist: true, Contains: []string{"model Motor", "end Motor;"}},
					{Path: filepath.Join(rootPath, "TargetPkg", "package.order"), ShouldExist: true, ContainsLines: []string{"Motor"}},
					{Path: filepath.Join(rootPath, "SourcePkg", "package.order"), ShouldExist: true, NotContainsLines: []string{"Motor"}},
					{Path: filepath.Join(rootPath, "SourcePkg", "Motor.mo"), ShouldExist: false},
				}
			},
		},
		{
			name:              "目录型 package 整体移动到目录型 package",
			sourceClassName:   dragMoveLibName + ".SourcePkg",
			targetPackageName: dragMoveLibName + ".TargetPkg",
			assertAfterMove: func(t *testing.T, packageID string) {
				assertModelChildrenNotContain(t, packageID, dragMoveLibName, "SourcePkg", "目录型 package 整体移动到目录型 package: 源父级")
				assertModelChildrenContain(t, packageID, dragMoveLibName+".TargetPkg", "SourcePkg", "目录型 package 整体移动到目录型 package: 目标 package")
				assertModelChildrenContain(t, packageID, dragMoveLibName+".TargetPkg.SourcePkg", "Motor", "目录型 package 整体移动到目录型 package: 子模型")
			},
			fileBefore: func(rootPath string) []modelhelpers.FileExpectation {
				return []modelhelpers.FileExpectation{
					{Path: filepath.Join(rootPath, "SourcePkg"), ShouldExist: true},
					{Path: filepath.Join(rootPath, "SourcePkg", "Motor.mo"), ShouldExist: true},
					{Path: filepath.Join(rootPath, "package.order"), ShouldExist: true, ContainsLines: []string{"SourcePkg"}},
					{Path: filepath.Join(rootPath, "TargetPkg", "SourcePkg"), ShouldExist: false},
					{Path: filepath.Join(rootPath, "TargetPkg", "SourcePkg.mo"), ShouldExist: false},
					{Path: filepath.Join(rootPath, "TargetPkg", "package.order"), ShouldExist: true, NotContainsLines: []string{"SourcePkg"}},
				}
			},
			fileAfter: func(rootPath string) []modelhelpers.FileExpectation {
				return []modelhelpers.FileExpectation{
					{Path: filepath.Join(rootPath, "TargetPkg", "SourcePkg.mo"), ShouldExist: true, Contains: []string{"package SourcePkg", "model Motor", "end SourcePkg;"}},
					{Path: filepath.Join(rootPath, "TargetPkg", "SourcePkg"), ShouldExist: false},
					{Path: filepath.Join(rootPath, "TargetPkg", "package.order"), ShouldExist: true, ContainsLines: []string{"SourcePkg"}},
					{Path: filepath.Join(rootPath, "package.order"), ShouldExist: true, NotContainsLines: []string{"SourcePkg"}},
					{Path: filepath.Join(rootPath, "SourcePkg"), ShouldExist: false},
				}
			},
		},
		{
			name:              "单 mo package 移动到目录型 package",
			sourceClassName:   dragMoveLibName + ".SingleFilePkg",
			targetPackageName: dragMoveLibName + ".TargetPkg",
			assertAfterMove: func(t *testing.T, packageID string) {
				assertModelChildrenNotContain(t, packageID, dragMoveLibName, "SingleFilePkg", "单 mo package 移动到目录型 package: 源父级")
				assertModelChildrenContain(t, packageID, dragMoveLibName+".TargetPkg", "SingleFilePkg", "单 mo package 移动到目录型 package: 目标 package")
				assertModelChildrenContain(t, packageID, dragMoveLibName+".TargetPkg.SingleFilePkg", "InnerModel", "单 mo package 移动到目录型 package: 子模型")
			},
		},
	}

	for _, tt := range successTests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			runWithDragMoveLibFixture(t, dragMoveLibName, dragMoveLibFileName, func(t *testing.T, packageID string) {
				var rootPath string
				skipFileReason, skipFileCheck := modelhelpers.FileExpectationSkipReason()
				if (tt.fileBefore != nil || tt.fileAfter != nil) && !skipFileCheck {
					var err error
					rootPath, err = modelhelpers.ModelPackageRootPath(packageID)
					if err != nil {
						t.Fatal(err)
					}
				}
				if tt.fileBefore != nil {
					if skipFileCheck {
						t.Logf("%s: 跳过移动前文件状态校验: %s", tt.name, skipFileReason)
					} else if err := modelhelpers.WaitForFileExpectations(tt.fileBefore(rootPath), fileWaitOptions); err != nil {
						t.Fatalf("%s: 移动前文件状态不符合预期: %v", tt.name, err)
					}
				}

				expectedNewClassName := tt.targetPackageName + "." + shortModelName(tt.sourceClassName)
				resp, err := testClient.Primary().MoveClassToPackage(packageID, packageID, tt.sourceClassName, tt.targetPackageName)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				assert.IsSuccess(resp, tt.name)
				assertMoveClassToPackageData(t, resp, tt.sourceClassName, expectedNewClassName, tt.name)
				tt.assertAfterMove(t, packageID)
				if tt.fileAfter != nil {
					if skipFileCheck {
						t.Logf("%s: 跳过移动后文件状态校验: %s", tt.name, skipFileReason)
					} else if err := modelhelpers.WaitForFileExpectations(tt.fileAfter(rootPath), fileWaitOptions); err != nil {
						t.Fatalf("%s: 移动后文件状态不符合预期: %v", tt.name, err)
					}
				}
			})
		})
	}

	// 错误场景测试
	errorTests := []struct {
		name              string
		sourceClassName   string
		targetPackageName string
		expectedErr       string
	}{
		{
			name:              "目标 package 存在同名对象返回 status=2",
			sourceClassName:   dragMoveLibName + ".SourcePkg.Motor",
			targetPackageName: dragMoveLibName + ".NameConflictPkg",
			expectedErr:       "同名",
		},
		{
			name:              "package 不能移动到自身后代返回 status=2",
			sourceClassName:   dragMoveLibName + ".CyclePkgA",
			targetPackageName: dragMoveLibName + ".CyclePkgA.CyclePkgB.CyclePkgC",
			expectedErr:       "子",
		},
		{
			name:              "目标不是 package 返回 status=2",
			sourceClassName:   dragMoveLibName + ".SourcePkg.Sensor",
			targetPackageName: dragMoveLibName + ".SourcePkg.Motor",
		},
	}

	for _, tt := range errorTests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			runWithDragMoveLibFixture(t, dragMoveLibName, dragMoveLibFileName, func(t *testing.T, packageID string) {
				resp, err := testClient.Primary().MoveClassToPackage(packageID, packageID, tt.sourceClassName, tt.targetPackageName)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				assert.IsError(resp, 2, tt.name)
				if tt.expectedErr != "" && !strings.Contains(resp.Err, tt.expectedErr) {
					t.Fatalf("%s: 期望 err 包含 %q，实际=%q", tt.name, tt.expectedErr, resp.Err)
				}
			})
		})
	}

	t.Run("顶层模型库移动到目录型 package", func(t *testing.T) {
		runWithDragMoveRootAndLibFixtures(t, dragMoveRootSourceName, dragMoveRootSourceFile, dragMoveLibName, dragMoveLibFileName, func(t *testing.T, sourcePackageID, targetPackageID string) {
			sourceClassName := dragMoveRootSourceName
			targetPackageName := dragMoveLibName + ".TargetPkg"
			expectedNewClassName := targetPackageName + "." + sourceClassName
			resp, err := testClient.Primary().MoveClassToPackage(sourcePackageID, targetPackageID, sourceClassName, targetPackageName)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsSuccess(resp, "顶层模型库移动到目录型 package")
			assertMoveClassToPackageData(t, resp, sourceClassName, expectedNewClassName, "顶层模型库移动到目录型 package")
			assertModelChildrenContain(t, targetPackageID, targetPackageName, sourceClassName, "顶层模型库移动到目录型 package: 目标 package")
		})
	})
}

// TestCreateModelPackage 验证 POST /file/create/package。
// 覆盖：
//   - 正常创建 package 类型用户包
//   - 重复名称返回 status=2
//   - 非法名称返回 status=2
//   - 缺少 str_type 返回 HTTP 400
func TestCreateModelPackage(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var existingPackageID string
	var existingPackageName string
	var createdPackageIDs []string
	var createdModelNames []string

	existingPackageName = uniqueName("CreatePkgExisting")
	newPackageName := uniqueName("CreatePkgNew")

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			resp, err := testClient.Primary().CreateModelPackage(existingPackageName, "package", map[string]any{
				"expand":       "",
				"insert_to":    "",
				"partial":      false,
				"encapsulated": false,
				"state":        false,
			})
			if err != nil {
				t.Fatalf("创建重复名夹具失败: %v", err)
			}
			if resp.Status != 0 {
				t.Fatalf("创建重复名夹具返回非成功状态: status=%d, msg=%s, err=%s", resp.Status, resp.Msg, resp.Err)
			}
			dataMap, err := helpers.ObjectData(resp, "创建重复名夹具")
			if err != nil {
				t.Fatal(err)
			}
			existingPackageID, _ = dataMap["id"].(string)
		},
		After: func(t *testing.T) {
			modelhelpers.CleanupTempModelFixture(testClient, existingPackageID, existingPackageName)
			for i := range createdPackageIDs {
				modelhelpers.CleanupTempModelFixture(testClient, createdPackageIDs[i], createdModelNames[i])
			}
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		tests := []struct {
			name               string
			packageName        string
			strType            string
			vars               map[string]any
			useRaw             bool
			expectError        bool
			expectedCode       int
			expectedHTTPStatus int
			expectedBody       string
		}{
			{
				name:        "正常创建 package 类型用户包",
				packageName: newPackageName,
				strType:     "package",
				vars: map[string]any{
					"expand":       "",
					"insert_to":    "",
					"partial":      false,
					"encapsulated": false,
					"state":        false,
				},
			},
			{
				name:         "重复名称返回 status=2",
				packageName:  existingPackageName,
				strType:      "package",
				vars:         map[string]any{"expand": "", "insert_to": "", "partial": false, "encapsulated": false, "state": false},
				expectError:  true,
				expectedCode: 2,
			},
			{
				name:         "非法名称返回 status=2",
				packageName:  "非法 包!",
				strType:      "package",
				vars:         map[string]any{"expand": "", "insert_to": "", "partial": false, "encapsulated": false, "state": false},
				expectError:  true,
				expectedCode: 2,
			},
			{
				name:               "缺少 str_type 返回 HTTP 400",
				packageName:        uniqueName("CreatePkgRaw"),
				vars:               map[string]any{"expand": "", "insert_to": "", "partial": false, "encapsulated": false, "state": false},
				useRaw:             true,
				expectedHTTPStatus: http.StatusBadRequest,
				expectedBody:       "",
			},
		}

		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				if tt.useRaw {
					rawResp, err := testClient.PostRawResponse("/file/create/package", map[string]any{
						"package_name": tt.packageName,
						"vars":         tt.vars,
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

				resp, err := testClient.Primary().CreateModelPackage(tt.packageName, tt.strType, tt.vars)
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
				packageID, ok := dataMap["id"].(string)
				if !ok || packageID == "" {
					t.Fatalf("%s: 返回 id 为空或类型错误: %v", tt.name, dataMap["id"])
				}
				modelName, ok := dataMap["model_name"].(string)
				if !ok || modelName == "" {
					t.Fatalf("%s: 返回 model_name 为空或类型错误: %v", tt.name, dataMap["model_name"])
				}
				createdPackageIDs = append(createdPackageIDs, packageID)
				createdModelNames = append(createdModelNames, modelName)
				assertCopiedModelVisibleInUserRoot(t, packageID, modelName, tt.name)
			})
		}
	})
}

// TestUpdateModelPackage 验证 POST /file/update/package。
// 覆盖：
//   - 正常更新用户模型源码
//   - 非法源码返回 status=2
//   - 不存在的 package_id 返回 HTTP 400
func TestUpdateModelPackage(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var packageID string
	var modelName string

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			{
				var err error
				packageID, modelName, err = modelhelpers.CreateTempModelFixture(testClient, tempModelSourcePackageID, tempModelSourceModelName)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := modelhelpers.EnsureModelLoaded(testClient, packageID, modelName, "更新模型源码夹具"); err != nil {
				t.Fatal(err)
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
			modelName          string
			modelStr           string
			useRaw             bool
			expectError        bool
			expectedCode       int
			expectedHTTPStatus int
			expectedBody       string
		}{
			{
				name:      "正常更新用户模型源码",
				packageID: packageID,
				modelName: modelName,
				modelStr:  updatedModelSource(modelName),
			},
			{
				name:         "非法源码返回 status=2",
				packageID:    packageID,
				modelName:    modelName,
				modelStr:     "not valid modelica code",
				expectError:  true,
				expectedCode: 2,
			},
			{
				name:               "不存在的 package_id 返回 HTTP 400",
				packageID:          "non-existent-package-id",
				modelName:          modelName,
				modelStr:           updatedModelSource(modelName),
				useRaw:             true,
				expectedHTTPStatus: http.StatusBadRequest,
				expectedBody:       "not found",
			},
		}

		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				if tt.useRaw {
					rawResp, err := testClient.PostRawResponse("/file/update/package", map[string]any{
						"package_id": tt.packageID,
						"model_name": tt.modelName,
						"model_str":  tt.modelStr,
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

				resp, err := testClient.Primary().UpdateModelPackage(tt.packageID, tt.modelName, tt.modelStr)
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
				if dataMap["id"] != packageID {
					t.Errorf("%s: 期望 id=%q，实际=%v", tt.name, packageID, dataMap["id"])
				}
				if dataMap["name"] != modelName {
					t.Errorf("%s: 期望 name=%q，实际=%v", tt.name, modelName, dataMap["name"])
				}
				assertModelCodeContains(t, packageID, modelName, "PI(k=8, Ti=0.75)", tt.name+": 源码回读")
			})
		}
	})
}

// TestGetPackageFileList 验证 GET /file/package/list。
// 覆盖：
//   - 正常返回当前用户空间模型文件列表
//   - 返回项包含 package_id / space_name / package_name / create_time / update_time
//   - 新创建的用户包可在列表中查到
func TestGetPackageFileList(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var packageID string
	var modelName string

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			{
				var err error
				packageID, modelName, err = modelhelpers.CreateTempPackageFixture(testClient, "TestResourcePackage")
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
		resp, err := testClient.Primary().GetPackageFileList()
		if err != nil {
			t.Fatalf("请求失败: %v", err)
		}
		assert.IsSuccess(resp, "正常返回当前用户空间模型文件列表")

		list, ok := resp.Data.([]any)
		if !ok {
			t.Fatalf("正常返回当前用户空间模型文件列表: data 不是数组: %T", resp.Data)
		}
		if len(list) == 0 {
			t.Fatalf("正常返回当前用户空间模型文件列表: 期望非空列表")
		}

		item := packageFileListItemByPackageID(list, packageID)
		if item == nil {
			t.Fatalf("正常返回当前用户空间模型文件列表: 未找到 package_id=%q 的夹具记录", packageID)
		}
		if item["package_name"] != modelName {
			t.Errorf("正常返回当前用户空间模型文件列表: 期望 package_name=%q，实际=%v", modelName, item["package_name"])
		}
		for _, field := range []string{"id", "package_id", "space_name", "package_name", "create_time", "update_time"} {
			if _, ok := item[field]; !ok {
				t.Errorf("正常返回当前用户空间模型文件列表: 缺少字段 %q", field)
			}
		}
		for _, field := range []string{"space_name", "create_time", "update_time"} {
			value, _ := item[field].(string)
			if strings.TrimSpace(value) == "" {
				t.Errorf("正常返回当前用户空间模型文件列表: 字段 %q 为空，实际=%v", field, item[field])
			}
		}
	})
}

// TestGetPackageFile 验证 POST /file/package/get。
// 覆盖：
//   - 正常获取用户模型文件下载信息
//   - 不存在的 package_id 返回 status=2
//   - 缺少 package_id 返回 HTTP 400
func TestGetPackageFile(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var packageID string
	var modelName string

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			{
				var err error
				packageID, modelName, err = modelhelpers.CreateTempPackageFixture(testClient, "TestResourcePackage")
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
			expectError        bool
			expectedCode       int
			expectedHTTPStatus int
			expectedBody       string
		}{
			{
				name:      "正常获取用户模型文件下载信息",
				packageID: packageID,
			},
			{
				name:         "不存在的 package_id 返回 status=2",
				packageID:    "non-existent-package-id",
				expectError:  true,
				expectedCode: 2,
			},
			{
				name:               "缺少 package_id 返回 HTTP 400",
				useRaw:             true,
				expectedHTTPStatus: http.StatusBadRequest,
				expectedBody:       "",
			},
		}

		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				if tt.useRaw {
					rawResp, err := testClient.PostRawResponse("/file/package/get", map[string]any{})
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

				resp, err := testClient.Primary().GetPackageFile(tt.packageID)
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
				url, _ := dataMap["url"].(string)
				if strings.TrimSpace(url) == "" {
					t.Fatalf("%s: 期望返回非空 url，实际=%v", tt.name, dataMap["url"])
				}
			})
		}
	})
}

// TestSaveModelCode 验证 POST /file/code/save。
// 覆盖：
//   - 正常保存用户模型代码
//   - 不存在的 package_id 返回 HTTP 400
//   - 缺少 package_id 返回 HTTP 400
func TestSaveModelCode(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var packageID string
	var modelName string

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			{
				var err error
				packageID, modelName, err = modelhelpers.CreateTempModelFixture(testClient, tempModelSourcePackageID, tempModelSourceModelName)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := modelhelpers.EnsureModelLoaded(testClient, packageID, modelName, "保存模型代码夹具"); err != nil {
				t.Fatal(err)
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
			expectedHTTPStatus int
			expectedBody       string
		}{
			{
				name:      "正常保存用户模型代码",
				packageID: packageID,
			},
			{
				name:               "不存在的 package_id 返回 HTTP 400",
				packageID:          "non-existent-package-id",
				useRaw:             true,
				expectedHTTPStatus: http.StatusBadRequest,
				expectedBody:       "not found",
			},
			{
				name:               "缺少 package_id 返回 HTTP 400",
				useRaw:             true,
				expectedHTTPStatus: http.StatusBadRequest,
				expectedBody:       "",
			},
		}

		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				if tt.useRaw {
					bodyMap := map[string]any{}
					if tt.packageID != "" {
						bodyMap["package_id"] = tt.packageID
					}
					rawResp, err := testClient.PostRawResponse("/file/code/save", bodyMap)
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

				resp, err := testClient.Primary().SaveModelCode(tt.packageID)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				assert.IsSuccess(resp, tt.name)
				if !strings.Contains(resp.Msg, "模型已保存") {
					t.Errorf("%s: 期望 msg 包含 模型已保存，实际=%q", tt.name, resp.Msg)
				}

				fileResp, err := testClient.GetPackageFile(packageID)
				if err != nil {
					t.Fatalf("%s: 回查文件下载信息失败: %v", tt.name, err)
				}
				assert.IsSuccess(fileResp, tt.name+": 回查文件下载信息")
				fileData, err := helpers.ObjectData(fileResp, tt.name+": 回查文件下载信息")
				if err != nil {
					t.Fatal(err)
				}
				url, _ := fileData["url"].(string)
				if strings.TrimSpace(url) == "" {
					t.Fatalf("%s: 回查文件下载信息 url 为空，实际=%v", tt.name, fileData["url"])
				}
			})
		}
	})
}

func updatedModelSource(modelName string) string {
	return fmt.Sprintf(`model %s
  Modelica.Blocks.Interfaces.RealInput u annotation(Placement(transformation(extent={{-120,-20},{-80,20}}), iconTransformation(extent={{-120,-20},{-80,20}})));
  Modelica.Blocks.Interfaces.RealOutput y annotation(Placement(transformation(extent={{80,-10},{100,10}}), iconTransformation(extent={{80,-10},{100,10}})));
  Modelica.Blocks.Continuous.PI PI(k=8, Ti=0.75) annotation(Placement(transformation(extent={{-20,-20},{20,20}})));
equation
  connect(u, PI.u) annotation(Line(points={{-100,0},{-20,0}}, color={0,0,127}));
  connect(PI.y, y) annotation(Line(points={{20,0},{90,0}}, color={0,0,127}));
annotation(
  Documentation(info="updated by TestUpdateModelPackage"),
  Icon(graphics={
    Rectangle(extent={{-100,-100},{100,100}}, lineColor={0,0,255}),
    Text(extent={{-100,60},{100,100}}, textString="%%name")
  })
);
end %s;`, modelName, modelName)
}

// TestSetModelDocument 验证 POST /model/document/set。
// 覆盖：
//   - 正常设置用户空间模型文档
//   - 系统包不允许设置文档，返回 status=2
//   - 缺少 document 返回 HTTP 400
func TestSetModelDocument(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var packageID string
	var modelName string
	var originalDocument string
	var originalRevisions string

	newDocument := "<html><p>test document</p></html>"
	newRevisions := "r1"

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			{
				var err error
				packageID, modelName, err = modelhelpers.CreateTempModelFixture(testClient, tempModelSourcePackageID, tempModelSourceModelName)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := modelhelpers.EnsureModelLoaded(testClient, packageID, modelName, "文档设置夹具"); err != nil {
				t.Fatal(err)
			}
			originalDocument, originalRevisions = getCurrentModelDocument(t, modelName)
		},
		After: func(t *testing.T) {
			if packageID != "" && modelName != "" {
				_, _ = testClient.Primary().SetModelDocument(packageID, modelName, originalDocument, originalRevisions)
				modelhelpers.CleanupTempModelFixture(testClient, packageID, modelName)
			}
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		tests := []struct {
			name               string
			useRaw             bool
			packageID          string
			modelName          string
			document           string
			revisions          string
			expectError        bool
			expectedCode       int
			expectedHTTPStatus int
		}{
			{
				name:      "正常设置用户空间模型文档",
				packageID: packageID,
				modelName: modelName,
				document:  newDocument,
				revisions: newRevisions,
			},
			{
				name:         "系统包不允许设置文档返回 status=2",
				packageID:    modelicaStdLibId,
				modelName:    "Modelica",
				document:     newDocument,
				revisions:    newRevisions,
				expectError:  true,
				expectedCode: 2,
			},
			{
				name:               "缺少 document 返回 HTTP 400",
				useRaw:             true,
				packageID:          packageID,
				modelName:          modelName,
				document:           "",
				revisions:          newRevisions,
				expectedHTTPStatus: 400,
			},
		}

		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				if tt.useRaw {
					rawResp, err := testClient.PostRawResponse("/model/document/set", map[string]any{
						"package_id": tt.packageID,
						"model_name": tt.modelName,
						"document":   tt.document,
						"revisions":  tt.revisions,
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

				resp, err := testClient.Primary().SetModelDocument(tt.packageID, tt.modelName, tt.document, tt.revisions)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				if tt.expectError {
					assert.IsError(resp, tt.expectedCode, tt.name)
					return
				}
				assert.IsSuccess(resp, tt.name)

				getResp, err := testClient.GetModelDocument(modelName)
				if err != nil {
					t.Fatalf("%s: 回读文档失败: %v", tt.name, err)
				}
				assert.IsSuccess(getResp, tt.name+": 回读")
				docData := extractModelDocumentData(t, getResp, tt.name+": 回读")
				documentStr, ok := docData["document"].(string)
				if !ok {
					t.Fatalf("%s: document 不是 string: %T", tt.name, docData["document"])
				}
				if !strings.Contains(documentStr, "test document") {
					t.Errorf("%s: 期望回读 document 包含 %q，实际=%q", tt.name, "test document", documentStr)
				}
				revisionsStr, ok := docData["revisions"].(string)
				if !ok {
					t.Fatalf("%s: revisions 不是 string: %T", tt.name, docData["revisions"])
				}
				if revisionsStr != "" && revisionsStr != newRevisions {
					t.Errorf("%s: 当前实现下 revisions 期望为空或等于 %q，实际=%q", tt.name, newRevisions, revisionsStr)
				}
			})
		}
	})
}

// TestLoadUserspaceModel 验证 POST /api/v2/model/userspace/model/load。
// 覆盖：
//   - 正常加载复制出的用户模型包
//   - 不存在的 package_id 返回 status=4
//   - 缺少 id 返回 HTTP 400
func TestLoadUserspaceModel(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var packageID string
	var modelName string

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			{
				var err error
				packageID, modelName, err = modelhelpers.CreateTempModelFixture(testClient, tempModelSourcePackageID, tempModelSourceModelName)
				if err != nil {
					t.Fatal(err)
				}
			}
			modelhelpers.TryUnloadPackage(testClient, packageID)
			_, _ = testClient.LoadstatusSet()
		},
		After: func(t *testing.T) {
			modelhelpers.CleanupTempModelFixture(testClient, packageID, modelName)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
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
				name:      "正常加载复制出的用户模型包",
				packageID: packageID,
				modelName: modelName,
			},
			{
				name:         "不存在的 package_id 返回 status=4",
				packageID:    "non-existent-package-id",
				modelName:    "Non.Existent.Package",
				expectError:  true,
				expectedCode: 4,
			},
			{
				name:               "缺少 id 返回 HTTP 400",
				useRaw:             true,
				packageID:          "",
				modelName:          modelName,
				expectedHTTPStatus: 400,
			},
		}

		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				if tt.useRaw {
					rawResp, err := testClient.PostRawResponse("/api/v2/model/userspace/model/load", map[string]any{
						"id":   tt.packageID,
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

				resp, err := testClient.Primary().LoadUserspaceModel(tt.packageID, tt.modelName)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				if tt.expectError {
					assert.IsError(resp, tt.expectedCode, tt.name)
					return
				}
				assert.IsSuccess(resp, tt.name)
				if !strings.Contains(resp.Msg, "加载成功") {
					t.Errorf("%s: 期望 msg 包含 加载成功，实际=%q", tt.name, resp.Msg)
				}
			})
		}
	})
}

// TestUnloadPackage 验证 POST /model/package/unload。
// 覆盖：
//   - 正常卸载复制出的用户模型包
//   - 不存在的 package_id 返回 HTTP 400
//   - 缺少 package_id 返回 HTTP 400
func TestUnloadPackage(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var packageID string
	var modelName string

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			{
				var err error
				packageID, modelName, err = modelhelpers.CreateTempModelFixture(testClient, tempModelSourcePackageID, tempModelSourceModelName)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := modelhelpers.EnsureModelLoaded(testClient, packageID, modelName, "卸载包夹具"); err != nil {
				t.Fatal(err)
			}
		},
		After: func(t *testing.T) {
			modelhelpers.CleanupTempModelFixture(testClient, packageID, modelName)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		tests := []struct {
			name               string
			useRaw             bool
			packageID          string
			expectedHTTPStatus int
		}{
			{
				name:      "正常卸载复制出的用户模型包",
				packageID: packageID,
			},
			{
				name:               "不存在的 package_id 返回 HTTP 400",
				useRaw:             true,
				packageID:          "non-existent-package-id",
				expectedHTTPStatus: 400,
			},
			{
				name:               "缺少 package_id 返回 HTTP 400",
				useRaw:             true,
				packageID:          "",
				expectedHTTPStatus: 400,
			},
		}

		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				if tt.useRaw {
					rawResp, err := testClient.PostRawResponse("/model/package/unload", map[string]any{
						"package_id": tt.packageID,
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

				resp, err := testClient.Primary().UnloadPackageByID(tt.packageID)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				assert.IsSuccess(resp, tt.name)
				if resp.Msg != "卸载成功" {
					t.Errorf("%s: 期望 msg=%q，实际=%q", tt.name, "卸载成功", resp.Msg)
				}
			})
		}
	})
}

// TestDeletePackage 验证 POST /model/package/delete。
// 覆盖：
//   - 正常删除复制出的用户模型包
//   - 不存在的 package_id 返回 HTTP 400
//   - 缺少 package_id 返回 HTTP 400
func TestDeletePackage(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var packageID string
	var modelName string
	var deleted bool

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			{
				var err error
				packageID, modelName, err = modelhelpers.CreateTempModelFixture(testClient, tempModelSourcePackageID, tempModelSourceModelName)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := modelhelpers.EnsureModelLoaded(testClient, packageID, modelName, "删除包夹具"); err != nil {
				t.Fatal(err)
			}
		},
		After: func(t *testing.T) {
			if !deleted {
				modelhelpers.CleanupTempModelFixture(testClient, packageID, modelName)
			}
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		tests := []struct {
			name               string
			useRaw             bool
			packageID          string
			modelName          string
			expectedHTTPStatus int
			assertBusiness     func(t *testing.T, resp *helpers.Response)
		}{
			{
				name:      "正常删除复制出的用户模型包",
				packageID: packageID,
				modelName: modelName,
				assertBusiness: func(t *testing.T, resp *helpers.Response) {
					assert.IsSuccess(resp, "正常删除复制出的用户模型包")
					deleted = true
				},
			},
			{
				name:               "不存在的 package_id 返回 HTTP 400",
				useRaw:             true,
				packageID:          "non-existent-package-id",
				modelName:          modelName,
				expectedHTTPStatus: 400,
			},
			{
				name:               "缺少 package_id 返回 HTTP 400",
				useRaw:             true,
				packageID:          "",
				modelName:          modelName,
				expectedHTTPStatus: 400,
			},
		}

		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				if tt.useRaw {
					rawResp, err := testClient.PostRawResponse("/model/package/delete", map[string]any{
						"package_id":  tt.packageID,
						"parent_name": "",
						"model_name":  tt.modelName,
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

				resp, err := testClient.Primary().DeletePackage(tt.packageID, "", tt.modelName)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				tt.assertBusiness(t, resp)
			})
		}
	})
}

// TestAddModelParameters 验证 POST /model/parameters/add。
// 覆盖：
//   - 正常给用户模型新增全局参数，并通过模型实例回读验证
//   - 系统包不允许新增参数，返回 status=2
//   - 缺少 var_type 返回 HTTP 400
func TestAddModelParameters(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var packageID string
	var modelName string
	var addedParameterName string

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			{
				var err error
				packageID, modelName, err = modelhelpers.CreateTempModelFixture(testClient, tempModelSourcePackageID, tempModelSourceModelName)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := modelhelpers.EnsureModelLoaded(testClient, packageID, modelName, "参数新增夹具"); err != nil {
				t.Fatal(err)
			}
		},
		After: func(t *testing.T) {
			if addedParameterName != "" {
				_, _ = testClient.DeleteModelParameters(packageID, modelName, addedParameterName)
			}
			modelhelpers.CleanupTempModelFixture(testClient, packageID, modelName)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		tests := []struct {
			name               string
			useRaw             bool
			packageID          string
			modelName          string
			parameterName      string
			varType            string
			expectError        bool
			expectedCode       int
			expectedHTTPStatus int
		}{
			{
				name:          "正常给用户模型新增全局参数",
				parameterName: uniqueName("addParam"),
				varType:       "Real",
			},
			{
				name:          "系统包不允许新增参数返回 status=2",
				packageID:     modelicaStdLibId,
				modelName:     tempModelSourceModelName,
				parameterName: uniqueName("sysParam"),
				varType:       "Real",
				expectError:   true,
				expectedCode:  2,
			},
			{
				name:               "缺少 var_type 返回 HTTP 400",
				useRaw:             true,
				parameterName:      uniqueName("rawParam"),
				expectedHTTPStatus: 400,
			},
		}

		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				targetPackageID := tt.packageID
				if targetPackageID == "" {
					targetPackageID = packageID
				}
				targetModelName := tt.modelName
				if targetModelName == "" {
					targetModelName = modelName
				}

				if tt.useRaw {
					rawResp, err := testClient.PostRawResponse("/model/parameters/add", map[string]any{
						"package_id":     targetPackageID,
						"model_name":     targetModelName,
						"parameter_name": tt.parameterName,
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

				resp, err := testClient.Primary().AddModelParameters(targetPackageID, targetModelName, tt.parameterName, tt.varType)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				if tt.expectError {
					assert.IsError(resp, tt.expectedCode, tt.name)
					return
				}
				assert.IsSuccess(resp, tt.name)
				addedParameterName = tt.parameterName
				assertModelParameterVisibleInInstance(t, targetPackageID, targetModelName, tt.parameterName, tt.name+": 实例回读")
			})
		}
	})
}

// TestDeleteModelParameters 验证 POST /model/parameters/delete。
// 覆盖：
//   - 正常删除已存在的用户模型全局参数，并通过模型实例回读验证
//   - 系统包不允许删除参数，返回 status=2
//   - 缺少 parameter_name 返回 HTTP 400
func TestDeleteModelParameters(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var packageID string
	var modelName string
	var parameterName string

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			{
				var err error
				packageID, modelName, err = modelhelpers.CreateTempModelFixture(testClient, tempModelSourcePackageID, tempModelSourceModelName)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := modelhelpers.EnsureModelLoaded(testClient, packageID, modelName, "参数删除夹具"); err != nil {
				t.Fatal(err)
			}
			parameterName = uniqueName("deleteParam")
			resp, err := testClient.AddModelParameters(packageID, modelName, parameterName, "Real")
			if err != nil {
				t.Fatalf("准备待删除参数失败: %v", err)
			}
			if resp.Status != 0 {
				t.Fatalf("准备待删除参数返回非成功状态: status=%d, msg=%s, err=%s", resp.Status, resp.Msg, resp.Err)
			}
			assertModelParameterVisibleInInstance(t, packageID, modelName, parameterName, "准备待删除参数")
		},
		After: func(t *testing.T) {
			modelhelpers.CleanupTempModelFixture(testClient, packageID, modelName)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		tests := []struct {
			name               string
			useRaw             bool
			packageID          string
			modelName          string
			parameterName      string
			expectError        bool
			expectedCode       int
			expectedHTTPStatus int
		}{
			{
				name:          "正常删除已存在的用户模型全局参数",
				parameterName: parameterName,
			},
			{
				name:          "系统包不允许删除参数返回 status=2",
				packageID:     modelicaStdLibId,
				modelName:     tempModelSourceModelName,
				parameterName: parameterName,
				expectError:   true,
				expectedCode:  2,
			},
			{
				name:               "缺少 parameter_name 返回 HTTP 400",
				useRaw:             true,
				expectedHTTPStatus: 400,
			},
		}

		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				targetPackageID := tt.packageID
				if targetPackageID == "" {
					targetPackageID = packageID
				}
				targetModelName := tt.modelName
				if targetModelName == "" {
					targetModelName = modelName
				}
				targetParameterName := tt.parameterName
				if targetParameterName == "" {
					targetParameterName = parameterName
				}

				if tt.useRaw {
					rawResp, err := testClient.PostRawResponse("/model/parameters/delete", map[string]any{
						"package_id": targetPackageID,
						"model_name": targetModelName,
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

				resp, err := testClient.Primary().DeleteModelParameters(targetPackageID, targetModelName, targetParameterName)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				if tt.expectError {
					assert.IsError(resp, tt.expectedCode, tt.name)
					return
				}
				assert.IsSuccess(resp, tt.name)
				assertModelParameterNotVisibleInInstance(t, targetPackageID, targetModelName, targetParameterName, tt.name+": 实例回读")
			})
		}
	})
}

// TestSetModelParameters 验证 POST /model/parameters/set。
// 覆盖：
//   - 正常批量设置模型参数，并通过模型实例回读 value
//   - 非 String 参数值含中文字符返回 status=2
//   - 系统库不允许修改返回 status=2
func TestSetModelParameters(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var packageID string
	var modelName string

	// parameter_value 在接口结构中就是 string；Real 参数传入 "7.5" 用于证明字符串承载的数值是合法值
	validParameters := []map[string]any{
		{
			"extend_name":         "Modelica.Units.SI.Real",
			"is_extend":           false,
			"element_name":        "PI",
			"parameter_name":      "k",
			"parameter_value":     "7.5",
			"redeclare":           false,
			"class_prefixes":      "Real",
			"replaced_class_type": "",
		},
		{
			"extend_name":         "Modelica.Units.SI.Time",
			"is_extend":           false,
			"element_name":        "PI",
			"parameter_name":      "Ti",
			"parameter_value":     "3",
			"redeclare":           false,
			"class_prefixes":      "Real",
			"replaced_class_type": "",
		},
	}

	// SetModelParametersView 只额外拦截非 String 类型中的中文字符，因此这里用中文触发业务校验失败。
	invalidParameters := []map[string]any{
		{
			"extend_name":         "Modelica.Units.SI.Real",
			"is_extend":           false,
			"element_name":        "PI",
			"parameter_name":      "k",
			"parameter_value":     "中文7.5",
			"redeclare":           false,
			"class_prefixes":      "Real",
			"replaced_class_type": "",
		},
	}

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			{
				var err error
				packageID, modelName, err = modelhelpers.CreateTempModelFixture(testClient, tempModelSourcePackageID, tempModelSourceModelName)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := modelhelpers.EnsureModelLoaded(testClient, packageID, modelName, "模型参数设置夹具"); err != nil {
				t.Fatal(err)
			}
		},
		After: func(t *testing.T) {
			modelhelpers.CleanupTempModelFixture(testClient, packageID, modelName)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		tests := []struct {
			name           string
			packageID      string
			modelName      string
			parameters     []map[string]any
			expectError    bool
			expectedCode   int
			assertBackfill func(t *testing.T)
		}{
			{
				name:       "正常批量设置模型参数",
				packageID:  packageID,
				modelName:  modelName,
				parameters: validParameters,
				assertBackfill: func(t *testing.T) {
					assertModelCodeContains(t, packageID, modelName, "k=7.5", "正常批量设置模型参数: k 源码回读")
					assertModelCodeContains(t, packageID, modelName, "Ti=3", "正常批量设置模型参数: Ti 源码回读")
				},
			},
			{
				name:         "非 String 参数值含中文字符返回 status=2",
				packageID:    packageID,
				modelName:    modelName,
				parameters:   invalidParameters,
				expectError:  true,
				expectedCode: 2,
			},
			{
				name:         "系统库不允许修改返回 status=2",
				packageID:    modelicaStdLibId,
				modelName:    tempModelSourceModelName,
				parameters:   validParameters,
				expectError:  true,
				expectedCode: 2,
			},
		}

		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				resp, err := testClient.Primary().SetModelParameters(tt.packageID, tt.modelName, tt.parameters)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				if tt.expectError {
					assert.IsError(resp, tt.expectedCode, tt.name)
					return
				}
				assert.IsSuccess(resp, tt.name)
				if tt.assertBackfill != nil {
					tt.assertBackfill(t)
				}
			})
		}
	})
}

// TestSetModelProperties 验证 POST /model/properties/set。
// 覆盖：
//   - 正常修改用户模型组件属性并重命名组件，通过模型实例回读验证
//   - 非法组件名返回 status=2
//   - 缺少 new_component_name 返回 HTTP 400
func TestSetModelProperties(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var packageID string
	var modelName string
	var currentComponentName string
	var commentText string

	newComponentName := uniqueName("RenamedGain")

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			{
				var err error
				packageID, modelName, err = modelhelpers.CreateTempModelFixture(testClient, tempModelSourcePackageID, tempModelSourceModelName)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := modelhelpers.EnsureModelLoaded(testClient, packageID, modelName, "模型属性夹具"); err != nil {
				t.Fatal(err)
			}
			currentComponentName = uniqueName("EditableGain")
			commentText = uniqueName("propComment")
			resp, err := testClient.AddModelComponent(packageID, modelName, []map[string]any{{
				"new_component_name": currentComponentName,
				"old_component_name": tempModelComponentClass,
				"origin":             "120,60",
				"extent":             []string{"-10,-10", "10,10"},
			}})
			if err != nil {
				t.Fatalf("准备待编辑组件失败: %v", err)
			}
			if resp.Status != 0 {
				t.Fatalf("准备待编辑组件返回非成功状态: status=%d, msg=%s, err=%s", resp.Status, resp.Msg, resp.Err)
			}
			assertComponentVisibleInInstance(t, packageID, modelName, currentComponentName, tempModelComponentClass, "准备待编辑组件")
		},
		After: func(t *testing.T) {
			modelhelpers.CleanupTempModelFixture(testClient, packageID, modelName)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		tests := []struct {
			name               string
			useRaw             bool
			oldComponentName   string
			newComponentName   string
			properties         map[string]any
			expectError        bool
			expectedCode       int
			expectedHTTPStatus int
		}{
			{
				name:             "正常修改用户模型组件属性并重命名",
				newComponentName: newComponentName,
				properties: map[string]any{
					"comment": commentText,
				},
			},
			{
				name:             "非法组件名返回 status=2",
				newComponentName: "1-invalid-name",
				expectError:      true,
				expectedCode:     2,
			},
			{
				name:               "缺少 new_component_name 返回 HTTP 400",
				useRaw:             true,
				expectedHTTPStatus: 400,
			},
		}

		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				oldComponentName := tt.oldComponentName
				if oldComponentName == "" {
					oldComponentName = currentComponentName
				}

				if tt.useRaw {
					rawResp, err := testClient.PostRawResponse("/model/properties/set", map[string]any{
						"package_id":         packageID,
						"model_name":         modelName,
						"old_component_name": oldComponentName,
						"comment":            commentText,
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

				resp, err := testClient.Primary().SetModelProperties(packageID, modelName, oldComponentName, tt.newComponentName, tt.properties)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				if tt.expectError {
					assert.IsError(resp, tt.expectedCode, tt.name)
					return
				}

				assert.IsSuccess(resp, tt.name)
				assertComponentNotExistsInInstance(t, packageID, modelName, oldComponentName, tt.name+": 旧组件回读")
				assertComponentVisibleInInstance(t, packageID, modelName, tt.newComponentName, tempModelComponentClass, tt.name+": 新组件回读")
				assertComponentParameterGroupInInstance(t, packageID, modelName, tt.newComponentName, commentText, tt.name+": 属性分组回读")
				currentComponentName = tt.newComponentName
			})
		}
	})
}

// TestAddModelComponent 验证 POST /api/v2/model/component/add。
// 覆盖：
//   - 正常给用户模型新增组件，并通过模型实例回读验证
//   - 系统包不允许新增组件，返回 status=2
//   - 缺少 component_list 返回 HTTP 400
func TestAddModelComponent(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var packageID string
	var modelName string
	var componentName string

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			{
				var err error
				packageID, modelName, err = modelhelpers.CreateTempModelFixture(testClient, tempModelSourcePackageID, tempModelSourceModelName)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := modelhelpers.EnsureModelLoaded(testClient, packageID, modelName, "组件新增夹具"); err != nil {
				t.Fatal(err)
			}
			componentName = uniqueName("GainAdd")
		},
		After: func(t *testing.T) {
			if componentName != "" {
				_, _ = testClient.DeleteModelComponent(packageID, modelName, []string{componentName})
			}
			modelhelpers.CleanupTempModelFixture(testClient, packageID, modelName)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		tests := []struct {
			name               string
			useRaw             bool
			packageID          string
			modelName          string
			componentName      string
			componentClassName string
			expectError        bool
			expectedCode       int
			expectedHTTPStatus int
		}{
			{
				name:               "正常给用户模型新增组件",
				componentName:      componentName,
				componentClassName: tempModelComponentClass,
			},
			{
				name:               "系统包不允许新增组件返回 status=2",
				packageID:          modelicaStdLibId,
				modelName:          tempModelSourceModelName,
				componentName:      uniqueName("SysGain"),
				componentClassName: tempModelComponentClass,
				expectError:        true,
				expectedCode:       2,
			},
			{
				name:               "缺少 component_list 返回 HTTP 400",
				useRaw:             true,
				expectedHTTPStatus: 400,
			},
		}

		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				targetPackageID := tt.packageID
				if targetPackageID == "" {
					targetPackageID = packageID
				}
				targetModelName := tt.modelName
				if targetModelName == "" {
					targetModelName = modelName
				}

				if tt.useRaw {
					rawResp, err := testClient.PostRawResponse("/api/v2/model/component/add", map[string]any{
						"package_id": targetPackageID,
						"model_name": targetModelName,
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

				resp, err := testClient.Primary().AddModelComponent(targetPackageID, targetModelName, []map[string]any{{
					"new_component_name": tt.componentName,
					"old_component_name": tt.componentClassName,
					"origin":             "120,60",
					"extent":             []string{"-10,-10", "10,10"},
				}})
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				if tt.expectError {
					assert.IsError(resp, tt.expectedCode, tt.name)
					return
				}

				assert.IsSuccess(resp, tt.name)
				assertComponentVisibleInInstance(t, targetPackageID, targetModelName, tt.componentName, tt.componentClassName, tt.name+": 实例回读")
			})
		}
	})
}

// TestDeleteModelComponent 验证 POST /model/component/delete。
// 覆盖：
//   - 正常删除已存在组件，并通过模型实例回读验证
//   - 系统包不允许删除组件，返回 status=2
//   - 缺少 component_list 返回 HTTP 400
func TestDeleteModelComponent(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var packageID string
	var modelName string
	var componentName string

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			{
				var err error
				packageID, modelName, err = modelhelpers.CreateTempModelFixture(testClient, tempModelSourcePackageID, tempModelSourceModelName)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := modelhelpers.EnsureModelLoaded(testClient, packageID, modelName, "组件删除夹具"); err != nil {
				t.Fatal(err)
			}
			componentName = uniqueName("GainDelete")
			resp, err := testClient.AddModelComponent(packageID, modelName, []map[string]any{{
				"new_component_name": componentName,
				"old_component_name": tempModelComponentClass,
				"origin":             "120,60",
				"extent":             []string{"-10,-10", "10,10"},
			}})
			if err != nil {
				t.Fatalf("准备待删除组件失败: %v", err)
			}
			if resp.Status != 0 {
				t.Fatalf("准备待删除组件返回非成功状态: status=%d, msg=%s, err=%s", resp.Status, resp.Msg, resp.Err)
			}
			assertComponentVisibleInInstance(t, packageID, modelName, componentName, tempModelComponentClass, "准备待删除组件")
		},
		After: func(t *testing.T) {
			modelhelpers.CleanupTempModelFixture(testClient, packageID, modelName)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		tests := []struct {
			name               string
			useRaw             bool
			packageID          string
			modelName          string
			componentList      []string
			expectError        bool
			expectedCode       int
			expectedHTTPStatus int
		}{
			{
				name:          "正常删除已存在组件",
				componentList: []string{componentName},
			},
			{
				name:          "系统包不允许删除组件返回 status=2",
				packageID:     modelicaStdLibId,
				modelName:     tempModelSourceModelName,
				componentList: []string{"PID"},
				expectError:   true,
				expectedCode:  2,
			},
			{
				name:               "缺少 component_list 返回 HTTP 400",
				useRaw:             true,
				expectedHTTPStatus: 400,
			},
		}

		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				targetPackageID := tt.packageID
				if targetPackageID == "" {
					targetPackageID = packageID
				}
				targetModelName := tt.modelName
				if targetModelName == "" {
					targetModelName = modelName
				}

				if tt.useRaw {
					rawResp, err := testClient.PostRawResponse("/model/component/delete", map[string]any{
						"package_id": targetPackageID,
						"model_name": targetModelName,
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

				resp, err := testClient.Primary().DeleteModelComponent(targetPackageID, targetModelName, tt.componentList)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				if tt.expectError {
					assert.IsError(resp, tt.expectedCode, tt.name)
					return
				}

				assert.IsSuccess(resp, tt.name)
				for _, name := range tt.componentList {
					assertComponentNotExistsInInstance(t, targetPackageID, targetModelName, name, tt.name+": 实例回读")
				}
			})
		}
	})
}

// TestUpdateModelComponent 验证 POST /model/component/update。
// 覆盖：
//   - 正常更新组件位置与旋转，并通过模型实例回读验证
//   - 系统包不允许更新组件，返回 status=2
//   - 缺少 component_list 返回 HTTP 400
func TestUpdateModelComponent(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var packageID string
	var modelName string
	var componentName string

	newOrigin := "{30,40}"
	newExtent := "{{-20,-20},{20,20}}"
	newRotation := "45"

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			{
				var err error
				packageID, modelName, err = modelhelpers.CreateTempModelFixture(testClient, tempModelSourcePackageID, tempModelSourceModelName)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := modelhelpers.EnsureModelLoaded(testClient, packageID, modelName, "组件更新夹具"); err != nil {
				t.Fatal(err)
			}
			componentName = uniqueName("GainUpdate")
			resp, err := testClient.AddModelComponent(packageID, modelName, []map[string]any{{
				"new_component_name": componentName,
				"old_component_name": tempModelComponentClass,
				"origin":             "120,60",
				"extent":             []string{"-10,-10", "10,10"},
			}})
			if err != nil {
				t.Fatalf("准备待更新组件失败: %v", err)
			}
			if resp.Status != 0 {
				t.Fatalf("准备待更新组件返回非成功状态: status=%d, msg=%s, err=%s", resp.Status, resp.Msg, resp.Err)
			}
			assertComponentVisibleInInstance(t, packageID, modelName, componentName, tempModelComponentClass, "准备待更新组件")
		},
		After: func(t *testing.T) {
			modelhelpers.CleanupTempModelFixture(testClient, packageID, modelName)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		tests := []struct {
			name               string
			useRaw             bool
			packageID          string
			modelName          string
			componentList      []map[string]any
			expectError        bool
			expectedCode       int
			expectedHTTPStatus int
		}{
			{
				name:      "正常更新组件位置与旋转",
				packageID: packageID,
				modelName: modelName,
				componentList: []map[string]any{{
					"component_name":       componentName,
					"component_class_name": tempModelComponentClass,
					"origin":               "30,40",
					"extent":               []string{"-20,-20", "20,20"},
					"rotation":             newRotation,
				}},
			},
			{
				name:      "系统包不允许更新组件返回 status=2",
				packageID: modelicaStdLibId,
				modelName: tempModelSourceModelName,
				componentList: []map[string]any{{
					"component_name":       "PID",
					"component_class_name": tempModelComponentClass,
					"origin":               "30,40",
					"extent":               []string{"-20,-20", "20,20"},
					"rotation":             newRotation,
				}},
				expectError:  true,
				expectedCode: 2,
			},
			{
				name:               "缺少 component_list 返回 HTTP 400",
				useRaw:             true,
				expectedHTTPStatus: 400,
			},
		}

		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				targetPackageID := tt.packageID
				if targetPackageID == "" {
					targetPackageID = packageID
				}
				targetModelName := tt.modelName
				if targetModelName == "" {
					targetModelName = modelName
				}

				if tt.useRaw {
					rawResp, err := testClient.PostRawResponse("/model/component/update", map[string]any{
						"package_id": targetPackageID,
						"model_name": targetModelName,
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

				resp, err := testClient.Primary().UpdateModelComponent(targetPackageID, targetModelName, tt.componentList)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				if tt.expectError {
					assert.IsError(resp, tt.expectedCode, tt.name)
					return
				}

				assert.IsSuccess(resp, tt.name)
				element := assertComponentVisibleInInstance(t, targetPackageID, targetModelName, componentName, tempModelComponentClass, tt.name+": 实例回读")
				assertStringFieldEquals(t, element, "origin", newOrigin, tt.name+": origin")
				assertStringFieldEquals(t, element, "extent", newExtent, tt.name+": extent")
				assertStringFieldEquals(t, element, "rotation", newRotation, tt.name+": rotation")
			})
		}
	})
}

// TestRenameModel 验证 POST /model/rename。
// 覆盖：
//   - 正常重命名用户模型，并通过用户模型树与实例接口回读验证
//   - 重命名为已存在名称返回 status=2
//   - 非法名称返回 status=2
//   - 缺少 new_name 返回 HTTP 400
func TestRenameModel(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var packageID string
	var currentModelName string
	var conflictPackageID string
	var conflictModelName string

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			{
				var err error
				packageID, currentModelName, err = modelhelpers.CreateTempModelFixture(testClient, tempModelSourcePackageID, tempModelSourceModelName)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := modelhelpers.EnsureModelLoaded(testClient, packageID, currentModelName, "模型重命名夹具"); err != nil {
				t.Fatal(err)
			}

			{
				var err error
				conflictPackageID, conflictModelName, err = modelhelpers.CreateTempModelFixture(testClient, tempModelSourcePackageID, tempModelSourceModelName)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := modelhelpers.EnsureModelLoaded(testClient, conflictPackageID, conflictModelName, "模型重命名冲突夹具"); err != nil {
				t.Fatal(err)
			}
		},
		After: func(t *testing.T) {
			modelhelpers.CleanupTempModelFixture(testClient, packageID, currentModelName)
			modelhelpers.CleanupTempModelFixture(testClient, conflictPackageID, conflictModelName)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		tests := []struct {
			name               string
			useRaw             bool
			newName            string
			expectError        bool
			expectedCode       int
			expectedHTTPStatus int
		}{
			{
				name:    "正常重命名用户模型",
				newName: modelhelpers.UniqueModelName("TestModel"),
			},
			{
				name:         "重命名为已存在名称返回 status=2",
				newName:      conflictModelName,
				expectError:  true,
				expectedCode: 2,
			},
			{
				name:         "非法名称返回 status=2",
				newName:      "1-invalid-name",
				expectError:  true,
				expectedCode: 2,
			},
			{
				name:               "缺少 new_name 返回 HTTP 400",
				useRaw:             true,
				expectedHTTPStatus: 400,
			},
		}

		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				oldModelName := currentModelName

				if tt.useRaw {
					rawResp, err := testClient.PostRawResponse("/model/rename", map[string]any{
						"package_id": packageID,
						"model_name": oldModelName,
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

				resp, err := testClient.Primary().RenameModel(packageID, oldModelName, tt.newName)
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
				if dataMap["id"] != packageID {
					t.Fatalf("%s: 期望返回 id=%q，实际=%v", tt.name, packageID, dataMap["id"])
				}
				nameVal, ok := dataMap["name"].(string)
				if !ok || nameVal != tt.newName {
					t.Fatalf("%s: 期望返回 name=%q，实际=%v", tt.name, tt.newName, dataMap["name"])
				}
				modelStr, ok := dataMap["model_str"].(string)
				if !ok || modelStr == "" {
					t.Fatalf("%s: 期望返回非空 model_str，实际=%v", tt.name, dataMap["model_str"])
				}

				assertCopiedModelVisibleInUserRoot(t, packageID, tt.newName, tt.name+": 用户树回读")
				assertModelNotVisibleInUserRoot(t, packageID, oldModelName, tt.name+": 旧名称用户树回读")
				_ = getCurrentModelInstanceModelData(t, packageID, tt.newName)
				currentModelName = tt.newName
			})
		}
	})
}

// TestCopyModelComponent 验证 POST /api/v2/model/component/copy。
// 覆盖：
//   - 正常在同一用户模型内复制组件，并通过实例接口回读验证
//   - 系统包不允许复制组件，返回 status=2
//   - 缺少 component_list 返回 HTTP 400
func TestCopyModelComponent(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var packageID string
	var modelName string
	var sourceComponentName string
	var sourceComponentClass string
	var copiedComponentName string

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			{
				var err error
				packageID, modelName, err = modelhelpers.CreateTempModelFixture(testClient, tempModelSourcePackageID, tempModelSourceModelName)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := modelhelpers.EnsureModelLoaded(testClient, packageID, modelName, "组件复制夹具"); err != nil {
				t.Fatal(err)
			}
			sourceComponentName, sourceComponentClass = getFirstCopyableComponentFromInstance(t, packageID, modelName)
			copiedComponentName = uniqueName("CopiedComp")
		},
		After: func(t *testing.T) {
			if copiedComponentName != "" {
				_, _ = testClient.DeleteModelComponent(packageID, modelName, []string{copiedComponentName})
			}
			modelhelpers.CleanupTempModelFixture(testClient, packageID, modelName)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		tests := []struct {
			name               string
			useRaw             bool
			packageID          string
			sourceModelName    string
			targetModelName    string
			componentList      []map[string]any
			expectError        bool
			expectedCode       int
			expectedHTTPStatus int
		}{
			{
				name:            "正常在同一用户模型内复制组件",
				packageID:       packageID,
				sourceModelName: modelName,
				targetModelName: modelName,
				componentList: []map[string]any{{
					"source_component_name": sourceComponentName,
					"target_component_name": copiedComponentName,
					"origin":                "180,90",
				}},
			},
			{
				name:            "系统包不允许复制组件返回 status=2",
				packageID:       modelicaStdLibId,
				sourceModelName: tempModelSourceModelName,
				targetModelName: tempModelSourceModelName,
				componentList: []map[string]any{{
					"source_component_name": "PI",
					"target_component_name": uniqueName("SysCopy"),
					"origin":                "180,90",
				}},
				expectError:  true,
				expectedCode: 2,
			},
			{
				name:               "缺少 component_list 返回 HTTP 400",
				useRaw:             true,
				packageID:          packageID,
				sourceModelName:    modelName,
				targetModelName:    modelName,
				expectedHTTPStatus: 400,
			},
		}

		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				if tt.useRaw {
					rawResp, err := testClient.PostRawResponse("/api/v2/model/component/copy", map[string]any{
						"package_id":        tt.packageID,
						"source_model_name": tt.sourceModelName,
						"target_model_name": tt.targetModelName,
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

				resp, err := testClient.Primary().CopyModelComponent(tt.packageID, tt.sourceModelName, tt.targetModelName, tt.componentList)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				if tt.expectError {
					assert.IsError(resp, tt.expectedCode, tt.name)
					return
				}

				assert.IsSuccess(resp, tt.name)
				element := assertComponentVisibleInInstance(t, tt.packageID, tt.targetModelName, copiedComponentName, sourceComponentClass, tt.name+": 实例回读")
				assertStringFieldEquals(t, element, "origin", "{180,90}", tt.name+": origin")
			})
		}
	})
}

// TestCreateConnection 验证 POST /model/connection/create。
// 覆盖：
//   - 正常创建组件连线，并通过模型实例回读验证
//   - 系统包不允许创建连线，返回 status=2
//   - 缺少 connect_list 返回 HTTP 400
func TestCreateConnection(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var packageID string
	var modelName string
	var leftComponent string
	var rightComponent string

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			packageID, modelName = createConnectionTestModel(t, 2)
			leftComponent = "ConnGainA"
			rightComponent = "ConnGainB"
		},
		After: func(t *testing.T) {
			modelhelpers.CleanupTempModelFixture(testClient, packageID, modelName)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		connectList := []map[string]any{buildSimpleConnectionPayload(leftComponent, rightComponent, "0,0,127", []string{"180,60", "220,60"})}
		tests := []struct {
			name               string
			useRaw             bool
			packageID          string
			modelName          string
			connectList        []map[string]any
			expectError        bool
			expectedCode       int
			expectedHTTPStatus int
		}{
			{
				name:        "正常创建组件连线",
				packageID:   packageID,
				modelName:   modelName,
				connectList: connectList,
			},
			{
				name:         "系统包不允许创建连线返回 status=2",
				packageID:    modelicaStdLibId,
				modelName:    tempModelSourceModelName,
				connectList:  connectList,
				expectError:  true,
				expectedCode: 2,
			},
			{
				name:               "缺少 connect_list 返回 HTTP 400",
				useRaw:             true,
				packageID:          packageID,
				modelName:          modelName,
				expectedHTTPStatus: 400,
			},
		}

		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				if tt.useRaw {
					rawResp, err := testClient.PostRawResponse("/model/connection/create", map[string]any{
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
						t.Fatalf("%s: 期望 HTTP %d，实际 %d，body=%q", tt.name, tt.expectedHTTPStatus, rawResp.StatusCode, string(body))
					}
					return
				}

				resp, err := testClient.Primary().CreateConnection(tt.packageID, tt.modelName, tt.connectList)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				if tt.expectError {
					assert.IsError(resp, tt.expectedCode, tt.name)
					return
				}

				assert.IsSuccess(resp, tt.name)
				conn := assertConnectionVisibleInInstance(t, tt.packageID, tt.modelName, []string{leftComponent, "y"}, []string{rightComponent, "u"}, tt.name+": 实例回读")
				assertStringFieldEquals(t, conn, "color", "{0,0,127}", tt.name+": color")
			})
		}
	})
}

// TestDeleteConnection 验证 POST /model/connection/delete。
// 覆盖：
//   - 正常删除已存在连线，并通过模型实例回读验证
//   - 系统包不允许删除连线，返回 status=2
//   - 缺少 connection_list 返回 HTTP 400
func TestDeleteConnection(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var packageID string
	var modelName string
	var leftComponent string
	var rightComponent string

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			packageID, modelName = createConnectionTestModel(t, 2)
			leftComponent = "ConnGainA"
			rightComponent = "ConnGainB"
			createDefaultConnectionForTest(t, packageID, modelName, leftComponent, rightComponent)
		},
		After: func(t *testing.T) {
			modelhelpers.CleanupTempModelFixture(testClient, packageID, modelName)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		connectionList := []map[string]any{buildConnectionListPayload(leftComponent, rightComponent)}
		tests := []struct {
			name               string
			useRaw             bool
			packageID          string
			modelName          string
			connectionList     []map[string]any
			expectError        bool
			expectedCode       int
			expectedHTTPStatus int
		}{
			{
				name:           "正常删除已存在连线",
				packageID:      packageID,
				modelName:      modelName,
				connectionList: connectionList,
			},
			{
				name:           "系统包不允许删除连线返回 status=2",
				packageID:      modelicaStdLibId,
				modelName:      tempModelSourceModelName,
				connectionList: connectionList,
				expectError:    true,
				expectedCode:   2,
			},
			{
				name:               "缺少 connection_list 返回 HTTP 400",
				useRaw:             true,
				packageID:          packageID,
				modelName:          modelName,
				expectedHTTPStatus: 400,
			},
		}

		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				if tt.useRaw {
					rawResp, err := testClient.PostRawResponse("/model/connection/delete", map[string]any{
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
						t.Fatalf("%s: 期望 HTTP %d，实际 %d，body=%q", tt.name, tt.expectedHTTPStatus, rawResp.StatusCode, string(body))
					}
					return
				}

				resp, err := testClient.Primary().DeleteConnection(tt.packageID, tt.modelName, tt.connectionList)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				if tt.expectError {
					assert.IsError(resp, tt.expectedCode, tt.name)
					return
				}

				assert.IsSuccess(resp, tt.name)
				assertConnectionNotExistsInInstance(t, tt.packageID, tt.modelName, []string{leftComponent, "y"}, []string{rightComponent, "u"}, tt.name+": 实例回读")
			})
		}
	})
}

// TestUpdateConnection 验证 POST /model/connection/update。
// 覆盖：
//   - 正常更新连线颜色与折点，并通过模型实例回读验证
//   - 系统包不允许更新连线，返回 status=2
//   - 缺少 connection_list 返回 HTTP 400
func TestUpdateConnection(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var packageID string
	var modelName string
	var leftComponent string
	var rightComponent string

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			packageID, modelName = createConnectionTestModel(t, 2)
			leftComponent = "ConnGainA"
			rightComponent = "ConnGainB"
			createDefaultConnectionForTest(t, packageID, modelName, leftComponent, rightComponent)
		},
		After: func(t *testing.T) {
			modelhelpers.CleanupTempModelFixture(testClient, packageID, modelName)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		connectionList := []map[string]any{{
			"connect_left":  []string{leftComponent, "y"},
			"connect_right": []string{rightComponent, "u"},
			"left_array":    []string{"", ""},
			"right_array":   []string{"", ""},
			"color":         "255,0,0",
			"line_points":   []string{"180,60", "200,80", "220,60"},
		}}
		tests := []struct {
			name               string
			useRaw             bool
			packageID          string
			modelName          string
			connectionList     []map[string]any
			expectError        bool
			expectedCode       int
			expectedHTTPStatus int
		}{
			{
				name:           "正常更新连线颜色与折点",
				packageID:      packageID,
				modelName:      modelName,
				connectionList: connectionList,
			},
			{
				name:           "系统包不允许更新连线返回 status=2",
				packageID:      modelicaStdLibId,
				modelName:      tempModelSourceModelName,
				connectionList: connectionList,
				expectError:    true,
				expectedCode:   2,
			},
			{
				name:               "缺少 connection_list 返回 HTTP 400",
				useRaw:             true,
				packageID:          packageID,
				modelName:          modelName,
				expectedHTTPStatus: 400,
			},
		}

		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				if tt.useRaw {
					rawResp, err := testClient.PostRawResponse("/model/connection/update", map[string]any{
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
						t.Fatalf("%s: 期望 HTTP %d，实际 %d，body=%q", tt.name, tt.expectedHTTPStatus, rawResp.StatusCode, string(body))
					}
					return
				}

				resp, err := testClient.Primary().UpdateConnection(tt.packageID, tt.modelName, tt.connectionList)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				if tt.expectError {
					assert.IsError(resp, tt.expectedCode, tt.name)
					return
				}

				assert.IsSuccess(resp, tt.name)
				conn := assertConnectionVisibleInInstance(t, tt.packageID, tt.modelName, []string{leftComponent, "y"}, []string{rightComponent, "u"}, tt.name+": 实例回读")
				assertStringFieldEquals(t, conn, "color", "{255,0,0}", tt.name+": color")
				assertStringFieldEquals(t, conn, "points", "{{180,60},{200,80},{220,60}}", tt.name+": points")
			})
		}
	})
}

// TestUpdateConnectionName 验证 POST /model/connection/name。
// 覆盖：
//   - 正常更新连线端点名称，并通过模型实例回读验证
//   - 系统包不允许更新连线名称，返回 status=2
//   - 缺少 connection_list 返回 HTTP 400
func TestUpdateConnectionName(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var packageID string
	var modelName string
	var leftComponent string
	var rightComponent string
	var replacementComponent string

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			packageID, modelName = createConnectionTestModel(t, 3)
			leftComponent = "ConnGainA"
			rightComponent = "ConnGainB"
			replacementComponent = "ConnGainC"
			createDefaultConnectionForTest(t, packageID, modelName, leftComponent, rightComponent)
		},
		After: func(t *testing.T) {
			modelhelpers.CleanupTempModelFixture(testClient, packageID, modelName)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		connectionList := []map[string]any{{
			"connect_left":      []string{leftComponent, "y"},
			"connect_right":     []string{rightComponent, "u"},
			"new_connect_left":  []string{replacementComponent, "y"},
			"new_connect_right": []string{rightComponent, "u"},
			"left_array":        []string{"", ""},
			"right_array":       []string{"", ""},
			"new_left_array":    []string{"", ""},
			"new_right_array":   []string{"", ""},
		}}
		tests := []struct {
			name               string
			useRaw             bool
			packageID          string
			modelName          string
			connectionList     []map[string]any
			expectError        bool
			expectedCode       int
			expectedHTTPStatus int
		}{
			{
				name:           "正常更新连线端点名称",
				packageID:      packageID,
				modelName:      modelName,
				connectionList: connectionList,
			},
			{
				name:           "系统包不允许更新连线名称返回 status=2",
				packageID:      modelicaStdLibId,
				modelName:      tempModelSourceModelName,
				connectionList: connectionList,
				expectError:    true,
				expectedCode:   2,
			},
			{
				name:               "缺少 connection_list 返回 HTTP 400",
				useRaw:             true,
				packageID:          packageID,
				modelName:          modelName,
				expectedHTTPStatus: 400,
			},
		}

		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				if tt.useRaw {
					rawResp, err := testClient.PostRawResponse("/model/connection/name", map[string]any{
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
						t.Fatalf("%s: 期望 HTTP %d，实际 %d，body=%q", tt.name, tt.expectedHTTPStatus, rawResp.StatusCode, string(body))
					}
					return
				}

				resp, err := testClient.Primary().UpdateConnectionName(tt.packageID, tt.modelName, tt.connectionList)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				if tt.expectError {
					assert.IsError(resp, tt.expectedCode, tt.name)
					return
				}

				assert.IsSuccess(resp, tt.name)
				assertConnectionNotExistsInInstance(t, tt.packageID, tt.modelName, []string{leftComponent, "y"}, []string{rightComponent, "u"}, tt.name+": 旧连线回读")
				assertConnectionVisibleInInstance(t, tt.packageID, tt.modelName, []string{replacementComponent, "y"}, []string{rightComponent, "u"}, tt.name+": 新连线回读")
			})
		}
	})
}

func runWithDragMoveLibFixture(t *testing.T, libraryName, fileName string, run func(t *testing.T, packageID string)) {
	t.Helper()
	var packageID string
	var modelName string
	helpers.RunWithHooks(t, helpers.TestHooks{
		Before: func(t *testing.T) {
			packageID, modelName = createDragMoveModelLibraryFixture(t, libraryName, fileName)
		},
		After: func(t *testing.T) {
			modelhelpers.CleanupTempModelFixture(testClient, packageID, modelName)
		},
	}, func(t *testing.T) {
		run(t, packageID)
	})
}

func runWithDragMoveRootAndLibFixtures(t *testing.T, sourceLibraryName, sourceFileName, targetLibraryName, targetFileName string, run func(t *testing.T, sourcePackageID, targetPackageID string)) {
	t.Helper()
	var sourcePackageID string
	var sourceModelName string
	var targetPackageID string
	var targetModelName string
	helpers.RunWithHooks(t, helpers.TestHooks{
		Before: func(t *testing.T) {
			targetPackageID, targetModelName = createDragMoveModelLibraryFixture(t, targetLibraryName, targetFileName)
			sourcePackageID, sourceModelName = createDragMoveModelLibraryFixture(t, sourceLibraryName, sourceFileName)
		},
		After: func(t *testing.T) {
			modelhelpers.CleanupTempModelFixture(testClient, sourcePackageID, sourceModelName)
			modelhelpers.CleanupTempModelFixture(testClient, targetPackageID, targetModelName)
		},
	}, func(t *testing.T) {
		run(t, sourcePackageID, targetPackageID)
	})
}

func createDragMoveModelLibraryFixture(t *testing.T, libraryName, fileName string) (string, string) {
	t.Helper()
	content, err := modelhelpers.ReadModelTestResource("MoveClassToPackage", fileName)
	if err != nil {
		t.Fatal(err)
	}
	packageID, modelName, err := modelhelpers.ImportModelLibraryFixture(testClient, libraryName, fileName, content)
	if err != nil {
		t.Fatal(err)
	}
	return packageID, modelName
}

// createMoveClassFixture 上传并加载移动排序接口专用模型库。
func createMoveClassFixture(t *testing.T, packageName, fileName string) string {
	t.Helper()
	if stalePackageID := findPackageIDByName(t, packageName, "清理移动排序测试模型残留"); stalePackageID != "" {
		modelhelpers.CleanupTempModelFixture(testClient, stalePackageID, packageName)
	}

	modelContent := readMoveClassModelResource(t, fileName)
	createResp, err := testClient.UploadModelPackage(fileName, modelContent)
	if err != nil {
		t.Fatalf("上传移动排序测试模型失败: %v", err)
	}
	if createResp.Status != 0 {
		t.Fatalf("上传移动排序测试模型返回非成功状态: status=%d, msg=%s, err=%s", createResp.Status, createResp.Msg, createResp.Err)
	}

	packageID := findPackageIDByName(t, packageName, "查找移动排序测试模型")
	if strings.TrimSpace(packageID) == "" {
		t.Fatalf("上传移动排序测试模型后未找到 package_id")
	}

	if err := modelhelpers.EnsureModelLoaded(testClient, packageID, packageName, "加载移动排序测试模型"); err != nil {
		t.Fatal(err)
	}
	return packageID
}

// readMoveClassModelResource 读取移动排序接口测试使用的固定模型库文件。
func readMoveClassModelResource(t *testing.T, fileName string) []byte {
	t.Helper()
	content, err := os.ReadFile(moveClassResourcePath(t, fileName))
	if err != nil {
		t.Fatalf("读取移动排序测试模型失败: %v", err)
	}
	return content
}

// moveClassResourcePath 返回移动排序接口测试资源文件的绝对路径。
// 通过 runtime.Caller 定位当前测试文件，避免 go test 工作目录变化导致相对路径失效。
func moveClassResourcePath(t *testing.T, fileName string) string {
	t.Helper()
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatalf("获取当前测试文件路径失败")
	}
	return filepath.Join(filepath.Dir(currentFile), "..", "resources", "MoveClass", fileName)
}

// assertMoveClassData 校验移动排序接口成功响应中的关键回显字段。
func assertMoveClassData(t *testing.T, resp *helpers.Response, packageID, modelName string, targetIndex int, message string) {
	t.Helper()
	dataMap, err := helpers.ObjectData(resp, message)
	if err != nil {
		t.Fatal(err)
	}
	actualPackageID, _ := dataMap["package_id"].(string)
	if actualPackageID != packageID {
		t.Fatalf("%s: 期望 package_id=%q，实际=%q", message, packageID, actualPackageID)
	}
	actualModelName, _ := dataMap["model_name"].(string)
	if actualModelName != modelName {
		t.Fatalf("%s: 期望 model_name=%q，实际=%q", message, modelName, actualModelName)
	}

	var actualTargetIndex int
	switch value := dataMap["target_index"].(type) {
	case float64:
		actualTargetIndex = int(value)
	case int:
		actualTargetIndex = value
	default:
		t.Fatalf("%s: target_index 类型错误: %T", message, dataMap["target_index"])
	}
	if actualTargetIndex != targetIndex {
		t.Fatalf("%s: 期望 target_index=%d，实际=%d", message, targetIndex, actualTargetIndex)
	}
}

func assertMoveClassToPackageData(t *testing.T, resp *helpers.Response, oldClassName, newClassName, message string) {
	t.Helper()
	dataMap, err := helpers.ObjectData(resp, message)
	if err != nil {
		t.Fatal(err)
	}
	if dataMap["old_class_name"] != oldClassName {
		t.Errorf("%s: 期望 old_class_name=%q，实际=%v", message, oldClassName, dataMap["old_class_name"])
	}
	if dataMap["new_class_name"] != newClassName {
		t.Errorf("%s: 期望 new_class_name=%q，实际=%v", message, newClassName, dataMap["new_class_name"])
	}
}

// assertModelChildOrder 回查指定父模型的子节点顺序。
func assertModelChildOrder(t *testing.T, packageID, parentModelName string, expected []string, message string) {
	t.Helper()
	resp, err := testClient.GetListModel(packageID, parentModelName)
	if err != nil {
		t.Fatalf("%s: 回查模型子节点失败: %v", message, err)
	}
	if resp.Status != 0 {
		t.Fatalf("%s: 回查模型子节点返回非成功状态: status=%d, msg=%s, err=%s", message, resp.Status, resp.Msg, resp.Err)
	}
	actual := modelChildShortNames(t, resp, message)
	if !reflect.DeepEqual(actual, expected) {
		t.Fatalf("%s: 子节点顺序不符合预期，期望=%v，实际=%v", message, expected, actual)
	}
}

func assertModelChildrenContain(t *testing.T, packageID, parentModelName, expectedChildName, message string) {
	t.Helper()
	names := getModelChildShortNames(t, packageID, parentModelName, message)
	for _, name := range names {
		if name == expectedChildName {
			return
		}
	}
	t.Fatalf("%s: 子节点中未找到 %q，actual=%v", message, expectedChildName, names)
}

func assertModelChildrenNotContain(t *testing.T, packageID, parentModelName, unexpectedChildName, message string) {
	t.Helper()
	names := getModelChildShortNames(t, packageID, parentModelName, message)
	for _, name := range names {
		if name == unexpectedChildName {
			t.Fatalf("%s: 子节点仍包含 %q，actual=%v", message, unexpectedChildName, names)
		}
	}
}

func getModelChildShortNames(t *testing.T, packageID, parentModelName, message string) []string {
	t.Helper()
	resp, err := testClient.GetListModel(packageID, parentModelName)
	if err != nil {
		t.Fatalf("%s: 回查模型子节点失败: %v", message, err)
	}
	if resp.Status != 0 {
		t.Fatalf("%s: 回查模型子节点返回非成功状态: status=%d, msg=%s, err=%s", message, resp.Status, resp.Msg, resp.Err)
	}
	return modelChildShortNames(t, resp, message)
}

// modelChildShortNames 从 /model/list_library 响应中提取子节点短名称。
func modelChildShortNames(t *testing.T, resp *helpers.Response, message string) []string {
	t.Helper()
	items, err := helpers.ArrayData(resp, message)
	if err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(items))
	for _, item := range items {
		node, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("%s: 子节点不是 object: %T", message, item)
		}
		name := modelNodeShortName(t, node, message)
		nodeType, _ := node["type"].(string)
		if name == "Resources" && nodeType == "static" {
			continue
		}
		names = append(names, name)
	}
	return names
}

// modelNodeShortName 从模型树节点中提取用于顺序比较的短名称。
func modelNodeShortName(t *testing.T, node map[string]any, message string) string {
	t.Helper()
	for _, field := range []string{"name", "model_name", "class_name", "full_name"} {
		value, _ := node[field].(string)
		if strings.TrimSpace(value) != "" {
			return shortModelName(value)
		}
	}
	t.Fatalf("%s: 子节点缺少可识别名称字段: %v", message, node)
	return ""
}

// shortModelName 返回完整模型名的最后一段。
func shortModelName(modelName string) string {
	modelName = strings.TrimSpace(modelName)
	if idx := strings.LastIndex(modelName, "."); idx >= 0 {
		return modelName[idx+1:]
	}
	return modelName
}

func getCurrentModelDocument(t *testing.T, modelName string) (string, string) {
	t.Helper()
	resp, err := testClient.GetModelDocument(modelName)
	if err != nil {
		t.Fatalf("获取当前模型文档失败: %v", err)
	}
	if resp.Status != 0 {
		t.Fatalf("获取当前模型文档返回非成功状态: status=%d, msg=%s, err=%s", resp.Status, resp.Msg, resp.Err)
	}
	dataMap := extractModelDocumentData(t, resp, "获取当前模型文档")
	document, _ := dataMap["document"].(string)
	revisions, _ := dataMap["revisions"].(string)
	return document, revisions
}

func getCurrentModelInstanceModelData(t *testing.T, packageID, modelName string) map[string]any {
	t.Helper()
	resp, err := testClient.GetModelInstance(packageID, modelName, "")
	if err != nil {
		t.Fatalf("获取当前模型实例失败: %v", err)
	}
	if resp.Status != 0 {
		t.Fatalf("获取当前模型实例返回非成功状态: status=%d, msg=%s, err=%s", resp.Status, resp.Msg, resp.Err)
	}
	dataMap, err := helpers.ObjectData(resp, "获取当前模型实例")
	if err != nil {
		t.Fatal(err)
	}
	modelData, ok := dataMap["model"].(map[string]any)
	if !ok {
		t.Fatalf("模型实例 data.model 不是 object: %T", dataMap["model"])
	}
	return modelData
}

func getCurrentModelIconData(t *testing.T, packageID, modelName string) map[string]any {
	t.Helper()
	resp, err := testClient.GetIconGraphics(packageID, modelName, "")
	if err != nil {
		t.Fatalf("获取当前模型图标失败: %v", err)
	}
	if resp.Status != 0 {
		t.Fatalf("获取当前模型图标返回非成功状态: status=%d, msg=%s, err=%s", resp.Status, resp.Msg, resp.Err)
	}
	dataMap, err := helpers.ObjectData(resp, "获取当前模型图标")
	if err != nil {
		t.Fatal(err)
	}
	graphics, ok := dataMap["graphics"].(map[string]any)
	if !ok {
		t.Fatalf("模型图标 data.graphics 不是 object: %T", dataMap["graphics"])
	}
	iconData, ok := graphics["icon"].(map[string]any)
	if !ok {
		t.Fatalf("模型图标 data.graphics.icon 不是 object: %T", graphics["icon"])
	}
	return iconData
}

func extractModelIconSubShapes(t *testing.T, iconData map[string]any) []map[string]any {
	t.Helper()
	list, ok := iconData["subShapes"].([]any)
	if !ok {
		t.Fatalf("模型图标 subShapes 不是数组: %T", iconData["subShapes"])
	}
	result := make([]map[string]any, 0, len(list))
	for _, item := range list {
		dataMap, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("模型图标图形项不是 object: %T", item)
		}
		result = append(result, dataMap)
	}
	return result
}

func extractModelIconConnectors(t *testing.T, iconData map[string]any) []map[string]any {
	t.Helper()
	list, ok := iconData["connectors"].([]any)
	if !ok {
		t.Fatalf("模型图标 connectors 不是数组: %T", iconData["connectors"])
	}
	result := make([]map[string]any, 0, len(list))
	for _, item := range list {
		dataMap, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("模型图标端口项不是 object: %T", item)
		}
		result = append(result, dataMap)
	}
	return result
}

func extractModelInstanceElements(t *testing.T, modelData map[string]any) []map[string]any {
	t.Helper()
	graphics, ok := modelData["graphics"].(map[string]any)
	if !ok {
		t.Fatalf("模型实例 graphics 不是 object: %T", modelData["graphics"])
	}
	elements, ok := graphics["elements"].(map[string]any)
	if !ok {
		t.Fatalf("模型实例 graphics.elements 不是 object: %T", graphics["elements"])
	}
	list, ok := elements["model"].([]any)
	if !ok {
		t.Fatalf("模型实例 graphics.elements.model 不是数组: %T", elements["model"])
	}
	components := make([]map[string]any, 0, len(list))
	for _, item := range list {
		dataMap, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("模型实例元素项不是 object: %T", item)
		}
		components = append(components, dataMap)
	}
	return components
}

func extractModelInstanceParameterGroups(t *testing.T, modelData map[string]any) []map[string]any {
	t.Helper()
	list, ok := modelData["parameters"].([]any)
	if !ok {
		t.Fatalf("模型实例 parameters 不是数组: %T", modelData["parameters"])
	}
	groups := make([]map[string]any, 0, len(list))
	for _, item := range list {
		dataMap, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("模型实例参数分组不是 object: %T", item)
		}
		groups = append(groups, dataMap)
	}
	return groups
}

func extractModelInstanceConnections(t *testing.T, modelData map[string]any) []map[string]any {
	t.Helper()
	graphics, ok := modelData["graphics"].(map[string]any)
	if !ok {
		t.Fatalf("模型实例 graphics 不是 object: %T", modelData["graphics"])
	}
	connections, ok := graphics["connections"].(map[string]any)
	if !ok {
		t.Fatalf("模型实例 graphics.connections 不是 object: %T", graphics["connections"])
	}
	list, ok := connections["model"].([]any)
	if !ok {
		t.Fatalf("模型实例 graphics.connections.model 不是数组: %T", connections["model"])
	}
	result := make([]map[string]any, 0, len(list))
	for _, item := range list {
		dataMap, ok := item.(map[string]any)
		if !ok {
			t.Fatalf("模型实例连线项不是 object: %T", item)
		}
		result = append(result, dataMap)
	}
	return result
}

func assertComponentVisibleInInstance(t *testing.T, packageID, modelName, componentName, componentClassName, message string) map[string]any {
	t.Helper()
	modelData := getCurrentModelInstanceModelData(t, packageID, modelName)
	components := extractModelInstanceElements(t, modelData)
	for _, item := range components {
		if item["name"] == componentName {
			if componentClassName != "" && item["classname"] != componentClassName {
				t.Fatalf("%s: 组件 %q 类型不匹配，期望=%q，实际=%v", message, componentName, componentClassName, item["classname"])
			}
			return item
		}
	}
	t.Fatalf("%s: 未找到组件 %q", message, componentName)
	return nil
}

func assertConnectionVisibleInInstance(t *testing.T, packageID, modelName string, lhs, rhs []string, message string) map[string]any {
	t.Helper()
	modelData := getCurrentModelInstanceModelData(t, packageID, modelName)
	connections := extractModelInstanceConnections(t, modelData)
	for _, item := range connections {
		left, ok := toStringSlice(item["lhs"])
		if !ok {
			continue
		}
		right, ok := toStringSlice(item["rhs"])
		if !ok {
			continue
		}
		if stringSliceEquals(left, lhs) && stringSliceEquals(right, rhs) {
			return item
		}
	}
	t.Fatalf("%s: 未找到连线 lhs=%v rhs=%v", message, lhs, rhs)
	return nil
}

func assertConnectionNotExistsInInstance(t *testing.T, packageID, modelName string, lhs, rhs []string, message string) {
	t.Helper()
	modelData := getCurrentModelInstanceModelData(t, packageID, modelName)
	connections := extractModelInstanceConnections(t, modelData)
	for _, item := range connections {
		left, ok := toStringSlice(item["lhs"])
		if !ok {
			continue
		}
		right, ok := toStringSlice(item["rhs"])
		if !ok {
			continue
		}
		if stringSliceEquals(left, lhs) && stringSliceEquals(right, rhs) {
			t.Fatalf("%s: 连线仍存在 lhs=%v rhs=%v item=%v", message, lhs, rhs, item)
		}
	}
}

func assertIconShapeVisibleInGraphics(t *testing.T, packageID, modelName string, predicate func(map[string]any) bool, message string) map[string]any {
	t.Helper()
	iconData := getCurrentModelIconData(t, packageID, modelName)
	shapes := extractModelIconSubShapes(t, iconData)
	for _, item := range shapes {
		if predicate(item) {
			return item
		}
	}
	t.Fatalf("%s: 未找到符合条件的图标图形", message)
	return nil
}

func assertIconConnectorVisibleInGraphics(t *testing.T, packageID, modelName, connectorName, message string) map[string]any {
	t.Helper()
	iconData := getCurrentModelIconData(t, packageID, modelName)
	connectors := extractModelIconConnectors(t, iconData)
	for _, item := range connectors {
		name, _ := item["name"].(string)
		if name == connectorName {
			return item
		}
	}
	t.Fatalf("%s: 未找到图标端口 %q", message, connectorName)
	return nil
}

func assertComponentNotExistsInInstance(t *testing.T, packageID, modelName, componentName, message string) {
	t.Helper()
	modelData := getCurrentModelInstanceModelData(t, packageID, modelName)
	components := extractModelInstanceElements(t, modelData)
	for _, item := range components {
		if item["name"] == componentName {
			t.Fatalf("%s: 组件 %q 仍存在，item=%v", message, componentName, item)
		}
	}
}

func assertModelParameterVisibleInInstance(t *testing.T, packageID, modelName, parameterName, message string) {
	t.Helper()
	modelData := getCurrentModelInstanceModelData(t, packageID, modelName)
	groups := extractModelInstanceParameterGroups(t, modelData)
	for _, group := range groups {
		if group["type"] == "model" && group["name"] == parameterName {
			return
		}
		params, ok := group["parameter"].([]any)
		if !ok {
			continue
		}
		for _, item := range params {
			param, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if param["name"] == parameterName {
				return
			}
		}
	}
	t.Fatalf("%s: 未在模型实例参数中找到 %q", message, parameterName)
}

func assertModelParameterNotVisibleInInstance(t *testing.T, packageID, modelName, parameterName, message string) {
	t.Helper()
	modelData := getCurrentModelInstanceModelData(t, packageID, modelName)
	groups := extractModelInstanceParameterGroups(t, modelData)
	for _, group := range groups {
		if group["type"] == "model" && group["name"] == parameterName {
			t.Fatalf("%s: 参数分组 %q 仍存在", message, parameterName)
		}
		params, ok := group["parameter"].([]any)
		if !ok {
			continue
		}
		for _, item := range params {
			param, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if param["name"] == parameterName {
				t.Fatalf("%s: 参数 %q 仍存在于实例参数列表中", message, parameterName)
			}
		}
	}
}

func assertModelParameterValueInInstance(t *testing.T, packageID, modelName, groupName, parameterName, wantValue, message string) {
	t.Helper()
	modelData := getCurrentModelInstanceModelData(t, packageID, modelName)
	groups := extractModelInstanceParameterGroups(t, modelData)
	for _, group := range groups {
		if group["name"] != groupName {
			continue
		}
		params, ok := group["parameter"].([]any)
		if !ok {
			t.Fatalf("%s: 参数组 %q 的 parameter 不是数组: %T", message, groupName, group["parameter"])
		}
		for _, item := range params {
			param, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if param["name"] == parameterName {
				if param["value"] != wantValue {
					t.Fatalf("%s: 期望 %s.%s=%q，实际=%v", message, groupName, parameterName, wantValue, param["value"])
				}
				return
			}
		}
	}
	t.Fatalf("%s: 未在参数组 %q 中找到参数 %q", message, groupName, parameterName)
}

func assertComponentParameterGroupInInstance(t *testing.T, packageID, modelName, componentName, commentText, message string) {
	t.Helper()
	modelData := getCurrentModelInstanceModelData(t, packageID, modelName)
	groups := extractModelInstanceParameterGroups(t, modelData)
	for _, group := range groups {
		if group["type"] != "component" || group["name"] != componentName {
			continue
		}
		properties, ok := group["properties"].(map[string]any)
		if !ok {
			t.Fatalf("%s: 组件参数分组 properties 不是 object: %T", message, group["properties"])
		}
		assertStringFieldEquals(t, properties, "comment", commentText, message+": comment")
		return
	}
	t.Fatalf("%s: 未找到组件参数分组 %q", message, componentName)
}

func assertStringFieldEquals(t *testing.T, dataMap map[string]any, field, want, message string) {
	t.Helper()
	got, ok := dataMap[field].(string)
	if !ok {
		t.Fatalf("%s: 字段 %q 不是 string: %T", message, field, dataMap[field])
	}
	if got != want {
		t.Fatalf("%s: 字段 %q 期望=%q，实际=%q", message, field, want, got)
	}
}

func createConnectionTestModel(t *testing.T, componentCount int) (string, string) {
	t.Helper()
	packageID, modelName, err := modelhelpers.CreateTempModelFixture(testClient, tempModelSourcePackageID, tempModelSourceModelName)
	if err != nil {
		t.Fatal(err)
	}
	if err := modelhelpers.EnsureModelLoaded(testClient, packageID, modelName, "连线测试准备数据"); err != nil {
		t.Fatal(err)
	}

	componentNames := []string{"ConnGainA", "ConnGainB", "ConnGainC"}
	componentList := make([]map[string]any, 0, componentCount)
	for i := 0; i < componentCount; i++ {
		componentList = append(componentList, map[string]any{
			"new_component_name": componentNames[i],
			"old_component_name": tempModelComponentClass,
			"origin":             []string{"120,60", "220,60", "320,60"}[i],
			"extent":             []string{"-10,-10", "10,10"},
		})
	}
	resp, err := testClient.AddModelComponent(packageID, modelName, componentList)
	if err != nil {
		t.Fatalf("准备连线测试组件失败: %v", err)
	}
	if resp.Status != 0 {
		t.Fatalf("准备连线测试组件返回非成功状态: status=%d, msg=%s, err=%s", resp.Status, resp.Msg, resp.Err)
	}
	for i := 0; i < componentCount; i++ {
		assertComponentVisibleInInstance(t, packageID, modelName, componentNames[i], tempModelComponentClass, "准备连线测试组件")
	}
	return packageID, modelName
}

func buildSimpleConnectionPayload(leftComponent, rightComponent, color string, linePoints []string) map[string]any {
	return map[string]any{
		"connect_left":  []string{leftComponent, "y"},
		"connect_right": []string{rightComponent, "u"},
		"left_array":    []string{"", ""},
		"right_array":   []string{"", ""},
		"color":         color,
		"line_points":   linePoints,
	}
}

func buildConnectionListPayload(leftComponent, rightComponent string) map[string]any {
	return map[string]any{
		"connect_left":  []string{leftComponent, "y"},
		"connect_right": []string{rightComponent, "u"},
		"left_array":    []string{"", ""},
		"right_array":   []string{"", ""},
	}
}

func createDefaultConnectionForTest(t *testing.T, packageID, modelName, leftComponent, rightComponent string) {
	t.Helper()
	resp, err := testClient.CreateConnection(packageID, modelName, []map[string]any{buildSimpleConnectionPayload(leftComponent, rightComponent, "0,0,127", []string{"180,60", "220,60"})})
	if err != nil {
		t.Fatalf("准备测试连线失败: %v", err)
	}
	if resp.Status != 0 {
		t.Fatalf("准备测试连线返回非成功状态: status=%d, msg=%s, err=%s", resp.Status, resp.Msg, resp.Err)
	}
	assertConnectionVisibleInInstance(t, packageID, modelName, []string{leftComponent, "y"}, []string{rightComponent, "u"}, "准备测试连线")
}

func toStringSlice(value any) ([]string, bool) {
	list, ok := value.([]any)
	if !ok {
		return nil, false
	}
	result := make([]string, 0, len(list))
	for _, item := range list {
		str, ok := item.(string)
		if !ok {
			return nil, false
		}
		result = append(result, str)
	}
	return result, true
}

func stringSliceEquals(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func getFirstCopyableComponentFromInstance(t *testing.T, packageID, modelName string) (string, string) {
	t.Helper()
	modelData := getCurrentModelInstanceModelData(t, packageID, modelName)
	components := extractModelInstanceElements(t, modelData)
	for _, item := range components {
		name, _ := item["name"].(string)
		className, _ := item["classname"].(string)
		if name != "" && className != "" {
			return name, className
		}
	}
	t.Fatalf("未在模型 %q 的实例数据中找到可复制组件", modelName)
	return "", ""
}

func getFirstIconConnectorName(t *testing.T, packageID, modelName string) string {
	t.Helper()
	iconData := getCurrentModelIconData(t, packageID, modelName)
	connectors := extractModelIconConnectors(t, iconData)
	for _, item := range connectors {
		name, _ := item["name"].(string)
		if name != "" {
			return name
		}
	}
	t.Fatalf("未在模型 %q 的图标数据中找到可更新端口", modelName)
	return ""
}

func assertCopiedModelVisibleInUserRoot(t *testing.T, packageID, modelName, message string) {
	t.Helper()
	resp, err := testClient.GetUserRootModel("", "")
	if err != nil {
		t.Fatalf("%s: 获取用户模型树失败: %v", message, err)
	}
	if resp.Status != 0 {
		t.Fatalf("%s: 获取用户模型树返回非成功状态: status=%d, msg=%s, err=%s", message, resp.Status, resp.Msg, resp.Err)
	}
	list, ok := resp.Data.([]any)
	if !ok {
		t.Fatalf("%s: data 不是数组: %T", message, resp.Data)
	}
	for _, item := range list {
		dataMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if dataMap["package_id"] == packageID && dataMap["model_name"] == modelName {
			return
		}
	}
	t.Fatalf("%s: 未在用户模型树中找到复制出的模型 package_id=%q model_name=%q", message, packageID, modelName)
}

func assertModelNotVisibleInUserRoot(t *testing.T, packageID, modelName, message string) {
	t.Helper()
	resp, err := testClient.GetUserRootModel("", "")
	if err != nil {
		t.Fatalf("%s: 获取用户模型树失败: %v", message, err)
	}
	if resp.Status != 0 {
		t.Fatalf("%s: 获取用户模型树返回非成功状态: status=%d, msg=%s, err=%s", message, resp.Status, resp.Msg, resp.Err)
	}
	list, ok := resp.Data.([]any)
	if !ok {
		t.Fatalf("%s: data 不是数组: %T", message, resp.Data)
	}
	for _, item := range list {
		dataMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if dataMap["package_id"] == packageID && dataMap["model_name"] == modelName {
			t.Fatalf("%s: 仍在用户模型树中找到模型 package_id=%q model_name=%q", message, packageID, modelName)
		}
	}
}

func packageFileListItemByPackageID(list []any, packageID string) map[string]any {
	for _, item := range list {
		dataMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if dataMap["package_id"] == packageID {
			return dataMap
		}
	}
	return nil
}

func extractModelDocumentData(t *testing.T, resp *helpers.Response, message string) map[string]any {
	t.Helper()
	dataMap, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("%s: data 不是 object: %T", message, resp.Data)
	}
	for _, field := range []string{"document", "revisions", "model_name"} {
		if _, ok := dataMap[field]; !ok {
			t.Fatalf("%s: data 缺少字段 %q", message, field)
		}
	}
	return dataMap
}
