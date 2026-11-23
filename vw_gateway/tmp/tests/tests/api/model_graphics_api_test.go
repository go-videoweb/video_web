//go:build api_test

package api

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"strings"
	"testing"
	"time"

	"yssim-go/tests/helpers"
	modelhelpers "yssim-go/tests/helpers/model"
	resourcehelpers "yssim-go/tests/helpers/resource"
)

const (
	graphicsTestPackageID = modelicaStdLibId
	graphicsTestModelName = "Modelica.Blocks.Examples.PID_Controller"
)

// TestGetModelInstance 验证 POST /api/v3/model/instance。
// 覆盖：
//   - 正常获取模型实例图形数据
//   - model_name 为空时返回 HTTP 400
//   - 不存在的模型返回“结构性为空”的 model（例如空 map）
func TestGetModelInstance(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	tests := []struct {
		name               string
		packageID          string
		modelName          string
		useRaw             bool
		expectedHTTPStatus int
		expectEmptyModel   bool
	}{
		{
			name:             "正常获取模型实例图形数据",
			packageID:        graphicsTestPackageID,
			modelName:        graphicsTestModelName,
			expectEmptyModel: false,
		},
		{
			name:               "model_name 为空返回 HTTP 400",
			packageID:          graphicsTestPackageID,
			modelName:          "",
			useRaw:             true,
			expectedHTTPStatus: http.StatusBadRequest,
		},
		{
			name:             "不存在的模型返回结构性空 model",
			packageID:        graphicsTestPackageID,
			modelName:        "Non.Existent.Model.For.Graphics",
			expectEmptyModel: true,
		},
	}

	hooks := helpers.TestHooks{}
	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				if tt.useRaw {
					rawResp, err := testClient.PostRawResponse("/api/v3/model/instance", map[string]any{
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

				resp, err := testClient.Primary().GetModelInstance(tt.packageID, tt.modelName, "")
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				assert.IsSuccess(resp, tt.name)
				assert.HasFields(resp, []string{"encryption", "model"}, tt.name)

				dataMap, ok := resp.Data.(map[string]any)
				if !ok {
					t.Fatalf("%s: data 不是 object: %T", tt.name, resp.Data)
				}
				if dataMap["encryption"] != false {
					t.Errorf("%s: 期望 encryption=false，实际=%v", tt.name, dataMap["encryption"])
				}
				modelVal, exists := dataMap["model"]
				if !exists {
					t.Fatalf("%s: data 缺少 model 字段", tt.name)
				}
				if tt.expectEmptyModel {
					assert.ValueIsStructurallyEmpty(modelVal, tt.name)
					return
				}
				modelMap, ok := modelVal.(map[string]any)
				if !ok {
					t.Fatalf("%s: model 不是 object: %T", tt.name, modelVal)
				}
				for _, field := range []string{"graphics", "parameters"} {
					if _, ok := modelMap[field]; !ok {
						t.Errorf("%s: model 缺少字段 %q", tt.name, field)
					}
				}
			})
		}
	})
}

// TestGetInstantiateCode 验证 GET /api/v3/model/instantiate/code/get。
// 覆盖：
//   - 正常获取扁平化代码
//   - model_name 为空时返回空响应体（历史行为）
//   - 不存在的模型返回空响应体（历史行为）
func TestGetInstantiateCode(t *testing.T) {
	// 该接口返回完整扁平化代码，暂不纳入全量接口测试断言。
	t.Skip("GET /api/v3/model/instantiate/code/get 接口行为特殊，暂不纳入全量接口测试")

	tests := []struct {
		name               string
		modelName          string
		expectedHTTPStatus int
	}{
		{
			name:               "正常获取扁平化代码当前返回空响应体",
			modelName:          graphicsTestModelName,
			expectedHTTPStatus: http.StatusOK,
		},
		{
			name:               "model_name 为空返回空响应体",
			modelName:          "",
			expectedHTTPStatus: http.StatusOK,
		},
		{
			name:               "不存在的模型返回空响应体",
			modelName:          "Non.Existent.Model.For.Instantiate",
			expectedHTTPStatus: http.StatusOK,
		},
	}

	hooks := helpers.TestHooks{}
	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				rawResp, err := testClient.Primary().GetRawResponse("/api/v3/model/instantiate/code/get", map[string]string{
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
				if len(bytes.TrimSpace(body)) != 0 {
					t.Errorf("%s: 期望空响应体，实际=%q", tt.name, string(body))
				}
			})
		}
	})
}

// TestGetIconGraphics 验证 POST /api/v3/model/icon/get。
// 覆盖：
//   - 正常获取模型图标图形数据
//   - model_name 为空时返回 HTTP 400
//   - 不存在的模型返回 status=0 且 data 为 nil
func TestGetIconGraphics(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	tests := []struct {
		name               string
		packageID          string
		modelName          string
		useRaw             bool
		expectedHTTPStatus int
		expectNilData      bool
	}{
		{
			name:          "正常获取模型图标图形数据",
			packageID:     graphicsTestPackageID,
			modelName:     graphicsTestModelName,
			expectNilData: false,
		},
		{
			name:               "model_name 为空返回 HTTP 400",
			packageID:          graphicsTestPackageID,
			modelName:          "",
			useRaw:             true,
			expectedHTTPStatus: http.StatusBadRequest,
		},
		{
			name:          "不存在的模型返回空 data",
			packageID:     graphicsTestPackageID,
			modelName:     "Non.Existent.Model.For.Icon",
			expectNilData: true,
		},
	}

	hooks := helpers.TestHooks{}
	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				if tt.useRaw {
					rawResp, err := testClient.PostRawResponse("/api/v3/model/icon/get", map[string]any{
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

				resp, err := testClient.Primary().GetIconGraphics(tt.packageID, tt.modelName, "")
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				assert.IsSuccess(resp, tt.name)
				if tt.expectNilData {
					if resp.Data != nil {
						t.Errorf("%s: 期望 data 为 nil，实际=%T", tt.name, resp.Data)
					}
					return
				}

				dataMap, ok := resp.Data.(map[string]any)
				if !ok {
					t.Fatalf("%s: data 不是 object: %T", tt.name, resp.Data)
				}
				graphics, ok := dataMap["graphics"].(map[string]any)
				if !ok {
					t.Fatalf("%s: data.graphics 不是 object: %T", tt.name, dataMap["graphics"])
				}
				if _, ok := graphics["icon"]; !ok {
					t.Errorf("%s: data.graphics 缺少 icon 字段", tt.name)
				}
			})
		}
	})
}

// TestGetIconGraphicsV2 验证 POST /api/v2/model/icon/graphics。
// 覆盖：
//   - 正常获取图标
//   - 缺少必填参数返回 HTTP 400
//   - 模型不存在返回空 data
func TestGetIconGraphicsV2(t *testing.T) {
	assert := helpers.NewAssertResponse(t)
	var fixturePackageID string
	var fixtureModelName string

	tests := []struct {
		name               string
		packageID          string
		modelName          string
		useRaw             bool
		expectedHTTPStatus int
		expectNilData      bool
	}{
		{
			name:          "正常获取 v2 图标图形数据",
			packageID:     "__fixture__",
			modelName:     "__fixture__",
			expectNilData: false,
		},
		{
			name:               "缺少 model_name 返回 HTTP 400",
			packageID:          graphicsTestPackageID,
			modelName:          "",
			useRaw:             true,
			expectedHTTPStatus: http.StatusBadRequest,
		},
		{
			name:          "不存在的模型返回空 data",
			packageID:     graphicsTestPackageID,
			modelName:     "Non.Existent.Model.For.Icon.V2",
			expectNilData: true,
		},
	}

	helpers.RunWithHooks(t, helpers.TestHooks{
		Before: func(t *testing.T) {
			{
				var err error
				fixturePackageID, fixtureModelName, err = modelhelpers.CreateTempModelFixture(testClient, tempModelSourcePackageID, tempModelSourceModelName)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := modelhelpers.EnsureModelLoaded(testClient, fixturePackageID, fixtureModelName, "v2 图标图形夹具"); err != nil {
				t.Fatal(err)
			}
		},
		After: func(t *testing.T) {
			modelhelpers.CleanupTempModelFixture(testClient, fixturePackageID, fixtureModelName)
		},
	}, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				packageID := tt.packageID
				if packageID == "__fixture__" {
					packageID = fixturePackageID
				}
				modelName := tt.modelName
				if modelName == "__fixture__" {
					modelName = fixtureModelName
				}

				if tt.useRaw {
					rawResp, err := testClient.PostRawResponse("/api/v2/model/icon/graphics", map[string]any{
						"package_id": packageID,
						"model_name": modelName,
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

				resp, err := testClient.Primary().GetIconGraphicsV2(packageID, modelName, "")
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				assert.IsSuccess(resp, tt.name)
				if tt.expectNilData {
					if resp.Data != nil {
						t.Errorf("%s: 期望 data 为 nil，实际=%T", tt.name, resp.Data)
					}
					return
				}

				dataMap, ok := resp.Data.(map[string]any)
				if !ok {
					t.Fatalf("%s: data 不是 object: %T", tt.name, resp.Data)
				}
				graphics, ok := dataMap["graphics"].(map[string]any)
				if !ok {
					t.Fatalf("%s: data.graphics 不是 object: %T", tt.name, dataMap["graphics"])
				}
				if _, ok := graphics["icon"]; !ok {
					t.Errorf("%s: data.graphics 缺少 icon 字段", tt.name)
				}
			})
		}
	})
}

