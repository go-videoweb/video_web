//go:build api_test

package api

import (
	"net/http"
	"strings"
	"testing"

	"yssim-go/tests/helpers"
	pipenethelpers "yssim-go/tests/helpers/pipenet"
)

// TestUploadPipeNetInfoFile 验证 POST /pipenet/info/upload。
// 覆盖正常上传、重复名称、非法记录名称，以及非 XML 后缀文件。
func TestUploadPipeNetInfoFile(t *testing.T) {
	assert := helpers.NewAssertResponse(t)
	content, err := pipenethelpers.ReadPipeNetResource(pipenethelpers.InfoFileResourceName)
	if err != nil {
		t.Fatal(err)
	}

	var createdIDs []string
	hooks := helpers.TestHooks{
		After: func(t *testing.T) {
			if err := pipenethelpers.CleanupInfoFiles(testClient, createdIDs); err != nil {
				t.Logf("清理管网信息文件失败: %v", err)
			}
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		name := pipenethelpers.UniqueName("PipeInfoUpload")

		t.Run("正常上传管网信息文件", func(t *testing.T) {
			resp, err := testClient.Primary().UploadPipeNetInfoFile(name, "upload api test", pipenethelpers.InfoFileResourceName, content)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsSuccess(resp, "正常上传管网信息文件")
			id, ok := resp.Data.(string)
			if !ok || strings.TrimSpace(id) == "" {
				t.Fatalf("正常上传管网信息文件: 期望 data 为非空 id，实际=%T %v", resp.Data, resp.Data)
			}
			createdIDs = append(createdIDs, id)
		})

		t.Run("重复名称返回业务错误", func(t *testing.T) {
			resp, err := testClient.Primary().UploadPipeNetInfoFile(name, "duplicate api test", pipenethelpers.InfoFileResourceName, content)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsError(resp, 2, "重复名称返回业务错误")
		})

		t.Run("非法记录名称返回业务错误", func(t *testing.T) {
			resp, err := testClient.Primary().UploadPipeNetInfoFile("Pipe Info Invalid", "invalid name", pipenethelpers.InfoFileResourceName, content)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsError(resp, 2, "非法记录名称返回业务错误")
		})

		t.Run("非 XML 后缀文件返回业务错误", func(t *testing.T) {
			resp, err := testClient.Primary().UploadPipeNetInfoFile(pipenethelpers.UniqueName("PipeInfoBadExt"), "bad extension", "pipe.txt", content)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsError(resp, 2, "非 XML 后缀文件返回业务错误")
		})
	})
}

// TestGetPipeNetInfoFileList 验证 GET /pipenet/info/list/get。
// 覆盖按关键词分页获取，并确认刚上传的记录出现在列表中。
func TestGetPipeNetInfoFileList(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	name := pipenethelpers.UniqueName("PipeInfoList")
	var id string
	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			var err error
			id, err = pipenethelpers.UploadInfoFileFixture(testClient, name, "list api test")
			if err != nil {
				t.Fatal(err)
			}
		},
		After: func(t *testing.T) {
			if err := pipenethelpers.CleanupInfoFiles(testClient, []string{id}); err != nil {
				t.Logf("清理管网信息文件失败: %v", err)
			}
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		resp, err := testClient.Primary().GetPipeNetInfoFileList(name, 1)
		if err != nil {
			t.Fatalf("请求失败: %v", err)
		}
		assert.IsSuccess(resp, "按关键词获取管网信息文件列表")
		contains, err := pipenethelpers.ResponseListContainsID(resp, "pipe_net_info_list", id)
		if err != nil {
			t.Fatal(err)
		}
		if !contains {
			t.Fatalf("按关键词获取管网信息文件列表: 未找到记录 id=%q", id)
		}
	})
}

