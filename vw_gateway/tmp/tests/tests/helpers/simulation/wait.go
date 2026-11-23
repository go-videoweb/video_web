package simulation

import (
	"fmt"
	"time"

	"yssim-go/tests/helpers"
	"yssim-go/tests/helpers/workspace"
)

// WaitWithTranslateFinished 只通过 poll 接口等待编译和仿真完成。
func WaitWithTranslateFinished(client *helpers.APIClient, packageID, modelName, experimentID, contextName string) error {
	const (
		pollInterval = 2 * time.Second
		maxWait      = 15 * time.Minute
	)

	deadline := time.Now().Add(maxWait)
	nextProgressLog := time.Now()
	lastLoggedPhase := ""
	sawSimulationTask := false
	var lastStatuses workspace.PollStatuses

	for {
		statuses, err := workspace.Poll(client, packageID, modelName, "api-test-simulation")
		if err != nil {
			return err
		}
		lastStatuses = statuses

		// simulate-with-translate 可能先下发编译任务。只要空间级编译任务仍属于当前模型，就先等待它结束。
		if statuses.Translate.MatchesTarget(packageID, modelName) {
			if statuses.Translate.Status == workspace.TaskStatusStopping {
				return fmt.Errorf("%s: 模型编译任务正在停止，状态=%v", contextName, statuses.Translate.Raw)
			}
			workspace.LogTaskProgress(contextName, "编译", statuses.Translate, &lastLoggedPhase, &nextProgressLog)
			time.Sleep(pollInterval)
			continue
		}

		if statuses.Simulate.MatchesTarget(packageID, modelName) {
			if statuses.Simulate.Status == workspace.TaskStatusStopping {
				return fmt.Errorf("%s: 仿真任务正在停止，状态=%v", contextName, statuses.Simulate.Raw)
			}
			sawSimulationTask = true
			workspace.LogTaskProgress(contextName, "仿真", statuses.Simulate, &lastLoggedPhase, &nextProgressLog)
			time.Sleep(pollInterval)
			continue
		}

		// space_data 只包含未完成任务；如果任务很快结束，必须用同一次 poll 的 model_data 按实验 ID 兜底，避免误读旧模型状态。
		if statuses.ModelSimulate.ID == experimentID {
			sawSimulationTask = true
			if statuses.ModelSimulate.Status == workspace.TaskStatusEnded {
				fmt.Printf("%s: poll 模型级仿真状态已结束 experiment_id=%s status=%d\n", contextName, experimentID, statuses.ModelSimulate.Status)
				return nil
			}
			workspace.LogModelSimulationProgress(contextName, statuses.ModelSimulate, &lastLoggedPhase, &nextProgressLog)
			time.Sleep(pollInterval)
			continue
		}

		if sawSimulationTask {
			fmt.Printf("%s: 空间级仿真任务已结束 package_id=%s model_name=%s，最后空间状态=%v\n", contextName, packageID, modelName, statuses.Simulate.Raw)
			return nil
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("%s: 等待仿真完成超时，最后 poll 状态=%+v", contextName, lastStatuses)
		}

		workspace.LogTaskProgress(contextName, "等待仿真任务出现", statuses.Simulate, &lastLoggedPhase, &nextProgressLog)
		time.Sleep(pollInterval)
	}
}