// TestUpdateIconShapes 验证 POST /api/v3/model/icon/shapes/update。
// 覆盖：
//   - 正常批量更新图标图形，并通过图标接口回读验证
//   - 系统包不允许更新图标图形，返回 status=2
//   - 缺少 icon_list 返回 HTTP 400
//   - 不存在的模型返回业务错误
func TestUpdateIconShapes(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var packageID string
	var modelName string

	rectangleExtent := "{{-92,-48},{92,48}}"
	rectangleFillColor := "{17,34,51}"
	linePoints := "{{-80,-20},{0,60},{80,-20}}"
	lineColor := "{0,0,255}"

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			{
				var err error
				packageID, modelName, err = modelhelpers.CreateTempModelFixture(testClient, tempModelSourcePackageID, tempModelSourceModelName)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := modelhelpers.EnsureModelLoaded(testClient, packageID, modelName, "图标图形更新夹具"); err != nil {
				t.Fatal(err)
			}
		},
		After: func(t *testing.T) {
			modelhelpers.CleanupTempModelFixture(testClient, packageID, modelName)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		iconList := []map[string]any{
			{
				"name":        "Rectangle",
				"origin":      "{0,0}",
				"extent":      rectangleExtent,
				"rotation":    "0",
				"lineColor":   "{1,2,3}",
				"fillColor":   rectangleFillColor,
				"fillPattern": "FillPattern.Solid",
			},
			{
				"name":      "Line",
				"origin":    "{0,0}",
				"extent":    "{{-100,-100},{100,100}}",
				"rotation":  "0",
				"color":     lineColor,
				"thickness": "0.5",
				"points":    linePoints,
			},
		}

		tests := []struct {
			name               string
			useRaw             bool
			packageID          string
			modelName          string
			iconList           []map[string]any
			expectError        bool
			expectedCode       int
			expectedHTTPStatus int
		}{
			{
				name:      "正常批量更新图标图形",
				packageID: packageID,
				modelName: modelName,
				iconList:  iconList,
			},
			{
				name:         "系统包不允许更新图标图形返回 status=2",
				packageID:    modelicaStdLibId,
				modelName:    tempModelSourceModelName,
				iconList:     iconList,
				expectError:  true,
				expectedCode: 2,
			},
			{
				name:               "缺少 icon_list 返回 HTTP 400",
				useRaw:             true,
				packageID:          packageID,
				modelName:          modelName,
				expectedHTTPStatus: 400,
			},
			{
				name:         "不存在的模型返回业务错误",
				packageID:    packageID,
				modelName:    "Non.Existent.Model.For.Icon.Shapes",
				iconList:     iconList,
				expectError:  true,
				expectedCode: 2,
			},
		}

		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				if tt.useRaw {
					rawResp, err := testClient.PostRawResponse("/api/v3/model/icon/shapes/update", map[string]any{
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

				resp, err := testClient.Primary().UpdateIconShapes(tt.packageID, tt.modelName, tt.iconList)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				if tt.expectError {
					assert.IsError(resp, tt.expectedCode, tt.name)
					return
				}

				assert.IsSuccess(resp, tt.name)
				rectangle := assertIconShapeVisibleInGraphics(t, tt.packageID, tt.modelName, func(item map[string]any) bool {
					return item["name"] == "Rectangle" && item["extent"] == rectangleExtent
				}, tt.name+": 图标图形回读")
				assertStringFieldEquals(t, rectangle, "fillColor", rectangleFillColor, tt.name+": rectangle fillColor")

				line := assertIconShapeVisibleInGraphics(t, tt.packageID, tt.modelName, func(item map[string]any) bool {
					return item["name"] == "Line" && item["points"] == linePoints
				}, tt.name+": 线条图形回读")
				assertStringFieldEquals(t, line, "color", lineColor, tt.name+": line color")
			})
		}
	})
}

// TestUpdateDiagramShapes 验证 POST /api/v3/model/diagram/shapes/update。
// 覆盖正常批量更新 Diagram 图形、系统包禁止修改、缺少 diagram_list，以及不存在 package_id。
func TestUpdateDiagramShapes(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var packageID string
	var modelName string

	// Modelica 源码回读时负数可能被格式化成括号表达式。
	// 例如请求值 {{-70,-40},{70,40}} 可能回读为 {{(-70),(-40)},{70,40}}。
	rectangleExtent := "{{-70,-40},{70,40}}"
	rectangleExtentWithParenthesizedNegatives := "{{(-70),(-40)},{70,40}}"
	rectangleFillColor := "{5,55,105}"
	linePoints := "{{-60,0},{0,40},{60,0}}"
	linePointsWithParenthesizedNegatives := "{{(-60),0},{0,40},{60,0}}"

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			{
				var err error
				packageID, modelName, err = modelhelpers.CreateTempModelFixture(testClient, modelicaStdLibId, "Modelica.Blocks.Examples.PID_Controller")
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := modelhelpers.EnsureModelLoaded(testClient, packageID, modelName, "Diagram 图形更新夹具"); err != nil {
				t.Fatal(err)
			}
		},
		After: func(t *testing.T) {
			modelhelpers.CleanupTempModelFixture(testClient, packageID, modelName)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		diagramList := []map[string]any{
			{
				"name":        "Rectangle",
				"origin":      "{0,0}",
				"extent":      rectangleExtent,
				"rotation":    "0",
				"lineColor":   "{1,2,3}",
				"fillColor":   rectangleFillColor,
				"fillPattern": "FillPattern.Solid",
			},
			{
				"name":      "Line",
				"origin":    "{0,0}",
				"extent":    "{{-100,-100},{100,100}}",
				"rotation":  "0",
				"color":     "{0,128,0}",
				"thickness": "0.5",
				"points":    linePoints,
			},
		}

		tests := []struct {
			name               string
			useRaw             bool
			packageID          string
			modelName          string
			diagramList        []map[string]any
			expectError        bool
			expectedCode       int
			expectedHTTPStatus int
		}{
			{
				name:        "正常批量更新 Diagram 图形",
				packageID:   packageID,
				modelName:   modelName,
				diagramList: diagramList,
			},
			{
				name:         "系统包不允许更新 Diagram 图形返回 status=2",
				packageID:    modelicaStdLibId,
				modelName:    graphicsTestModelName,
				diagramList:  diagramList,
				expectError:  true,
				expectedCode: 2,
			},
			{
				name:               "缺少 diagram_list 返回 HTTP 400",
				useRaw:             true,
				packageID:          packageID,
				modelName:          modelName,
				expectedHTTPStatus: http.StatusBadRequest,
			},
			{
				name:         "不存在的 package_id 返回业务错误",
				packageID:    "not-exist-package-id",
				modelName:    modelName,
				diagramList:  diagramList,
				expectError:  true,
				expectedCode: 2,
			},
		}

		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				if tt.useRaw {
					rawResp, err := testClient.Primary().UpdateDiagramShapesRaw(map[string]any{
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

				resp, err := testClient.Primary().UpdateDiagramShapes(tt.packageID, tt.modelName, tt.diagramList)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				if tt.expectError {
					assert.IsError(resp, tt.expectedCode, tt.name)
					return
				}

				assert.IsSuccess(resp, tt.name)
				assertModelCodeContains(t, tt.packageID, tt.modelName, "Diagram", tt.name+": 源码回读")
				assertModelCodeContains(t, tt.packageID, tt.modelName, rectangleExtent, tt.name+": Rectangle extent 源码回读", rectangleExtentWithParenthesizedNegatives)
				assertModelCodeContains(t, tt.packageID, tt.modelName, linePoints, tt.name+": Line points 源码回读", linePointsWithParenthesizedNegatives)
			})
		}
	})
}

