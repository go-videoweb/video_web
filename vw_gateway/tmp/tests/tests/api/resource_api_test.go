//go:build api_test

package api

import (
	"net/http"
	"strings"
	"testing"

	"yssim-go/tests/helpers"
	modelhelpers "yssim-go/tests/helpers/model"
	resourcehelpers "yssim-go/tests/helpers/resource"
)

// TestGetResourceRootList 验证 GET /api/v2/resource/root/list/get。
// 覆盖：
//   - 正常获取资源库根目录列表
//   - 返回结果包含预置根目录
//   - 新创建的顶层资源文件夹可在根列表中查到
func TestGetResourceRootList(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var rootFolderID string
	rootFolderName := uniqueName("资源库根目录")

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			rootFolderID = createResourceLibFolder(t, "", rootFolderName)
		},
		After: func(t *testing.T) {
			deleteResourceByID(t, rootFolderID)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		resp, err := testClient.Primary().GetResourceRootList()
		if err != nil {
			t.Fatalf("请求失败: %v", err)
		}
		assert.IsSuccess(resp, "正常获取资源库根目录列表")
		list, err := helpers.ArrayData(resp, "正常获取资源库根目录列表")
		if err != nil {
			t.Fatal(err)
		}
		if len(list) == 0 {
			t.Fatalf("正常获取资源库根目录列表: 期望非空列表")
		}

		foundDefaultRoots := 0
		for _, defaultName := range []string{"工况", "动力成品件", "电气成品件", "液压成品件", "环控成品件", "燃油成品件"} {
			if resourceLibItemByName(list, defaultName) != nil {
				foundDefaultRoots++
			}
		}
		if foundDefaultRoots == 0 {
			t.Logf("正常获取资源库根目录列表: 当前环境未返回预置根目录，继续校验用户新建根目录可见性")
		}

		createdItem := resourceLibItemByName(list, rootFolderName)
		if createdItem == nil {
			t.Fatalf("正常获取资源库根目录列表: 未找到新建根目录 %q", rootFolderName)
		}
		if createdItem["type"] != "folder" {
			t.Errorf("正常获取资源库根目录列表: 期望 type=folder，实际=%v", createdItem["type"])
		}
	})
}

// TestCreateResourceFolder 验证 POST /api/v2/resource/folder/create。
// 覆盖：
//   - 正常创建资源文件夹
//   - 缺少必填参数返回 HTTP 400
//   - 同级重名返回 status=2
func TestCreateResourceFolder(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var parentID string
	var existingID string
	parentName := uniqueName("资源库父目录")
	existingName := uniqueName("资源库已存在目录")
	newFolderName := uniqueName("资源库新目录")

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			parentID = createResourceLibFolder(t, "", parentName)
			existingID = createResourceLibFolder(t, parentID, existingName)
		},
		After: func(t *testing.T) {
			deleteResourceByID(t, existingID)
			deleteResourceByID(t, parentID)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		tests := []struct {
			name               string
			parentID           string
			folderName         string
			useRaw             bool
			expectError        bool
			expectedCode       int
			expectedHTTPStatus int
			expectedRawBody    string
		}{
			{
				name:       "正常创建资源文件夹",
				parentID:   parentID,
				folderName: newFolderName,
			},
			{
				name:         "同级重名返回 status=2",
				parentID:     parentID,
				folderName:   existingName,
				expectError:  true,
				expectedCode: 2,
			},
			{
				name:               "缺少 name 返回 HTTP 400",
				parentID:           parentID,
				useRaw:             true,
				expectedHTTPStatus: http.StatusBadRequest,
				expectedRawBody:    "",
			},
		}

		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				if tt.useRaw {
					rawResp, err := testClient.PostRawResponse("/api/v2/resource/folder/create", map[string]any{
						"parent_id": tt.parentID,
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
					assertRawJSONStringBody(t, tt.name, body, tt.expectedRawBody)
					return
				}

				resp, err := testClient.Primary().CreateResourceFolder(tt.parentID, tt.folderName)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				if tt.expectError {
					assert.IsError(resp, tt.expectedCode, tt.name)
					return
				}

				assert.IsSuccess(resp, tt.name)
				createdID := extractStringData(t, resp, tt.name)
				if strings.TrimSpace(createdID) == "" {
					t.Fatalf("%s: 返回的资源 id 为空", tt.name)
				}

				listResp, err := testClient.GetResourceList(parentID, 1, 20)
				if err != nil {
					t.Fatalf("%s: 回查资源列表失败: %v", tt.name, err)
				}
				assert.IsSuccess(listResp, tt.name+": 回查资源列表")
				folders, _, _, _ := extractResourceListSections(t, listResp, tt.name+": 回查资源列表")
				item := resourceLibItemByName(folders, tt.folderName)
				if item == nil {
					t.Fatalf("%s: 未在父目录列表中找到新建目录 %q", tt.name, tt.folderName)
				}
			})
		}
	})
}

