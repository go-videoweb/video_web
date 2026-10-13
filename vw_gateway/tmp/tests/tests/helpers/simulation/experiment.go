package simulation

import (
	"fmt"

	"github.com/google/uuid"
	"yssim-go/app/DataBase"
	"yssim-go/config"
	"yssim-go/tests/helpers"
)

// ExperimentOptions 描述创建测试实验时需要的模型、名称和预处理选项。
type ExperimentOptions struct {
	PackageID             string
	ModelName             string
	NamePrefix            string
	SimulateVarData       map[string]string
	WarmDefaultExperiment bool
}

// DefaultSimulateVarData 返回创建实验常用的默认仿真参数。
func DefaultSimulateVarData() map[string]string {
	return map[string]string{
		"startTime":         "0",
		"stopTime":          "1",
		"method":            "Dassl",
		"numberOfIntervals": "500",
		"tolerance":         "1e-6",
		"interval":          "0.002",
	}
}

// DefaultExperimentOptionsData 返回 v2 设置实验仿真选项接口常用的默认请求体。
func DefaultExperimentOptionsData() map[string]any {
	return map[string]any{
		"start_time":          "0",
		"stop_time":           "1",
		"method":              "Dassl",
		"number_of_intervals": "500",
		"tolerance":           "1e-6",
		"interval":            "0.002",
		"multiple_run_sim":    false,
	}
}

// CreateExperiment 创建测试实验，并返回实验 ID 与实际实验名。
func CreateExperiment(client *helpers.APIClient, opts ExperimentOptions) (string, string, error) {
	if opts.WarmDefaultExperiment {
		resp, err := client.ExperimentGet(opts.PackageID, opts.ModelName)
		if err != nil {
			return "", "", fmt.Errorf("为模型 %s 预热默认实验失败: %w", opts.ModelName, err)
		}
		if resp.Status != 0 {
			return "", "", fmt.Errorf("为模型 %s 预热默认实验返回非成功状态: status=%d, msg=%s, err=%s", opts.ModelName, resp.Status, resp.Msg, resp.Err)
		}
	}

	actualName := uniqueExperimentName(opts.NamePrefix)
	simulateVarData := opts.SimulateVarData
	if simulateVarData == nil {
		simulateVarData = DefaultSimulateVarData()
	}

	resp, err := client.ExperimentCreate(opts.PackageID, opts.ModelName, actualName, simulateVarData)
	if err != nil {
		return "", "", fmt.Errorf("为模型 %s 创建测试实验失败: %w", opts.ModelName, err)
	}
	if resp.Status != 0 {
		return "", "", fmt.Errorf("为模型 %s 创建测试实验返回非成功状态: status=%d, msg=%s, err=%s", opts.ModelName, resp.Status, resp.Msg, resp.Err)
	}

	dataMap, ok := resp.Data.(map[string]any)
	if !ok {
		return "", "", fmt.Errorf("创建测试实验返回数据格式异常: %T", resp.Data)
	}
	id, ok := dataMap["id"].(string)
	if !ok || id == "" {
		return "", "", fmt.Errorf("创建测试实验返回的 id 为空或类型错误: %v", dataMap["id"])
	}
	return id, actualName, nil
}

// DeleteExperiment 清理测试实验；若实验已经不存在，也不会让测试失败。
func DeleteExperiment(client *helpers.APIClient, experimentID string) {
	if experimentID == "" {
		return
	}

	// 删除接口会拒绝默认实验；测试清理阶段直接取消默认标记，保证夹具能被幂等清理。
	_ = config.DB.Model(&DataBase.YssimExperimentRecord{}).Where("id = ?", experimentID).Update("is_default", false).Error
	_, _ = client.ExperimentDelete(experimentID)
}

// ExperimentIsDefault 查询指定实验当前是否为默认实验。
func ExperimentIsDefault(experimentID string) (bool, error) {
	var record DataBase.YssimExperimentRecord
	if err := config.DB.Select("id", "is_default").Where("id = ?", experimentID).First(&record).Error; err != nil {
		return false, err
	}
	return record.IsDefault, nil
}

// ExportVariableInList 判断 detail 接口返回的导出变量列表中是否存在指定变量。
func ExportVariableInList(vars []any, name string) bool {
	for _, v := range vars {
		item, ok := v.(map[string]any)
		if !ok {
			continue
		}
		if item["name"] == name {
			return true
		}
	}
	return false
}

// uniqueExperimentName 为测试实验名添加短随机后缀，避免环境残留数据造成名称冲突。
func uniqueExperimentName(prefix string) string {
	return fmt.Sprintf("%s_%s", prefix, uuid.NewString()[:8])
}