// TestEditPipeNetInfoFile 验证 POST /pipenet/info/edit。
// 覆盖正常编辑、记录不存在、非法名称，以及缺少必填 id。
func TestEditPipeNetInfoFile(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	name := pipenethelpers.UniqueName("PipeInfoEdit")
	var id string
	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			var err error
			id, err = pipenethelpers.UploadInfoFileFixture(testClient, name, "edit api test")
			if err != nil {
				t.Fatal(err)
			}
		},
		After: func(t *testing.T) {
			if err := pipenethelpers.CleanupInfoFiles(testClient, []string{id}); err != nil {
				t.Logf("清理管网信息文件失败: %v", err)
			}
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		t.Run("正常编辑管网信息文件", func(t *testing.T) {
			resp, err := testClient.Primary().EditPipeNetInfoFile(id, name+"_Edited", "edited description")
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsSuccess(resp, "正常编辑管网信息文件")
			if !strings.Contains(resp.Msg, "编辑成功") {
				t.Errorf("正常编辑管网信息文件: 期望 msg 包含编辑成功，实际=%q", resp.Msg)
			}
		})

		t.Run("记录不存在返回业务错误", func(t *testing.T) {
			resp, err := testClient.Primary().EditPipeNetInfoFile("pipe-info-id-not-exist", name+"_Missing", "missing")
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsError(resp, 2, "记录不存在返回业务错误")
		})

		t.Run("非法名称返回业务错误", func(t *testing.T) {
			resp, err := testClient.Primary().EditPipeNetInfoFile(id, "Pipe Info Invalid", "invalid")
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsError(resp, 2, "非法名称返回业务错误")
		})

		t.Run("缺少 id 返回 HTTP 400", func(t *testing.T) {
			rawResp, err := testClient.Primary().EditPipeNetInfoFileRaw(map[string]any{
				"name":        name + "_NoID",
				"description": "missing id",
			})
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			body, err := helpers.ReadAndCloseRawBody(rawResp)
			if err != nil {
				t.Fatal(err)
			}
			if rawResp.StatusCode != http.StatusBadRequest {
				t.Fatalf("缺少 id 返回 HTTP 400: 期望 HTTP 400，实际=%d，body=%q", rawResp.StatusCode, string(body))
			}
		})
	})
}

// TestCopyPipeNetInfoFile 验证 POST /pipenet/info/copy。
// 覆盖正常复制并从数据库确认副本，以及缺少 id_list 的绑定错误。
func TestCopyPipeNetInfoFile(t *testing.T) {
	assert := helpers.NewAssertResponse(t)
	username := helpers.DefaultTestConfig().Username

	name := pipenethelpers.UniqueName("PipeInfoCopy")
	var id string
	var copiedID string
	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			var err error
			id, err = pipenethelpers.UploadInfoFileFixture(testClient, name, "copy api test")
			if err != nil {
				t.Fatal(err)
			}
		},
		After: func(t *testing.T) {
			if err := pipenethelpers.CleanupInfoFiles(testClient, []string{id, copiedID}); err != nil {
				t.Logf("清理管网信息文件失败: %v", err)
			}
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		t.Run("正常复制管网信息文件", func(t *testing.T) {
			resp, err := testClient.Primary().CopyPipeNetInfoFile([]string{id})
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsSuccess(resp, "正常复制管网信息文件")
			copiedID, err = pipenethelpers.FindInfoFileIDByName(username, name+"_副本")
			if err != nil {
				t.Fatalf("正常复制管网信息文件: 未找到副本记录: %v", err)
			}
			if strings.TrimSpace(copiedID) == "" {
				t.Fatal("正常复制管网信息文件: 副本 id 为空")
			}
		})

		t.Run("缺少 id_list 返回 HTTP 400", func(t *testing.T) {
			rawResp, err := testClient.Primary().CopyPipeNetInfoFileRaw(map[string]any{})
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			body, err := helpers.ReadAndCloseRawBody(rawResp)
			if err != nil {
				t.Fatal(err)
			}
			if rawResp.StatusCode != http.StatusBadRequest {
				t.Fatalf("缺少 id_list 返回 HTTP 400: 期望 HTTP 400，实际=%d，body=%q", rawResp.StatusCode, string(body))
			}
		})
	})
}

