# BUG 回归测试
## 目录结构组织
以 bug 号或问题特征为目录名，该目录下包含复现该 bug 所需的资源。如 bug2752 目录，其下会包含测试文件、复现该 bug 需要的模型文件以及其他资源文件。

## 测试文件说明
为了与单元测试、常规 API 测试区分，bug 回归测试应当在每个测试文件开头添加且仅添加 build tag `go:build bug_regression_test`。

即使测试需要复用 `tests/helpers` 中的 APIClient 或其他接口调用 helper，也禁止叠加 `api_test` build tag，例如：

```go
//go:build bug_regression_test
```

## 运行方式

bug 回归测试不应进入普通单元测试或常规接口测试。需要手动指定 build tag 运行：

```bash
go test -tags "bug_regression_test" -v ./tests/bugRegression/...
```

运行前需按照 API 测试环境要求设置 `PORT`、`TEST_USERNAME`、`TEST_SPACE_ID` 等环境变量。