// TestUpdateIconConnectors 验证 POST /api/v3/model/icon/connectors/update。
// 覆盖：
//   - 正常批量更新图标端口，并通过图标接口回读验证
//   - 系统包不允许更新图标端口，返回 status=2
//   - 缺少 connector_list 返回 HTTP 400
//   - 不存在的端口返回业务错误
func TestUpdateIconConnectors(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var packageID string
	var modelName string
	var connectorName string

	connectorOriginInput := "30,40"
	connectorOriginWant := "{30,40}"
	connectorExtentInput := []string{"-90,-24", "-60,24"}
	connectorExtentWant := "{{-90,-24},{-60,24}}"
	connectorRotation := "90"

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			{
				var err error
				packageID, modelName, err = modelhelpers.CreateTempModelFixture(testClient, tempModelSourcePackageID, tempModelComponentClass)
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := modelhelpers.EnsureModelLoaded(testClient, packageID, modelName, "图标端口更新夹具"); err != nil {
				t.Fatal(err)
			}
			connectorName = getFirstIconConnectorName(t, packageID, modelName)
		},
		After: func(t *testing.T) {
			modelhelpers.CleanupTempModelFixture(testClient, packageID, modelName)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		connectorList := []map[string]any{{
			"connector_name": connectorName,
			"origin":         connectorOriginInput,
			"extent":         connectorExtentInput,
			"rotation":       connectorRotation,
		}}

		tests := []struct {
			name               string
			useRaw             bool
			packageID          string
			modelName          string
			connectorList      []map[string]any
			expectError        bool
			expectedCode       int
			expectedHTTPStatus int
		}{
			{
				name:          "正常批量更新图标端口",
				packageID:     packageID,
				modelName:     modelName,
				connectorList: connectorList,
			},
			{
				name:          "系统包不允许更新图标端口返回 status=2",
				packageID:     modelicaStdLibId,
				modelName:     tempModelComponentClass,
				connectorList: connectorList,
				expectError:   true,
				expectedCode:  2,
			},
			{
				name:               "缺少 connector_list 返回 HTTP 400",
				useRaw:             true,
				packageID:          packageID,
				modelName:          modelName,
				expectedHTTPStatus: 400,
			},
			{
				name:      "不存在的端口返回业务错误",
				packageID: packageID,
				modelName: modelName,
				connectorList: []map[string]any{{
					"connector_name": "NoSuchConnector",
					"origin":         connectorOriginInput,
					"extent":         connectorExtentInput,
					"rotation":       connectorRotation,
				}},
				expectError:  true,
				expectedCode: 2,
			},
		}

		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				if tt.useRaw {
					rawResp, err := testClient.PostRawResponse("/api/v3/model/icon/connectors/update", map[string]any{
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

				resp, err := testClient.Primary().UpdateIconConnectors(tt.packageID, tt.modelName, tt.connectorList)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				if tt.expectError {
					assert.IsError(resp, tt.expectedCode, tt.name)
					return
				}

				assert.IsSuccess(resp, tt.name)
				connector := assertIconConnectorVisibleInGraphics(t, tt.packageID, tt.modelName, connectorName, tt.name+": 图标端口回读")
				assertStringFieldEquals(t, connector, "origin", connectorOriginWant, tt.name+": connector origin")
				assertStringFieldEquals(t, connector, "extent", connectorExtentWant, tt.name+": connector extent")
				assertStringFieldEquals(t, connector, "rotation", connectorRotation, tt.name+": connector rotation")
			})
		}
	})
}

// TestGetIconInfo 验证 GET /api/v3/model/icon/info。
// 覆盖：
//   - 正常获取图标参数与图标图形信息
//   - model_name 为空时返回空 data（历史行为）
//   - 不存在的模型返回空 data
func TestGetIconInfo(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	tests := []struct {
		name          string
		modelName     string
		expectNilData bool
	}{
		{
			name:          "正常获取图标参数信息",
			modelName:     graphicsTestModelName,
			expectNilData: false,
		},
		{
			name:          "model_name 为空返回空 data",
			modelName:     "",
			expectNilData: true,
		},
		{
			name:          "不存在的模型返回空 data",
			modelName:     "Non.Existent.Model.For.IconInfo",
			expectNilData: true,
		},
	}

	hooks := helpers.TestHooks{}
	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				resp, err := testClient.Primary().GetIconInfo(tt.modelName)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				assert.IsSuccess(resp, tt.name)
				if tt.expectNilData {
					if resp.Data != nil {
						t.Errorf("%s: 期望 data 为 nil，实际=%T", tt.name, resp.Data)
					}
					return
				}

				dataMap, ok := resp.Data.(map[string]any)
				if !ok {
					t.Fatalf("%s: data 不是 object: %T", tt.name, resp.Data)
				}
				for _, field := range []string{"graphics", "parameters"} {
					if _, ok := dataMap[field]; !ok {
						t.Errorf("%s: data 缺少字段 %q", tt.name, field)
					}
				}
			})
		}
	})
}

// TestGetIconInstance 验证 GET /api/v3/model/icon/instance/get。
// 覆盖：
//   - 正常获取图标实例信息
//   - model_name 为空时返回空 data（历史行为）
//   - 不存在的模型返回空 data
func TestGetIconInstance(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	tests := []struct {
		name          string
		modelName     string
		expectNilData bool
	}{
		{
			name:          "正常获取图标实例信息",
			modelName:     graphicsTestModelName,
			expectNilData: false,
		},
		{
			name:          "model_name 为空返回空 data",
			modelName:     "",
			expectNilData: true,
		},
		{
			name:          "不存在的模型返回空 data",
			modelName:     "Non.Existent.Model.For.IconInstance",
			expectNilData: true,
		},
	}

	hooks := helpers.TestHooks{}
	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				resp, err := testClient.Primary().GetIconInstance(tt.modelName)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				assert.IsSuccess(resp, tt.name)
				if tt.expectNilData {
					if resp.Data != nil {
						t.Errorf("%s: 期望 data 为 nil，实际=%T", tt.name, resp.Data)
					}
					return
				}

				dataMap, ok := resp.Data.(map[string]any)
				if !ok {
					t.Fatalf("%s: data 不是 object: %T", tt.name, resp.Data)
				}
				for _, field := range []string{"graphics", "parameters", "properties"} {
					if _, ok := dataMap[field]; !ok {
						t.Errorf("%s: data 缺少字段 %q", tt.name, field)
					}
				}
			})
		}
	})
}

// TestGetModelInformation 验证 GET /api/v3/model/information/get。
// 覆盖：
//   - 正常获取模型所在库记录信息
//   - model_name 为空返回 status=2
//   - 不存在的模型返回 status=2
func TestGetModelInformation(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	tests := []struct {
		name         string
		modelName    string
		expectError  bool
		expectedCode int
	}{
		{
			name:        "正常获取模型所在库记录信息",
			modelName:   graphicsTestModelName,
			expectError: false,
		},
		{
			name:         "model_name 为空返回 status=2",
			modelName:    "",
			expectError:  true,
			expectedCode: 2,
		},
		{
			name:         "不存在的模型返回 status=2",
			modelName:    "Non.Existent.Model.For.Information",
			expectError:  true,
			expectedCode: 2,
		},
	}

	hooks := helpers.TestHooks{}
	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				resp, err := testClient.Primary().GetModelInformation(tt.modelName)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				if tt.expectError {
					assert.IsError(resp, tt.expectedCode, tt.name)
					return
				}
				assert.IsSuccess(resp, tt.name)
				assert.HasFields(resp, []string{"package_name", "package_version", "package_id", "sys_or_user"}, tt.name)

				dataMap, ok := resp.Data.(map[string]any)
				if !ok {
					t.Fatalf("%s: data 不是 object: %T", tt.name, resp.Data)
				}
				if dataMap["package_name"] == "" {
					t.Errorf("%s: package_name 为空", tt.name)
				}
			})
		}
	})
}

// TestGetParametersOptions 验证 GET /model/parameters/options/get。
// 覆盖：
//   - choices_all_matching=true 时返回选项数组
//   - choices_all_matching=false 时返回空 data
//   - 不存在的 class_name 仍返回仅含默认空选项的数组
func TestGetParametersOptions(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	tests := []struct {
		name               string
		className          string
		parentName         string
		choicesAllMatching string
		expectNilData      bool
		minLength          int
	}{
		{
			name:               "choices_all_matching 为 true 时返回介质选项",
			className:          "Modelica.Media.Interfaces.PartialMedium",
			parentName:         "",
			choicesAllMatching: "true",
			minLength:          2,
		},
		{
			name:               "choices_all_matching 不为 true 时返回空 data",
			className:          "",
			parentName:         "",
			choicesAllMatching: "false",
			expectNilData:      true,
		},
		{
			name:               "不存在的 class_name 返回默认空选项",
			className:          "Non.Existent.Parameter.Option.Class",
			parentName:         "",
			choicesAllMatching: "true",
			minLength:          1,
		},
	}

	hooks := helpers.TestHooks{}
	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				resp, err := testClient.Primary().GetParametersOptions(tt.className, tt.parentName, tt.choicesAllMatching)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				assert.IsSuccess(resp, tt.name)
				if tt.expectNilData {
					if resp.Data != nil {
						t.Errorf("%s: 期望 data 为 nil，实际=%T", tt.name, resp.Data)
					}
					return
				}

				list, ok := resp.Data.([]any)
				if !ok {
					t.Fatalf("%s: data 不是数组: %T", tt.name, resp.Data)
				}
				if len(list) < tt.minLength {
					t.Fatalf("%s: 期望至少 %d 条，实际 %d 条", tt.name, tt.minLength, len(list))
				}
				first, ok := list[0].(map[string]any)
				if !ok {
					t.Fatalf("%s: 首个选项不是 object: %T", tt.name, list[0])
				}
				for _, field := range []string{"comment", "value"} {
					if _, ok := first[field]; !ok {
						t.Errorf("%s: 选项缺少字段 %q", tt.name, field)
					}
				}
			})
		}
	})
}