// TestDownloadPipeNetInfoFile 验证 POST /pipenet/info/download。
// 覆盖正常下载返回 zip 路径，以及缺少 id_list 的绑定错误。
func TestDownloadPipeNetInfoFile(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	name := pipenethelpers.UniqueName("PipeInfoDownload")
	var id string
	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			var err error
			id, err = pipenethelpers.UploadInfoFileFixture(testClient, name, "download api test")
			if err != nil {
				t.Fatal(err)
			}
		},
		After: func(t *testing.T) {
			if err := pipenethelpers.CleanupInfoFiles(testClient, []string{id}); err != nil {
				t.Logf("清理管网信息文件失败: %v", err)
			}
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		t.Run("正常下载管网信息文件", func(t *testing.T) {
			resp, err := testClient.Primary().DownloadPipeNetInfoFile([]string{id})
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsSuccess(resp, "正常下载管网信息文件")
			urlValue, err := pipenethelpers.URLFromResponse(resp, "正常下载管网信息文件")
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasSuffix(urlValue, ".zip") {
				t.Fatalf("正常下载管网信息文件: 期望返回 zip 路径，实际=%q", urlValue)
			}
		})

		t.Run("缺少 id_list 返回 HTTP 400", func(t *testing.T) {
			rawResp, err := testClient.Primary().DownloadPipeNetInfoFileRaw(map[string]any{})
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			body, err := helpers.ReadAndCloseRawBody(rawResp)
			if err != nil {
				t.Fatal(err)
			}
			if rawResp.StatusCode != http.StatusBadRequest {
				t.Fatalf("缺少 id_list 返回 HTTP 400: 期望 HTTP 400，实际=%d，body=%q", rawResp.StatusCode, string(body))
			}
		})
	})
}

// TestDeletePipeNetInfoFile 验证 POST /pipenet/info/delete。
// 覆盖正常删除并确认数据库记录消失，以及缺少 id_list 的绑定错误。
func TestDeletePipeNetInfoFile(t *testing.T) {
	assert := helpers.NewAssertResponse(t)
	username := helpers.DefaultTestConfig().Username

	name := pipenethelpers.UniqueName("PipeInfoDelete")
	id, err := pipenethelpers.UploadInfoFileFixture(testClient, name, "delete api test")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("正常删除管网信息文件", func(t *testing.T) {
		resp, err := testClient.Primary().DeletePipeNetInfoFile([]string{id})
		if err != nil {
			t.Fatalf("请求失败: %v", err)
		}
		assert.IsSuccess(resp, "正常删除管网信息文件")
		exists, err := pipenethelpers.InfoFileExists(username, id)
		if err != nil {
			t.Fatal(err)
		}
		if exists {
			t.Fatalf("正常删除管网信息文件: 删除后数据库仍存在 id=%q", id)
		}
	})

	t.Run("缺少 id_list 返回 HTTP 400", func(t *testing.T) {
		rawResp, err := testClient.Primary().DeletePipeNetInfoFileRaw(map[string]any{})
		if err != nil {
			t.Fatalf("请求失败: %v", err)
		}
		body, err := helpers.ReadAndCloseRawBody(rawResp)
		if err != nil {
			t.Fatal(err)
		}
		if rawResp.StatusCode != http.StatusBadRequest {
			t.Fatalf("缺少 id_list 返回 HTTP 400: 期望 HTTP 400，实际=%d，body=%q", rawResp.StatusCode, string(body))
		}
	})
}

