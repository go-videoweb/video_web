//go:build api_test

package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"yssim-go/tests/helpers"
	modelhelpers "yssim-go/tests/helpers/model"
	resourcehelpers "yssim-go/tests/helpers/resource"
)

// TestUploadModelVarFile 验证 POST /file/upload/var。
// 覆盖：
//   - 正常上传 csv 参数文件
//   - 非法文件后缀返回 status=2
//   - 缺少文件返回 HTTP 400
//   - 不存在的 package_id 返回 HTTP 400
func TestUploadModelVarFile(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var packageID string
	var modelName string
	const componentName = "PI"
	const fileName = "params.csv"
	const fileContent = "time,k\n0,1\n1,2\n"

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			{
				var err error
				packageID, modelName, err = modelhelpers.CreateTempPackageFixture(testClient, "TestResourcePackage")
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := modelhelpers.EnsureModelLoaded(testClient, packageID, modelName, "模型参数文件上传夹具"); err != nil {
				t.Fatal(err)
			}
		},
		After: func(t *testing.T) {
			modelhelpers.CleanupTempModelFixture(testClient, packageID, modelName)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		t.Run("正常上传 csv 参数文件", func(t *testing.T) {
			resp, err := testClient.Primary().UploadModelVarFile(packageID, modelName, componentName, fileName, []byte(fileContent))
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsSuccess(resp, "正常上传 csv 参数文件")

			list, ok := resp.Data.([]any)
			if !ok || len(list) != 1 {
				t.Fatalf("正常上传 csv 参数文件: data 不是单元素数组: %T %v", resp.Data, resp.Data)
			}
			loadExpr, ok := list[0].(string)
			if !ok || !strings.Contains(loadExpr, "Modelica.Utilities.Files.loadResource") {
				t.Fatalf("正常上传 csv 参数文件: 返回表达式不符合预期: %v", list[0])
			}
			if !strings.Contains(loadExpr, fileName) {
				t.Errorf("正常上传 csv 参数文件: 返回表达式未包含文件名 %q，实际=%q", fileName, loadExpr)
			}

			resourceURI := extractLoadResourceURI(t, loadExpr)
			rawResp, err := testClient.GetResourcesFileDataRaw(resourceURI)
			if err != nil {
				t.Fatalf("正常上传 csv 参数文件: 读取资源文件失败: %v", err)
			}
			body, err := helpers.ReadAndCloseRawBody(rawResp)
			if err != nil {
				t.Fatal(err)
			}
			if rawResp.StatusCode != http.StatusOK {
				t.Fatalf("正常上传 csv 参数文件: 期望 HTTP 200，实际=%d，body=%q", rawResp.StatusCode, string(body))
			}
			if string(body) != fileContent {
				t.Fatalf("正常上传 csv 参数文件: 期望资源内容=%q，实际=%q", fileContent, string(body))
			}
		})

		t.Run("非法文件后缀返回 status=2", func(t *testing.T) {
			resp, err := testClient.Primary().UploadModelVarFile(packageID, modelName, componentName, "params.bin", []byte("bad"))
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsError(resp, 2, "非法文件后缀返回 status=2")
		})

		t.Run("缺少文件返回 HTTP 400", func(t *testing.T) {
			rawResp, body, err := resourcehelpers.PostMultipartRaw("/file/upload/var", map[string]string{
				"package_id":     packageID,
				"model_name":     modelName,
				"component_name": componentName,
			}, false, "file", "", nil)
			if err != nil {
				t.Fatal(err)
			}
			if rawResp.StatusCode != http.StatusBadRequest {
				t.Fatalf("缺少文件返回 HTTP 400: 期望 HTTP 400，实际=%d，body=%q", rawResp.StatusCode, string(body))
			}
		})

		t.Run("不存在的 package_id 返回 HTTP 400", func(t *testing.T) {
			rawResp, body, err := resourcehelpers.PostMultipartRaw("/file/upload/var", map[string]string{
				"package_id":     "non-existent-package-id",
				"model_name":     modelName,
				"component_name": componentName,
			}, true, "file", fileName, []byte(fileContent))
			if err != nil {
				t.Fatal(err)
			}
			if rawResp.StatusCode != http.StatusBadRequest {
				t.Fatalf("不存在的 package_id 返回 HTTP 400: 期望 HTTP 400，实际=%d，body=%q", rawResp.StatusCode, string(body))
			}
			assertRawJSONStringBody(t, "不存在的 package_id 返回 HTTP 400", body, "not found")
		})
	})
}