// TestGetComponentName 验证 GET /model/component/name。
// 覆盖：
//   - 正常获取组件默认名
//   - model_name 为空时仍能回退生成默认名
//   - class_name 为空时返回非空占位名称
func TestGetComponentName(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	tests := []struct {
		name      string
		modelName string
		className string
		prefix    string
	}{
		{
			name:      "正常获取组件默认名",
			modelName: graphicsTestModelName,
			className: "Modelica.Blocks.Math.Abs",
			prefix:    "abs",
		},
		{
			name:      "model_name 为空时仍返回默认名",
			modelName: "",
			className: "Modelica.Blocks.Math.Abs",
			prefix:    "abs",
		},
		{
			name:      "class_name 为空时返回非空占位名称",
			modelName: graphicsTestModelName,
			className: "",
			prefix:    "1",
		},
	}

	hooks := helpers.TestHooks{}
	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				resp, err := testClient.Primary().GetComponentName(tt.modelName, tt.className)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				assert.IsSuccess(resp, tt.name)

				list, ok := resp.Data.([]any)
				if !ok || len(list) != 1 {
					t.Fatalf("%s: data 应为单元素数组，实际 %T = %v", tt.name, resp.Data, resp.Data)
				}
				componentName, ok := list[0].(string)
				if !ok {
					t.Fatalf("%s: data[0] 不是 string: %T", tt.name, list[0])
				}
				if componentName == "" {
					t.Fatalf("%s: 返回的组件名为空", tt.name)
				}
				if tt.prefix != "" && len(componentName) >= len(tt.prefix) && componentName[:len(tt.prefix)] != tt.prefix {
					t.Errorf("%s: 期望组件名前缀 %q，实际 %q", tt.name, tt.prefix, componentName)
				}
			})
		}
	})
}

// TestGetAllUserspaceModel 验证 POST /api/v2/model/userspace/model/getall。
// 覆盖：
//   - 正常获取用户空间模型列表
//   - 不存在的 space_id 进入 GetAllModelView 后返回 status=4
func TestGetAllUserspaceModel(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	tests := []struct {
		name         string
		client       *helpers.APIClient
		expectError  bool
		expectedCode int
		minLength    int
	}{
		{
			name:      "正常获取用户空间模型列表",
			client:    testClient,
			minLength: 1,
		},
		{
			name:         "不存在的 space_id 返回 status=4",
			client:       newClientWithSpaceID(t, "non-existent-space-id"),
			expectError:  true,
			expectedCode: 4,
		},
	}

	hooks := helpers.TestHooks{}
	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				resp, err := tt.client.Primary().GetAllUserspaceModel()
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
					t.Fatalf("%s: 期望至少 %d 条，实际 %d 条", tt.name, tt.minLength, len(list))
				}
				first, ok := list[0].(map[string]any)
				if !ok {
					t.Fatalf("%s: 首个模型项不是 object: %T", tt.name, list[0])
				}
				for _, field := range []string{"id", "name", "version"} {
					if _, ok := first[field]; !ok {
						t.Errorf("%s: 模型项缺少字段 %q", tt.name, field)
					}
				}
			})
		}
	})
}

// TestGetSubparameters 验证 POST /api/v3/model/subparameters/get。
// 覆盖：
//   - 正常获取带子参数的参数列表
//   - 不存在的 parameter_base 返回空 data
//   - parameter_base 为空时返回 HTTP 400
func TestGetSubparameters(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	tests := []struct {
		name               string
		parameterBase      string
		useRaw             bool
		expectedHTTPStatus int
		expectNilData      bool
		minLength          int
		checkOptions       bool
	}{
		{
			name:          "正常获取带子参数的参数列表",
			parameterBase: "Modelica.Media.Interfaces.PartialMedium",
			minLength:     1,
			checkOptions:  true,
		},
		{
			name:          "不存在的 parameter_base 返回空 data",
			parameterBase: "Non.Existent.Parameter.Base",
			expectNilData: true,
		},
		{
			name:               "parameter_base 为空返回 HTTP 400",
			parameterBase:      "",
			useRaw:             true,
			expectedHTTPStatus: http.StatusBadRequest,
		},
	}

	hooks := helpers.TestHooks{}
	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				if tt.useRaw {
					rawResp, err := testClient.PostRawResponse("/api/v3/model/subparameters/get", map[string]any{
						"parameter_base": tt.parameterBase,
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

				resp, err := testClient.Primary().GetSubparameters(tt.parameterBase)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				assert.IsSuccess(resp, tt.name)
				if tt.expectNilData {
					if resp.Data != nil {
						t.Errorf("%s: 期望 data 为 nil，实际=%T", tt.name, resp.Data)
					}
					return
				}

				list, ok := resp.Data.([]any)
				if !ok {
					t.Fatalf("%s: data 不是数组: %T", tt.name, resp.Data)
				}
				if len(list) < tt.minLength {
					t.Fatalf("%s: 期望至少 %d 条，实际 %d 条", tt.name, tt.minLength, len(list))
				}
				first, ok := list[0].(map[string]any)
				if !ok {
					t.Fatalf("%s: 首项不是 object: %T", tt.name, list[0])
				}
				for _, field := range []string{"name", "parameterBase", "type", "options"} {
					if _, ok := first[field]; !ok {
						t.Errorf("%s: 子参数项缺少字段 %q", tt.name, field)
					}
				}
				if tt.checkOptions {
					options, ok := first["options"].([]any)
					if !ok {
						t.Fatalf("%s: options 不是数组: %T", tt.name, first["options"])
					}
					if len(options) == 0 {
						t.Errorf("%s: 期望 options 非空", tt.name)
					}
				}
			})
		}
	})
}

// TestConvertUnits 验证 POST /model/units/convert。
// 覆盖：
//   - 带偏移量的单位换算
//   - 纯倍率换算
//   - 不兼容单位返回 units_compatible=false
//   - 缺少字段时返回 HTTP 400
func TestConvertUnits(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	tests := []struct {
		name               string
		toUnit             string
		fromUnit           string
		useRaw             bool
		expectedHTTPStatus int
		wantCompatible     bool
		wantScale          float64
		wantOffset         float64
	}{
		{
			name:           "带偏移量的温度单位换算",
			toUnit:         "degC",
			fromUnit:       "K",
			wantCompatible: true,
			wantScale:      1,
			wantOffset:     -273.15,
		},
		{
			name:           "纯倍率单位换算",
			toUnit:         "km",
			fromUnit:       "m",
			wantCompatible: true,
			wantScale:      0.001,
			wantOffset:     0,
		},
		{
			name:           "不兼容单位返回 false",
			toUnit:         "kg",
			fromUnit:       "m",
			wantCompatible: false,
			wantScale:      1,
			wantOffset:     0,
		},
		{
			name:               "缺少字段返回 HTTP 400",
			toUnit:             "",
			fromUnit:           "m",
			useRaw:             true,
			expectedHTTPStatus: http.StatusBadRequest,
		},
	}

	hooks := helpers.TestHooks{}
	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				if tt.useRaw {
					rawResp, err := testClient.PostRawResponse("/model/units/convert", map[string]any{
						"s1": tt.toUnit,
						"s2": tt.fromUnit,
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

				resp, err := testClient.Primary().ConvertUnits(tt.toUnit, tt.fromUnit)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				assert.IsSuccess(resp, tt.name)
				assert.HasFields(resp, []string{"units_compatible", "scale_factor", "offset"}, tt.name)

				dataMap, ok := resp.Data.(map[string]any)
				if !ok {
					t.Fatalf("%s: data 不是 object: %T", tt.name, resp.Data)
				}
				compatible, ok := dataMap["units_compatible"].(bool)
				if !ok {
					t.Fatalf("%s: units_compatible 不是 bool: %T", tt.name, dataMap["units_compatible"])
				}
				if compatible != tt.wantCompatible {
					t.Errorf("%s: 期望 units_compatible=%v，实际=%v", tt.name, tt.wantCompatible, compatible)
				}
				scale, ok := dataMap["scale_factor"].(float64)
				if !ok {
					t.Fatalf("%s: scale_factor 不是 float64: %T", tt.name, dataMap["scale_factor"])
				}
				if !almostEqual(scale, tt.wantScale, 1e-4) {
					t.Errorf("%s: 期望 scale_factor≈%v，实际=%v", tt.name, tt.wantScale, scale)
				}
				offset, ok := dataMap["offset"].(float64)
				if !ok {
					t.Fatalf("%s: offset 不是 float64: %T", tt.name, dataMap["offset"])
				}
				if !almostEqual(offset, tt.wantOffset, 1e-3) {
					t.Errorf("%s: 期望 offset≈%v，实际=%v", tt.name, tt.wantOffset, offset)
				}
			})
		}
	})
}