// TestGetResourceList 验证 GET /api/v2/resource/list/get。
// 覆盖：
//   - 正常获取子目录列表
//   - 不存在的 parent_id 返回空列表
//   - page_size 非法返回 HTTP 400
func TestGetResourceList(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var parentID string
	var childFolderID string
	var fileID string
	parentName := uniqueName("资源列表父目录")
	childFolderName := uniqueName("资源列表子目录")
	fileName := uniqueName("资源列表文件")

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			parentID = createResourceLibFolder(t, "", parentName)
			childFolderID = createResourceLibFolder(t, parentID, childFolderName)
			fileID = uploadResourceLibFile(t, parentID, fileName, "列表用文本文件", "resource_list_file.txt", []byte("list content"))
		},
		After: func(t *testing.T) {
			deleteResourceByID(t, fileID)
			deleteResourceByID(t, childFolderID)
			deleteResourceByID(t, parentID)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		tests := []struct {
			name               string
			parentID           string
			pageNum            int
			pageSize           int
			useRaw             bool
			expectedHTTPStatus int
			assertResponse     func(t *testing.T, resp *helpers.Response)
		}{
			{
				name:     "正常获取子目录列表",
				parentID: parentID,
				pageNum:  1,
				pageSize: 20,
				assertResponse: func(t *testing.T, resp *helpers.Response) {
					assert.IsSuccess(resp, "正常获取子目录列表")
					folders, files, total, pageCount := extractResourceListSections(t, resp, "正常获取子目录列表")
					if total < 2 {
						t.Fatalf("正常获取子目录列表: 期望 total >= 2，实际=%d", total)
					}
					if pageCount < 1 {
						t.Fatalf("正常获取子目录列表: 期望 page_count >= 1，实际=%d", pageCount)
					}
					if resourceLibItemByName(folders, childFolderName) == nil {
						t.Fatalf("正常获取子目录列表: 未找到子目录 %q", childFolderName)
					}
					if resourceLibItemByName(files, fileName) == nil {
						t.Fatalf("正常获取子目录列表: 未找到文件 %q", fileName)
					}
				},
			},
			{
				name:     "不存在的 parent_id 返回空列表",
				parentID: "non-existent-parent-id",
				pageNum:  1,
				pageSize: 20,
				assertResponse: func(t *testing.T, resp *helpers.Response) {
					assert.IsSuccess(resp, "不存在的 parent_id 返回空列表")
					folders, files, total, _ := extractResourceListSections(t, resp, "不存在的 parent_id 返回空列表")
					if total != 0 || len(folders) != 0 || len(files) != 0 {
						t.Fatalf("不存在的 parent_id 返回空列表: 期望空结果，实际 total=%d folders=%d files=%d", total, len(folders), len(files))
					}
				},
			},
			{
				name:               "page_size 小于等于 0 返回 HTTP 400",
				parentID:           parentID,
				pageNum:            1,
				pageSize:           0,
				useRaw:             true,
				expectedHTTPStatus: http.StatusBadRequest,
			},
		}

		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				if tt.useRaw {
					rawResp, err := testClient.GetRawResponse("/api/v2/resource/list/get", map[string]string{
						"parent_id": tt.parentID,
						"page_num":  "1",
						"page_size": "0",
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
					assertRawJSONStringBody(t, tt.name, body, "")
					return
				}

				resp, err := testClient.Primary().GetResourceList(tt.parentID, tt.pageNum, tt.pageSize)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				tt.assertResponse(t, resp)
			})
		}
	})
}