// TestUploadMappingConfig 验证 POST /pipenet/mapping_config/upload。
// 覆盖正常上传、重复名称、非法记录名称，以及非 JSON 后缀文件。
func TestUploadMappingConfig(t *testing.T) {
	assert := helpers.NewAssertResponse(t)
	content, err := pipenethelpers.ReadPipeNetResource(pipenethelpers.MappingConfigResourceName)
	if err != nil {
		t.Fatal(err)
	}

	var createdIDs []string
	hooks := helpers.TestHooks{
		After: func(t *testing.T) {
			if err := pipenethelpers.CleanupMappingConfigs(testClient, createdIDs); err != nil {
				t.Logf("清理管网映射表失败: %v", err)
			}
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		name := pipenethelpers.UniqueName("PipeMapUpload")

		t.Run("正常上传管网映射表", func(t *testing.T) {
			resp, err := testClient.Primary().UploadMappingConfig(name, "upload api test", pipenethelpers.MappingConfigResourceName, content)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsSuccess(resp, "正常上传管网映射表")
			id, ok := resp.Data.(string)
			if !ok || strings.TrimSpace(id) == "" {
				t.Fatalf("正常上传管网映射表: 期望 data 为非空 id，实际=%T %v", resp.Data, resp.Data)
			}
			createdIDs = append(createdIDs, id)
		})

		t.Run("重复名称返回业务错误", func(t *testing.T) {
			resp, err := testClient.Primary().UploadMappingConfig(name, "duplicate api test", pipenethelpers.MappingConfigResourceName, content)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsError(resp, 2, "重复名称返回业务错误")
		})

		t.Run("非法记录名称返回业务错误", func(t *testing.T) {
			resp, err := testClient.Primary().UploadMappingConfig("Pipe Map Invalid", "invalid name", pipenethelpers.MappingConfigResourceName, content)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsError(resp, 2, "非法记录名称返回业务错误")
		})

		t.Run("非 JSON 后缀文件返回业务错误", func(t *testing.T) {
			resp, err := testClient.Primary().UploadMappingConfig(pipenethelpers.UniqueName("PipeMapBadExt"), "bad extension", "map.txt", content)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsError(resp, 2, "非 JSON 后缀文件返回业务错误")
		})
	})
}

// TestGetMappingConfigList 验证 GET /pipenet/mapping_config/list/get。
// 覆盖按关键词分页获取，并确认刚上传的映射表出现在列表中。
func TestGetMappingConfigList(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	name := pipenethelpers.UniqueName("PipeMapList")
	var id string
	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			var err error
			id, err = pipenethelpers.UploadMappingConfigFixture(testClient, name, "list api test")
			if err != nil {
				t.Fatal(err)
			}
		},
		After: func(t *testing.T) {
			if err := pipenethelpers.CleanupMappingConfigs(testClient, []string{id}); err != nil {
				t.Logf("清理管网映射表失败: %v", err)
			}
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		resp, err := testClient.Primary().GetMappingConfigList(name, 1, 10)
		if err != nil {
			t.Fatalf("请求失败: %v", err)
		}
		assert.IsSuccess(resp, "按关键词获取管网映射表列表")
		contains, err := pipenethelpers.ResponseListContainsID(resp, "mapping_config_list", id)
		if err != nil {
			t.Fatal(err)
		}
		if !contains {
			t.Fatalf("按关键词获取管网映射表列表: 未找到记录 id=%q", id)
		}
	})
}

// TestEditMappingConfig 验证 POST /pipenet/mapping_config/edit。
// 覆盖正常编辑、记录不存在、非法名称，以及缺少必填 id。
func TestEditMappingConfig(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	name := pipenethelpers.UniqueName("PipeMapEdit")
	var id string
	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			var err error
			id, err = pipenethelpers.UploadMappingConfigFixture(testClient, name, "edit api test")
			if err != nil {
				t.Fatal(err)
			}
		},
		After: func(t *testing.T) {
			if err := pipenethelpers.CleanupMappingConfigs(testClient, []string{id}); err != nil {
				t.Logf("清理管网映射表失败: %v", err)
			}
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		t.Run("正常编辑管网映射表", func(t *testing.T) {
			resp, err := testClient.Primary().EditMappingConfig(id, name+"_Edited", "edited description")
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsSuccess(resp, "正常编辑管网映射表")
			if !strings.Contains(resp.Msg, "编辑成功") {
				t.Errorf("正常编辑管网映射表: 期望 msg 包含编辑成功，实际=%q", resp.Msg)
			}
		})

		t.Run("记录不存在返回业务错误", func(t *testing.T) {
			resp, err := testClient.Primary().EditMappingConfig("mapping-config-id-not-exist", name+"_Missing", "missing")
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsError(resp, 2, "记录不存在返回业务错误")
		})

		t.Run("非法名称返回业务错误", func(t *testing.T) {
			resp, err := testClient.Primary().EditMappingConfig(id, "Pipe Map Invalid", "invalid")
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsError(resp, 2, "非法名称返回业务错误")
		})

		t.Run("缺少 id 返回 HTTP 400", func(t *testing.T) {
			rawResp, err := testClient.Primary().EditMappingConfigRaw(map[string]any{
				"name":        name + "_NoID",
				"description": "missing id",
			})
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			body, err := helpers.ReadAndCloseRawBody(rawResp)
			if err != nil {
				t.Fatal(err)
			}
			if rawResp.StatusCode != http.StatusBadRequest {
				t.Fatalf("缺少 id 返回 HTTP 400: 期望 HTTP 400，实际=%d，body=%q", rawResp.StatusCode, string(body))
			}
		})
	})
}

