package helpers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"yssim-go/app/view/model"
)

// APIClient 封装 HTTP 客户端
type APIClient struct {
	baseURL     string
	client      *http.Client
	config      *TestConfig
	markPrimary bool
}

// 工作空间状态常量（对应 app/service/workspace/status.go）
const (
	WorkspaceStatusNotStarted   = 0 // Pod 未启动
	WorkspaceStatusStarting     = 1 // Pod 启动中
	WorkspaceStatusNeedLoad     = 2 // Pod 就绪，标准库未加载
	WorkspaceStatusReady        = 3 // 已就绪（标准库已加载）
	WorkspaceStatusOOMRestarted = 4 // OOM 重启，仅需重新加载系统库
	WorkspaceStatusError        = 5 // 异常，无法自动恢复
)

var (
	once             sync.Once
	workspaceReadyMu sync.Mutex
)

// MarkAPI 输出主测接口覆盖标记，供覆盖率脚本从 go test -json 中统计实际覆盖接口。
func MarkAPI(method, path string) {
	method = strings.ToUpper(strings.TrimSpace(method))
	path = normalizeMarkedAPIPath(path)
	if method == "" || path == "" {
		return
	}
	fmt.Printf("API_MARK role=primary method=%s path=%s\n", method, path)
}

// Primary 返回只用于主测接口调用的客户端视图，避免辅助接口污染覆盖统计。
func (c *APIClient) Primary() *APIClient {
	primaryClient := *c
	primaryClient.markPrimary = true
	return &primaryClient
}

