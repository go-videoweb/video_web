package model

import (
	"yssim-go/tests/helpers"
	"yssim-go/tests/helpers/workspace"
)

// WaitCompileFinished 轮询工作空间级编译状态，直到指定模型的编译任务结束。
func WaitCompileFinished(client *helpers.APIClient, packageID, modelName, contextName string) error {
	return workspace.WaitTaskFinished(client, packageID, modelName, workspace.TranslateStatusField, "api-test-model-compile", contextName, true)
}