// TestGetMappingConfigDetails 验证 GET /pipenet/mapping_config/details/get。
// 覆盖正常获取映射表详情，以及不存在记录的业务错误。
func TestGetMappingConfigDetails(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	name := pipenethelpers.UniqueName("PipeMapDetails")
	var id string
	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			var err error
			id, err = pipenethelpers.UploadMappingConfigFixture(testClient, name, "details api test")
			if err != nil {
				t.Fatal(err)
			}
		},
		After: func(t *testing.T) {
			if err := pipenethelpers.CleanupMappingConfigs(testClient, []string{id}); err != nil {
				t.Logf("清理管网映射表失败: %v", err)
			}
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		t.Run("正常获取映射表详情", func(t *testing.T) {
			resp, err := testClient.Primary().GetMappingConfigDetails(id)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsSuccess(resp, "正常获取映射表详情")
			dataMap, err := helpers.ObjectData(resp, "正常获取映射表详情")
			if err != nil {
				t.Fatal(err)
			}
			for _, field := range []string{"id", "name", "description", "system", "medium", "parts"} {
				if _, ok := dataMap[field]; !ok {
					t.Errorf("正常获取映射表详情: 缺少字段 %q", field)
				}
			}
		})

		t.Run("不存在记录返回业务错误", func(t *testing.T) {
			resp, err := testClient.Primary().GetMappingConfigDetails("mapping-config-id-not-exist")
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsError(resp, 2, "不存在记录返回业务错误")
		})
	})
}

// TestEditMappingConfigDetails 验证 POST /pipenet/mapping_config/details/edit。
// 覆盖正常 replace 映射表详情、非法 op，以及缺少必填 id。
func TestEditMappingConfigDetails(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	name := pipenethelpers.UniqueName("PipeMapDetailsEdit")
	var id string
	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			var err error
			id, err = pipenethelpers.UploadMappingConfigFixture(testClient, name, "details edit api test")
			if err != nil {
				t.Fatal(err)
			}
		},
		After: func(t *testing.T) {
			if err := pipenethelpers.CleanupMappingConfigs(testClient, []string{id}); err != nil {
				t.Logf("清理管网映射表失败: %v", err)
			}
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		detailsResp, err := testClient.GetMappingConfigDetails(id)
		if err != nil {
			t.Fatalf("准备映射表详情失败: %v", err)
		}
		assert.IsSuccess(detailsResp, "准备映射表详情")
		details, err := helpers.ObjectData(detailsResp, "准备映射表详情")
		if err != nil {
			t.Fatal(err)
		}
		parts, ok := details["parts"].([]any)
		if !ok || len(parts) == 0 {
			t.Fatalf("准备映射表详情: 期望 parts 为非空数组，实际=%T %v", details["parts"], details["parts"])
		}
		firstPart, ok := parts[0].(map[string]any)
		if !ok {
			t.Fatalf("准备映射表详情: parts[0] 不是 object，实际=%T %v", parts[0], parts[0])
		}
		// replace 操作要求传入完整 part 结构；直接复用详情接口返回值，避免手写大块易过期字段。
		replacementParts := []map[string]any{firstPart}

		t.Run("正常编辑映射表详情", func(t *testing.T) {
			resp, err := testClient.Primary().EditMappingConfigDetails(id, "replace", pipenethelpers.StringValue(details["system"]), pipenethelpers.StringValue(details["medium"]), replacementParts)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsSuccess(resp, "正常编辑映射表详情")
			if !strings.Contains(resp.Msg, "编辑成功") {
				t.Errorf("正常编辑映射表详情: 期望 msg 包含编辑成功，实际=%q", resp.Msg)
			}
		})

		t.Run("非法 op 返回业务错误", func(t *testing.T) {
			resp, err := testClient.Primary().EditMappingConfigDetails(id, "invalid-op", pipenethelpers.StringValue(details["system"]), pipenethelpers.StringValue(details["medium"]), replacementParts)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsError(resp, 2, "非法 op 返回业务错误")
		})

		t.Run("缺少 id 返回 HTTP 400", func(t *testing.T) {
			rawResp, err := testClient.Primary().EditMappingConfigDetailsRaw(map[string]any{
				"op":     "replace",
				"system": details["system"],
				"medium": details["medium"],
				"parts":  replacementParts,
			})
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			body, err := helpers.ReadAndCloseRawBody(rawResp)
			if err != nil {
				t.Fatal(err)
			}
			if rawResp.StatusCode != http.StatusBadRequest {
				t.Fatalf("缺少 id 返回 HTTP 400: 期望 HTTP 400，实际=%d，body=%q", rawResp.StatusCode, string(body))
			}
		})
	})
}