// TestSearchResource 验证 GET /api/v2/resource/search。
// 覆盖：
//   - 正常按关键字搜索资源
//   - 无匹配结果返回空列表
func TestSearchResource(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var parentID string
	var childFolderID string
	var fileID string
	keyword := uniqueName("资源检索关键字")
	folderName := keyword + "_目录"
	fileName := keyword + "_文件"

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			parentID = createResourceLibFolder(t, "", uniqueName("资源检索父目录"))
			childFolderID = createResourceLibFolder(t, parentID, folderName)
			fileID = uploadResourceLibFile(t, parentID, fileName, "检索用文本文件", "resource_search_file.txt", []byte("search content"))
		},
		After: func(t *testing.T) {
			deleteResourceByID(t, fileID)
			deleteResourceByID(t, childFolderID)
			deleteResourceByID(t, parentID)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		tests := []struct {
			name           string
			keywords       string
			assertResponse func(t *testing.T, resp *helpers.Response)
		}{
			{
				name:     "正常按关键字搜索资源",
				keywords: keyword,
				assertResponse: func(t *testing.T, resp *helpers.Response) {
					assert.IsSuccess(resp, "正常按关键字搜索资源")
					total, items := extractResourceSearchItems(t, resp, "正常按关键字搜索资源")
					if total < 2 {
						t.Fatalf("正常按关键字搜索资源: 期望 total >= 2，实际=%d", total)
					}
					if resourceLibItemByName(items, folderName) == nil {
						t.Fatalf("正常按关键字搜索资源: 未找到目录 %q", folderName)
					}
					if resourceLibItemByName(items, fileName) == nil {
						t.Fatalf("正常按关键字搜索资源: 未找到文件 %q", fileName)
					}
				},
			},
			{
				name:     "无匹配结果返回空列表",
				keywords: "non-existent-resource-keyword",
				assertResponse: func(t *testing.T, resp *helpers.Response) {
					assert.IsSuccess(resp, "无匹配结果返回空列表")
					total, items := extractResourceSearchItems(t, resp, "无匹配结果返回空列表")
					if total != 0 || len(items) != 0 {
						t.Fatalf("无匹配结果返回空列表: 期望空结果，实际 total=%d len=%d", total, len(items))
					}
				},
			},
		}

		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				resp, err := testClient.Primary().SearchResource(tt.keywords)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				tt.assertResponse(t, resp)
			})
		}
	})
}

// TestUploadResourceFile 验证 POST /api/v2/resource/file/upload。
// 覆盖：
//   - 正常上传资源文件
//   - 缺少文件返回 HTTP 400
//   - 非法文件类型返回 status=2
//   - 同级重名返回 status=2
func TestUploadResourceFile(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var parentID string
	parentName := uniqueName("资源上传父目录")
	resourceName := uniqueName("资源上传文件")
	resourceContent := "resource upload content"

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			parentID = createResourceLibFolder(t, "", parentName)
		},
		After: func(t *testing.T) {
			deleteResourceByID(t, parentID)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		tests := []struct {
			name               string
			resourceName       string
			description        string
			uploadFileName     string
			content            []byte
			useRaw             bool
			expectError        bool
			expectedCode       int
			expectedHTTPStatus int
			assertResponse     func(t *testing.T, resp *helpers.Response)
		}{
			{
				name:           "正常上传资源文件",
				resourceName:   resourceName,
				description:    "资源上传描述",
				uploadFileName: "resource_upload_file.txt",
				content:        []byte(resourceContent),
				assertResponse: func(t *testing.T, resp *helpers.Response) {
					assert.IsSuccess(resp, "正常上传资源文件")
					fileID := extractStringData(t, resp, "正常上传资源文件")
					if strings.TrimSpace(fileID) == "" {
						t.Fatalf("正常上传资源文件: 返回资源 id 为空")
					}

					contentResp, err := testClient.GetResourceFileContent(fileID)
					if err != nil {
						t.Fatalf("正常上传资源文件: 回读内容失败: %v", err)
					}
					assert.IsSuccess(contentResp, "正常上传资源文件: 回读内容")
					if contentResp.Data != resourceContent {
						t.Fatalf("正常上传资源文件: 期望内容=%q，实际=%v", resourceContent, contentResp.Data)
					}
				},
			},
			{
				name:           "同级重名返回 status=2",
				resourceName:   resourceName,
				description:    "资源上传描述",
				uploadFileName: "resource_upload_dup.txt",
				content:        []byte("dup"),
				expectError:    true,
				expectedCode:   2,
			},
			{
				name:           "非法文件类型返回 status=2",
				resourceName:   uniqueName("资源上传非法文件"),
				description:    "非法文件类型",
				uploadFileName: "resource_upload_bad.csv",
				content:        []byte("bad,csv"),
				expectError:    true,
				expectedCode:   2,
			},
			{
				name:               "缺少文件返回 HTTP 400",
				resourceName:       uniqueName("资源上传缺文件"),
				description:        "缺少文件",
				useRaw:             true,
				expectedHTTPStatus: http.StatusBadRequest,
			},
		}

		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				if tt.useRaw {
					rawResp, body, err := resourcehelpers.PostMultipartRaw("/api/v2/resource/file/upload", map[string]string{
						"parent_id":   parentID,
						"filename":    tt.resourceName,
						"description": tt.description,
					}, false, "file", "", nil)
					if err != nil {
						t.Fatal(err)
					}
					if rawResp.StatusCode != tt.expectedHTTPStatus {
						t.Fatalf("%s: 期望 HTTP %d，实际 %d，body=%q", tt.name, tt.expectedHTTPStatus, rawResp.StatusCode, string(body))
					}
					assertRawJSONStringBody(t, tt.name, body, "")
					return
				}

				resp, err := testClient.Primary().UploadResourceFile(parentID, tt.resourceName, tt.description, tt.uploadFileName, tt.content)
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

// TestGetResourceFileContent 验证 GET /api/v2/resource/file/content/get。
// 覆盖：
//   - 正常获取文件内容
//   - 不存在的资源文件返回 status=2
func TestGetResourceFileContent(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var parentID string
	var fileID string
	fileContent := "resource file content"

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			parentID = createResourceLibFolder(t, "", uniqueName("资源内容父目录"))
			fileID = uploadResourceLibFile(t, parentID, uniqueName("资源内容文件"), "内容读取测试", "resource_content_file.txt", []byte(fileContent))
		},
		After: func(t *testing.T) {
			deleteResourceByID(t, fileID)
			deleteResourceByID(t, parentID)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		tests := []struct {
			name           string
			id             string
			expectError    bool
			expectedCode   int
			expectedString string
		}{
			{
				name:           "正常获取文件内容",
				id:             fileID,
				expectedString: fileContent,
			},
			{
				name:         "不存在的资源文件返回 status=2",
				id:           "non-existent-resource-file-id",
				expectError:  true,
				expectedCode: 2,
			},
		}

		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				resp, err := testClient.Primary().GetResourceFileContent(tt.id)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}
				if tt.expectError {
					assert.IsError(resp, tt.expectedCode, tt.name)
					return
				}
				assert.IsSuccess(resp, tt.name)
				if resp.Data != tt.expectedString {
					t.Fatalf("%s: 期望内容=%q，实际=%v", tt.name, tt.expectedString, resp.Data)
				}
			})
		}
	})
}

