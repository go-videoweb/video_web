# API 接口测试用例编写指南

本文面向新增或修改后端接口的开发同事。目标是：接口作者在完成接口后，可以同步补充对应 API 测试用例，避免测试长期集中由少数人维护。

## 整体测试框架

API 测试主要由三部分组成：

- `tests/api`：放具体接口测试用例。新增接口测试时，优先在这里找同模块已有文件补充；如果没有合适文件，再新增 `xxx_api_test.go`。
- `tests/helpers`：放公共测试能力，例如 HTTP 客户端、响应断言、测试前准备和测试后清理、模型编译等待、仿真数据库辅助查询等。多个测试都会用到的逻辑应放到这里。
- `tests/resources`：放测试需要上传或读取的固定文件，例如图片、模型文件、CSV 文件。上传文件测试不要临时依赖开发者本机任意路径，应把文件放到这里并从这里读取。

接口测试和普通单元测试是分开的。接口测试文件需要使用 `api_test` 构建标记，运行时也必须带 `-tags api_test`。

## 新接口测试应该补在哪里

新增接口后，按下面顺序补测试：

1. 在 `tests/api` 找对应业务模块的测试文件。

   例如用户空间接口放用户相关测试文件，模型编辑接口放模型编辑相关测试文件，参数标定接口放参数标定测试文件。

2. 如果该模块已有文件，就在已有文件里新增一个独立的 `TestXxx`。

   建议一个接口对应一个测试函数，不要把多个接口塞进同一个大测试函数里。

3. 如果没有合适文件，再新建 `xxx_api_test.go`。

4. 如果请求逻辑是多个测试都会复用的，先在 `tests/helpers/client.go` 给 `APIClient` 增加方法。

5. 如果断言、空值判断、等待逻辑、文件路径逻辑会被多个测试复用，放到 `tests/helpers`，不要散落在测试文件里。

## 编写前先确认接口行为

写测试前先看接口实现，而不是只看印象或接口文档。至少确认这些内容：

- 路由在哪个 `router/*.go` 中注册。
- 实际处理函数在哪个 `app/view/*` 文件中。
- 请求方法是 GET、POST 还是 multipart 上传。
- 参数来自 query、JSON body、form-data 还是文件字段。
- JSON 字段名以 handler 绑定的结构体 tag 为准。
- 成功时 `status`、`data`、`msg` 大概是什么结构。
- 失败时是 HTTP 400，还是 HTTP 200 加业务 `status != 0`。
- 是否依赖工作空间、模型加载、数据库记录、上传文件或远端计算节点。

字段名不要猜。测试请求字段应和 view handler 的 JSON tag / binding 结构保持一致。

## 在 APIClient 中封装请求

测试文件里不要到处手写 HTTP 请求。普通 JSON 接口先在 `tests/helpers/client.go` 增加方法，例如：

```go
// CreateXxx 创建 xxx。
// 对应 POST /api/v2/xxx/create。
func (c *APIClient) CreateXxx(name string) (*Response, error) {
	return c.Post("/api/v2/xxx/create", map[string]any{
		"name": name,
	}, nil)
}
```

约定：

- 不要把 `username`、`space-id` 作为业务参数传进方法，默认请求头由测试客户端统一注入。
- 请求头使用 `space-id`，不要使用旧的 `space_id`。
- 路径优先使用当前正式版本的接口。
- 新增方法前先搜索已有 helper，能复用就复用。
- 如果需要断言 HTTP 状态码、原始响应、SSE 或文件流，再补一个 `XxxRaw` 方法。

## 推荐的测试函数结构

一个测试函数通常包含四段：

1. 定义断言工具和公共变量。
2. 准备测试数据。
3. 按场景逐个发请求并断言。
4. 清理测试数据。

示例：

```go
func TestXxx(t *testing.T) {
	assert := helpers.NewAssertResponse(t)
	var recordID string

	tests := []struct {
		name         string
		id           string
		expectError  bool
		expectedCode int
	}{
		{name: "正常获取"},
		{name: "不存在的 id 返回业务错误", id: "not-exist", expectError: true, expectedCode: 2},
	}

	hooks := helpers.TestHooks{
		Before: func(t *testing.T) {
			recordID = createTestXxx(t)
		},
		After: func(t *testing.T) {
			deleteTestXxx(t, recordID)
		},
	}

	helpers.RunWithHooks(t, hooks, func(t *testing.T) {
		for _, tt := range tests {
			tt := tt
			t.Run(tt.name, func(t *testing.T) {
				id := tt.id
				if id == "" {
					id = recordID
				}

				resp, err := testClient.GetXxx(id)
				if err != nil {
					t.Fatalf("请求失败: %v", err)
				}

				if tt.expectError {
					assert.IsError(resp, tt.expectedCode, tt.name)
					return
				}
				assert.IsSuccess(resp, tt.name)
			})
		}
	})
}
```