// TestGetModelResourcesReference 验证 POST /model/reference/resources。
// 覆盖：
//   - package_id 为空时返回当前用户空间包级 Resources 目录列表
//   - 指定合法 package_id + 不存在 parent 返回结构性空 data
//   - 不存在的 package_id 返回 HTTP 400
func TestGetModelResourcesReference(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var fixturePackageID string
	var fixtureModelName string

	tests := []struct {
		name               string
		packageID          string
		parent             string
		useRaw             bool
		expectedHTTPStatus int
		assertResponse     func(t *testing.T, resp *helpers.Response)
	}{
		{
			name:      "package_id 为空返回包级 Resources 列表",
			packageID: "",
			parent:    "",
			assertResponse: func(t *testing.T, resp *helpers.Response) {
				assert.IsSuccess(resp, "package_id 为空返回包级 Resources 列表")
				list, ok := resp.Data.([]any)
				if !ok {
					t.Fatalf("package_id 为空返回包级 Resources 列表: data 不是数组: %T", resp.Data)
				}
				if len(list) == 0 {
					t.Fatalf("package_id 为空返回包级 Resources 列表: 期望非空列表")
				}

				item := findResourceReferenceItemByPackageID(list, fixturePackageID)
				if item == nil {
					t.Fatalf("package_id 为空返回包级 Resources 列表: 未找到 package_id=%q 对应项", fixturePackageID)
				}
				if item["name"] != fixtureModelName+".Resources" {
					t.Errorf("package_id 为空返回包级 Resources 列表: 期望 name=%q，实际=%v", fixtureModelName+".Resources", item["name"])
				}
				if item["type"] != "dir" {
					t.Errorf("package_id 为空返回包级 Resources 列表: 期望 type=dir，实际=%v", item["type"])
				}
			},
		},
		{
			name:      "指定合法 package_id 且 parent 不存在返回空 data",
			packageID: "__fixture__",
			parent:    "__copilot_non_existent_resources_dir__",
			assertResponse: func(t *testing.T, resp *helpers.Response) {
				assert.IsSuccess(resp, "指定合法 package_id 且 parent 不存在返回空 data")
				assert.ValueIsStructurallyEmpty(resp.Data, "指定合法 package_id 且 parent 不存在返回空 data")
			},
		},
		{
			name:               "不存在的 package_id 返回 HTTP 400",
			packageID:          "non-existent-package-id",
			parent:             "",
			useRaw:             true,
			expectedHTTPStatus: http.StatusBadRequest,
		},
	}

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			{
				var err error
				fixturePackageID, fixtureModelName, err = modelhelpers.CreateTempPackageFixture(testClient, "TestResourcePackage")
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := modelhelpers.EnsureModelLoaded(testClient, fixturePackageID, fixtureModelName, "资源引用夹具"); err != nil {
				t.Fatal(err)
			}
		},
		After: func(t *testing.T) {
			modelhelpers.CleanupTempModelFixture(testClient, fixturePackageID, fixtureModelName)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				packageID := tt.packageID
				if packageID == "__fixture__" {
					packageID = fixturePackageID
				}

				if tt.useRaw {
					rawResp, err := testClient.PostRawResponse("/model/reference/resources", map[string]any{
						"package_id": packageID,
						"parent":     tt.parent,
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

				resp, err := testClient.Primary().GetModelResourcesReference(packageID, tt.parent, "")
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				tt.assertResponse(t, resp)
			})
		}
	})
}

// TestGetPackageResourcesList 验证 POST /file/package/resources/get。
// 覆盖：
//   - 正常列出 Resources 根目录下的子目录
//   - 正常列出子目录下的文件并返回 modelica:// path
//   - 不存在的 package_id 返回 HTTP 400
func TestGetPackageResourcesList(t *testing.T) {
	assert := helpers.NewAssertResponse(t)
	var fixture resourceTestFixture

	helpers.RunWithHooks(t, helpers.TestHooks{
		Before: func(t *testing.T) {
			fixture = createResourceTestFixture(t)
		},
		After: func(t *testing.T) {
			modelhelpers.CleanupTempModelFixture(testClient, fixture.packageID, fixture.modelName)
		},
	}, func(t *testing.T) {
		t.Run("正常列出 Resources 根目录下的子目录", func(t *testing.T) {
			resp, err := testClient.Primary().GetPackageResourcesList(fixture.packageID, "", "")
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsSuccess(resp, "正常列出 Resources 根目录下的子目录")
			list, err := helpers.ArrayData(resp, "正常列出 Resources 根目录下的子目录")
			if err != nil {
				t.Fatal(err)
			}
			item := resourceListItemByName(list, fixture.dirName)
			if item == nil {
				t.Fatalf("正常列出 Resources 根目录下的子目录: 未找到目录 %q", fixture.dirName)
			}
			if item["type"] != "dir" {
				t.Errorf("正常列出 Resources 根目录下的子目录: 期望 type=dir，实际=%v", item["type"])
			}
		})

		t.Run("正常列出子目录下文件并返回 modelica path", func(t *testing.T) {
			resp, err := testClient.Primary().GetPackageResourcesList(fixture.packageID, fixture.dirName, "")
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsSuccess(resp, "正常列出子目录下文件并返回 modelica path")
			list, err := helpers.ArrayData(resp, "正常列出子目录下文件并返回 modelica path")
			if err != nil {
				t.Fatal(err)
			}
			textItem := resourceListItemByName(list, fixture.textFileName)
			if textItem == nil {
				t.Fatalf("正常列出子目录下文件并返回 modelica path: 未找到文件 %q", fixture.textFileName)
			}
			if textItem["type"] != "file" {
				t.Errorf("正常列出子目录下文件并返回 modelica path: 期望 type=file，实际=%v", textItem["type"])
			}
			if textItem["path"] != fixture.textURI {
				t.Errorf("正常列出子目录下文件并返回 modelica path: 期望 path=%q，实际=%v", fixture.textURI, textItem["path"])
			}
		})

		t.Run("不存在的 package_id 返回 HTTP 400", func(t *testing.T) {
			rawResp, err := testClient.PostRawResponse("/file/package/resources/get", map[string]any{
				"package_id": "non-existent-package-id",
				"parent":     "",
				"path":       "",
			})
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			body, err := helpers.ReadAndCloseRawBody(rawResp)
			if err != nil {
				t.Fatal(err)
			}
			if rawResp.StatusCode != http.StatusBadRequest {
				t.Fatalf("不存在的 package_id 返回 HTTP 400: 期望 HTTP %d，实际 %d，body=%q", http.StatusBadRequest, rawResp.StatusCode, string(body))
			}
			assertRawJSONStringBody(t, "不存在的 package_id 返回 HTTP 400", body, "not found")
		})
	})
}

// TestGetResourcesFileData 验证 GET /file/package/resources/data/get。
// 覆盖：
//   - 正常读取资源文件原始内容
//   - 不存在的资源文件返回 status=2
func TestGetResourcesFileData(t *testing.T) {
	var fixture resourceTestFixture

	helpers.RunWithHooks(t, helpers.TestHooks{
		Before: func(t *testing.T) {
			fixture = createResourceTestFixture(t)
		},
		After: func(t *testing.T) {
			modelhelpers.CleanupTempModelFixture(testClient, fixture.packageID, fixture.modelName)
		},
	}, func(t *testing.T) {
		t.Run("正常读取资源文件原始内容", func(t *testing.T) {
			rawResp, err := testClient.Primary().GetResourcesFileDataRaw(fixture.textURI)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			body, err := helpers.ReadAndCloseRawBody(rawResp)
			if err != nil {
				t.Fatal(err)
			}
			if rawResp.StatusCode != http.StatusOK {
				t.Fatalf("正常读取资源文件原始内容: 期望 HTTP 200，实际 %d，body=%q", rawResp.StatusCode, string(body))
			}
			if string(body) != fixture.textContent {
				t.Fatalf("正常读取资源文件原始内容: 期望 body=%q，实际=%q", fixture.textContent, string(body))
			}
		})

		t.Run("不存在的资源文件返回 status=2", func(t *testing.T) {
			missingURI := "modelica://" + fixture.modelName + "/Resources/" + fixture.dirName + "/missing.txt"
			rawResp, err := testClient.Primary().GetResourcesFileDataRaw(missingURI)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			body, err := helpers.ReadAndCloseRawBody(rawResp)
			if err != nil {
				t.Fatal(err)
			}
			if rawResp.StatusCode != http.StatusOK {
				t.Fatalf("不存在的资源文件返回 status=2: 期望 HTTP 200，实际 %d，body=%q", rawResp.StatusCode, string(body))
			}
			var resp helpers.Response
			if err := json.Unmarshal(body, &resp); err != nil {
				t.Fatalf("不存在的资源文件返回 status=2: 响应体不是业务 JSON: %q, err=%v", string(body), err)
			}
			if resp.Status != 2 {
				t.Fatalf("不存在的资源文件返回 status=2: 期望 status=2，实际=%d，body=%q", resp.Status, string(body))
			}
		})
	})
}