// TestUploadModelPackage 验证 POST /file/upload/package。
// 覆盖：
//   - 正常上传 mo 模型包文件
//   - 重复上传同名包返回 status=2
//   - 非法文件后缀返回 status=2
//   - 缺少文件返回 HTTP 400
func TestUploadModelPackage(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	packageName := uniqueName("UploadPkg")
	fileName := packageName + ".mo"
	moContent := []byte(minimalPackageSource(packageName))
	var uploadedPackageID string

	hooks := helpers.TestHooks{
		After: func(t *testing.T) {
			modelhelpers.CleanupTempModelFixture(testClient, uploadedPackageID, packageName)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		t.Run("正常上传 mo 模型包文件", func(t *testing.T) {
			resp, err := testClient.Primary().UploadModelPackage(fileName, moContent)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsSuccess(resp, "正常上传 mo 模型包文件")
			if !strings.Contains(resp.Msg, "上传成功") {
				t.Errorf("正常上传 mo 模型包文件: 期望 msg 包含 上传成功，实际=%q", resp.Msg)
			}

			packageID := findPackageIDByName(t, packageName, "正常上传 mo 模型包文件")
			if packageID == "" {
				t.Fatalf("正常上传 mo 模型包文件: 未找到上传后的 package_id")
			}
			uploadedPackageID = packageID
		})

		t.Run("重复上传同名包返回 status=2", func(t *testing.T) {
			resp, err := testClient.Primary().UploadModelPackage(fileName, moContent)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsError(resp, 2, "重复上传同名包返回 status=2")
			expectedErr := "当前模型库列表中已存在同名模型库 " + packageName
			if !strings.Contains(resp.Err, expectedErr) {
				t.Errorf("重复上传同名包返回 status=2: 期望 err 包含 %q，实际=%q", expectedErr, resp.Err)
			}
		})

		t.Run("非法文件后缀返回 status=2", func(t *testing.T) {
			resp, err := testClient.Primary().UploadModelPackage(packageName+".txt", moContent)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsError(resp, 2, "非法文件后缀返回 status=2")
			if !strings.Contains(resp.Msg, "mo或zip格式的文件") {
				t.Errorf("非法文件后缀返回 status=2: 期望 msg 包含 mo或zip格式的文件，实际=%q", resp.Msg)
			}
		})

		t.Run("缺少文件返回 HTTP 400", func(t *testing.T) {
			rawResp, body, err := resourcehelpers.PostMultipartRaw("/file/upload/package", nil, false, "file", "", nil)
			if err != nil {
				t.Fatal(err)
			}
			if rawResp.StatusCode != http.StatusBadRequest {
				t.Fatalf("缺少文件返回 HTTP 400: 期望 HTTP 400，实际=%d，body=%q", rawResp.StatusCode, string(body))
			}
		})
	})
}

