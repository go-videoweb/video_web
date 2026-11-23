package workspace

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"yssim-go/tests/helpers"
)

const (
	// TaskStatusProcessing 表示工作空间任务正在执行。
	TaskStatusProcessing = 2
	// TaskStatusStopping 表示工作空间任务正在停止。
	TaskStatusStopping = 3
	// TaskStatusEnded 表示工作空间任务已结束。
	TaskStatusEnded = 4

	// TranslateStatusField 是 poll 响应中编译任务状态字段名。
	TranslateStatusField = "translate_status_data"
	// SimulateStatusField 是 poll 响应中仿真任务状态字段名。
	SimulateStatusField = "simulate_status_data"

	taskLogInterval = 30 * time.Second
)

// TaskStatus 表示 poll 响应中 space_data 下的空间级任务状态。
type TaskStatus struct {
	Status    int
	TaskID    string
	PackageID string
	ModelName string
	Raw       map[string]any
}

// ModelSimulationStatus 表示 poll 响应中 model_data.simulate_status_data 的模型级仿真状态。
type ModelSimulationStatus struct {
	Status int
	ID     string
	Raw    map[string]any
}

// PollStatuses 汇总 poll 响应里测试需要关注的编译和仿真状态。
type PollStatuses struct {
	Translate     TaskStatus
	Simulate      TaskStatus
	ModelSimulate ModelSimulationStatus
}

// Poll 调用工作空间 poll 接口，并提取空间级和模型级仿真状态。
func Poll(client *helpers.APIClient, packageID, modelName, tabID string) (PollStatuses, error) {
	resp, err := client.WorkspaceChangePoll(packageID, modelName, tabID)
	if err != nil {
		return PollStatuses{}, fmt.Errorf("轮询工作空间状态失败: %w", err)
	}
	if resp.Status != 0 {
		return PollStatuses{}, fmt.Errorf("轮询工作空间状态返回非成功状态: status=%d msg=%s err=%s", resp.Status, resp.Msg, resp.Err)
	}
	return ExtractPollStatuses(resp)
}

// ExtractPollStatuses 从 poll 响应中提取测试需要关注的状态字段。
func ExtractPollStatuses(resp *helpers.Response) (PollStatuses, error) {
	dataMap, err := responseDataObject(resp.Data, "poll data")
	if err != nil {
		return PollStatuses{}, err
	}
	spaceData, err := responseDataObject(dataMap["space_data"], "poll data.space_data")
	if err != nil {
		return PollStatuses{}, err
	}
	translateStatus, err := taskStatusFromSpaceData(spaceData, TranslateStatusField)
	if err != nil {
		return PollStatuses{}, err
	}
	simulateStatus, err := taskStatusFromSpaceData(spaceData, SimulateStatusField)
	if err != nil {
		return PollStatuses{}, err
	}
	statuses := PollStatuses{
		Translate: translateStatus,
		Simulate:  simulateStatus,
	}

	if modelDataValue, ok := dataMap["model_data"]; ok && modelDataValue != nil {
		modelData, err := responseDataObject(modelDataValue, "poll data.model_data")
		if err != nil {
			return PollStatuses{}, err
		}
		if simulateValue, ok := modelData["simulate_status_data"]; ok && simulateValue != nil {
			modelSimulate, err := modelSimulationStatusFromModelData(simulateValue)
			if err != nil {
				return PollStatuses{}, err
			}
			statuses.ModelSimulate = modelSimulate
		}
	}
	return statuses, nil
}

// WaitTaskFinished 轮询工作空间任务状态，直到指定模型的任务结束。
func WaitTaskFinished(client *helpers.APIClient, packageID, modelName, statusField, tabID, contextName string, requireObserved bool) error {
	const (
		pollInterval = 2 * time.Second
		maxWait      = 5 * time.Minute
	)

	deadline := time.Now().Add(maxWait)
	nextProgressLog := time.Now()
	lastLoggedPhase := ""
	sawTargetTask := false
	var lastStatus TaskStatus

	for {
		statuses, err := Poll(client, packageID, modelName, tabID)
		if err != nil {
			return err
		}
		current := statuses.ByField(statusField)
		lastStatus = current

		if current.MatchesTarget(packageID, modelName) {
			sawTargetTask = true
			if current.Status == TaskStatusStopping {
				return fmt.Errorf("%s: 工作空间任务正在停止，状态=%v", contextName, current.Raw)
			}
			LogTaskProgress(contextName, statusField, current, &lastLoggedPhase, &nextProgressLog)
			if time.Now().After(deadline) {
				return fmt.Errorf("%s: 等待工作空间任务结束超时 package_id=%s model_name=%s field=%s task_id=%s，最后状态=%v", contextName, packageID, modelName, statusField, current.TaskID, current.Raw)
			}
			time.Sleep(pollInterval)
			continue
		}

		if sawTargetTask || (current.Status == TaskStatusEnded && !requireObserved) {
			fmt.Printf("%s: 工作空间任务已结束 package_id=%s model_name=%s field=%s，最后状态=%v\n", contextName, packageID, modelName, statusField, current.Raw)
			return nil
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("%s: 等待工作空间任务结束超时，最后状态=%v", contextName, lastStatus.Raw)
		}

		LogTaskProgress(contextName, "等待任务出现 "+statusField, current, &lastLoggedPhase, &nextProgressLog)
		time.Sleep(pollInterval)
	}
}