// TestCopyMappingConfig 验证 POST /pipenet/mapping_config/copy。
// 覆盖正常复制并从数据库确认副本，以及缺少 mapping_config_id_list 的绑定错误。
func TestCopyMappingConfig(t *testing.T) {
	assert := helpers.NewAssertResponse(t)
	username := helpers.DefaultTestConfig().Username

	name := pipenethelpers.UniqueName("PipeMapCopy")
	var id string
	var copiedID string
	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			var err error
			id, err = pipenethelpers.UploadMappingConfigFixture(testClient, name, "copy api test")
			if err != nil {
				t.Fatal(err)
			}
		},
		After: func(t *testing.T) {
			if err := pipenethelpers.CleanupMappingConfigs(testClient, []string{id, copiedID}); err != nil {
				t.Logf("清理管网映射表失败: %v", err)
			}
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		t.Run("正常复制管网映射表", func(t *testing.T) {
			resp, err := testClient.Primary().CopyMappingConfig([]string{id})
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsSuccess(resp, "正常复制管网映射表")
			copiedID, err = pipenethelpers.FindMappingConfigIDByName(username, name+"_副本")
			if err != nil {
				t.Fatalf("正常复制管网映射表: 未找到副本记录: %v", err)
			}
			if strings.TrimSpace(copiedID) == "" {
				t.Fatal("正常复制管网映射表: 副本 id 为空")
			}
		})

		t.Run("缺少 mapping_config_id_list 返回 HTTP 400", func(t *testing.T) {
			rawResp, err := testClient.Primary().CopyMappingConfigRaw(map[string]any{})
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			body, err := helpers.ReadAndCloseRawBody(rawResp)
			if err != nil {
				t.Fatal(err)
			}
			if rawResp.StatusCode != http.StatusBadRequest {
				t.Fatalf("缺少 mapping_config_id_list 返回 HTTP 400: 期望 HTTP 400，实际=%d，body=%q", rawResp.StatusCode, string(body))
			}
		})
	})
}

// TestDownloadMappingConfig 验证 POST /pipenet/mapping_config/download。
// 覆盖正常下载返回 zip 路径，以及缺少 mapping_config_id_list 的绑定错误。
func TestDownloadMappingConfig(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	name := pipenethelpers.UniqueName("PipeMapDownload")
	var id string
	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			var err error
			id, err = pipenethelpers.UploadMappingConfigFixture(testClient, name, "download api test")
			if err != nil {
				t.Fatal(err)
			}
		},
		After: func(t *testing.T) {
			if err := pipenethelpers.CleanupMappingConfigs(testClient, []string{id}); err != nil {
				t.Logf("清理管网映射表失败: %v", err)
			}
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		t.Run("正常下载管网映射表", func(t *testing.T) {
			resp, err := testClient.Primary().DownloadMappingConfig([]string{id})
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsSuccess(resp, "正常下载管网映射表")
			urlValue, err := pipenethelpers.URLFromResponse(resp, "正常下载管网映射表")
			if err != nil {
				t.Fatal(err)
			}
			if !strings.HasSuffix(urlValue, ".zip") {
				t.Fatalf("正常下载管网映射表: 期望返回 zip 路径，实际=%q", urlValue)
			}
		})

		t.Run("缺少 mapping_config_id_list 返回 HTTP 400", func(t *testing.T) {
			rawResp, err := testClient.Primary().DownloadMappingConfigRaw(map[string]any{})
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			body, err := helpers.ReadAndCloseRawBody(rawResp)
			if err != nil {
				t.Fatal(err)
			}
			if rawResp.StatusCode != http.StatusBadRequest {
				t.Fatalf("缺少 mapping_config_id_list 返回 HTTP 400: 期望 HTTP 400，实际=%d，body=%q", rawResp.StatusCode, string(body))
			}
		})
	})
}