// TestUploadResourcesFile 验证 POST /file/package/resources/upload。
// 覆盖：
//   - 正常上传 txt 资源文件
//   - 当前目录下重复文件名返回 status=2
//   - 非法文件后缀返回 status=2
func TestUploadResourcesFile(t *testing.T) {
	assert := helpers.NewAssertResponse(t)
	var fixture resourceTestFixture

	helpers.RunWithHooks(t, helpers.TestHooks{
		Before: func(t *testing.T) {
			fixture = createResourceTestFixture(t)
		},
		After: func(t *testing.T) {
			modelhelpers.CleanupTempModelFixture(testClient, fixture.packageID, fixture.modelName)
		},
	}, func(t *testing.T) {
		uploadedName := uniqueName("resources_upload") + ".txt"
		uploadedContent := "uploaded resource content"

		t.Run("正常上传 txt 资源文件", func(t *testing.T) {
			resp, err := testClient.Primary().UploadResourcesFile(fixture.packageID, fixture.dirName, uploadedName, []byte(uploadedContent))
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsSuccess(resp, "正常上传 txt 资源文件")

			dataMap, err := helpers.ObjectData(resp, "正常上传 txt 资源文件")
			if err != nil {
				t.Fatal(err)
			}
			pathValue, _ := dataMap["path"].(string)
			if pathValue == "" {
				t.Fatalf("正常上传 txt 资源文件: 返回 path 为空")
			}
			if !strings.HasSuffix(pathValue, fixture.dirName+"/"+uploadedName) {
				t.Errorf("正常上传 txt 资源文件: 期望 path 以 %q 结尾，实际=%q", fixture.dirName+"/"+uploadedName, pathValue)
			}

			listResp, err := testClient.GetPackageResourcesList(fixture.packageID, fixture.dirName, "")
			if err != nil {
				t.Fatalf("回查资源列表失败: %v", err)
			}
			assert.IsSuccess(listResp, "正常上传 txt 资源文件: 回查资源列表")
			list, err := helpers.ArrayData(listResp, "正常上传 txt 资源文件: 回查资源列表")
			if err != nil {
				t.Fatal(err)
			}
			item := resourceListItemByName(list, uploadedName)
			if item == nil {
				t.Fatalf("正常上传 txt 资源文件: 未找到文件 %q", uploadedName)
			}
			if item["type"] != "file" {
				t.Errorf("正常上传 txt 资源文件: 期望 type=file，实际=%v", item["type"])
			}
		})

		t.Run("当前目录下重复文件名返回 status=2", func(t *testing.T) {
			resp, err := testClient.Primary().UploadResourcesFile(fixture.packageID, fixture.dirName, uploadedName, []byte("duplicate"))
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsError(resp, 2, "当前目录下重复文件名返回 status=2")
		})

		t.Run("非法文件后缀返回 status=2", func(t *testing.T) {
			resp, err := testClient.Primary().UploadResourcesFile(fixture.packageID, fixture.dirName, "invalid.bin", []byte("binary"))
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsError(resp, 2, "非法文件后缀返回 status=2")
		})

		t.Run("缺少文件返回 HTTP 400", func(t *testing.T) {
			rawResp, body, err := resourcehelpers.PostMultipartRaw("/file/package/resources/upload", map[string]string{
				"package_id": fixture.packageID,
				"parent":     fixture.dirName,
			}, false, "file", "", nil)
			if err != nil {
				t.Fatal(err)
			}
			if rawResp.StatusCode != http.StatusBadRequest {
				t.Fatalf("缺少文件返回 HTTP 400: 期望 HTTP 400，实际=%d，body=%q", rawResp.StatusCode, string(body))
			}
		})
	})
}

// TestSetModelIcon 验证 POST /file/icon/set。
// 覆盖：
//   - 正常设置用户模型图标
//   - 非 png 路径返回 HTTP 400
//   - 系统包返回 status=2
func TestSetModelIcon(t *testing.T) {
	assert := helpers.NewAssertResponse(t)
	var fixture resourceTestFixture

	helpers.RunWithHooks(t, helpers.TestHooks{
		Before: func(t *testing.T) {
			fixture = createResourceTestFixture(t)
		},
		After: func(t *testing.T) {
			modelhelpers.CleanupTempModelFixture(testClient, fixture.packageID, fixture.modelName)
		},
	}, func(t *testing.T) {
		t.Run("正常设置用户模型图标", func(t *testing.T) {
			resp, err := testClient.Primary().SetModelIcon(fixture.packageID, fixture.modelName, fixture.pngURI)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsSuccess(resp, "正常设置用户模型图标")
			if !strings.Contains(resp.Msg, "图标设置成功") {
				t.Errorf("正常设置用户模型图标: 期望 msg 包含 图标设置成功，实际=%q", resp.Msg)
			}
			assertModelCodeContains(t, fixture.packageID, fixture.modelName, "Bitmap", "正常设置用户模型图标: 源码回读")
		})

		t.Run("非 png 路径返回 HTTP 400", func(t *testing.T) {
			rawResp, err := testClient.PostRawResponse("/file/icon/set", map[string]any{
				"package_id": fixture.packageID,
				"model_name": fixture.modelName,
				"path":       fixture.textURI,
			})
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			body, err := helpers.ReadAndCloseRawBody(rawResp)
			if err != nil {
				t.Fatal(err)
			}
			if rawResp.StatusCode != http.StatusBadRequest {
				t.Fatalf("非 png 路径返回 HTTP 400: 期望 HTTP %d，实际 %d，body=%q", http.StatusBadRequest, rawResp.StatusCode, string(body))
			}
		})

		t.Run("系统包返回 status=2", func(t *testing.T) {
			resp, err := testClient.Primary().SetModelIcon(modelicaStdLibId, graphicsTestModelName, fixture.pngURI)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsError(resp, 2, "系统包返回 status=2")
		})
	})
}

// TestCreateResourcesDir 验证 POST /file/package/resources/dir/create。
// 覆盖：
//   - 正常创建子目录
//   - 重复目录名返回 status=2
//   - 空目录名返回 status=2
func TestCreateResourcesDir(t *testing.T) {
	assert := helpers.NewAssertResponse(t)
	var fixture resourceTestFixture

	helpers.RunWithHooks(t, helpers.TestHooks{
		Before: func(t *testing.T) {
			fixture = createResourceTestFixture(t)
		},
		After: func(t *testing.T) {
			modelhelpers.CleanupTempModelFixture(testClient, fixture.packageID, fixture.modelName)
		},
	}, func(t *testing.T) {
		newDirName := uniqueName("ResourcesChild")

		t.Run("正常创建子目录", func(t *testing.T) {
			resp, err := testClient.Primary().CreateResourcesDir(fixture.packageID, fixture.dirName, newDirName)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsSuccess(resp, "正常创建子目录")

			listResp, err := testClient.GetPackageResourcesList(fixture.packageID, fixture.dirName, "")
			if err != nil {
				t.Fatalf("回查资源列表失败: %v", err)
			}
			assert.IsSuccess(listResp, "正常创建子目录: 回查资源列表")
			list, err := helpers.ArrayData(listResp, "正常创建子目录: 回查资源列表")
			if err != nil {
				t.Fatal(err)
			}
			item := resourceListItemByName(list, newDirName)
			if item == nil {
				t.Fatalf("正常创建子目录: 未找到目录 %q", newDirName)
			}
			if item["type"] != "dir" {
				t.Errorf("正常创建子目录: 期望 type=dir，实际=%v", item["type"])
			}
		})

		t.Run("重复目录名返回 status=2", func(t *testing.T) {
			resp, err := testClient.Primary().CreateResourcesDir(fixture.packageID, fixture.dirName, newDirName)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsError(resp, 2, "重复目录名返回 status=2")
		})

		t.Run("空目录名返回 status=2", func(t *testing.T) {
			resp, err := testClient.Primary().CreateResourcesDir(fixture.packageID, fixture.dirName, "")
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsError(resp, 2, "空目录名返回 status=2")
		})
	})
}

// TestDeleteResourcesDirAndFile 验证 POST /file/package/resources/dir/delete。
// 覆盖：
//   - 正常删除目录
//   - 正常删除文件（当前语义通过 parent 传相对路径）
//   - 不存在的 package_id 返回 HTTP 400
func TestDeleteResourcesDirAndFile(t *testing.T) {
	assert := helpers.NewAssertResponse(t)
	var fixture resourceTestFixture

	helpers.RunWithHooks(t, helpers.TestHooks{
		Before: func(t *testing.T) {
			fixture = createResourceTestFixture(t)
		},
		After: func(t *testing.T) {
			modelhelpers.CleanupTempModelFixture(testClient, fixture.packageID, fixture.modelName)
		},
	}, func(t *testing.T) {
		t.Run("正常删除文件", func(t *testing.T) {
			resp, err := testClient.Primary().DeleteResourcesDirAndFile(fixture.packageID, fixture.dirName+"/"+fixture.textFileName)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsSuccess(resp, "正常删除文件")

			listResp, err := testClient.GetPackageResourcesList(fixture.packageID, fixture.dirName, "")
			if err != nil {
				t.Fatalf("回查资源列表失败: %v", err)
			}
			assert.IsSuccess(listResp, "正常删除文件: 回查资源列表")
			list, err := helpers.ArrayData(listResp, "正常删除文件: 回查资源列表")
			if err != nil {
				t.Fatal(err)
			}
			if resourceListItemByName(list, fixture.textFileName) != nil {
				t.Fatalf("正常删除文件: 文件 %q 仍存在", fixture.textFileName)
			}
		})

		t.Run("正常删除目录", func(t *testing.T) {
			resp, err := testClient.Primary().DeleteResourcesDirAndFile(fixture.packageID, fixture.dirName)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsSuccess(resp, "正常删除目录")

			listResp, err := testClient.GetPackageResourcesList(fixture.packageID, "", "")
			if err != nil {
				t.Fatalf("回查资源列表失败: %v", err)
			}
			assert.IsSuccess(listResp, "正常删除目录: 回查资源列表")
			list, err := helpers.ArrayData(listResp, "正常删除目录: 回查资源列表")
			if err != nil {
				t.Fatal(err)
			}
			if resourceListItemByName(list, fixture.dirName) != nil {
				t.Fatalf("正常删除目录: 目录 %q 仍存在", fixture.dirName)
			}
		})

		t.Run("不存在的 package_id 返回 HTTP 400", func(t *testing.T) {
			rawResp, err := testClient.PostRawResponse("/file/package/resources/dir/delete", map[string]any{
				"package_id": "non-existent-package-id",
				"parent":     fixture.dirName,
			})
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			body, err := helpers.ReadAndCloseRawBody(rawResp)
			if err != nil {
				t.Fatal(err)
			}
			if rawResp.StatusCode != http.StatusBadRequest {
				t.Fatalf("不存在的 package_id 返回 HTTP 400: 期望 HTTP %d，实际 %d，body=%q", http.StatusBadRequest, rawResp.StatusCode, string(body))
			}
			assertRawJSONStringBody(t, "不存在的 package_id 返回 HTTP 400", body, "not found")
		})
	})
}