// MatchesTarget 判断空间级任务是否属于指定模型。
func (s TaskStatus) MatchesTarget(packageID, modelName string) bool {
	return strings.TrimSpace(s.PackageID) == strings.TrimSpace(packageID) && strings.TrimSpace(s.ModelName) == strings.TrimSpace(modelName)
}

// ByField 根据 poll 响应字段名返回对应的空间级任务状态。
func (s PollStatuses) ByField(fieldName string) TaskStatus {
	switch fieldName {
	case TranslateStatusField:
		return s.Translate
	case SimulateStatusField:
		return s.Simulate
	default:
		return TaskStatus{Raw: map[string]any{"unknown_field": fieldName}}
	}
}

// LogTaskProgress 控制空间级任务等待日志的输出频率。
func LogTaskProgress(contextName, phase string, status TaskStatus, lastLoggedPhase *string, nextProgressLog *time.Time) {
	now := time.Now()
	currentPhase := fmt.Sprintf("%s:%d:%s", phase, status.Status, status.TaskID)
	if currentPhase != *lastLoggedPhase || !now.Before(*nextProgressLog) {
		fmt.Printf("%s: %s 状态=%d task_id=%s package_id=%s model=%s\n", contextName, phase, status.Status, status.TaskID, status.PackageID, status.ModelName)
		*lastLoggedPhase = currentPhase
		*nextProgressLog = now.Add(taskLogInterval)
	}
}

// LogModelSimulationProgress 控制模型级仿真状态等待日志的输出频率。
func LogModelSimulationProgress(contextName string, status ModelSimulationStatus, lastLoggedPhase *string, nextProgressLog *time.Time) {
	now := time.Now()
	currentPhase := fmt.Sprintf("模型级仿真:%d:%s", status.Status, status.ID)
	if currentPhase != *lastLoggedPhase || !now.Before(*nextProgressLog) {
		fmt.Printf("%s: 模型级仿真状态=%d experiment_id=%s\n", contextName, status.Status, status.ID)
		*lastLoggedPhase = currentPhase
		*nextProgressLog = now.Add(taskLogInterval)
	}
}

// taskStatusFromSpaceData 提取 space_data 下的编译或仿真任务状态。
func taskStatusFromSpaceData(spaceData map[string]any, fieldName string) (TaskStatus, error) {
	taskData, err := responseDataObject(spaceData[fieldName], "poll data.space_data."+fieldName)
	if err != nil {
		return TaskStatus{}, err
	}
	status, ok := valueAsInt(taskData["status"])
	if !ok {
		return TaskStatus{}, fmt.Errorf("poll data.space_data.%s.status 不是数字: %T %v", fieldName, taskData["status"], taskData)
	}
	return TaskStatus{
		Status:    status,
		TaskID:    valueAsString(taskData["task_id"]),
		PackageID: valueAsString(taskData["package_id"]),
		ModelName: valueAsString(taskData["model"]),
		Raw:       taskData,
	}, nil
}

// modelSimulationStatusFromModelData 提取 model_data 下的模型级仿真状态。
func modelSimulationStatusFromModelData(value any) (ModelSimulationStatus, error) {
	data, err := responseDataObject(value, "poll data.model_data.simulate_status_data")
	if err != nil {
		return ModelSimulationStatus{}, err
	}
	status, ok := valueAsInt(data["status"])
	if !ok {
		return ModelSimulationStatus{}, fmt.Errorf("poll data.model_data.simulate_status_data.status 不是数字: %T %v", data["status"], data)
	}
	return ModelSimulationStatus{
		Status: status,
		ID:     valueAsString(data["id"]),
		Raw:    data,
	}, nil
}

// responseDataObject 将响应字段断言为对象，便于逐层读取嵌套数据。
func responseDataObject(value any, fieldName string) (map[string]any, error) {
	dataMap, ok := value.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s 不是 object: %T %v", fieldName, value, value)
	}
	return dataMap, nil
}

// valueAsString 将响应字段统一转为字符串，兼容 nil 和非字符串状态值。
func valueAsString(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case fmt.Stringer:
		return v.String()
	case nil:
		return ""
	default:
		return fmt.Sprint(v)
	}
}

// valueAsInt 将响应字段转成 int，兼容 JSON 数字和字符串数字。
func valueAsInt(value any) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int32:
		return int(v), true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case float32:
		return int(v), true
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(v))
		return parsed, err == nil
	default:
		return 0, false
	}
}