// TestDeleteMappingConfig 验证 POST /pipenet/mapping_config/delete。
// 覆盖正常删除并确认数据库记录消失，以及缺少 mapping_config_id_list 的绑定错误。
func TestDeleteMappingConfig(t *testing.T) {
	assert := helpers.NewAssertResponse(t)
	username := helpers.DefaultTestConfig().Username

	name := pipenethelpers.UniqueName("PipeMapDelete")
	id, err := pipenethelpers.UploadMappingConfigFixture(testClient, name, "delete api test")
	if err != nil {
		t.Fatal(err)
	}

	t.Run("正常删除管网映射表", func(t *testing.T) {
		resp, err := testClient.Primary().DeleteMappingConfig([]string{id})
		if err != nil {
			t.Fatalf("请求失败: %v", err)
		}
		assert.IsSuccess(resp, "正常删除管网映射表")
		exists, err := pipenethelpers.MappingConfigExists(username, id)
		if err != nil {
			t.Fatal(err)
		}
		if exists {
			t.Fatalf("正常删除管网映射表: 删除后数据库仍存在 id=%q", id)
		}
	})

	t.Run("缺少 mapping_config_id_list 返回 HTTP 400", func(t *testing.T) {
		rawResp, err := testClient.Primary().DeleteMappingConfigRaw(map[string]any{})
		if err != nil {
			t.Fatalf("请求失败: %v", err)
		}
		body, err := helpers.ReadAndCloseRawBody(rawResp)
		if err != nil {
			t.Fatal(err)
		}
		if rawResp.StatusCode != http.StatusBadRequest {
			t.Fatalf("缺少 mapping_config_id_list 返回 HTTP 400: 期望 HTTP 400，实际=%d，body=%q", rawResp.StatusCode, string(body))
		}
	})
}

// TestGetPipeNetInstanceMapping 验证 GET /pipenet/model/instance_mapping/get。
// 覆盖正常生成实例映射树，以及管网信息文件不存在的业务错误。
func TestGetPipeNetInstanceMapping(t *testing.T) {
	assert := helpers.NewAssertResponse(t)

	infoName := pipenethelpers.UniqueName("PipeInfoMapping")
	mappingName := pipenethelpers.UniqueName("PipeMapMapping")
	var infoID string
	var mappingID string
	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			var err error
			infoID, err = pipenethelpers.UploadInfoFileFixture(testClient, infoName, "instance mapping api test")
			if err != nil {
				t.Fatal(err)
			}
			mappingID, err = pipenethelpers.UploadMappingConfigFixture(testClient, mappingName, "instance mapping api test")
			if err != nil {
				t.Fatal(err)
			}
		},
		After: func(t *testing.T) {
			if err := pipenethelpers.CleanupInfoFiles(testClient, []string{infoID}); err != nil {
				t.Logf("清理管网信息文件失败: %v", err)
			}
			if err := pipenethelpers.CleanupMappingConfigs(testClient, []string{mappingID}); err != nil {
				t.Logf("清理管网映射表失败: %v", err)
			}
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		t.Run("正常获取管网实例映射树", func(t *testing.T) {
			resp, err := testClient.Primary().GetPipeNetInstanceMapping(infoID, mappingID)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsSuccess(resp, "正常获取管网实例映射树")
			dataMap, err := helpers.ObjectData(resp, "正常获取管网实例映射树")
			if err != nil {
				t.Fatal(err)
			}
			for _, field := range []string{"file_tree", "mapping_tree"} {
				if _, ok := dataMap[field]; !ok {
					t.Errorf("正常获取管网实例映射树: 缺少字段 %q", field)
				}
			}
		})

		t.Run("管网信息文件不存在返回错误", func(t *testing.T) {
			resp, err := testClient.Primary().GetPipeNetInstanceMapping("pipe-info-id-not-exist", mappingID)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			if strings.TrimSpace(resp.Err) == "" {
				t.Fatalf("管网信息文件不存在返回错误: 期望 err 非空，实际=%q", resp.Err)
			}
		})
	})
}