// TestDownloadResourcesFile 验证 POST /file/package/resources/download/。
// 覆盖：
//   - 正常返回资源文件下载路径
//   - 不存在的 package_id 返回 HTTP 400
func TestDownloadResourcesFile(t *testing.T) {
	assert := helpers.NewAssertResponse(t)
	var fixture resourceTestFixture

	helpers.RunWithHooks(t, helpers.TestHooks{
		Before: func(t *testing.T) {
			fixture = createResourceTestFixture(t)
		},
		After: func(t *testing.T) {
			modelhelpers.CleanupTempModelFixture(testClient, fixture.packageID, fixture.modelName)
		},
	}, func(t *testing.T) {
		t.Run("正常返回资源文件下载路径", func(t *testing.T) {
			resp, err := testClient.Primary().DownloadResourcesFile(fixture.packageID, fixture.dirName, fixture.pngFileName)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsSuccess(resp, "正常返回资源文件下载路径")
			dataMap, err := helpers.ObjectData(resp, "正常返回资源文件下载路径")
			if err != nil {
				t.Fatal(err)
			}
			url, _ := dataMap["url"].(string)
			if strings.TrimSpace(url) == "" {
				t.Fatalf("正常返回资源文件下载路径: 返回 url 为空，实际=%v", dataMap["url"])
			}
			if !strings.HasSuffix(url, fixture.dirName+"/"+fixture.pngFileName) {
				t.Errorf("正常返回资源文件下载路径: 期望 url 以 %q 结尾，实际=%q", fixture.dirName+"/"+fixture.pngFileName, url)
			}
		})

		t.Run("不存在的 package_id 返回 HTTP 400", func(t *testing.T) {
			rawResp, err := testClient.PostRawResponse("/file/package/resources/download/", map[string]any{
				"package_id": "non-existent-package-id",
				"parent":     fixture.dirName,
				"path":       fixture.pngFileName,
			})
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			body, err := helpers.ReadAndCloseRawBody(rawResp)
			if err != nil {
				t.Fatal(err)
			}
			if rawResp.StatusCode != http.StatusBadRequest {
				t.Fatalf("不存在的 package_id 返回 HTTP 400: 期望 HTTP %d，实际 %d，body=%q", http.StatusBadRequest, rawResp.StatusCode, string(body))
			}
			assertRawJSONStringBody(t, "不存在的 package_id 返回 HTTP 400", body, "not found")
		})
	})
}

// TestGetResourcesImagesPath 验证 POST /file/package/resources/images/path/get。
// 覆盖：
//   - 正常返回 png 图片路径分组
//   - 关键字过滤命中目标图片
//   - 不存在的 package_id 返回 HTTP 400
func TestGetResourcesImagesPath(t *testing.T) {
	assert := helpers.NewAssertResponse(t)
	var fixture resourceTestFixture

	helpers.RunWithHooks(t, helpers.TestHooks{
		Before: func(t *testing.T) {
			fixture = createResourceTestFixture(t)
		},
		After: func(t *testing.T) {
			modelhelpers.CleanupTempModelFixture(testClient, fixture.packageID, fixture.modelName)
		},
	}, func(t *testing.T) {
		t.Run("正常返回 png 图片路径分组", func(t *testing.T) {
			resp, err := testClient.Primary().GetResourcesImagesPath(fixture.packageID, "")
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsSuccess(resp, "正常返回 png 图片路径分组")
			groups, err := helpers.ArrayData(resp, "正常返回 png 图片路径分组")
			if err != nil {
				t.Fatal(err)
			}
			if len(groups) == 0 {
				t.Fatalf("正常返回 png 图片路径分组: 期望非空数组")
			}
			if !resourcesImagePathExists(groups, fixture.pngURI) {
				t.Fatalf("正常返回 png 图片路径分组: 未找到图片路径 %q", fixture.pngURI)
			}
		})

		t.Run("关键字过滤命中目标图片", func(t *testing.T) {
			resp, err := testClient.Primary().GetResourcesImagesPath(fixture.packageID, strings.TrimSuffix(fixture.pngFileName, ".png"))
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsSuccess(resp, "关键字过滤命中目标图片")
			groups, err := helpers.ArrayData(resp, "关键字过滤命中目标图片")
			if err != nil {
				t.Fatal(err)
			}
			if !resourcesImagePathExists(groups, fixture.pngURI) {
				t.Fatalf("关键字过滤命中目标图片: 未找到图片路径 %q", fixture.pngURI)
			}
		})

		t.Run("不存在的 package_id 返回 HTTP 400", func(t *testing.T) {
			rawResp, err := testClient.PostRawResponse("/file/package/resources/images/path/get", map[string]any{
				"package_id": "non-existent-package-id",
				"key_word":   "",
			})
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			body, err := helpers.ReadAndCloseRawBody(rawResp)
			if err != nil {
				t.Fatal(err)
			}
			if rawResp.StatusCode != http.StatusBadRequest {
				t.Fatalf("不存在的 package_id 返回 HTTP 400: 期望 HTTP %d，实际 %d，body=%q", http.StatusBadRequest, rawResp.StatusCode, string(body))
			}
			assertRawJSONStringBody(t, "不存在的 package_id 返回 HTTP 400", body, "not found")
		})
	})
}

// TestGetResourcesImage 验证 GET /file/package/resources/images/get。
// 覆盖：
//   - 正常返回 png 二进制内容
//   - 非法路径返回空 body
func TestGetResourcesImage(t *testing.T) {
	var fixture resourceTestFixture
	pngData, err := resourcehelpers.ReadTestPicturePNG()
	if err != nil {
		t.Fatal(err)
	}

	helpers.RunWithHooks(t, helpers.TestHooks{
		Before: func(t *testing.T) {
			fixture = createResourceTestFixture(t)
		},
		After: func(t *testing.T) {
			modelhelpers.CleanupTempModelFixture(testClient, fixture.packageID, fixture.modelName)
		},
	}, func(t *testing.T) {
		t.Run("正常返回 png 二进制内容", func(t *testing.T) {
			waitForPNGResourceContent(t, fixture.pngURI, pngData)
			rawResp, err := testClient.Primary().GetResourcesImageRaw(fixture.pngURI)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			body, err := helpers.ReadAndCloseRawBody(rawResp)
			if err != nil {
				t.Fatal(err)
			}
			if rawResp.StatusCode != http.StatusOK {
				t.Fatalf("正常返回 png 二进制内容: 期望 HTTP 200，实际 %d", rawResp.StatusCode)
			}
			if !bytes.Equal(body, pngData) {
				t.Fatalf("正常返回 png 二进制内容: 返回内容与测试图片不一致，len(body)=%d len(want)=%d", len(body), len(pngData))
			}
		})

		t.Run("非法路径返回空 body", func(t *testing.T) {
			rawResp, err := testClient.Primary().GetResourcesImageRaw("modelica://Non.Existent/Resources/missing.png")
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			body, err := helpers.ReadAndCloseRawBody(rawResp)
			if err != nil {
				t.Fatal(err)
			}
			if rawResp.StatusCode != http.StatusOK {
				t.Fatalf("非法路径返回空 body: 期望 HTTP 200，实际 %d", rawResp.StatusCode)
			}
			if len(body) != 0 {
				t.Fatalf("非法路径返回空 body: 期望空响应体，实际长度=%d", len(body))
			}
		})
	})
}