// NewAPIClient 创建 API 客户端
func NewAPIClient(baseURL string) *APIClient {
	return &APIClient{
		baseURL: baseURL,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// NewAPIClientWithConfig 使用配置创建 API 客户端
func NewAPIClientWithConfig(config *TestConfig) *APIClient {
	apiCli := &APIClient{
		baseURL: config.BaseURL,
		client: &http.Client{
			Timeout: config.Timeout,
		},
		config: config,
	}

	// 初始化操作：根据工作空间当前状态决定需要执行哪些步骤，避免重复初始化。
	// 对齐设计文档《工作空间Pod状态管理详细设计文档》§8 前端适配指南。
	once.Do(func() {
		if err := apiCli.ensureWorkspaceReady(); err != nil {
			panic(err)
		}
	})
	return apiCli
}

// Get 发送 GET 请求
func (c *APIClient) Get(path string, params map[string]string, headers map[string]string) (*Response, error) {
	if err := c.withoutPrimaryMark().ensureWorkspaceReadyBeforeRequest(path); err != nil {
		return nil, err
	}

	return c.doRequestWithRetry(func() (*http.Request, error) {
		return c.buildRequest(http.MethodGet, path, params, nil, headers, false)
	})
}

// Post 发送 POST 请求
func (c *APIClient) Post(path string, body any, headers map[string]string) (*Response, error) {
	if err := c.withoutPrimaryMark().ensureWorkspaceReadyBeforeRequest(path); err != nil {
		return nil, err
	}

	return c.doRequestWithRetry(func() (*http.Request, error) {
		return c.buildRequest(http.MethodPost, path, nil, body, headers, true)
	})
}

// PrimaryGet 发送主测接口 GET 请求，并在请求完成后输出覆盖标记。
func (c *APIClient) PrimaryGet(path string, params map[string]string, headers map[string]string) (*Response, error) {
	return c.Primary().Get(path, params, headers)
}

// PrimaryPost 发送主测接口 POST 请求，并在请求完成后输出覆盖标记。
func (c *APIClient) PrimaryPost(path string, body any, headers map[string]string) (*Response, error) {
	return c.Primary().Post(path, body, headers)
}

// GetRawResponse 获取原始 HTTP 响应
func (c *APIClient) GetRawResponse(path string, params map[string]string) (*http.Response, error) {
	if err := c.withoutPrimaryMark().ensureWorkspaceReadyBeforeRequest(path); err != nil {
		return nil, err
	}

	return c.doRawRequestWithRetry(func() (*http.Request, error) {
		return c.buildRequest(http.MethodGet, path, params, nil, nil, false)
	})
}

// PostRawResponse 发送 POST 请求并获取原始响应
func (c *APIClient) PostRawResponse(path string, body any) (*http.Response, error) {
	if err := c.withoutPrimaryMark().ensureWorkspaceReadyBeforeRequest(path); err != nil {
		return nil, err
	}

	return c.doRawRequestWithRetry(func() (*http.Request, error) {
		return c.buildRequest(http.MethodPost, path, nil, body, nil, true)
	})
}

// 初始化测试相关接口
func (c *APIClient) RestartSMC() (*Response, error) {
	return c.Post("/user/service/restart", nil, nil)
}

// 模型相关接口

// GetSysRootModel 获取系统库
func (c *APIClient) GetSysRootModel() (*Response, error) {
	return c.Get("/api/v2/model/root_library/sys", nil, nil)
}

// GetUserRootModel 获取用户库
func (c *APIClient) GetUserRootModel(keyword, typ string) (*Response, error) {
	return c.Get("/model/root_library/user", map[string]string{
		"keywords": keyword,
		"type":     typ,
	}, nil)
}

// GetUserPackage 获取当前工作空间已加载的用户 package 列表
// 对应 GET /model/user/get
func (c *APIClient) GetUserPackage() (*Response, error) {
	return c.Get("/model/user/get", nil, nil)
}

// GetPackageAndVersion 获取模型库及版本列表。
// 对应 GET /model/package/get/all。
func (c *APIClient) GetPackageAndVersion() (*Response, error) {
	return c.Get("/model/package/get/all", nil, nil)
}

// LoadModel 加载模型
func (c *APIClient) LoadModel(packageId string, modelName string) (*Response, error) {
	body := model.LoadModel{
		Id:   packageId,
		Name: modelName,
	}

	return c.Post("/model/package/load", body, nil)
}

// LoadUserspaceModel 加载用户空间中的单个模型包
// 对应 POST /api/v2/model/userspace/model/load
func (c *APIClient) LoadUserspaceModel(packageId, modelName string) (*Response, error) {
	return c.Post("/api/v2/model/userspace/model/load", model.LoadModel{
		Id:   packageId,
		Name: modelName,
	}, nil)
}

// UnLoadModel 卸载模型
func (c *APIClient) UnLoadModel(modelName string) (*Response, error) {
	return c.Post("/model/package/unload", map[string]any{
		"modelName": modelName,
	}, nil)
}

// UnloadPackageByID 通过 package_id 卸载模型包
// 对应 POST /model/package/unload
func (c *APIClient) UnloadPackageByID(packageId string) (*Response, error) {
	return c.Post("/model/package/unload", map[string]any{
		"package_id": packageId,
	}, nil)
}

// SetModelParameters 设置模型参数
// 对应 POST /model/parameters/set
func (c *APIClient) SetModelParameters(packageId, modelName string, parameters []map[string]any) (*Response, error) {
	return c.Post("/model/parameters/set", map[string]any{
		"package_id": packageId,
		"model_name": modelName,
		"parameter":  parameters,
	}, nil)
}

// CheckModelV2 提交新版模型检查任务。
// 对应 POST /api/v2/model/check。
func (c *APIClient) CheckModelV2(packageId, modelName string) (*Response, error) {
	return c.Post("/api/v2/model/check", map[string]any{
		"package_id": packageId,
		"model_name": modelName,
	}, nil)
}

// CheckModelV2Raw 使用原始请求体提交新版模型检查任务。
// 对应 POST /api/v2/model/check。
func (c *APIClient) CheckModelV2Raw(body any) (*http.Response, error) {
	return c.PostRawResponse("/api/v2/model/check", body)
}

// TranslateModelV3 提交 v3 模型翻译任务。
// 对应 POST /api/v3/model/translate。
func (c *APIClient) TranslateModelV3(packageId, modelName string) (*Response, error) {
	return c.Post("/api/v3/model/translate", map[string]any{
		"package_id": packageId,
		"model_name": modelName,
	}, nil)
}

// TranslateModelV3Raw 使用原始请求体提交 v3 模型翻译任务。
// 对应 POST /api/v3/model/translate。
func (c *APIClient) TranslateModelV3Raw(body any) (*http.Response, error) {
	return c.PostRawResponse("/api/v3/model/translate", body)
}

// AddModelParameters 新增模型全局参数
// 对应 POST /model/parameters/add
func (c *APIClient) AddModelParameters(packageId, modelName, parameterName, varType string) (*Response, error) {
	return c.Post("/model/parameters/add", map[string]any{
		"package_id":     packageId,
		"model_name":     modelName,
		"parameter_name": parameterName,
		"var_type":       varType,
	}, nil)
}

// DeleteModelParameters 删除模型全局参数
// 对应 POST /model/parameters/delete
func (c *APIClient) DeleteModelParameters(packageId, modelName, parameterName string) (*Response, error) {
	return c.Post("/model/parameters/delete", map[string]any{
		"package_id":     packageId,
		"model_name":     modelName,
		"parameter_name": parameterName,
	}, nil)
}

// SetModelProperties 设置模型组件属性
// 对应 POST /model/properties/set
func (c *APIClient) SetModelProperties(packageId, modelName, oldComponentName, newComponentName string, properties map[string]any) (*Response, error) {
	body := map[string]any{
		"package_id":         packageId,
		"model_name":         modelName,
		"old_component_name": oldComponentName,
		"new_component_name": newComponentName,
	}
	for k, v := range properties {
		body[k] = v
	}
	return c.Post("/model/properties/set", body, nil)
}

// AddModelComponent 批量新增模型组件
// 对应 POST /api/v2/model/component/add
func (c *APIClient) AddModelComponent(packageId, modelName string, componentList []map[string]any) (*Response, error) {
	return c.Post("/api/v2/model/component/add", map[string]any{
		"package_id":     packageId,
		"model_name":     modelName,
		"component_list": componentList,
	}, nil)
}

// DeleteModelComponent 批量删除模型组件
// 对应 POST /model/component/delete
func (c *APIClient) DeleteModelComponent(packageId, modelName string, componentList []string) (*Response, error) {
	return c.Post("/model/component/delete", map[string]any{
		"package_id":     packageId,
		"model_name":     modelName,
		"component_list": componentList,
	}, nil)
}

// UpdateModelComponent 批量更新模型组件图形信息
// 对应 POST /model/component/update
func (c *APIClient) UpdateModelComponent(packageId, modelName string, componentList []map[string]any) (*Response, error) {
	return c.Post("/model/component/update", map[string]any{
		"package_id":     packageId,
		"model_name":     modelName,
		"component_list": componentList,
	}, nil)
}

// RenameModel 重命名模型
// 对应 POST /model/rename
func (c *APIClient) RenameModel(packageId, modelName, newName string) (*Response, error) {
	return c.Post("/model/rename", map[string]any{
		"package_id": packageId,
		"model_name": modelName,
		"new_name":   newName,
	}, nil)
}

// CopyModelComponent 复制模型组件
// 对应 POST /api/v2/model/component/copy
func (c *APIClient) CopyModelComponent(packageId, sourceModelName, targetModelName string, componentList []map[string]any) (*Response, error) {
	return c.Post("/api/v2/model/component/copy", map[string]any{
		"package_id":        packageId,
		"source_model_name": sourceModelName,
		"target_model_name": targetModelName,
		"component_list":    componentList,
	}, nil)
}

// CreateConnection 创建模型连线
// 对应 POST /model/connection/create
func (c *APIClient) CreateConnection(packageId, modelName string, connectList []map[string]any) (*Response, error) {
	return c.Post("/model/connection/create", map[string]any{
		"package_id":   packageId,
		"model_name":   modelName,
		"connect_list": connectList,
	}, nil)
}

// DeleteConnection 删除模型连线
// 对应 POST /model/connection/delete
func (c *APIClient) DeleteConnection(packageId, modelName string, connectionList []map[string]any) (*Response, error) {
	return c.Post("/model/connection/delete", map[string]any{
		"package_id":      packageId,
		"model_name":      modelName,
		"connection_list": connectionList,
	}, nil)
}

// UpdateConnection 更新模型连线注解
// 对应 POST /model/connection/update
func (c *APIClient) UpdateConnection(packageId, modelName string, connectionList []map[string]any) (*Response, error) {
	return c.Post("/model/connection/update", map[string]any{
		"package_id":      packageId,
		"model_name":      modelName,
		"connection_list": connectionList,
	}, nil)
}

// UpdateConnectionName 更新模型连线端点名称
// 对应 POST /model/connection/name
func (c *APIClient) UpdateConnectionName(packageId, modelName string, connectionList []map[string]any) (*Response, error) {
	return c.Post("/model/connection/name", map[string]any{
		"package_id":      packageId,
		"model_name":      modelName,
		"connection_list": connectionList,
	}, nil)
}

// UpdateIconShapes 批量更新模型图标中的图形信息
// 对应 POST /api/v3/model/icon/shapes/update
func (c *APIClient) UpdateIconShapes(packageId, modelName string, iconList []map[string]any) (*Response, error) {
	return c.Post("/api/v3/model/icon/shapes/update", map[string]any{
		"package_id": packageId,
		"model_name": modelName,
		"icon_list":  iconList,
	}, nil)
}

// UpdateDiagramShapes 批量更新模型 Diagram 中的图形信息。
// 对应 POST /api/v3/model/diagram/shapes/update。
func (c *APIClient) UpdateDiagramShapes(packageId, modelName string, diagramList []map[string]any) (*Response, error) {
	return c.Post("/api/v3/model/diagram/shapes/update", map[string]any{
		"package_id":   packageId,
		"model_name":   modelName,
		"diagram_list": diagramList,
	}, nil)
}

// UpdateDiagramShapesRaw 使用原始请求体批量更新模型 Diagram 中的图形信息。
// 对应 POST /api/v3/model/diagram/shapes/update。
func (c *APIClient) UpdateDiagramShapesRaw(body any) (*http.Response, error) {
	return c.PostRawResponse("/api/v3/model/diagram/shapes/update", body)
}

// UpdateIconConnectors 批量更新模型图标中的端口信息
// 对应 POST /api/v3/model/icon/connectors/update
func (c *APIClient) UpdateIconConnectors(packageId, modelName string, connectorList []map[string]any) (*Response, error) {
	return c.Post("/api/v3/model/icon/connectors/update", map[string]any{
		"package_id":     packageId,
		"model_name":     modelName,
		"connector_list": connectorList,
	}, nil)
}

// CopyClass 从已有类复制出一个新模型
// 对应 POST /model/class/copy
func (c *APIClient) CopyClass(fromPackageID, toPackageID, parentName, modelName, copiedClassName string) (*Response, error) {
	return c.Post("/model/class/copy", map[string]any{
		"from_package_id":   fromPackageID,
		"to_package_id":     toPackageID,
		"parent_name":       parentName,
		"model_name":        modelName,
		"copied_class_name": copiedClassName,
	}, nil)
}

// MoveClass 调整同一父模型下子模型或子 package 的展示顺序。
// 对应 POST /model/class/move。
func (c *APIClient) MoveClass(packageID, modelName string, targetIndex int) (*Response, error) {
	return c.Post("/model/class/move", map[string]any{
		"package_id":   packageID,
		"model_name":   modelName,
		"target_index": targetIndex,
	}, nil)
}

// MoveClassToPackage 将模型或子 package 移动到目标 package 下。
// 对应 POST /model/class/move-to-package。
func (c *APIClient) MoveClassToPackage(sourcePackageID, targetPackageID, sourceClassName, targetPackageName string) (*Response, error) {
	return c.Post("/model/class/move-to-package", map[string]any{
		"source_package_id":   sourcePackageID,
		"target_package_id":   targetPackageID,
		"source_class_name":   sourceClassName,
		"target_package_name": targetPackageName,
	}, nil)
}

// MoveClassToPackageRaw 使用原始请求体移动模型或子 package。
// 对应 POST /model/class/move-to-package。
func (c *APIClient) MoveClassToPackageRaw(body any) (*http.Response, error) {
	return c.PostRawResponse("/model/class/move-to-package", body)
}

// DeletePackage 删除模型包或包内模型
// 对应 POST /model/package/delete
func (c *APIClient) DeletePackage(packageID, parentName, modelName string) (*Response, error) {
	return c.Post("/model/package/delete", map[string]any{
		"package_id":  packageID,
		"parent_name": parentName,
		"model_name":  modelName,
	}, nil)
}

// CreateModelPackage 创建模型或模型包文件
// 对应 POST /file/create/package
func (c *APIClient) CreateModelPackage(packageName, strType string, vars map[string]any) (*Response, error) {
	body := map[string]any{
		"package_name": packageName,
		"str_type":     strType,
		"vars":         vars,
	}
	return c.Post("/file/create/package", body, nil)
}

// UpdateModelPackage 更新模型源码
// 对应 POST /file/update/package
func (c *APIClient) UpdateModelPackage(packageID, modelName, modelStr string) (*Response, error) {
	return c.Post("/file/update/package", map[string]any{
		"package_id": packageID,
		"model_name": modelName,
		"model_str":  modelStr,
	}, nil)
}

// GetPackageFileList 获取当前用户空间的模型文件列表
// 对应 GET /file/package/list
func (c *APIClient) GetPackageFileList() (*Response, error) {
	return c.Get("/file/package/list", nil, nil)
}

// GetPackageFile 获取指定���户模型文件的下载信息
// 对应 POST /file/package/get
func (c *APIClient) GetPackageFile(packageID string) (*Response, error) {
	return c.Post("/file/package/get", map[string]any{
		"package_id": packageID,
	}, nil)
}

// SaveModelCode 保存模型所在包代码到 .mo 文件
// 对应 POST /file/code/save
func (c *APIClient) SaveModelCode(packageID string) (*Response, error) {
	return c.Post("/file/code/save", map[string]any{
		"package_id": packageID,
	}, nil)
}

// GetPackageResourcesList 获取模型包 Resources 目录子项列表
// 对应 POST /file/package/resources/get
func (c *APIClient) GetPackageResourcesList(packageID, parent, path string) (*Response, error) {
	return c.Post("/file/package/resources/get", map[string]any{
		"package_id": packageID,
		"parent":     parent,
		"path":       path,
	}, nil)
}

// GetResourcesFileDataRaw 获取资源文件原始响应
// 对应 GET /file/package/resources/data/get
func (c *APIClient) GetResourcesFileDataRaw(path string) (*http.Response, error) {
	return c.GetRawResponse("/file/package/resources/data/get", map[string]string{
		"path": path,
	})
}

// CreateResourcesDir 创建 Resources 子目录
// 对应 POST /file/package/resources/dir/create
func (c *APIClient) CreateResourcesDir(packageID, parent, path string) (*Response, error) {
	return c.Post("/file/package/resources/dir/create", map[string]any{
		"package_id": packageID,
		"parent":     parent,
		"path":       path,
	}, nil)
}

// DeleteResourcesDirAndFile 删除 Resources 子目录或文件
// 对应 POST /file/package/resources/dir/delete
func (c *APIClient) DeleteResourcesDirAndFile(packageID, parent string) (*Response, error) {
	return c.Post("/file/package/resources/dir/delete", map[string]any{
		"package_id": packageID,
		"parent":     parent,
	}, nil)
}

// DownloadResourcesFile 获取资源文件下载路径信息
// 对应 POST /file/package/resources/download/
func (c *APIClient) DownloadResourcesFile(packageID, parent, path string) (*Response, error) {
	return c.Post("/file/package/resources/download/", map[string]any{
		"package_id": packageID,
		"parent":     parent,
		"path":       path,
	}, nil)
}

// UploadResourcesFile 上传 Resources 文件
// 对应 POST /file/package/resources/upload
func (c *APIClient) UploadResourcesFile(packageID, parent, fileName string, content []byte) (*Response, error) {
	return c.doRequestWithRetry(func() (*http.Request, error) {
		return c.buildMultipartRequest("/file/package/resources/upload", "file", fileName, content, map[string]string{
			"package_id": packageID,
			"parent":     parent,
		}, nil)
	})
}

// GetResourcesImagesPath 获取 Resources 下 png 图片的分组路径列表
// 对应 POST /file/package/resources/images/path/get
func (c *APIClient) GetResourcesImagesPath(packageID, keyWord string) (*Response, error) {
	return c.Post("/file/package/resources/images/path/get", map[string]any{
		"package_id": packageID,
		"key_word":   keyWord,
	}, nil)
}

// GetResourcesImageRaw 获取指定资源图片的原始响应
// 对应 GET /file/package/resources/images/get
func (c *APIClient) GetResourcesImageRaw(path string) (*http.Response, error) {
	return c.GetRawResponse("/file/package/resources/images/get", map[string]string{
		"path": path,
	})
}

// UploadModelIcon 上传模型图标图片
// 对应 POST /file/upload/icon
func (c *APIClient) UploadModelIcon(packageID, modelName, fileName string, content []byte) (*Response, error) {
	return c.doRequestWithRetry(func() (*http.Request, error) {
		return c.buildMultipartRequest("/file/upload/icon", "file", fileName, content, map[string]string{
			"package_id": packageID,
			"model_name": modelName,
		}, nil)
	})
}

// UploadModelVarFile 上传模型参数文件
// 对应 POST /file/upload/var
func (c *APIClient) UploadModelVarFile(packageID, modelName, componentName, fileName string, content []byte) (*Response, error) {
	return c.doRequestWithRetry(func() (*http.Request, error) {
		return c.buildMultipartRequest("/file/upload/var", "file", fileName, content, map[string]string{
			"package_id":     packageID,
			"model_name":     modelName,
			"component_name": componentName,
		}, nil)
	})
}

// UploadModelPackage 上传模型包文件
// 对应 POST /file/upload/package
func (c *APIClient) UploadModelPackage(fileName string, content []byte) (*Response, error) {
	return c.doRequestWithRetry(func() (*http.Request, error) {
		return c.buildMultipartRequest("/file/upload/package", "file", fileName, content, nil, nil)
	})
}

// UploadUserBackground 上传用户背景图
// 对应 POST /user/background/upload
func (c *APIClient) UploadUserBackground(fileName string, content []byte) (*Response, error) {
	return c.doRequestWithRetry(func() (*http.Request, error) {
		return c.buildMultipartRequest("/user/background/upload", "file", fileName, content, nil, nil)
	})
}

// WorkspaceChangePoll 获取工作空间轮询状态。
// 对应 GET /user/userspace/change/poll。
func (c *APIClient) WorkspaceChangePoll(packageID, modelName, tabID string) (*Response, error) {
	return c.Get("/user/userspace/change/poll", map[string]string{
		"package_id":       packageID,
		"model_name":       modelName,
		"last_version":     "0",
		"last_log_version": "0",
	}, map[string]string{
		"tab-id": tabID,
	})
}

// SetModelIcon 设置模型图标
// 对应 POST /file/icon/set
func (c *APIClient) SetModelIcon(packageID, modelName, path string) (*Response, error) {
	return c.Post("/file/icon/set", map[string]any{
		"package_id": packageID,
		"model_name": modelName,
		"path":       path,
	}, nil)
}

// CreateResourceFolder 创建资源库文件夹
// 对应 POST /api/v2/resource/folder/create
func (c *APIClient) CreateResourceFolder(parentID, name string) (*Response, error) {
	return c.Post("/api/v2/resource/folder/create", map[string]any{
		"parent_id": parentID,
		"name":      name,
	}, nil)
}

// UploadResourceFile 上传资源库文件
// 对应 POST /api/v2/resource/file/upload
func (c *APIClient) UploadResourceFile(parentID, filename, description, uploadFileName string, content []byte) (*Response, error) {
	return c.doRequestWithRetry(func() (*http.Request, error) {
		return c.buildMultipartRequest("/api/v2/resource/file/upload", "file", uploadFileName, content, map[string]string{
			"parent_id":   parentID,
			"filename":    filename,
			"description": description,
		}, nil)
	})
}

// GetResourceFileContent 获取资源文件内容
// 对应 GET /api/v2/resource/file/content/get
func (c *APIClient) GetResourceFileContent(id string) (*Response, error) {
	return c.Get("/api/v2/resource/file/content/get", map[string]string{
		"id": id,
	}, nil)
}

// ParseResourceFileContent 解析资源文件内容
// 对应 GET /api/v2/resource/file/content/parse
func (c *APIClient) ParseResourceFileContent(id string) (*Response, error) {
	return c.Get("/api/v2/resource/file/content/parse", map[string]string{
		"id": id,
	}, nil)
}

// CopyLibFileToResources 复制资源库文件到模型 Resources
// 对应 POST /api/v2/resource/file/resources/copy
func (c *APIClient) CopyLibFileToResources(id, packageID, parent string) (*Response, error) {
	return c.Post("/api/v2/resource/file/resources/copy", map[string]any{
		"id":         id,
		"package_id": packageID,
		"parent":     parent,
	}, nil)
}

// EditResourceInfo 编辑资源库文件夹/文件信息
// 对应 POST /api/v2/resource/edit
func (c *APIClient) EditResourceInfo(parentID, id, resourceType, name, description string) (*Response, error) {
	return c.Post("/api/v2/resource/edit", map[string]any{
		"parent_id":   parentID,
		"id":          id,
		"type":        resourceType,
		"name":        name,
		"description": description,
	}, nil)
}

// DeleteResource 删除资源库文件夹/文件
// 对应 POST /api/v2/resource/delete
func (c *APIClient) DeleteResource(id string) (*Response, error) {
	return c.Post("/api/v2/resource/delete", map[string]any{
		"id": id,
	}, nil)
}

// GetResourceRootList 获取资源库根目录列表
// 对应 GET /api/v2/resource/root/list/get
func (c *APIClient) GetResourceRootList() (*Response, error) {
	return c.Get("/api/v2/resource/root/list/get", nil, nil)
}

// GetResourceList 获取资源库子目录列表
// 对�� GET /api/v2/resource/list/get
func (c *APIClient) GetResourceList(parentID string, pageNum, pageSize int) (*Response, error) {
	return c.Get("/api/v2/resource/list/get", map[string]string{
		"parent_id": parentID,
		"page_num":  strconv.Itoa(pageNum),
		"page_size": strconv.Itoa(pageSize),
	}, nil)
}

// SearchResource 搜索资源库内容
// 对应 GET /api/v2/resource/search
func (c *APIClient) SearchResource(keywords string) (*Response, error) {
	return c.Get("/api/v2/resource/search", map[string]string{
		"keywords": keywords,
	}, nil)
}

// GetModelCode 获取模型代码
func (c *APIClient) GetModelCode(packageId string, modelName string) (*Response, error) {
	return c.Get("/model/code", map[string]string{
		"package_id": packageId,
		"model_name": modelName,
	}, nil)
}

// SearchModel 关键字搜索模型（GET /model/search）
// keywords 与 parent 都允许为空字符串。
func (c *APIClient) SearchModel(keywords, parent string) (*Response, error) {
	return c.Get("/model/search", map[string]string{
		"keywords": keywords,
		"parent":   parent,
	}, nil)
}

// SearchFunctionType 搜索类型为 function/record 的模型
// 对应 GET /model/function/search
func (c *APIClient) SearchFunctionType(parent string) (*Response, error) {
	return c.Get("/model/function/search", map[string]string{
		"parent": parent,
	}, nil)
}

// CreateCollection 新增模型收藏
// 对应 POST /model/collection/create
// 正常返回业务 JSON；package_id / model_name 不存在时后端返回 HTTP 400。
func (c *APIClient) CreateCollection(packageId, modelName string) (*Response, error) {
	return c.Post("/model/collection/create", map[string]any{
		"package_id": packageId,
		"model_name": modelName,
	}, nil)
}

// GetCollection 获取收藏模型列表
// 对应 GET /model/collection/get
// keywords 允许为空字符串，表示不过滤直接返回全部收藏。
func (c *APIClient) GetCollection(keywords string) (*Response, error) {
	return c.Get("/model/collection/get", map[string]string{
		"keywords": keywords,
	}, nil)
}

// DeleteCollection 删除收藏模型
// 对应 GET /model/collection/delete
// 该接口不存在“幂等成功”语义：id 不存在时后端返回 HTTP 400。
func (c *APIClient) DeleteCollection(id string) (*Response, error) {
	return c.Get("/model/collection/delete", map[string]string{
		"id": id,
	}, nil)
}

// GetModelInstance 获取模型实例图形数据
// 对应 POST /api/v3/model/instance
// body 使用 package_id / model_name / component_name，与 view 的 ModelGraphicsData 对齐。
func (c *APIClient) GetModelInstance(packageId, modelName, componentName string) (*Response, error) {
	body := map[string]any{
		"package_id": packageId,
		"model_name": modelName,
	}
	if componentName != "" {
		body["component_name"] = componentName
	}
	return c.Post("/api/v3/model/instance", body, nil)
}

// GetInstantiateCode 获取模型扁平化代码
// 对应 GET /api/v3/model/instantiate/code/get
func (c *APIClient) GetInstantiateCode(modelName string) (*Response, error) {
	return c.Get("/api/v3/model/instantiate/code/get", map[string]string{
		"model_name": modelName,
	}, nil)
}

// GetIconGraphics 获取模型图标图形数据
// 对应 POST /api/v3/model/icon/get
// body 使用 package_id / model_name / component_name，与 view 的 ModelGraphicsData 对齐。
func (c *APIClient) GetIconGraphics(packageId, modelName, componentName string) (*Response, error) {
	body := map[string]any{
		"package_id": packageId,
		"model_name": modelName,
	}
	if componentName != "" {
		body["component_name"] = componentName
	}
	return c.Post("/api/v3/model/icon/get", body, nil)
}

// GetIconGraphicsV2 获取模型图标图形数据（v2）
// 对应 POST /api/v2/model/icon/graphics
func (c *APIClient) GetIconGraphicsV2(packageId, modelName, componentName string) (*Response, error) {
	body := map[string]any{
		"package_id": packageId,
		"model_name": modelName,
	}
	if componentName != "" {
		body["component_name"] = componentName
	}
	return c.Post("/api/v2/model/icon/graphics", body, nil)
}

// GetIconInfo 获取图标参数信息
// 对应 GET /api/v3/model/icon/info
func (c *APIClient) GetIconInfo(modelName string) (*Response, error) {
	return c.Get("/api/v3/model/icon/info", map[string]string{
		"model_name": modelName,
	}, nil)
}

// GetIconInstance 获取模型图标实例信息
// 对应 GET /api/v3/model/icon/instance/get
func (c *APIClient) GetIconInstance(modelName string) (*Response, error) {
	return c.Get("/api/v3/model/icon/instance/get", map[string]string{
		"model_name": modelName,
	}, nil)
}

// GetModelInformation 获取模型所在库记录信息
// 对应 GET /api/v3/model/information/get
func (c *APIClient) GetModelInformation(modelName string) (*Response, error) {
	return c.Get("/api/v3/model/information/get", map[string]string{
		"model_name": modelName,
	}, nil)
}

// GetSubparameters 获取子参数参数列表
// 对应 POST /api/v3/model/subparameters/get
func (c *APIClient) GetSubparameters(parameterBase string) (*Response, error) {
	return c.Post("/api/v3/model/subparameters/get", map[string]any{
		"parameter_base": parameterBase,
	}, nil)
}

// ConvertUnits 执行单位换算
// 对应 POST /model/units/convert
func (c *APIClient) ConvertUnits(toUnit, fromUnit string) (*Response, error) {
	return c.Post("/model/units/convert", map[string]any{
		"s1": toUnit,
		"s2": fromUnit,
	}, nil)
}

// GetModelResourcesReference 获取模型包 Resources 目录的静态资源 Reference 列表
// 对应 POST /model/reference/resources
// package_id 为空时返回当前用户空间下各包的顶层 Resources 目录引用。
func (c *APIClient) GetModelResourcesReference(packageId, parent, path string) (*Response, error) {
	body := map[string]any{
		"package_id": packageId,
		"parent":     parent,
		"path":       path,
	}
	return c.Post("/model/reference/resources", body, nil)
}

// ModelExists 判断模型是否存在（GET /model/exists）
// 返回 data 形如 [bool]，单元素布尔数组。
func (c *APIClient) ModelExists(modelName string) (*Response, error) {
	return c.Get("/model/exists", map[string]string{
		"model_name": modelName,
	}, nil)
}

// 仿真相关接口

// ModelSimulateWithTranslate 执行带翻译的新 v4 仿真。
// 对应 POST /api/v4/simulation/simulate-with-translate。
func (c *APIClient) ModelSimulateWithTranslate(experimentId string) (*Response, error) {
	return c.Post("/api/v4/simulation/simulate-with-translate", map[string]any{
		"experiment_id": experimentId,
	}, nil)
}

// ModelSimulateWithTranslateRaw 提交带翻译的新 v4 仿真并返回原始 HTTP 响应。
// 对应 POST /api/v4/simulation/simulate-with-translate。
func (c *APIClient) ModelSimulateWithTranslateRaw(body any) (*http.Response, error) {
	return c.PostRawResponse("/api/v4/simulation/simulate-with-translate", body)
}

// 参数标定相关接口

// CalibrationTranslateFmu 生成参数标定准备记录。
// 对应 POST /calibration/translate/fmu。
func (c *APIClient) CalibrationTranslateFmu(packageID, modelName string) (*Response, error) {
	return c.Post("/calibration/translate/fmu", map[string]any{
		"package_id": packageID,
		"model_name": modelName,
	}, nil)
}

// CalibrationTranslateFmuRaw 生成参数标定准备记录并返回原始响应。
// 对应 POST /calibration/translate/fmu。
func (c *APIClient) CalibrationTranslateFmuRaw(body any) (*http.Response, error) {
	return c.PostRawResponse("/calibration/translate/fmu", body)
}

// ExportFmuRaw 导出模型 FMU 并返回原始 HTTP 响应。
// 对应 POST /file/fmu/export。
func (c *APIClient) ExportFmuRaw(body any) (*http.Response, error) {
	return c.PostRawResponse("/file/fmu/export", body)
}

// UploadCalibrationData 上传参数标定观测 CSV。
// 对应 POST /calibration/data/upload。
func (c *APIClient) UploadCalibrationData(id string, isEdit bool, fileName string, content []byte) (*Response, error) {
	return c.doRequestWithRetry(func() (*http.Request, error) {
		return c.buildMultipartRequest("/calibration/data/upload", "file", fileName, content, map[string]string{
			"id":      id,
			"is_edit": strconv.FormatBool(isEdit),
		}, nil)
	})
}

// CalibrationParseCSVColumns 解析参数标定 CSV 列。
// 对应 POST /calibration/data/columns/parse。
func (c *APIClient) CalibrationParseCSVColumns(id string, isEdit bool) (*Response, error) {
	return c.Post("/calibration/data/columns/parse", map[string]any{
		"id":      id,
		"is_edit": isEdit,
	}, nil)
}

// CalibrationParseCSVColumnsRaw 解析参数标定 CSV 列并返回原始响应。
// 对应 POST /calibration/data/columns/parse。
func (c *APIClient) CalibrationParseCSVColumnsRaw(body any) (*http.Response, error) {
	return c.PostRawResponse("/calibration/data/columns/parse", body)
}

// CalibrationCreate 创建参数标定记录。
// 对应 POST /calibration/create。
func (c *APIClient) CalibrationCreate(payload map[string]any) (*Response, error) {
	return c.Post("/calibration/create", payload, nil)
}

// CalibrationCreateRaw 创建参数标定记录并返回原始响应。
// 对应 POST /calibration/create。
func (c *APIClient) CalibrationCreateRaw(body any) (*http.Response, error) {
	return c.PostRawResponse("/calibration/create", body)
}

// CalibrationListRaw 获取参数标定记录列表 SSE 流。
// 对应 GET /calibration/list。
func (c *APIClient) CalibrationListRaw(packageID, modelName string, pageNum int) (*http.Response, error) {
	return c.GetRawResponse("/calibration/list", map[string]string{
		"package_id": packageID,
		"model_name": modelName,
		"page_num":   strconv.Itoa(pageNum),
	})
}

// CalibrationList 获取参数标定记录列表的非 SSE 错误响应。
// 对应 GET /calibration/list。
func (c *APIClient) CalibrationList(packageID, modelName string, pageNum int) (*Response, error) {
	return c.Get("/calibration/list", map[string]string{
		"package_id": packageID,
		"model_name": modelName,
		"page_num":   strconv.Itoa(pageNum),
	}, nil)
}

// CalibrationTree 获取参数标定变量树。
// 对应 GET /calibration/tree。
func (c *APIClient) CalibrationTree(id string, isEdit bool, parentNode, keyWords, model string) (*Response, error) {
	return c.Get("/calibration/tree", map[string]string{
		"id":          id,
		"is_edit":     strconv.FormatBool(isEdit),
		"parent_node": parentNode,
		"key_words":   keyWords,
		"model":       model,
	}, nil)
}

// CalibrationEdit 编辑参数标定记录。
// 对应 POST /calibration/edit。
func (c *APIClient) CalibrationEdit(payload map[string]any) (*Response, error) {
	return c.Post("/calibration/edit", payload, nil)
}

// CalibrationEditRaw 编辑参数标定记录并返回原始响应。
// 对应 POST /calibration/edit。
func (c *APIClient) CalibrationEditRaw(body any) (*http.Response, error) {
	return c.PostRawResponse("/calibration/edit", body)
}

// CalibrationDelete 删除参数标定记录。
// 对应 POST /calibration/delete。
func (c *APIClient) CalibrationDelete(id string) (*Response, error) {
	return c.Post("/calibration/delete", map[string]any{
		"id": id,
	}, nil)
}

// CalibrationDeleteRaw 删除参数标定记录并返回原始响应。
// 对应 POST /calibration/delete。
func (c *APIClient) CalibrationDeleteRaw(body any) (*http.Response, error) {
	return c.PostRawResponse("/calibration/delete", body)
}

// CalibrationStart 启动参数标定任务。
// 对应 POST /calibration/start。
func (c *APIClient) CalibrationStart(id string) (*Response, error) {
	return c.Post("/calibration/start", map[string]any{
		"id": id,
	}, nil)
}

// CalibrationStartRaw 启动参数标定任务并返回原始响应。
// 对应 POST /calibration/start。
func (c *APIClient) CalibrationStartRaw(body any) (*http.Response, error) {
	return c.PostRawResponse("/calibration/start", body)
}

// CalibrationStop 停止参数标定任务。
// 对应 POST /calibration/stop。
func (c *APIClient) CalibrationStop(id string) (*Response, error) {
	return c.Post("/calibration/stop", map[string]any{
		"id": id,
	}, nil)
}

// CalibrationDetail 获取参数标定详情的非 SSE 错误响应。
// 对应 GET /calibration/detail。
func (c *APIClient) CalibrationDetail(id string) (*Response, error) {
	return c.Get("/calibration/detail", map[string]string{
		"id": id,
	}, nil)
}

// CalibrationDetailRaw 获取参数标定详情 SSE 流。
// 对应 GET /calibration/detail。
func (c *APIClient) CalibrationDetailRaw(id string) (*http.Response, error) {
	return c.GetRawResponse("/calibration/detail", map[string]string{
		"id": id,
	})
}

// CalibrationSaveExperimentParameter 保存标定结果到原实验参数。
// 对应 POST /calibration/experiment/parameter/save。
func (c *APIClient) CalibrationSaveExperimentParameter(id string) (*Response, error) {
	return c.Post("/calibration/experiment/parameter/save", map[string]any{
		"id": id,
	}, nil)
}

// CalibrationSaveExperimentParameterRaw 保存标定结果到原实验参数并返回原始响应。
// 对应 POST /calibration/experiment/parameter/save。
func (c *APIClient) CalibrationSaveExperimentParameterRaw(body any) (*http.Response, error) {
	return c.PostRawResponse("/calibration/experiment/parameter/save", body)
}

// CalibrationSaveNewExperimentParameter 保存标定结果到新实验参数。
// 对应 POST /calibration/experiment/parameter/save/new。
func (c *APIClient) CalibrationSaveNewExperimentParameter(id string) (*Response, error) {
	return c.Post("/calibration/experiment/parameter/save/new", map[string]any{
		"id": id,
	}, nil)
}

// CalibrationSaveNewExperimentParameterRaw 保存标定结果到新实验参数并返回原始响应。
// 对应 POST /calibration/experiment/parameter/save/new。
func (c *APIClient) CalibrationSaveNewExperimentParameterRaw(body any) (*http.Response, error) {
	return c.PostRawResponse("/calibration/experiment/parameter/save/new", body)
}

// SetRealtimeFmuVariables 设置实时仿真的曲线变量。
// 对应 POST /simulation/realtime-fmu/variables/set。
func (c *APIClient) SetRealtimeFmuVariables(experimentId string, variables []string) (*Response, error) {
	return c.Post("/simulation/realtime-fmu/variables/set", map[string]any{
		"experiment_id": experimentId,
		"variables":     variables,
	}, nil)
}

// SetRealtimeFmuVariablesRaw 设置实时仿真的曲线变量并返回原始 HTTP 响应。
// 对应 POST /simulation/realtime-fmu/variables/set。
func (c *APIClient) SetRealtimeFmuVariablesRaw(body any) (*http.Response, error) {
	return c.PostRawResponse("/simulation/realtime-fmu/variables/set", body)
}

// SetRealtimeFmuParameters 设置实时仿真的在线调参参数。
// 对应 POST /simulation/realtime-fmu/parameters/set。
func (c *APIClient) SetRealtimeFmuParameters(experimentId string, parameters []map[string]any) (*Response, error) {
	return c.Post("/simulation/realtime-fmu/parameters/set", map[string]any{
		"experiment_id": experimentId,
		"parameters":    parameters,
	}, nil)
}

// SetRealtimeFmuParametersRaw 设置实时仿真的在线调参参数并返回原始 HTTP 响应。
// 对应 POST /simulation/realtime-fmu/parameters/set。
func (c *APIClient) SetRealtimeFmuParametersRaw(body any) (*http.Response, error) {
	return c.PostRawResponse("/simulation/realtime-fmu/parameters/set", body)
}

// GetRealtimeFmuStream 获取实时 FMU 流接口的非流式错误响应。
// 对应 GET /simulation/realtime-fmu/stream。
func (c *APIClient) GetRealtimeFmuStream(experimentId, taskId, lastId string) (*Response, error) {
	params := map[string]string{
		"experiment_id": experimentId,
	}
	if taskId != "" {
		params["task_id"] = taskId
	}
	if lastId != "" {
		params["last_id"] = lastId
	}
	return c.Get("/simulation/realtime-fmu/stream", params, nil)
}

// GetRealtimeFmuStreamRaw 获取实时 FMU SSE 流原始响应。
// 对应 GET /simulation/realtime-fmu/stream。
func (c *APIClient) GetRealtimeFmuStreamRaw(experimentId, taskId, lastId string) (*http.Response, error) {
	params := map[string]string{
		"experiment_id": experimentId,
	}
	if taskId != "" {
		params["task_id"] = taskId
	}
	if lastId != "" {
		params["last_id"] = lastId
	}
	return c.GetRawResponse("/simulation/realtime-fmu/stream", params)
}

// GetSimulationOptions 获取旧版模型仿真参数
// 对应 GET /simulation/options/get。
func (c *APIClient) GetSimulationOptions(modelName string) (*Response, error) {
	return c.Get("/simulation/options/get", map[string]string{
		"model_name": modelName,
	}, nil)
}

// SetSimulationOptions 设置旧版模型仿真参数。
// 对应 POST /simulation/options/set。
// 注意该接口当前使用扁平 body：package_id / model_name / startTime / stopTime / ...。
func (c *APIClient) SetSimulationOptions(packageId, modelName string, options map[string]any) (*Response, error) {
	body := map[string]any{
		"package_id": packageId,
		"model_name": modelName,
	}
	for k, v := range options {
		body[k] = v
	}
	return c.Post("/simulation/options/set", body, nil)
}

// SetSimulationOptionsRaw 设置旧版模型仿真参数并返回原始 HTTP 响应。
// 对应 POST /simulation/options/set。
func (c *APIClient) SetSimulationOptionsRaw(body any) (*http.Response, error) {
	return c.PostRawResponse("/simulation/options/set", body)
}

// GetSimulationState 获取旧版单模型仿真状态。
// 对应 GET /simulation/state/get。
func (c *APIClient) GetSimulationState(packageId, modelName string) (*Response, error) {
	return c.Get("/simulation/state/get", map[string]string{
		"package_id": packageId,
		"model_name": modelName,
	}, nil)
}

// GetAllSimulationStateV2Raw 获取 v2 全空间仿真状态 SSE 原始响应。
// 对应 GET /api/v2/simulation/state/get/all。
func (c *APIClient) GetAllSimulationStateV2Raw() (*http.Response, error) {
	return c.GetRawResponse("/api/v2/simulation/state/get/all", nil)
}

// GetSimulationResultFileV2 获取仿真结果文件下载路径。
// 对应 POST /api/v2/file/result/all/get。
func (c *APIClient) GetSimulationResultFileV2(experimentID string) (*Response, error) {
	return c.Post("/api/v2/file/result/all/get", map[string]any{
		"experiment_id": experimentID,
	}, nil)
}

// GetSimulationResultFileV2Raw 获取仿真结果文件下载路径的原始 HTTP 响应。
// 对应 POST /api/v2/file/result/all/get。
func (c *APIClient) GetSimulationResultFileV2Raw(body any) (*http.Response, error) {
	return c.PostRawResponse("/api/v2/file/result/all/get", body)
}

// GetSimulationResult 获取仿真结果
func (c *APIClient) GetSimulationResult(recordID string) (*Response, error) {
	return c.Get("/simulation/record/details", map[string]string{
		"id": recordID,
	}, nil)
}

// DeleteSimulationRecord 删除旧版仿真结果记录。
// 对应 GET /simulation/record/delete。
// 该接口当前使用 GET + JSON body 的历史契约，recordId 实际匹配 task_id。
func (c *APIClient) DeleteSimulationRecord(recordId string) (*Response, error) {
	return c.doRequestWithRetry(func() (*http.Request, error) {
		return c.buildRequest(http.MethodGet, "/simulation/record/delete", nil, map[string]any{
			"record_id": recordId,
		}, nil, true)
	})
}

// DeleteSimulationRecordRaw 删除旧版仿真结果记录并返回原始 HTTP 响应。
// 对应 GET /simulation/record/delete。
func (c *APIClient) DeleteSimulationRecordRaw(body any) (*http.Response, error) {
	return c.doRawRequestWithRetry(func() (*http.Request, error) {
		return c.buildRequest(http.MethodGet, "/simulation/record/delete", nil, body, nil, true)
	})
}

// SimulationRecordTerminateV3 终止 v3 仿真任务。
// 对应 POST /api/v3/simulation/record/terminate。
func (c *APIClient) SimulationRecordTerminateV3(experimentId string) (*Response, error) {
	return c.Post("/api/v3/simulation/record/terminate", map[string]any{
		"experiment_id": experimentId,
	}, nil)
}

// SimulationRecordTerminateV3Raw 终止 v3 仿真任务并返回原始 HTTP 响应。
// 对应 POST /api/v3/simulation/record/terminate。
func (c *APIClient) SimulationRecordTerminateV3Raw(body any) (*http.Response, error) {
	return c.PostRawResponse("/api/v3/simulation/record/terminate", body)
}

// ExperimentExists 判断实验是否存在。
// 对应 POST /simulation/experiment/exists。
func (c *APIClient) ExperimentExists(packageId, modelName string, simulateVarData map[string]string, modelVarData any) (*Response, error) {
	body := map[string]any{
		"package_id":        packageId,
		"model_name":        modelName,
		"simulate_var_data": simulateVarData,
	}
	if modelVarData != nil {
		body["model_var_data"] = modelVarData
	}
	return c.Post("/simulation/experiment/exists", body, nil)
}

// ExperimentExistsRaw 判断实验是否存在并返回原始 HTTP 响应。
// 对应 POST /simulation/experiment/exists。
func (c *APIClient) ExperimentExistsRaw(body any) (*http.Response, error) {
	return c.PostRawResponse("/simulation/experiment/exists", body)
}

// GetExperimentCleanupStats 获取可清理仿真结果统计。
// 对应 GET /api/v3/storage/experiment/result/count。
func (c *APIClient) GetExperimentCleanupStats(soonerThan string) (*Response, error) {
	return c.Get("/api/v3/storage/experiment/result/count", map[string]string{
		"sooner_than": soonerThan,
	}, nil)
}

// GetExperimentCleanupStatsRaw 获取可清理仿真结果统计的原始 HTTP 响应。
// 对应 GET /api/v3/storage/experiment/result/count。
func (c *APIClient) GetExperimentCleanupStatsRaw(params map[string]string) (*http.Response, error) {
	return c.GetRawResponse("/api/v3/storage/experiment/result/count", params)
}

// ExperimentCleanup 清理仿真结果数据。
// 对应 POST /api/v3/storage/experiment/result/cleanup。
func (c *APIClient) ExperimentCleanup(soonerThan string) (*Response, error) {
	return c.Post("/api/v3/storage/experiment/result/cleanup", map[string]any{
		"sooner_than": soonerThan,
	}, nil)
}

// ExperimentCleanupRaw 清理仿真结果数据并返回原始 HTTP 响应。
// 对应 POST /api/v3/storage/experiment/result/cleanup。
func (c *APIClient) ExperimentCleanupRaw(body any) (*http.Response, error) {
	return c.PostRawResponse("/api/v3/storage/experiment/result/cleanup", body)
}

// GetSimulationRecordListV2 获取仿真记录列表。
// 对应 GET /api/v2/simulation/record/list。
func (c *APIClient) GetSimulationRecordListV2(packageId, modelName string, pageNum int) (*Response, error) {
	return c.Get("/api/v2/simulation/record/list", map[string]string{
		"package_id": packageId,
		"model_name": modelName,
		"page_num":   strconv.Itoa(pageNum),
	}, nil)
}

// GetSimulationRecordList 获取仿真记录列表（兼容旧测试签名）。
// 旧调用方未传 package_id/page_num，这里默认查询第一页。
func (c *APIClient) GetSimulationRecordList(modelName string) (*Response, error) {
	return c.GetSimulationRecordListV2("", modelName, 1)
}

// GetSimulationRecordFinishedV2 获取已完成仿真记录列表。
// 对应 GET /api/v2/simulation/record/get/finished。
func (c *APIClient) GetSimulationRecordFinishedV2(packageId, modelName string) (*Response, error) {
	return c.Get("/api/v2/simulation/record/get/finished", map[string]string{
		"package_id": packageId,
		"model_name": modelName,
	}, nil)
}

// GetSimulationRecordTreeV2 获取仿真结果树。
// 对应 POST /api/v2/simulation/record/tree。
func (c *APIClient) GetSimulationRecordTreeV2(experimentIdList []string, parentNode, keyWords string) (*Response, error) {
	body := map[string]any{
		"experiment_id_list": experimentIdList,
	}
	if parentNode != "" {
		body["parent_node"] = parentNode
	}
	if keyWords != "" {
		body["key_words"] = keyWords
	}
	return c.Post("/api/v2/simulation/record/tree", body, nil)
}

// GetSimulationRecordTreeV2Raw 获取仿真结果树接口的原始 HTTP 响应。
// 对应 POST /api/v2/simulation/record/tree。
func (c *APIClient) GetSimulationRecordTreeV2Raw(body map[string]any) (*http.Response, error) {
	return c.PostRawResponse("/api/v2/simulation/record/tree", body)
}

// GetSimulationResultV2 获取单变量仿真结果。
// 对应 POST /api/v2/simulation/result。
func (c *APIClient) GetSimulationResultV2(recordIdList []string, variable string) (*Response, error) {
	return c.Post("/api/v2/simulation/result", map[string]any{
		"id":       recordIdList,
		"variable": variable,
	}, nil)
}

// GetSimulationResultV2Raw 获取单变量仿真结果接口的原始 HTTP 响应。
// 对应 POST /api/v2/simulation/result。
func (c *APIClient) GetSimulationResultV2Raw(body map[string]any) (*http.Response, error) {
	return c.PostRawResponse("/api/v2/simulation/result", body)
}

// GetSimulationResultSingularV2 获取多变量仿真结果。
// 对应 POST /api/v2/simulation/result/singular。
func (c *APIClient) GetSimulationResultSingularV2(items []map[string]any) (*Response, error) {
	return c.Post("/api/v2/simulation/result/singular", items, nil)
}

// GetSimulationResultSingularV2Raw 获取多变量仿真结果接口的原始 HTTP 响应。
// 对应 POST /api/v2/simulation/result/singular。
func (c *APIClient) GetSimulationResultSingularV2Raw(body any) (*http.Response, error) {
	return c.PostRawResponse("/api/v2/simulation/result/singular", body)
}

// SnapshotCreate 创建快照
// 对应 POST /simulation/snapshot/create
func (c *APIClient) SnapshotCreate(payload map[string]any) (*Response, error) {
	return c.Post("/simulation/snapshot/create", payload, nil)
}

// SnapshotList 获取快照列表
// 对应 GET /simulation/snapshot/list
func (c *APIClient) SnapshotList(modelName, packageId string) (*Response, error) {
	return c.Get("/simulation/snapshot/list", map[string]string{
		"model_name": modelName,
		"package_id": packageId,
	}, nil)
}

// SnapshotDetail 获取快照详情
// 对应 GET /simulation/snapshot/detail
func (c *APIClient) SnapshotDetail(snapshotId string) (*Response, error) {
	return c.Get("/simulation/snapshot/detail", map[string]string{
		"snapshot_id": snapshotId,
	}, nil)
}

// SnapshotDelete 删除快照
// 对应 POST /simulation/snapshot/delete
func (c *APIClient) SnapshotDelete(snapshotId string) (*Response, error) {
	return c.Post("/simulation/snapshot/delete", map[string]any{
		"snapshot_id": snapshotId,
	}, nil)
}

// SnapshotEdit 编辑快照
// 对应 POST /simulation/snapshot/edit
func (c *APIClient) SnapshotEdit(payload map[string]any) (*Response, error) {
	return c.Post("/simulation/snapshot/edit", payload, nil)
}

// 实验相关接口

// ExperimentCreate 创建仿真实验
func (c *APIClient) ExperimentCreate(packageId, modelName, experimentName string, simulateVarData map[string]string) (*Response, error) {
	return c.Post("/api/v2/simulation/experiment/create", map[string]any{
		"package_id":        packageId,
		"model_name":        modelName,
		"experiment_name":   experimentName,
		"simulate_var_data": simulateVarData,
	}, nil)
}

// ExperimentGet 获取仿真实验列表
func (c *APIClient) ExperimentGet(packageId, modelName string) (*Response, error) {
	return c.Get("/api/v2/simulation/experiment/list", map[string]string{
		"package_id": packageId,
		"model_name": modelName,
	}, nil)
}

// ExperimentDelete 删除仿真实验
func (c *APIClient) ExperimentDelete(experimentId string) (*Response, error) {
	return c.Post("/api/v2/simulation/experiment/delete", map[string]any{
		"experiment_id": experimentId,
	}, nil)
}

// ExperimentOptionsSet 设置实验仿真选项
// 对应 POST /api/v2/simulation/experiment/options/set
// 替代原 ExperimentEdit 中"修改 startTime/stopTime/method/..." 的语义。
// options 内的字段名遵循 SetSimulationOptionsDataV2，例如 start_time / stop_time /
// number_of_intervals / interval / method / tolerance / multiple_run_sim。
func (c *APIClient) ExperimentOptionsSet(experimentId string, options map[string]any) (*Response, error) {
	body := map[string]any{
		"experiment_id": experimentId,
	}
	for k, v := range options {
		body[k] = v
	}
	return c.Post("/api/v2/simulation/experiment/options/set", body, nil)
}

// ExperimentParametersSet 设置实验模型参数
// 对应 POST /api/v2/simulation/experiment/parameters/set
// parameters 数组每一项需包含 element_name / parameter_name / parameter_value 等字段，
// 与 simulate.ParameterData 结构对齐。
func (c *APIClient) ExperimentParametersSet(experimentId string, parameters []map[string]any) (*Response, error) {
	return c.Post("/api/v2/simulation/experiment/parameters/set", map[string]any{
		"experiment_id": experimentId,
		"parameter":     parameters,
	}, nil)
}

// ExperimentParametersDelete 删除实验模型参数
// 对应 POST /api/v2/simulation/experiment/parameters/delete
func (c *APIClient) ExperimentParametersDelete(experimentId string, parameterNames []string) (*Response, error) {
	return c.Post("/api/v2/simulation/experiment/parameters/delete", map[string]any{
		"experiment_id":       experimentId,
		"parameter_name_list": parameterNames,
	}, nil)
}

// ExperimentExportVariableSet 设置导出变量
// 对应 POST /api/v2/simulation/experiment/export_variables/set
func (c *APIClient) ExperimentExportVariableSet(experimentId, variableName string, isComponent bool) (*Response, error) {
	return c.Post("/api/v2/simulation/experiment/export_variables/set", map[string]any{
		"experiment_id": experimentId,
		"variable_name": variableName,
		"is_component":  isComponent,
	}, nil)
}

// ExperimentExportVariableUnset 取消导出变量
// 对应 POST /api/v2/simulation/experiment/export_variables/unset
// 注意路由为 unset 而非 delete。
func (c *APIClient) ExperimentExportVariableUnset(experimentId, variableName string) (*Response, error) {
	return c.Post("/api/v2/simulation/experiment/export_variables/unset", map[string]any{
		"experiment_id": experimentId,
		"variable_name": variableName,
	}, nil)
}

// ExperimentNameEdit 修改仿真实验名称
// 对应 POST /simulation/experiment/name/edit
func (c *APIClient) ExperimentNameEdit(packageId, modelName, experimentId, newExperimentName string) (*Response, error) {
	return c.Post("/simulation/experiment/name/edit", map[string]any{
		"package_id":          packageId,
		"model_name":          modelName,
		"experiment_id":       experimentId,
		"new_experiment_name": newExperimentName,
	}, nil)
}

// ExperimentDetails 获取实验详情（/simulation/experiment/detail）
// 返回的 data 包含：
//   - simulate_var_data: { startTime, stopTime, method, ... } 仿真选项
//   - parameters: [ {name, value, unit, displayUnit}, ... ] 模型参数列表
//   - export_variables: 导出变量列表
//
// 该接口已替代旧的 /simulation/experiment/parameters，统一作为参数/选项的查询入口。
func (c *APIClient) ExperimentDetails(packageId, modelName, experimentId string) (*Response, error) {
	return c.Get("/simulation/experiment/detail", map[string]string{
		"package_id":    packageId,
		"model_name":    modelName,
		"experiment_id": experimentId,
	}, nil)
}

// ExperimentCopy 复制仿真实验
// 对应 POST /simulation/experiment/copy
func (c *APIClient) ExperimentCopy(sourceExperimentId, newExperimentName string) (*Response, error) {
	return c.Post("/simulation/experiment/copy", map[string]any{
		"source_experiment_id": sourceExperimentId,
		"new_experiment_name":  newExperimentName,
	}, nil)
}

// ExperimentCaseInfo 获取实验 case 信息
// 对应 GET /simulation/experiment/case-info
// 返回 data.case_count / data.case_size / data.total_size。
func (c *APIClient) ExperimentCaseInfo(experimentId string) (*Response, error) {
	return c.Get("/simulation/experiment/case-info", map[string]string{
		"experiment_id": experimentId,
	}, nil)
}

// SetDefaultExperiment 将指定仿真实验设置为默认实验。
// 对应 POST /simulation/experiment/default/set。
func (c *APIClient) SetDefaultExperiment(experimentId string) (*Response, error) {
	return c.Post("/simulation/experiment/default/set", map[string]any{
		"experiment_id": experimentId,
	}, nil)
}

// SetDefaultExperimentRaw 使用原始请求体设置默认实验。
// 对应 POST /simulation/experiment/default/set。
func (c *APIClient) SetDefaultExperimentRaw(body any) (*http.Response, error) {
	return c.PostRawResponse("/simulation/experiment/default/set", body)
}

// 用户相关接口

// GetUserSpaceNew 获取新版用户空间列表
// 对应 GET /user/userspace/get/new
// keywords / collect 允许为空字符串，表示不过滤。
func (c *APIClient) GetUserSpaceNew(keywords, collect string) (*Response, error) {
	return c.Get("/user/userspace/get/new", map[string]string{
		"keywords": keywords,
		"collect":  collect,
	}, nil)
}

// GetUserSpaceV2 获取 v2 用户空间列表。
// 对应 GET /api/v2/user/userspace/get；page/pageSize 小于 1 时表示不启用分页。
func (c *APIClient) GetUserSpaceV2(keywords, collect string, page, pageSize int) (*Response, error) {
	params := map[string]string{
		"keywords": keywords,
		"collect":  collect,
	}
	if page > 0 || pageSize > 0 {
		params["page"] = strconv.Itoa(page)
		params["page_size"] = strconv.Itoa(pageSize)
	}
	return c.Get("/api/v2/user/userspace/get", params, nil)
}

// GetUserSpaceUsage 获取用户空间资源占用和任务状态。
// 对应 GET /api/v2/user/userspace/usage；withStatus 为空时使用接口默认值。
func (c *APIClient) GetUserSpaceUsage(keywords, collect string, page, pageSize int, withStatus string) (*Response, error) {
	params := map[string]string{
		"keywords": keywords,
		"collect":  collect,
	}
	if page > 0 || pageSize > 0 {
		params["page"] = strconv.Itoa(page)
		params["page_size"] = strconv.Itoa(pageSize)
	}
	if withStatus != "" {
		params["with_status"] = withStatus
	}
	return c.GetUserSpaceUsageRaw(params)
}

// GetUserSpaceUsageRaw 使用指定 query 获取用户空间资源占用和任务状态。
// 对应 GET /api/v2/user/userspace/usage，用于覆盖非法 query 等场景。
func (c *APIClient) GetUserSpaceUsageRaw(params map[string]string) (*Response, error) {
	return c.Get("/api/v2/user/userspace/usage", params, nil)
}

// GetUserSpace 获取旧版用户空间列表
// 对应 GET /user/userspace/get
func (c *APIClient) GetUserSpace() (*Response, error) {
	return c.Get("/user/userspace/get", nil, nil)
}

// GetUserExamples 获取示例数据
// 对应 GET /user/examples
func (c *APIClient) GetUserExamples() (*Response, error) {
	return c.Get("/user/examples", nil, nil)
}

// CreateUserSpace 创建用户空间
// 对应 POST /user/userspace/create
func (c *APIClient) CreateUserSpace(spaceName, description, background, icon, iconColor string) (*Response, error) {
	return c.Post("/user/userspace/create", map[string]any{
		"space_name":        spaceName,
		"space_description": description,
		"space_background":  background,
		"space_icon":        icon,
		"space_icon_color":  iconColor,
	}, nil)
}

// DeleteUserSpace 删除用户空间
// 对应 POST /user/userspace/delete
func (c *APIClient) DeleteUserSpace(spaceIDs []string) (*Response, error) {
	return c.Post("/user/userspace/delete", map[string]any{
		"space_id": spaceIDs,
	}, nil)
}

// EditUserSpace 编辑用户空间
// 对应 POST /user/userspace/edit
func (c *APIClient) EditUserSpace(spaceId, spaceName, description, background, icon, iconColor string) (*Response, error) {
	return c.Post("/user/userspace/edit", map[string]any{
		"space_id":          spaceId,
		"space_name":        spaceName,
		"space_description": description,
		"space_background":  background,
		"space_icon":        icon,
		"space_icon_color":  iconColor,
	}, nil)
}

// CollectUserSpace 收藏或取消收藏用户空间
// 对应 POST /user/userspace/collect
func (c *APIClient) CollectUserSpace(spaceIDs []string, collect bool) (*Response, error) {
	return c.Post("/user/userspace/collect", map[string]any{
		"space_id": spaceIDs,
		"collect":  collect,
	}, nil)
}

// ClearUserSpaceTaskLogs 清除指定模型的工作空间任务日志游标。
// 对应 POST /user/userspace/task-log/clear；tabID 用于模拟前端标签页。
func (c *APIClient) ClearUserSpaceTaskLogs(packageID, modelName string, logTypes []string, tabID string) (*Response, error) {
	return c.ClearUserSpaceTaskLogsRaw(map[string]any{
		"package_id": packageID,
		"model_name": modelName,
		"log_types":  logTypes,
	}, tabID)
}

// ClearUserSpaceTaskLogsRaw 使用指定 body 清除工作空间任务日志游标。
// 对应 POST /user/userspace/task-log/clear，用于覆盖缺必填或非法 log_types 场景。
func (c *APIClient) ClearUserSpaceTaskLogsRaw(body any, tabID string) (*Response, error) {
	return c.Post("/user/userspace/task-log/clear", body, map[string]string{
		"tab-id": tabID,
	})
}

// TerminateAllUserSpaceTasks 停止当前或指定工作空间下的所有任务。
// 对应 POST /user/userspace/task/terminate/all；spaceID 为空时由后端使用请求头 space-id。
func (c *APIClient) TerminateAllUserSpaceTasks(spaceID string) (*Response, error) {
	return c.Post("/user/userspace/task/terminate/all", map[string]any{
		"space-id": spaceID,
	}, nil)
}

// GetUserRecentlyOpened 获取最近打开的用户空间列表
// 对应 GET /user/userspace/recent
func (c *APIClient) GetUserRecentlyOpened() (*Response, error) {
	return c.Get("/user/userspace/recent", nil, nil)
}

// GetUserSettings 获取用户设置
// 对应 GET /user/settings/get
func (c *APIClient) GetUserSettings() (*Response, error) {
	return c.Get("/user/settings/get", nil, nil)
}

// SetUserSettings 设置用户设置
// 对应 POST /user/settings/set
func (c *APIClient) SetUserSettings(gridDisplay bool) (*Response, error) {
	// 保留旧 helper 签名给只关心 grid_display 的用例；新用例需要覆盖 interactive 时使用 SetUserSettingsFull。
	return c.SetUserSettingsFull(gridDisplay, false)
}

// SetUserSettingsFull 设置完整用户设置
// 对应 POST /user/settings/set
func (c *APIClient) SetUserSettingsFull(gridDisplay, interactive bool) (*Response, error) {
	return c.Post("/user/settings/set", map[string]any{
		"grid_display": gridDisplay,
		"interactive":  interactive,
	}, nil)
}

// GetLoadStatus 获取工作空间加载状态（GET /user/userspace/model/loadstatus/get）
// 响应 data.workspace_status: 0=未启动 1=启动中 2=需加载 3=就绪 4=OOM重启 5=异常
func (c *APIClient) GetLoadStatus() (*Response, error) {
	return c.Get("/user/userspace/model/loadstatus/get", nil, nil)
}

// LoadstatusSet 设置工作空间状态为 Ready(3)
func (c *APIClient) LoadstatusSet() (*Response, error) {
	return c.Post("/user/userspace/model/loadstatus/set", nil, nil)
}

// GetListModel 获取左侧模型树子节点（GET /model/list_library）
// packageId: 模型包的 id
// modelName: 模型的父节点名称
func (c *APIClient) GetListModel(packageId, modelName string) (*Response, error) {
	return c.Get("/model/list_library", map[string]string{
		"package_id": packageId,
		"model_name": modelName,
	}, nil)
}

// GetModelComponents 获取模型的全部组件数据（GET /model/components/get）
// modelName: 需要查询属性数据的模型名称，全称，例如 "Modelica.Blocks.Examples.PID_Controller"
func (c *APIClient) GetModelComponents(modelName string) (*Response, error) {
	return c.Get("/model/components/get", map[string]string{
		"model_name": modelName,
	}, nil)
}

// GetModelDocument 获取模型的文档数据（GET /model/document/get）
// modelName: 需要查询文档的模型名称，全称，例如 "Modelica.Blocks.Examples.PID_Controller"
func (c *APIClient) GetModelDocument(modelName string) (*Response, error) {
	return c.Get("/model/document/get", map[string]string{
		"model_name": modelName,
	}, nil)
}

// SetModelDocument 设置模型文档
// 对应 POST /model/document/set
func (c *APIClient) SetModelDocument(packageId, modelName, document, revisions string) (*Response, error) {
	return c.Post("/model/document/set", map[string]any{
		"package_id": packageId,
		"model_name": modelName,
		"document":   document,
		"revisions":  revisions,
	}, nil)
}

// GetUML 获取模型/包的 UML 数据
// 对应 GET /model/uml/get
func (c *APIClient) GetUML(className string) (*Response, error) {
	return c.Get("/model/uml/get", map[string]string{
		"className": className,
	}, nil)
}

// GetExtendedModel 获取模型继承的父类列表
// 对应 GET /model/extend/get
func (c *APIClient) GetExtendedModel(modelName string) (*Response, error) {
	return c.Get("/model/extend/get", map[string]string{
		"model_name": modelName,
	}, nil)
}

// GetParametersOptions 获取介质参数的选项信息
// 对应 GET /model/parameters/options/get
func (c *APIClient) GetParametersOptions(className, parentName, choicesAllMatching string) (*Response, error) {
	return c.Get("/model/parameters/options/get", map[string]string{
		"class_name":           className,
		"parent_name":          parentName,
		"choices_all_matching": choicesAllMatching,
	}, nil)
}

// GetComponentName 获取模型组件默认名称
// 对应 GET /model/component/name
func (c *APIClient) GetComponentName(modelName, className string) (*Response, error) {
	return c.Get("/model/component/name", map[string]string{
		"model_name": modelName,
		"class_name": className,
	}, nil)
}

// GetAllUserspaceModel 获取用户空间所有模型信息
// 对应 POST /api/v2/model/userspace/model/getall
func (c *APIClient) GetAllUserspaceModel() (*Response, error) {
	return c.Post("/api/v2/model/userspace/model/getall", map[string]any{}, nil)
}

// ParseTranslateLog 解析模型翻译日志。
// 对应 POST /model/translate/log/parse。
func (c *APIClient) ParseTranslateLog(logText string) (*Response, error) {
	return c.Post("/model/translate/log/parse", map[string]any{
		"log_text": logText,
	}, nil)
}

// ParseTranslateLogRaw 使用原始请求体解析模型翻译日志。
// 对应 POST /model/translate/log/parse。
func (c *APIClient) ParseTranslateLogRaw(body any) (*http.Response, error) {
	return c.PostRawResponse("/model/translate/log/parse", body)
}

// UploadPipeNetInfoFile 上传管网信息 XML 文件。
// 对应 POST /pipenet/info/upload。
func (c *APIClient) UploadPipeNetInfoFile(filename, description, uploadFileName string, content []byte) (*Response, error) {
	return c.doRequestWithRetry(func() (*http.Request, error) {
		return c.buildMultipartRequest("/pipenet/info/upload", "file", uploadFileName, content, map[string]string{
			"filename":    filename,
			"description": description,
		}, nil)
	})
}

// DownloadPipeNetInfoFile 下载管网信息文件。
// 对应 POST /pipenet/info/download。
func (c *APIClient) DownloadPipeNetInfoFile(idList []string) (*Response, error) {
	return c.Post("/pipenet/info/download", map[string]any{
		"id_list": idList,
	}, nil)
}

// DownloadPipeNetInfoFileRaw 使用原始请求体下载管网信息文件。
// 对应 POST /pipenet/info/download。
func (c *APIClient) DownloadPipeNetInfoFileRaw(body any) (*http.Response, error) {
	return c.PostRawResponse("/pipenet/info/download", body)
}

// DeletePipeNetInfoFile 删除管网信息文件。
// 对应 POST /pipenet/info/delete。
func (c *APIClient) DeletePipeNetInfoFile(idList []string) (*Response, error) {
	return c.Post("/pipenet/info/delete", map[string]any{
		"id_list": idList,
	}, nil)
}

// DeletePipeNetInfoFileRaw 使用原始请求体删除管网信息文件。
// 对应 POST /pipenet/info/delete。
func (c *APIClient) DeletePipeNetInfoFileRaw(body any) (*http.Response, error) {
	return c.PostRawResponse("/pipenet/info/delete", body)
}

// GetPipeNetInfoFileList 获取管网信息文件列表。
// 对应 GET /pipenet/info/list/get。
func (c *APIClient) GetPipeNetInfoFileList(keywords string, pageNum int) (*Response, error) {
	return c.Get("/pipenet/info/list/get", map[string]string{
		"keywords": keywords,
		"page_num": strconv.Itoa(pageNum),
	}, nil)
}

// EditPipeNetInfoFile 编辑管网信息文件基本信息。
// 对应 POST /pipenet/info/edit。
func (c *APIClient) EditPipeNetInfoFile(id, name, description string) (*Response, error) {
	return c.Post("/pipenet/info/edit", map[string]any{
		"id":          id,
		"name":        name,
		"description": description,
	}, nil)
}

// EditPipeNetInfoFileRaw 使用原始请求体编辑管网信息文件基本信息。
// 对应 POST /pipenet/info/edit。
func (c *APIClient) EditPipeNetInfoFileRaw(body any) (*http.Response, error) {
	return c.PostRawResponse("/pipenet/info/edit", body)
}

// CopyPipeNetInfoFile 复制管网信息文件。
// 对应 POST /pipenet/info/copy。
func (c *APIClient) CopyPipeNetInfoFile(idList []string) (*Response, error) {
	return c.Post("/pipenet/info/copy", map[string]any{
		"id_list": idList,
	}, nil)
}

// CopyPipeNetInfoFileRaw 使用原始请求体复制管网信息文件。
// 对应 POST /pipenet/info/copy。
func (c *APIClient) CopyPipeNetInfoFileRaw(body any) (*http.Response, error) {
	return c.PostRawResponse("/pipenet/info/copy", body)
}

// GetPipeNetInfoFile 解析并获取单个管网信息文件内容。
// 对应 GET /pipenet/info/get。
func (c *APIClient) GetPipeNetInfoFile(id string) (*Response, error) {
	return c.Get("/pipenet/info/get", map[string]string{
		"id": id,
	}, nil)
}

// UploadMappingConfig 上传管网模型映射表 JSON 文件。
// 对应 POST /pipenet/mapping_config/upload。
func (c *APIClient) UploadMappingConfig(filename, description, uploadFileName string, content []byte) (*Response, error) {
	return c.doRequestWithRetry(func() (*http.Request, error) {
		return c.buildMultipartRequest("/pipenet/mapping_config/upload", "file", uploadFileName, content, map[string]string{
			"filename":    filename,
			"description": description,
		}, nil)
	})
}

// DownloadMappingConfig 下载管网模型映射表。
// 对应 POST /pipenet/mapping_config/download。
func (c *APIClient) DownloadMappingConfig(idList []string) (*Response, error) {
	return c.Post("/pipenet/mapping_config/download", map[string]any{
		"mapping_config_id_list": idList,
	}, nil)
}

// DownloadMappingConfigRaw 使用原始请求体下载管网模型映射表。
// 对应 POST /pipenet/mapping_config/download。
func (c *APIClient) DownloadMappingConfigRaw(body any) (*http.Response, error) {
	return c.PostRawResponse("/pipenet/mapping_config/download", body)
}

// DeleteMappingConfig 删除管网模型映射表。
// 对应 POST /pipenet/mapping_config/delete。
func (c *APIClient) DeleteMappingConfig(idList []string) (*Response, error) {
	return c.Post("/pipenet/mapping_config/delete", map[string]any{
		"mapping_config_id_list": idList,
	}, nil)
}

// DeleteMappingConfigRaw 使用原始请求体删除管网模型映射表。
// 对应 POST /pipenet/mapping_config/delete。
func (c *APIClient) DeleteMappingConfigRaw(body any) (*http.Response, error) {
	return c.PostRawResponse("/pipenet/mapping_config/delete", body)
}

// CopyMappingConfig 复制管网模型映射表。
// 对应 POST /pipenet/mapping_config/copy。
func (c *APIClient) CopyMappingConfig(idList []string) (*Response, error) {
	return c.Post("/pipenet/mapping_config/copy", map[string]any{
		"mapping_config_id_list": idList,
	}, nil)
}

// CopyMappingConfigRaw 使用原始请求体复制管网模型映射表。
// 对应 POST /pipenet/mapping_config/copy。
func (c *APIClient) CopyMappingConfigRaw(body any) (*http.Response, error) {
	return c.PostRawResponse("/pipenet/mapping_config/copy", body)
}

// GetMappingConfigList 获取管网模型映射表列表。
// 对应 GET /pipenet/mapping_config/list/get。
func (c *APIClient) GetMappingConfigList(keywords string, pageNum, pageSize int) (*Response, error) {
	return c.Get("/pipenet/mapping_config/list/get", map[string]string{
		"keywords":  keywords,
		"page_num":  strconv.Itoa(pageNum),
		"page_size": strconv.Itoa(pageSize),
	}, nil)
}

// EditMappingConfig 编辑管网模型映射表基本信息。
// 对应 POST /pipenet/mapping_config/edit。
func (c *APIClient) EditMappingConfig(id, name, description string) (*Response, error) {
	return c.Post("/pipenet/mapping_config/edit", map[string]any{
		"id":          id,
		"name":        name,
		"description": description,
	}, nil)
}

// EditMappingConfigRaw 使用原始请求体编辑管网模型映射表基本信息。
// 对应 POST /pipenet/mapping_config/edit。
func (c *APIClient) EditMappingConfigRaw(body any) (*http.Response, error) {
	return c.PostRawResponse("/pipenet/mapping_config/edit", body)
}

// GetMappingConfigDetails 获取管网模型映射表详情。
// 对应 GET /pipenet/mapping_config/details/get。
func (c *APIClient) GetMappingConfigDetails(id string) (*Response, error) {
	return c.Get("/pipenet/mapping_config/details/get", map[string]string{
		"mapping_config_id": id,
	}, nil)
}

// EditMappingConfigDetails 编辑管网模型映射表详情。
// 对应 POST /pipenet/mapping_config/details/edit。
func (c *APIClient) EditMappingConfigDetails(id, op, system, medium string, parts []map[string]any) (*Response, error) {
	return c.Post("/pipenet/mapping_config/details/edit", map[string]any{
		"id":     id,
		"op":     op,
		"system": system,
		"medium": medium,
		"parts":  parts,
	}, nil)
}

// EditMappingConfigDetailsRaw 使用原始请求体编辑管网模型映射表详情。
// 对应 POST /pipenet/mapping_config/details/edit。
func (c *APIClient) EditMappingConfigDetailsRaw(body any) (*http.Response, error) {
	return c.PostRawResponse("/pipenet/mapping_config/details/edit", body)
}

// GetPipeNetInstanceMapping 获取管网实例映射树。
// 对应 GET /pipenet/model/instance_mapping/get。
func (c *APIClient) GetPipeNetInstanceMapping(pipeNetInfoID, mappingConfigID string) (*Response, error) {
	return c.Get("/pipenet/model/instance_mapping/get", map[string]string{
		"pipe_net_info_id":  pipeNetInfoID,
		"mapping_config_id": mappingConfigID,
	}, nil)
}

// CreatePipeNetModel 根据管网信息文件和映射表生成顶层模型。
// 对应 POST /pipenet/model/create。
func (c *APIClient) CreatePipeNetModel(packageName, strType, packageID, comment string, vars map[string]any, pipeNetInfoID, mappingConfigID string) (*Response, error) {
	return c.Post("/pipenet/model/create", map[string]any{
		"package_name":      packageName,
		"str_type":          strType,
		"package_id":        packageID,
		"comment":           comment,
		"vars":              vars,
		"pipe_net_info_id":  pipeNetInfoID,
		"mapping_config_id": mappingConfigID,
	}, nil)
}

// CreatePipeNetModelRaw 使用原始请求体生成管网模型。
// 对应 POST /pipenet/model/create。
func (c *APIClient) CreatePipeNetModelRaw(body any) (*http.Response, error) {
	return c.PostRawResponse("/pipenet/model/create", body)
}

// SaveExperimentToOriginModel 保存实验参数到原模型
// 对应 POST /api/v3/simulation/experiment/original-model/save
func (c *APIClient) SaveExperimentToOriginModel(experimentId string) (*Response, error) {
	return c.Post("/api/v3/simulation/experiment/original-model/save", map[string]any{
		"experiment_id": experimentId,
	}, nil)
}

// SaveExperimentToNewModel 保存实验参数到新模型
// 对应 POST /api/v3/simulation/experiment/new-model/save
func (c *APIClient) SaveExperimentToNewModel(experimentId, newModelName string) (*Response, error) {
	return c.Post("/api/v3/simulation/experiment/new-model/save", map[string]any{
		"experiment_id":  experimentId,
		"new_model_name": newModelName,
	}, nil)
}

// ExportEncryptPackage 将非加密模型包导出为加密包
// 对应 GET /file/package/encrypt/export
// 当前 handler 额外从 query 读取 space_id，因此这里复用测试配置中的 SpaceID 自动注入。
func (c *APIClient) ExportEncryptPackage(packageId string) (*Response, error) {
	params := map[string]string{
		"package_id": packageId,
	}
	if c.config != nil && c.config.SpaceID != "" {
		params["space_id"] = c.config.SpaceID
	}
	return c.Get("/file/package/encrypt/export", params, nil)
}

// ensureWorkspaceReady 将工作空间恢复到 Ready(3)。
// 初始化 client 和每次调用依赖 SMC 的接口前都复用这套流程，避免全量测试中途工作空间被远端移除后产生级联失败。
func (c *APIClient) ensureWorkspaceReady() error {
	workspaceReadyMu.Lock()
	defer workspaceReadyMu.Unlock()

	if c.config == nil || c.config.SpaceID == "" || c.config.Username == "" {
		return nil
	}

	wsStatus := c.waitForWorkspaceReady()

	switch wsStatus {
	case WorkspaceStatusReady:
		return nil

	case WorkspaceStatusNotStarted:
		fmt.Println("Workspace not started before SMC-dependent request, restarting SMC...")
		if err := c.retrySMCRestart(); err != nil {
			return fmt.Errorf("RestartSMC failed: %w", err)
		}
		return c.loadStandardLibsAndMarkReady()

	case WorkspaceStatusNeedLoad:
		fmt.Println("Workspace needs library load before SMC-dependent request, loading standard libs...")
		return c.loadStandardLibsAndMarkReady()

	case WorkspaceStatusOOMRestarted:
		fmt.Println("Workspace OOM restarted before SMC-dependent request, loading standard libs...")
		return c.loadStandardLibsAndMarkReady()

	case WorkspaceStatusError:
		return fmt.Errorf("workspace is in error state (status=5), cannot initialize test environment")

	default:
		return fmt.Errorf("unexpected workspace status: %d", wsStatus)
	}
}

// waitForWorkspaceReady 等待工作空间离开 Starting(1) 状态，返回最终状态码。
// 若超时仍处于 Starting，则 panic。
func (c *APIClient) waitForWorkspaceReady() int {
	const (
		pollInterval = 2 * time.Second
		maxWait      = 120 * time.Second
	)
	deadline := time.Now().Add(maxWait)

	for {
		status, err := c.getWorkspaceStatus()
		if err != nil {
			// 服务可能尚未启动，先等待一下再重试
			fmt.Printf("getWorkspaceStatus error: %v, retrying...\n", err)
			time.Sleep(pollInterval)
			if time.Now().After(deadline) {
				panic(fmt.Errorf("timed out waiting for workspace to become accessible: %v", err))
			}
			continue
		}

		if status != WorkspaceStatusStarting {
			return status
		}

		fmt.Printf("Workspace is starting (status=1), waiting %s...\n", pollInterval)
		time.Sleep(pollInterval)
		if time.Now().After(deadline) {
			panic(fmt.Errorf("timed out waiting for workspace to leave Starting state after %s", maxWait))
		}
	}
}

// getWorkspaceStatus 调用 loadstatus/get，返回 data.workspace_status 整数值。
func (c *APIClient) getWorkspaceStatus() (int, error) {
	resp, err := c.GetLoadStatus()
	if err != nil {
		return 0, err
	}
	dataMap, ok := resp.Data.(map[string]any)
	if !ok {
		return 0, fmt.Errorf("loadstatus/get: unexpected data type %T", resp.Data)
	}
	statusVal, ok := dataMap["workspace_status"]
	if !ok {
		return 0, fmt.Errorf("loadstatus/get: missing field workspace_status")
	}
	// JSON 反序列化数字默认是 float64
	statusFloat, ok := statusVal.(float64)
	if !ok {
		return 0, fmt.Errorf("loadstatus/get: workspace_status is not a number, got %T", statusVal)
	}
	return int(statusFloat), nil
}

// retrySMCRestart 最多重试 5 次重启工作空间。
func (c *APIClient) retrySMCRestart() error {
	var err error
	for i := 0; i < 5; i++ {
		var resp *Response
		resp, err = c.RestartSMC()
		if err == nil && resp.Status == 0 {
			return nil
		}
		if err == nil {
			err = fmt.Errorf("status=%d msg=%s err=%s", resp.Status, resp.Msg, resp.Err)
		}
		time.Sleep(2 * time.Second)
	}
	return err
}

func (c *APIClient) loadStandardLibsAndMarkReady() error {
	if err := c.loadBothLibs(); err != nil {
		return err
	}
	if err := c.loadstatusSet(); err != nil {
		return err
	}
	return nil
}

func (c *APIClient) loadBothLibs() error {
	if err := c.loadModelForWorkspaceReady("2942f1a3-c5b3-45cd-9cdb-c951784e01ce", ""); err != nil {
		return fmt.Errorf("LoadModel (system lib #1) failed: %w", err)
	}
	if err := c.loadModelForWorkspaceReady("1d602d98-01b2-4625-a0cd-940b9a91980b", ""); err != nil {
		return fmt.Errorf("LoadModel (system lib #2) failed: %w", err)
	}
	return nil
}

func (c *APIClient) loadModelForWorkspaceReady(packageID, modelName string) error {
	resp, err := c.LoadModel(packageID, modelName)
	if err != nil {
		return err
	}
	if resp.Status != 0 {
		return fmt.Errorf("status=%d msg=%s err=%s", resp.Status, resp.Msg, resp.Err)
	}
	return nil
}

func (c *APIClient) loadstatusSet() error {
	resp, err := c.LoadstatusSet()
	if err != nil {
		return err
	}
	if resp.Status != 0 {
		return fmt.Errorf("status=%d msg=%s err=%s", resp.Status, resp.Msg, resp.Err)
	}
	return nil
}

// mustLoadBothLibs 加载 Modelica 系统库 #1 和 Buildings 系统库 #2，失败则 panic。
func (c *APIClient) mustLoadBothLibs() {
	if err := c.loadBothLibs(); err != nil {
		panic(err)
	}
}

// mustLoadstatusSet 设置工作空间状态为 Ready(3)，失败则 panic。
func (c *APIClient) mustLoadstatusSet() {
	if err := c.loadstatusSet(); err != nil {
		panic(fmt.Errorf("LoadstatusSet failed: %w", err))
	}
}

// getDefaultHeaders 获取默认请求头
func (c *APIClient) getDefaultHeaders() map[string]string {
	headers := make(map[string]string)
	if c.config != nil {
		if c.config.Username != "" {
			headers["username"] = c.config.Username
		}
		if c.config.SpaceID != "" {
			headers["space-id"] = c.config.SpaceID
		}
		if c.config.Token != "" {
			headers["Authorization"] = c.config.Token
		}
	}
	return headers
}

// mergeHeaders 合并默认请求头和自定义请求头
func (c *APIClient) mergeHeaders(customHeaders map[string]string) map[string]string {
	merged := c.getDefaultHeaders()
	for k, v := range customHeaders {
		merged[k] = v
	}
	return merged
}

// buildURL 构建带 query 参数的请求 URL。
func (c *APIClient) buildURL(path string, params map[string]string) string {
	requestURL := c.baseURL + path
	if len(params) == 0 {
		return requestURL
	}

	query := url.Values{}
	for k, v := range params {
		query.Set(k, v)
	}
	return requestURL + "?" + query.Encode()
}

// ensureWorkspaceReadyBeforeRequest 在依赖 SMC 的接口调用前恢复工作空间。
// loadstatus/restart/模型加载等初始化接口必须跳过，否则恢复流程会递归调用自身。
func (c *APIClient) ensureWorkspaceReadyBeforeRequest(path string) error {
	if !shouldEnsureWorkspaceReady(path) {
		return nil
	}
	return c.ensureWorkspaceReady()
}

// shouldEnsureWorkspaceReady 判断当前请求是否需要在发送前确保工作空间可用。
func shouldEnsureWorkspaceReady(path string) bool {
	if path == "" ||
		strings.HasPrefix(path, "/user/") ||
		strings.HasPrefix(path, "/static") {
		return false
	}

	initPaths := []string{
		"/model/package/load",
		"/simulation/solver/list",
	}
	for _, initPath := range initPaths {
		if path == initPath {
			return false
		}
	}
	return true
}

// buildRequest 构建 HTTP 请求。
func (c *APIClient) buildRequest(method, path string, params map[string]string, body any, headers map[string]string, hasJSONBody bool) (*http.Request, error) {
	requestURL := c.buildURL(path, params)

	var bodyReader io.Reader
	if hasJSONBody {
		bodyBytes, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		bodyReader = bytes.NewBuffer(bodyBytes)
	}

	req, err := http.NewRequest(method, requestURL, bodyReader)
	if err != nil {
		return nil, err
	}

	if hasJSONBody {
		req.Header.Set("Content-Type", "application/json")
	}

	mergedHeaders := c.mergeHeaders(headers)
	for k, v := range mergedHeaders {
		req.Header.Set(k, v)
	}

	return req, nil
}

func (c *APIClient) buildMultipartRequest(path, fileField, fileName string, content []byte, fields map[string]string, headers map[string]string) (*http.Request, error) {
	if err := c.withoutPrimaryMark().ensureWorkspaceReadyBeforeRequest(path); err != nil {
		return nil, err
	}

	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	for k, v := range fields {
		if err := writer.WriteField(k, v); err != nil {
			return nil, err
		}
	}
	part, err := writer.CreateFormFile(fileField, fileName)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(content); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodPost, c.baseURL+path, &body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	mergedHeaders := c.mergeHeaders(headers)
	for k, v := range mergedHeaders {
		req.Header.Set(k, v)
	}
	return req, nil
}

// parseJSONResponse 将 HTTP JSON 响应解析为测试通用响应结构。
func parseJSONResponse(resp *http.Response) (*Response, error) {
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result Response
	if err := json.Unmarshal(body, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

// withoutPrimaryMark 返回关闭主测标记的客户端视图，用于工作空间检查等辅助请求。
func (c *APIClient) withoutPrimaryMark() *APIClient {
	if !c.markPrimary {
		return c
	}
	auxiliaryClient := *c
	auxiliaryClient.markPrimary = false
	return &auxiliaryClient
}

// normalizeMarkedAPIPath 规范化覆盖标记中的接口路径，避免查询参数或多余斜杠影响统计去重。
func normalizeMarkedAPIPath(path string) string {
	path = strings.TrimSpace(path)
	if path == "" {
		return ""
	}
	if parsedURL, err := url.Parse(path); err == nil && parsedURL.Path != "" {
		path = parsedURL.Path
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	for strings.Contains(path, "//") {
		path = strings.ReplaceAll(path, "//", "/")
	}
	if len(path) > 1 {
		path = strings.TrimRight(path, "/")
	}
	return path
}