这里可以简单理解为：

- `tests` 是测试场景列表。
- `Before` 是测试开始前准备数据。
- `After` 是测试结束后清理数据。
- `RunWithHooks` 负责保证准备和清理按顺序执行。

## 每个接口至少覆盖哪些场景

每个接口至少考虑：

- 正常成功场景。
- 缺少必填参数。
- 参数非法，例如空字符串、错误枚举值、格式错误。
- 资源不存在，例如不存在的 id。
- 业务边界，例如重复创建、删除后再查询、空列表返回。

如果接口有多个重要分支，不要只测一个成功路径。测试用例名称应写清楚场景，例如：

```text
正常创建记录
缺少 name 返回业务错误
不存在的 id 返回业务错误
重复名称返回业务错误
删除后查询不到记录
```

## 上传文件接口怎么写

上传接口使用的文件应放在 `tests/resources` 下。

建议按业务模块建目录，例如：

```text
tests/resources/ParameterCalibration/ParameterCalibration.mo
tests/resources/ParameterCalibration/Acceleration measurements.csv
tests/resources/test_picture.png
```

测试中通过相对项目路径读取这些文件，不要写成本机个人目录，例如不要依赖：

```text
C:\Users\某个人\Desktop\xxx.csv
```

上传接口至少覆盖：

- 正常上传文件。
- 缺少文件字段。
- 非法文件后缀。
- 文件内容为空或不符合业务格式时的返回。

如果上传后的文件会被远端服务继续使用，例如模型编译、仿真、参数标定，应确认测试请求打到 147 环境，避免文件只写到本机导致远端节点找不到。

## 数据准备和清理

测试创建的数据必须清理，避免影响下一次运行。

常见测试数据包括：

- 用户空间。
- 模型包。
- 实验记录。
- 参数标定记录。
- 资源文件。
- 仿真记录。

建议：

- 准备数据放在 `Before` 中。
- 清理数据放在 `After` 中。
- 清理函数应尽量允许重复调用，不要因为数据已经不存在就让测试失败。
- 如果已有公共创建或清理函数，优先复用。
- 不要为了规避清理问题额外创建没有断言价值的数据。

## 断言怎么写

常用断言方式：

```go
assert := helpers.NewAssertResponse(t)
assert.IsSuccess(resp, "正常创建记录")
assert.IsError(resp, 2, "缺少 name 返回业务错误")
```

约定：

- HTTP 请求本身失败，用 `t.Fatalf`。
- 接口业务成功，用 `assert.IsSuccess`。
- 接口业务失败，用 `assert.IsError`。
- 判断空值、空数组、空对象时，优先使用 `tests/helpers/assertions.go` 中已有函数。
- 如果通用断言不够用，扩展 `tests/helpers`，不要在测试文件里复制一份类似逻辑。

## 运行测试前的环境要求

运行接口测试前，先确认 147 环境上的后端服务已经更新为本次要测试的代码版本。

推荐流程：

1. 将最新后端服务更新到 147 环境。
2. 确认 147 服务已重启并可访问。
3. 确认测试用的 `TEST_USERNAME` 和 `TEST_SPACE_ID` 有效。
4. 在本地运行接口测试。

本地运行时设置必要环境变量，重点包括：

```powershell
$env:PORT = '30311'
$env:TEST_USERNAME = 'test-user'
$env:TEST_SPACE_ID = '38ae7ea7-b1d0-4f7f-8993-61edb4980b1d'
```

接口测试默认请求：

```text
http://192.168.121.147:$PORT
```

不要默认切到本机后端。对于依赖上传文件、模型编译、仿真结果、参数标定结果的接口，本机后端容易产生文件不同步问题。

## 运行命令

只跑单个接口测试：

```powershell
go test -tags api_test -json ./tests/api/... -run 'TestXxx' -count=1 -v
```

跑某一类接口测试：

```powershell
go test -tags api_test -json ./tests/api/... -run 'TestCalibration' -count=1 -v -timeout 5m
```

跑全量接口测试：

```powershell
go test -tags api_test -json ./tests/api/... -timeout 20m
```

普通单元测试不要带 `api_test`。

## 提交前检查清单

提交新接口代码前，请确认：

- 新接口有对应测试函数。
- 成功场景和主要失败场景都已覆盖。
- 新增公共请求方法已放入 `tests/helpers/client.go`。
- 上传文件已放入 `tests/resources`。
- 测试数据能自动清理。
- 本地已至少运行过目标测试。
- 如果接口依赖远端文件或计算节点，已在 147 环境验证过。