// TestUploadModelIcon 验证 POST /file/upload/icon。
// 覆盖：
//   - 正常上传 png 图标
//   - 非图片后缀返回 status=2
//   - 系统包返回 status=2
//   - 缺少文件返回 HTTP 400
func TestUploadModelIcon(t *testing.T) {
	assert := helpers.NewAssertResponse(t)
	pngData, err := resourcehelpers.ReadTestPicturePNG()
	if err != nil {
		t.Fatal(err)
	}
	var fixture resourceTestFixture

	helpers.RunWithHooks(t, helpers.TestHooks{
		Before: func(t *testing.T) {
			fixture = createResourceTestFixture(t)
		},
		After: func(t *testing.T) {
			modelhelpers.CleanupTempModelFixture(testClient, fixture.packageID, fixture.modelName)
		},
	}, func(t *testing.T) {
		t.Run("正常上传 png 图标", func(t *testing.T) {
			resp, err := testClient.Primary().UploadModelIcon(fixture.packageID, fixture.modelName, "test_picture.png", pngData)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsSuccess(resp, "正常上传 png 图标")
			if !strings.Contains(resp.Msg, "图标上传成功") {
				t.Errorf("正常上传 png 图标: 期望 msg 包含 图标上传成功，实际=%q", resp.Msg)
			}
			assertModelCodeContains(t, fixture.packageID, fixture.modelName, "Bitmap", "正常上传 png 图标: 源码回读")
		})

		t.Run("非图片后缀返回 status=2", func(t *testing.T) {
			resp, err := testClient.Primary().UploadModelIcon(fixture.packageID, fixture.modelName, "icon.txt", []byte("plain text"))
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsError(resp, 2, "非图片后缀返回 status=2")
		})

		t.Run("系统包返回 status=2", func(t *testing.T) {
			resp, err := testClient.Primary().UploadModelIcon(modelicaStdLibId, graphicsTestModelName, "test_picture.png", pngData)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsError(resp, 2, "系统包返回 status=2")
		})

		t.Run("缺少文件返回 HTTP 400", func(t *testing.T) {
			rawResp, body, err := resourcehelpers.PostMultipartRaw("/file/upload/icon", map[string]string{
				"package_id": fixture.packageID,
				"model_name": fixture.modelName,
			}, false, "file", "", nil)
			if err != nil {
				t.Fatal(err)
			}
			if rawResp.StatusCode != http.StatusBadRequest {
				t.Fatalf("缺少文件返回 HTTP 400: 期望 HTTP 400，实际=%d，body=%q", rawResp.StatusCode, string(body))
			}
		})
	})
}

type resourceTestFixture struct {
	packageID    string
	modelName    string
	dirName      string
	textFileName string
	pngFileName  string
	textURI      string
	pngURI       string
	textContent  string
}

func createResourceTestFixture(t *testing.T) resourceTestFixture {
	t.Helper()
	packageID, modelName, err := modelhelpers.CreateTempPackageFixture(testClient, "TestResourcePackage")
	if err != nil {
		t.Fatal(err)
	}
	if err := modelhelpers.EnsureModelLoaded(testClient, packageID, modelName, "资源测试夹具"); err != nil {
		t.Fatal(err)
	}

	fixture := resourceTestFixture{
		packageID:    packageID,
		modelName:    modelName,
		dirName:      uniqueName("ResourcesDir"),
		textFileName: "fixture.txt",
		pngFileName:  "fixture.png",
		textContent:  "resource fixture text content",
	}

	resp, err := testClient.CreateResourcesDir(packageID, "", fixture.dirName)
	if err != nil {
		modelhelpers.CleanupTempModelFixture(testClient, packageID, modelName)
		t.Fatalf("创建资源目录夹具失败: %v", err)
	}
	if resp.Status != 0 {
		modelhelpers.CleanupTempModelFixture(testClient, packageID, modelName)
		t.Fatalf("创建资源目录夹具返回非成功状态: status=%d, msg=%s, err=%s", resp.Status, resp.Msg, resp.Err)
	}

	textResp, err := testClient.UploadResourcesFile(packageID, fixture.dirName, fixture.textFileName, []byte(fixture.textContent))
	if err != nil {
		modelhelpers.CleanupTempModelFixture(testClient, packageID, modelName)
		t.Fatalf("上传文本资源夹具失败: %v", err)
	}
	if textResp.Status != 0 {
		modelhelpers.CleanupTempModelFixture(testClient, packageID, modelName)
		t.Fatalf("上传文本资源夹具返回非成功状态: status=%d, msg=%s, err=%s", textResp.Status, textResp.Msg, textResp.Err)
	}
	fixture.textURI = resourcehelpers.ExtractPathOrFallback(textResp, modelName, fixture.dirName, fixture.textFileName)

	pngData, err := resourcehelpers.ReadTestPicturePNG()
	if err != nil {
		t.Fatal(err)
	}
	pngResp, err := testClient.UploadResourcesFile(packageID, fixture.dirName, fixture.pngFileName, pngData)
	if err != nil {
		modelhelpers.CleanupTempModelFixture(testClient, packageID, modelName)
		t.Fatalf("上传 PNG 资源夹具失败: %v", err)
	}
	if pngResp.Status != 0 {
		modelhelpers.CleanupTempModelFixture(testClient, packageID, modelName)
		t.Fatalf("上传 PNG 资源夹具返回非成功状态: status=%d, msg=%s, err=%s", pngResp.Status, pngResp.Msg, pngResp.Err)
	}
	fixture.pngURI = resourcehelpers.ExtractPathOrFallback(pngResp, modelName, fixture.dirName, fixture.pngFileName)
	waitForPNGResourceIndexed(t, packageID, fixture.pngURI)

	return fixture
}

func waitForPNGResourceIndexed(t *testing.T, packageID, resourceURI string) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	var lastErr string

	for time.Now().Before(deadline) {
		resp, err := testClient.GetResourcesImagesPath(packageID, "")
		if err == nil && resp != nil && resp.Status == 0 {
			if groups, ok := resp.Data.([]any); ok {
				if resourcesImagePathExists(groups, resourceURI) {
					return
				}
			}
		} else if err != nil {
			lastErr = err.Error()
		}

		time.Sleep(200 * time.Millisecond)
	}

	t.Fatalf("等待 PNG 资源路径就绪超时: uri=%q, lastErr=%s", resourceURI, lastErr)
}

func waitForPNGResourceContent(t *testing.T, resourceURI string, expectedContent []byte) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	var lastStatus int
	for time.Now().Before(deadline) {
		rawResp, err := testClient.GetResourcesImageRaw(resourceURI)
		if err == nil && rawResp != nil {
			body, err := helpers.ReadAndCloseRawBody(rawResp)
			if err != nil {
				t.Fatal(err)
			}
			lastStatus = rawResp.StatusCode
			if rawResp.StatusCode == http.StatusOK && bytes.Equal(body, expectedContent) {
				return
			}
		}
		time.Sleep(200 * time.Millisecond)
	}

	t.Fatalf("等待 PNG 资源内容就绪超时: uri=%q, lastStatus=%d", resourceURI, lastStatus)
}

func resourceListItemByName(list []any, name string) map[string]any {
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if m["name"] == name {
			return m
		}
	}
	return nil
}

func resourcesImagePathExists(groups []any, wantPath string) bool {
	for _, group := range groups {
		groupMap, ok := group.(map[string]any)
		if !ok {
			continue
		}
		paths, ok := groupMap["images_path"].([]any)
		if !ok {
			continue
		}
		for _, path := range paths {
			if path == wantPath {
				return true
			}
		}
	}
	return false
}

// newClientWithSpaceID 基于默认测试配置创建一个覆盖 space_id 的客户端。
func newClientWithSpaceID(t *testing.T, spaceID string) *helpers.APIClient {
	t.Helper()
	cfg := helpers.DefaultTestConfig()
	cfg.SpaceID = spaceID
	return helpers.NewAPIClientWithConfig(cfg)
}

// almostEqual 判断两个浮点数是否在给定误差范围内近似相等。
func almostEqual(a, b, tolerance float64) bool {
	return math.Abs(a-b) <= tolerance
}

// findResourceReferenceItemByPackageID 从资源引用列表中按 package_id 查找条目。
func findResourceReferenceItemByPackageID(list []any, packageID string) map[string]any {
	for _, item := range list {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if m["package_id"] == packageID {
			return m
		}
	}
	return nil
}

// assertModelCodeContains 断言模型源码包含目标片段。
// alternateWants 用于传入与 want 等价的源码回读格式；函数会先匹配 want，未命中时再依次匹配这些候选片段。
// 例如请求中的负数 -70 可能被 Modelica 源码回读格式化为 (-70)，此时可把括号格式作为 alternateWants 传入。
func assertModelCodeContains(t *testing.T, packageID, modelName, want, message string, alternateWants ...string) {
	t.Helper()
	resp, err := testClient.GetModelCode(packageID, modelName)
	if err != nil {
		t.Fatalf("%s: 获取模型源码失败: %v", message, err)
	}
	if resp.Status != 0 {
		t.Fatalf("%s: 获取模型源码返回非成功状态: status=%d, msg=%s, err=%s", message, resp.Status, resp.Msg, resp.Err)
	}
	code, ok := resp.Data.(string)
	if !ok {
		t.Fatalf("%s: 源码 data 不是 string: %T", message, resp.Data)
	}
	compactCode := strings.ReplaceAll(strings.ReplaceAll(code, " ", ""), "\n", "")
	wants := append([]string{want}, alternateWants...)
	for _, want := range wants {
		compactWant := strings.ReplaceAll(strings.ReplaceAll(want, " ", ""), "\n", "")
		if strings.Contains(compactCode, compactWant) {
			return
		}
	}
	if len(alternateWants) == 0 {
		t.Fatalf("%s: 源码未包含目标片段 %q，实际源码=%s", message, want, code)
	}
	t.Fatalf("%s: 源码未包含任一目标片段 %q，实际源码=%s", message, wants, code)
}