// WaitTerminateFinished 轮询工作空间状态，直到终止中的仿真进入结束态。
func WaitTerminateFinished(client *helpers.APIClient, packageID, modelName, experimentID, contextName string) error {
	const (
		pollInterval = 2 * time.Second
		maxWait      = 5 * time.Minute
	)

	deadline := time.Now().Add(maxWait)
	nextProgressLog := time.Now()
	lastLoggedPhase := ""
	sawTargetTask := false
	var lastStatuses workspace.PollStatuses

	for {
		statuses, err := workspace.Poll(client, packageID, modelName, "api-test-simulation-terminate")
		if err != nil {
			return err
		}
		lastStatuses = statuses

		if statuses.Simulate.MatchesTarget(packageID, modelName) {
			sawTargetTask = true
			if statuses.Simulate.Status == workspace.TaskStatusEnded {
				fmt.Printf("%s: poll 空间级仿真状态已结束 experiment_id=%s status=%d\n", contextName, experimentID, statuses.Simulate.Status)
				return nil
			}
			workspace.LogTaskProgress(contextName, "终止仿真", statuses.Simulate, &lastLoggedPhase, &nextProgressLog)
			time.Sleep(pollInterval)
			continue
		}

		// 终止完成后 space_data 可能只保留空的结束态；model_data 可用于确认本次实验已经进入结束态。
		if statuses.ModelSimulate.ID == experimentID && statuses.ModelSimulate.Status == workspace.TaskStatusEnded {
			fmt.Printf("%s: poll 模型级仿真状态已结束 experiment_id=%s status=%d\n", contextName, experimentID, statuses.ModelSimulate.Status)
			return nil
		}
		if sawTargetTask && statuses.Simulate.Status == workspace.TaskStatusEnded {
			fmt.Printf("%s: poll 空间级仿真状态已结束 experiment_id=%s status=%d\n", contextName, experimentID, statuses.Simulate.Status)
			return nil
		}

		if time.Now().After(deadline) {
			return fmt.Errorf("%s: 等待仿真终止完成超时，最后 poll 状态=%+v", contextName, lastStatuses)
		}

		workspace.LogTaskProgress(contextName, "等待终止状态", statuses.Simulate, &lastLoggedPhase, &nextProgressLog)
		time.Sleep(pollInterval)
	}
}

// WaitSimulationRunning 通过 poll 模型级仿真状态等待指定实验进入运行态。
func WaitSimulationRunning(client *helpers.APIClient, packageID, modelName, experimentID, tabID, contextName string, maxWait time.Duration) error {
	_, err := WaitModelSimulationStatus(client, packageID, modelName, experimentID, tabID, workspace.TaskStatusProcessing, contextName, maxWait)
	return err
}

// WaitSimulationFinished 通过 poll 模型级仿真状态等待指定实验进入结束态。
func WaitSimulationFinished(client *helpers.APIClient, packageID, modelName, experimentID, tabID, contextName string, maxWait time.Duration) error {
	_, err := WaitModelSimulationStatus(client, packageID, modelName, experimentID, tabID, workspace.TaskStatusEnded, contextName, maxWait)
	return err
}

// WaitModelSimulationStatus 通过 poll 的 model_data.simulate_status_data 等待指定实验进入目标状态。
func WaitModelSimulationStatus(client *helpers.APIClient, packageID, modelName, experimentID, tabID string, expectedStatus int, contextName string, maxWait time.Duration) (workspace.ModelSimulationStatus, error) {
	if maxWait <= 0 {
		maxWait = 5 * time.Minute
	}
	if tabID == "" {
		tabID = "api-test-model-simulation-status"
	}

	deadline := time.Now().Add(maxWait)
	nextProgressLog := time.Now()
	lastLoggedPhase := ""
	var lastStatus workspace.ModelSimulationStatus

	for {
		statuses, err := workspace.Poll(client, packageID, modelName, tabID)
		if err != nil {
			return workspace.ModelSimulationStatus{}, err
		}
		lastStatus = statuses.ModelSimulate
		if statuses.ModelSimulate.ID == experimentID {
			workspace.LogModelSimulationProgress(contextName, statuses.ModelSimulate, &lastLoggedPhase, &nextProgressLog)
			if statuses.ModelSimulate.Status == expectedStatus {
				return statuses.ModelSimulate, nil
			}
			if expectedStatus == workspace.TaskStatusProcessing && statuses.ModelSimulate.Status == workspace.TaskStatusEnded {
				return workspace.ModelSimulationStatus{}, fmt.Errorf("%s: 仿真在到达运行态前已经结束，experiment_id=%s", contextName, experimentID)
			}
		} else {
			workspace.LogTaskProgress(contextName, "等待模型级仿真状态", statuses.Simulate, &lastLoggedPhase, &nextProgressLog)
		}

		if time.Now().After(deadline) {
			return workspace.ModelSimulationStatus{}, fmt.Errorf("%s: 等待仿真状态 %d 超时，experiment_id=%s，最后模型级状态=%+v", contextName, expectedStatus, experimentID, lastStatus)
		}
		time.Sleep(250 * time.Millisecond)
	}
}