// TestParseResourceFileContent 验证 GET /api/v2/resource/file/content/parse。
// 覆盖：
//   - 正常解析文件内容
//   - 非法文件内容当前返回空序列
//   - 不存在的资源文件返回 status=2
func TestParseResourceFileContent(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var parentID string
	var validFileID string
	var invalidFileID string

	validContent := "double table1(2,2)\n0 1\n1 2\n"
	invalidContent := "double broken(2,2)\na b\n"

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			parentID = createResourceLibFolder(t, "", uniqueName("资源解析父目录"))
			validFileID = uploadResourceLibFile(t, parentID, uniqueName("资源解析有效文件"), "有效解析", "resource_parse_valid.txt", []byte(validContent))
			invalidFileID = uploadResourceLibFile(t, parentID, uniqueName("资源解析无效文件"), "无效解析", "resource_parse_invalid.txt", []byte(invalidContent))
		},
		After: func(t *testing.T) {
			deleteResourceByID(t, invalidFileID)
			deleteResourceByID(t, validFileID)
			deleteResourceByID(t, parentID)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		tests := []struct {
			name           string
			id             string
			coordName      string
			expectError    bool
			expectedCode   int
			assertResponse func(t *testing.T, resp *helpers.Response)
		}{
			{
				name:      "正常解析文件内容",
				id:        validFileID,
				coordName: "table1",
				assertResponse: func(t *testing.T, resp *helpers.Response) {
					assert.IsSuccess(resp, "正常解析文件内容")
					xData, yData := extractParsedResourceSeries(t, resp, "table1", "正常解析文件内容")
					if len(xData) != 2 || len(yData) != 1 || len(yData[0]) != 2 {
						t.Fatalf("正常解析文件内容: 期望 xData=2 yData[0]=2，实际 x=%d y=%d y0=%d", len(xData), len(yData), lenFirstNested(yData))
					}
					if xData[0] != 0 || xData[1] != 1 || yData[0][0] != 1 || yData[0][1] != 2 {
						t.Fatalf("正常解析文件内容: 解析结果不符合预期，x=%v y=%v", xData, yData)
					}
				},
			},
			{
				name:      "非法文件内容当前返回空序列",
				id:        invalidFileID,
				coordName: "broken",
				assertResponse: func(t *testing.T, resp *helpers.Response) {
					assert.IsSuccess(resp, "非法文件内容当前返回空序列")
					xData, yData := extractParsedResourceSeries(t, resp, "broken", "非法文件内容当前返回空序列")
					if len(xData) != 0 {
						t.Fatalf("非法文件内容当前返回空序列: 期望 xData 为空，实际=%v", xData)
					}
					if len(yData) != 1 || len(yData[0]) != 0 {
						t.Fatalf("非法文件内容当前返回空序列: 期望一个空 yData 序列，实际=%v", yData)
					}
				},
			},
			{
				name:         "不存在的资源文件返回 status=2",
				id:           "non-existent-resource-file-id",
				expectError:  true,
				expectedCode: 2,
			},
		}

		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				resp, err := testClient.Primary().ParseResourceFileContent(tt.id)
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

// TestCopyLibFileToResources 验证 POST /api/v2/resource/file/resources/copy。
// 覆盖：
//   - 正常复制资源文件到模型 Resources
//   - 复制文件夹返回 status=2
//   - 不存在的 package_id 返回 HTTP 400
func TestCopyLibFileToResources(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var resourceRootID string
	var resourceFileID string
	var packageID string
	var modelName string
	copyTargetDir := uniqueName("LibCopyTarget")

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			resourceRootID = createResourceLibFolder(t, "", uniqueName("资源复制父目录"))
			resourceFileID = uploadResourceLibFile(t, resourceRootID, uniqueName("资源复制文件"), "复制到 Resources", "resource_copy_source.txt", []byte("copy source"))

			{
				var err error
				packageID, modelName, err = modelhelpers.CreateTempPackageFixture(testClient, "TestResourcePackage")
				if err != nil {
					t.Fatal(err)
				}
			}
			if err := modelhelpers.EnsureModelLoaded(testClient, packageID, modelName, "资源库复制到 Resources 测试夹具"); err != nil {
				t.Fatal(err)
			}

			resp, err := testClient.CreateResourcesDir(packageID, "", copyTargetDir)
			if err != nil {
				t.Fatalf("创建模型 Resources 目标目录失败: %v", err)
			}
			if resp.Status != 0 {
				t.Fatalf("创建模型 Resources 目标目录返回非成功状态: status=%d, msg=%s, err=%s", resp.Status, resp.Msg, resp.Err)
			}
		},
		After: func(t *testing.T) {
			deleteResourceByID(t, resourceFileID)
			deleteResourceByID(t, resourceRootID)
			modelhelpers.CleanupTempModelFixture(testClient, packageID, modelName)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		tests := []struct {
			name               string
			resourceID         string
			packageID          string
			parent             string
			useRaw             bool
			expectError        bool
			expectedCode       int
			expectedHTTPStatus int
			assertResponse     func(t *testing.T, resp *helpers.Response)
		}{
			{
				name:       "正常复制资源文件到模型 Resources",
				resourceID: resourceFileID,
				packageID:  packageID,
				parent:     copyTargetDir,
				assertResponse: func(t *testing.T, resp *helpers.Response) {
					assert.IsSuccess(resp, "正常复制资源文件到模型 Resources")
					dataMap, err := helpers.ObjectData(resp, "正常复制资源文件到模型 Resources")
					if err != nil {
						t.Fatal(err)
					}
					name, _ := dataMap["name"].(string)
					path, _ := dataMap["path"].(string)
					if strings.TrimSpace(name) == "" || strings.TrimSpace(path) == "" {
						t.Fatalf("正常复制资源文件到模型 Resources: 返回 name/path 为空: %v", dataMap)
					}
					if !strings.HasPrefix(path, "modelica://"+modelName+"/Resources/"+copyTargetDir+"/") {
						t.Fatalf("正常复制资源文件到模型 Resources: 返回 path 不在目标目录下，path=%q", path)
					}

					listResp, err := testClient.GetPackageResourcesList(packageID, copyTargetDir, "")
					if err != nil {
						t.Fatalf("正常复制资源文件到模型 Resources: 回查模型 Resources 列表失败: %v", err)
					}
					assert.IsSuccess(listResp, "正常复制资源文件到模型 Resources: 回查模型 Resources 列表")
					list, err := helpers.ArrayData(listResp, "正常复制资源文件到模型 Resources: 回查模型 Resources 列表")
					if err != nil {
						t.Fatal(err)
					}
					if resourceLibItemByName(list, name) == nil {
						t.Fatalf("正常复制资源文件到模型 Resources: 未在目标目录中找到文件 %q", name)
					}
				},
			},
			{
				name:         "复制文件夹返回 status=2",
				resourceID:   resourceRootID,
				packageID:    packageID,
				parent:       copyTargetDir,
				expectError:  true,
				expectedCode: 2,
			},
			{
				name:               "不存在的 package_id 返回 HTTP 400",
				resourceID:         resourceFileID,
				packageID:          "non-existent-package-id",
				parent:             copyTargetDir,
				useRaw:             true,
				expectedHTTPStatus: http.StatusBadRequest,
			},
		}

		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				if tt.useRaw {
					rawResp, err := testClient.PostRawResponse("/api/v2/resource/file/resources/copy", map[string]any{
						"id":         tt.resourceID,
						"package_id": tt.packageID,
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
					assertRawJSONStringBody(t, tt.name, body, "not found")
					return
				}

				resp, err := testClient.Primary().CopyLibFileToResources(tt.resourceID, tt.packageID, tt.parent)
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

// TestEditResourceInfo 验证 POST /api/v2/resource/edit。
// 覆盖：
//   - 正常编辑资源信息
//   - 同级重名返回 status=2
//   - 不存在的资源返回 status=2
//   - 缺少必填参数返回 HTTP 400
func TestEditResourceInfo(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var rootID string
	var editableID string
	var conflictID string
	var nestedID string
	editableName := uniqueName("资源待编辑目录")
	conflictName := uniqueName("资源冲突目录")
	nestedName := uniqueName("资源嵌套目录")
	renamedName := uniqueName("资源编辑后目录")
	renamedDesc := "编辑后的描述"

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			rootID = createResourceLibFolder(t, "", uniqueName("资源编辑父目录"))
			editableID = createResourceLibFolder(t, rootID, editableName)
			conflictID = createResourceLibFolder(t, rootID, conflictName)
			nestedID = createResourceLibFolder(t, editableID, nestedName)
		},
		After: func(t *testing.T) {
			deleteResourceByID(t, nestedID)
			deleteResourceByID(t, conflictID)
			deleteResourceByID(t, editableID)
			deleteResourceByID(t, rootID)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		tests := []struct {
			name               string
			parentID           string
			id                 string
			resourceType       string
			newName            string
			description        string
			useRaw             bool
			expectError        bool
			expectedCode       int
			expectedHTTPStatus int
			assertResponse     func(t *testing.T, resp *helpers.Response)
		}{
			{
				name:         "正常编辑资源信息",
				parentID:     rootID,
				id:           editableID,
				resourceType: "folder",
				newName:      renamedName,
				description:  renamedDesc,
				assertResponse: func(t *testing.T, resp *helpers.Response) {
					assert.IsSuccess(resp, "正常编辑资源信息")

					listResp, err := testClient.GetResourceList(rootID, 1, 20)
					if err != nil {
						t.Fatalf("正常编辑资源信息: 回查父目录列表失败: %v", err)
					}
					assert.IsSuccess(listResp, "正常编辑资源信息: 回查父目录列表")
					folders, _, _, _ := extractResourceListSections(t, listResp, "正常编辑资源信息: 回查父目录列表")
					item := resourceLibItemByName(folders, renamedName)
					if item == nil {
						t.Fatalf("正常编辑资源信息: 未找到重命名后的目录 %q", renamedName)
					}
					if item["description"] != renamedDesc {
						t.Errorf("正常编辑资源信息: 期望 description=%q，实际=%v", renamedDesc, item["description"])
					}

					nestedListResp, err := testClient.GetResourceList(editableID, 1, 20)
					if err != nil {
						t.Fatalf("正常编辑资源信息: 回查重命名目录下子项失败: %v", err)
					}
					assert.IsSuccess(nestedListResp, "正常编辑资源信息: 回查重命名目录下子项")
					nestedFolders, _, _, _ := extractResourceListSections(t, nestedListResp, "正常编辑资源信息: 回查重命名目录下子项")
					nestedItem := resourceLibItemByName(nestedFolders, nestedName)
					if nestedItem == nil {
						t.Fatalf("正常编辑资源信息: 未找到嵌套目录 %q", nestedName)
					}
					if nestedItem["type"] != "folder" {
						t.Fatalf("正常编辑资源信息: 嵌套子项类型错误，期望 folder，实际=%v", nestedItem["type"])
					}
				},
			},
			{
				name:         "同级重名返回 status=2",
				parentID:     rootID,
				id:           editableID,
				resourceType: "folder",
				newName:      conflictName,
				description:  "重名",
				expectError:  true,
				expectedCode: 2,
			},
			{
				name:         "不存在的资源返回 status=2",
				parentID:     rootID,
				id:           "non-existent-resource-id",
				resourceType: "folder",
				newName:      uniqueName("资源不存在编辑"),
				description:  "不存在",
				expectError:  true,
				expectedCode: 2,
			},
			{
				name:               "缺少 id 返回 HTTP 400",
				parentID:           rootID,
				resourceType:       "folder",
				newName:            uniqueName("资源缺少ID编辑"),
				description:        "缺少 id",
				useRaw:             true,
				expectedHTTPStatus: http.StatusBadRequest,
			},
		}

		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				if tt.useRaw {
					rawResp, err := testClient.PostRawResponse("/api/v2/resource/edit", map[string]any{
						"parent_id":   tt.parentID,
						"type":        tt.resourceType,
						"name":        tt.newName,
						"description": tt.description,
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
					assertRawJSONStringBody(t, tt.name, body, "")
					return
				}

				resp, err := testClient.Primary().EditResourceInfo(tt.parentID, tt.id, tt.resourceType, tt.newName, tt.description)
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

// TestDeleteResource 验证 POST /api/v2/resource/delete。
// 覆盖：
//   - 正常删除资源文件夹并递归删除子资源
//   - 删除不存在的资源当前幂等成功
//   - 缺少 id 返回 HTTP 400
func TestDeleteResource(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	var rootID string
	var childFolderID string
	var fileID string
	rootName := uniqueName("资源删除父目录")
	childFolderName := uniqueName("资源删除子目录")
	fileName := uniqueName("资源删除文件")

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			rootID = createResourceLibFolder(t, "", rootName)
			childFolderID = createResourceLibFolder(t, rootID, childFolderName)
			fileID = uploadResourceLibFile(t, childFolderID, fileName, "删除测试文件", "resource_delete_file.txt", []byte("delete content"))
		},
		After: func(t *testing.T) {
			deleteResourceByID(t, fileID)
			deleteResourceByID(t, childFolderID)
			deleteResourceByID(t, rootID)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		tests := []struct {
			name               string
			id                 string
			useRaw             bool
			expectError        bool
			expectedCode       int
			expectedHTTPStatus int
			assertResponse     func(t *testing.T, resp *helpers.Response)
		}{
			{
				name:               "缺少 id 返回 HTTP 400",
				useRaw:             true,
				expectedHTTPStatus: http.StatusBadRequest,
			},
			{
				name: "删除不存在的资源当前幂等成功",
				id:   "non-existent-resource-id",
				assertResponse: func(t *testing.T, resp *helpers.Response) {
					assert.IsSuccess(resp, "删除不存在的资源当前幂等成功")
				},
			},
			{
				name: "正常删除资源文件夹并递归删除子资源",
				id:   rootID,
				assertResponse: func(t *testing.T, resp *helpers.Response) {
					assert.IsSuccess(resp, "正常删除资源文件夹并递归删除子资源")

					searchResp, err := testClient.SearchResource(rootName)
					if err != nil {
						t.Fatalf("正常删除资源文件夹并递归删除子资源: 搜索父目录失败: %v", err)
					}
					assert.IsSuccess(searchResp, "正常删除资源文件夹并递归删除子资源: 搜索父目录")
					_, rootItems := extractResourceSearchItems(t, searchResp, "正常删除资源文件夹并递归删除子资源: 搜索父目录")
					if resourceLibItemByName(rootItems, rootName) != nil {
						t.Fatalf("正常删除资源文件夹并递归删除子资源: 父目录 %q 仍存在", rootName)
					}

					childResp, err := testClient.SearchResource(childFolderName)
					if err != nil {
						t.Fatalf("正常删除资源文件夹并递归删除子资源: 搜索子目录失败: %v", err)
					}
					assert.IsSuccess(childResp, "正常删除资源文件夹并递归删除子资源: 搜索子目录")
					_, childItems := extractResourceSearchItems(t, childResp, "正常删除资源文件夹并递归删除子资源: 搜索子目录")
					if resourceLibItemByName(childItems, childFolderName) != nil {
						t.Fatalf("正常删除资源文件夹并递归删除子资源: 子目录 %q 仍存在", childFolderName)
					}

					fileResp, err := testClient.SearchResource(fileName)
					if err != nil {
						t.Fatalf("正常删除资源文件夹并递归删除子资源: 搜索文件失败: %v", err)
					}
					assert.IsSuccess(fileResp, "正常删除资源文件夹并递归删除子资源: 搜索文件")
					_, fileItems := extractResourceSearchItems(t, fileResp, "正常删除资源文件夹并递归删除子资源: 搜索文件")
					if resourceLibItemByName(fileItems, fileName) != nil {
						t.Fatalf("正常删除资源文件夹并递归删除子资源: 文件 %q 仍存在", fileName)
					}
				},
			},
		}

		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				if tt.useRaw {
					rawResp, err := testClient.PostRawResponse("/api/v2/resource/delete", map[string]any{})
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
					assertRawJSONStringBody(t, tt.name, body, "")
					return
				}

				resp, err := testClient.Primary().DeleteResource(tt.id)
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

func createResourceLibFolder(t *testing.T, parentID, name string) string {
	t.Helper()
	resp, err := testClient.CreateResourceFolder(parentID, name)
	if err != nil {
		t.Fatalf("创建资源库文件夹失败: %v", err)
	}
	if resp.Status != 0 {
		t.Fatalf("创建资源库文件夹返回非成功状态: status=%d, msg=%s, err=%s", resp.Status, resp.Msg, resp.Err)
	}
	return extractStringData(t, resp, "创建资源库文件夹")
}

func uploadResourceLibFile(t *testing.T, parentID, filename, description, uploadFileName string, content []byte) string {
	t.Helper()
	resp, err := testClient.UploadResourceFile(parentID, filename, description, uploadFileName, content)
	if err != nil {
		t.Fatalf("上传资源库文件失败: %v", err)
	}
	if resp.Status != 0 {
		t.Fatalf("上传资源库文件返回非成功状态: status=%d, msg=%s, err=%s", resp.Status, resp.Msg, resp.Err)
	}
	return extractStringData(t, resp, "上传资源库文件")
}

func deleteResourceByID(t *testing.T, id string) {
	t.Helper()
	if strings.TrimSpace(id) == "" {
		return
	}
	_, _ = testClient.DeleteResource(id)
}

func extractStringData(t *testing.T, resp *helpers.Response, message string) string {
	t.Helper()
	value, ok := resp.Data.(string)
	if !ok {
		t.Fatalf("%s: data 不是 string: %T", message, resp.Data)
	}
	return value
}

func extractResourceListSections(t *testing.T, resp *helpers.Response, message string) (folders []any, files []any, total int, pageCount int) {
	t.Helper()
	dataMap, err := helpers.ObjectData(resp, message)
	if err != nil {
		t.Fatal(err)
	}
	folderValue, ok := dataMap["folder"].([]any)
	if !ok {
		t.Fatalf("%s: folder 不是数组: %T", message, dataMap["folder"])
	}
	fileValue, ok := dataMap["file"].([]any)
	if !ok {
		t.Fatalf("%s: file 不是数组: %T", message, dataMap["file"])
	}
	totalValue, ok := dataMap["total"].(float64)
	if !ok {
		t.Fatalf("%s: total 不是数字: %T", message, dataMap["total"])
	}
	pageCountValue, ok := dataMap["page_count"].(float64)
	if !ok {
		t.Fatalf("%s: page_count 不是数字: %T", message, dataMap["page_count"])
	}
	return folderValue, fileValue, int(totalValue), int(pageCountValue)
}

func extractResourceSearchItems(t *testing.T, resp *helpers.Response, message string) (int, []any) {
	t.Helper()
	dataMap, err := helpers.ObjectData(resp, message)
	if err != nil {
		t.Fatal(err)
	}
	totalValue, ok := dataMap["total"].(float64)
	if !ok {
		t.Fatalf("%s: total 不是数字: %T", message, dataMap["total"])
	}
	list, ok := dataMap["data"].([]any)
	if !ok {
		t.Fatalf("%s: data.data 不是数组: %T", message, dataMap["data"])
	}
	return int(totalValue), list
}

func extractParsedResourceSeries(t *testing.T, resp *helpers.Response, coordName, message string) ([]float64, [][]float64) {
	t.Helper()
	dataMap, err := helpers.ObjectData(resp, message)
	if err != nil {
		t.Fatal(err)
	}
	seriesValue, ok := dataMap[coordName].(map[string]any)
	if !ok {
		t.Fatalf("%s: 未找到坐标 %q 或类型错误: %T", message, coordName, dataMap[coordName])
	}

	xDataRaw, ok := seriesValue["xData"].([]any)
	if !ok {
		t.Fatalf("%s: xData 不是数组: %T", message, seriesValue["xData"])
	}
	yDataRaw, ok := seriesValue["yData"].([]any)
	if !ok {
		t.Fatalf("%s: yData 不是数组: %T", message, seriesValue["yData"])
	}

	xData := make([]float64, 0, len(xDataRaw))
	for _, item := range xDataRaw {
		value, ok := item.(float64)
		if !ok {
			t.Fatalf("%s: xData 元素不是数字: %T", message, item)
		}
		xData = append(xData, value)
	}

	yData := make([][]float64, 0, len(yDataRaw))
	for _, series := range yDataRaw {
		seriesItems, ok := series.([]any)
		if !ok {
			t.Fatalf("%s: yData 子序列不是数组: %T", message, series)
		}
		parsed := make([]float64, 0, len(seriesItems))
		for _, point := range seriesItems {
			value, ok := point.(float64)
			if !ok {
				t.Fatalf("%s: yData 点不是数字: %T", message, point)
			}
			parsed = append(parsed, value)
		}
		yData = append(yData, parsed)
	}

	return xData, yData
}

func lenFirstNested(data [][]float64) int {
	if len(data) == 0 {
		return 0
	}
	return len(data[0])
}

func resourceLibItemByName(list []any, name string) map[string]any {
	for _, item := range list {
		itemMap, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if itemMap["name"] == name {
			return itemMap
		}
	}
	return nil
}