// TestExportFmu 验证 POST /file/fmu/export。
// 覆盖正常导出 FMU、模型不存在的业务错误，以及缺少必填参数的绑定错误。
func TestExportFmu(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	helpers.RunWithHooks(t, helpers.TestHooks{}, func(t *testing.T) {
		t.Run("正常导出 FMU 返回文件 URL", func(t *testing.T) {
			if err := modelhelpers.EnsureSystemModelLoaded(testClient, modelicaStdLibId, "", "FMU 导出前置加载 Modelica"); err != nil {
				t.Fatal(err)
			}

			resp := exportFmuJSON(t, map[string]any{
				"solver":         "CVODE",
				"package_id":     modelicaStdLibId,
				"package_name":   "Modelica",
				"model_name":     testModelName,
				"fmu_name":       "api_test_pid_controller_" + time.Now().Format("20060102150405"),
				"fmuPar":         defaultExportFmuParameters(),
				"download_local": true,
			})
			assert.IsSuccess(resp, "正常导出 FMU 返回文件 URL")

			dataMap, ok := resp.Data.(map[string]any)
			if !ok {
				t.Fatalf("正常导出 FMU 返回文件 URL: data 不是 object: %T", resp.Data)
			}
			url, ok := dataMap["url"].(string)
			if !ok || url == "" {
				t.Fatalf("正常导出 FMU 返回文件 URL: data.url 为空或类型错误: %v", dataMap["url"])
			}
			if !strings.HasSuffix(strings.ToLower(url), ".fmu") {
				t.Fatalf("正常导出 FMU 返回文件 URL: 期望 .fmu 文件，实际 url=%q", url)
			}
		})

		t.Run("不存在的模型返回 status=2", func(t *testing.T) {
			resp := exportFmuJSON(t, map[string]any{
				"solver":         "CVODE",
				"package_id":     modelicaStdLibId,
				"package_name":   "Modelica",
				"model_name":     "Non.Existent.Model.For.FmuExport",
				"fmu_name":       "api_test_missing_model",
				"fmuPar":         defaultExportFmuParameters(),
				"download_local": true,
			})
			assert.IsError(resp, 2, "不存在的模型返回 status=2")
		})

		t.Run("缺少必填参数返回 HTTP 400", func(t *testing.T) {
			rawResp, err := testClient.Primary().ExportFmuRaw(map[string]any{
				"model_name": testModelName,
			})
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			body, err := helpers.ReadAndCloseRawBody(rawResp)
			if err != nil {
				t.Fatal(err)
			}
			if rawResp.StatusCode != http.StatusBadRequest {
				t.Fatalf("缺少必填参数返回 HTTP 400: 期望 HTTP 400，实际=%d，body=%q", rawResp.StatusCode, string(body))
			}
			if len(body) == 0 {
				t.Fatalf("缺少必填参数返回 HTTP 400: 响应体为空")
			}
		})
	})
}

// defaultExportFmuParameters 返回后端内部 FMU 导出流程使用的默认参数。
func defaultExportFmuParameters() map[string]any {
	return map[string]any{
		"fmiType":       "all",
		"fmiVersion":    "2",
		"includeImage":  0,
		"includeSource": false,
		"storeResult":   false,
	}
}

// exportFmuJSON 调用 FMU 导出接口并解析标准 JSON 响应。
func exportFmuJSON(t *testing.T, body map[string]any) *helpers.Response {
	t.Helper()
	rawResp, err := testClient.Primary().ExportFmuRaw(body)
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	respBody, err := helpers.ReadAndCloseRawBody(rawResp)
	if err != nil {
		t.Fatal(err)
	}
	if rawResp.StatusCode != http.StatusOK {
		t.Fatalf("FMU 导出接口期望 HTTP 200，实际=%d，body=%q", rawResp.StatusCode, string(respBody))
	}
	var resp helpers.Response
	if err := json.Unmarshal(respBody, &resp); err != nil {
		t.Fatalf("FMU 导出接口响应不是 JSON: %v，body=%q", err, string(respBody))
	}
	return &resp
}

func extractLoadResourceURI(t *testing.T, loadExpr string) string {
	t.Helper()
	const prefix = "loadResource(\""
	start := strings.Index(loadExpr, prefix)
	if start == -1 {
		t.Fatalf("loadResource 表达式缺少前缀: %q", loadExpr)
	}
	start += len(prefix)
	end := strings.Index(loadExpr[start:], "\"")
	if end == -1 {
		t.Fatalf("loadResource 表达式缺少结束引号: %q", loadExpr)
	}
	return loadExpr[start : start+end]
}

func minimalPackageSource(packageName string) string {
	return "package " + packageName + "\n  annotation(version=\"1.0.0\");\nend " + packageName + ";\n"
}

func findPackageIDByName(t *testing.T, packageName, message string) string {
	t.Helper()
	resp, err := testClient.GetPackageFileList()
	if err != nil {
		t.Fatalf("%s: 获取包文件列表失败: %v", message, err)
	}
	if resp.Status != 0 {
		t.Fatalf("%s: 获取包文件列表返回非成功状态: status=%d, msg=%s, err=%s", message, resp.Status, resp.Msg, resp.Err)
	}
	list, ok := resp.Data.([]any)
	if !ok {
		t.Fatalf("%s: data 不是数组: %T", message, resp.Data)
	}
	for _, item := range list {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if itemMap["package_name"] == packageName {
			if packageID, ok := itemMap["package_id"].(string); ok {
				return packageID
			}
		}
	}
	return ""
}
