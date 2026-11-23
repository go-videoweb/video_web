# API 测试骨架

本目录只提供 `vw_gateway` 的接口测试基础设施，不包含真实业务用例。

## 目录约定

- `api/`：带 `api_test` 构建标签的远程 API 测试入口和后续业务用例。
- `helpers/`：HTTP 客户端、响应解析、断言、生命周期钩子和覆盖标记。
- `resources/`：测试需要上传的固定资源文件。

接口测试默认通过环境变量配置，不依赖 `tmp/tests` 中的外部环境、数据库或业务配置：

```powershell
$env:API_TEST_BASE_URL = 'http://127.0.0.1:8001'
$env:API_TEST_USERNAME = 'test-user'
$env:API_TEST_PASSWORD = 'test-password'
go test -tags api_test ./tests/api/...
```

T1 阶段只验证测试骨架和公共能力。真实 API 用例、删除接口清理和 TeamCity 接入在后续阶段单独完成。