// TestCreatePipeNetModel 验证 POST /pipenet/model/create。
// 覆盖正常生成顶层管网模型、非法模型名，以及缺少 package_name 的绑定错误。
func TestCreatePipeNetModel(t *testing.T) {
	assert := helpers.NewAssertResponse(t)
	username := helpers.DefaultTestConfig().Username

	infoName := pipenethelpers.UniqueName("PipeInfoModel")
	mappingName := pipenethelpers.UniqueName("PipeMapModel")
	modelName := pipenethelpers.UniqueName("PipeNetModel")
	var infoID string
	var mappingID string
	var packageID string
	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			var err error
			infoID, err = pipenethelpers.UploadInfoFileFixture(testClient, infoName, "model create api test")
			if err != nil {
				t.Fatal(err)
			}
			mappingID, err = pipenethelpers.UploadMappingConfigFixture(testClient, mappingName, "model create api test")
			if err != nil {
				t.Fatal(err)
			}
		},
		After: func(t *testing.T) {
			if err := pipenethelpers.CleanupGeneratedModel(testClient, username, packageID, modelName); err != nil {
				t.Logf("清理管网生成模型失败: %v", err)
			}
			if err := pipenethelpers.CleanupInfoFiles(testClient, []string{infoID}); err != nil {
				t.Logf("清理管网信息文件失败: %v", err)
			}
			if err := pipenethelpers.CleanupMappingConfigs(testClient, []string{mappingID}); err != nil {
				t.Logf("清理管网映射表失败: %v", err)
			}
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		vars := map[string]any{
			"expand":       "",
			"insert_to":    "",
			"partial":      false,
			"encapsulated": false,
			"state":        false,
		}

		t.Run("正常生成顶层管网模型", func(t *testing.T) {
			resp, err := testClient.Primary().CreatePipeNetModel(modelName, "model", "", "", vars, infoID, mappingID)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsSuccess(resp, "正常生成顶层管网模型")
			dataMap, err := helpers.ObjectData(resp, "正常生成顶层管网模型")
			if err != nil {
				t.Fatal(err)
			}
			id, ok := dataMap["package_id"].(string)
			if !ok || strings.TrimSpace(id) == "" {
				t.Fatalf("正常生成顶层管网模型: 期望 package_id 非空，实际=%T %v", dataMap["package_id"], dataMap["package_id"])
			}
			packageID = id
			if _, ok := dataMap["model"]; !ok {
				t.Error("正常生成顶层管网模型: data 缺少 model 字段")
			}
		})

		t.Run("非法模型名返回业务错误", func(t *testing.T) {
			resp, err := testClient.Primary().CreatePipeNetModel("1InvalidPipeNetModel", "model", "", "", vars, infoID, mappingID)
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			assert.IsError(resp, 2, "非法模型名返回业务错误")
		})

		t.Run("缺少 package_name 返回 HTTP 400", func(t *testing.T) {
			rawResp, err := testClient.Primary().CreatePipeNetModelRaw(map[string]any{
				"str_type":          "model",
				"package_id":        "",
				"comment":           "",
				"vars":              vars,
				"pipe_net_info_id":  infoID,
				"mapping_config_id": mappingID,
			})
			if err != nil {
				t.Fatalf("请求失败: %v", err)
			}
			body, err := helpers.ReadAndCloseRawBody(rawResp)
			if err != nil {
				t.Fatal(err)
			}
			if rawResp.StatusCode != http.StatusBadRequest {
				t.Fatalf("缺少 package_name 返回 HTTP 400: 期望 HTTP 400，实际=%d，body=%q", rawResp.StatusCode, string(body))
			}
		})
	})
}
