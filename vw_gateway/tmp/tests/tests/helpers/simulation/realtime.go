package simulation

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"yssim-go/app/DataBase"
	simulateService "yssim-go/app/service/simulate"
	"yssim-go/config"
	"yssim-go/tests/helpers"

	"github.com/go-redis/redis/v8"
)

// SubscribeRealtimeVariables 设置实时 FMU 输出变量，并返回唯一仿真 task_id。
func SubscribeRealtimeVariables(client *helpers.APIClient, experimentID string, variables []string, contextName string, maxWait time.Duration) (string, error) {
	if maxWait <= 0 {
		maxWait = 20 * time.Second
	}

	deadline := time.Now().Add(maxWait)
	var lastErr error
	var lastResp *helpers.Response

	for time.Now().Before(deadline) {
		resp, err := client.SetRealtimeFmuVariables(experimentID, variables)
		if err == nil && resp.Status == 0 {
			taskID, parseErr := RealtimeSingleTaskIDFromVariablesResponse(resp, contextName)
			if parseErr != nil {
				return "", parseErr
			}
			fmt.Printf("%s: 实时变量订阅成功 experiment_id=%s task_id=%s variables=%v msg=%s\n", contextName, experimentID, taskID, variables, resp.Msg)
			return taskID, nil
		}
		lastErr = err
		lastResp = resp
		time.Sleep(250 * time.Millisecond)
	}

	if lastErr != nil {
		return "", fmt.Errorf("%s: 订阅实时变量失败: %w", contextName, lastErr)
	}
	if lastResp != nil {
		return "", fmt.Errorf("%s: 订阅实时变量超时，最后响应 status=%d msg=%s err=%s", contextName, lastResp.Status, lastResp.Msg, lastResp.Err)
	}
	return "", fmt.Errorf("%s: 订阅实时变量超时，未获得接口响应", contextName)
}

// RealtimeSingleTaskIDFromVariablesResponse 从实时变量设置响应中读取唯一 task_id。
func RealtimeSingleTaskIDFromVariablesResponse(resp *helpers.Response, contextName string) (string, error) {
	dataMap, err := helpers.ObjectData(resp, contextName)
	if err != nil {
		return "", err
	}
	taskIDs, ok := dataMap["task_ids"].([]any)
	if !ok || len(taskIDs) != 1 {
		return "", fmt.Errorf("%s: 期望返回唯一 task_ids，actual=%T %v", contextName, dataMap["task_ids"], dataMap["task_ids"])
	}
	taskID, ok := taskIDs[0].(string)
	if !ok || strings.TrimSpace(taskID) == "" {
		return "", fmt.Errorf("%s: task_ids[0] 不是非空字符串，actual=%T %v", contextName, taskIDs[0], taskIDs[0])
	}
	return taskID, nil
}

// WaitRealtimeSampleStreamAvailable 等待实时 FMU Redis Stream 至少出现一条样本。
func WaitRealtimeSampleStreamAvailable(taskID, contextName string, maxWait time.Duration) error {
	if maxWait <= 0 {
		maxWait = 20 * time.Second
	}

	key := simulateService.InteractiveOutputRedisKey(taskID)
	deadline := time.Now().Add(maxWait)
	nextDiagnosticAt := time.Now().Add(5 * time.Second)
	var lastLen int64
	var lastErr error

	fmt.Printf("%s: 开始等待实时 Redis Stream 样本 task_id=%s sample_key=%s max_wait=%s\n", contextName, taskID, key, maxWait)

	for time.Now().Before(deadline) {
		streamLen, err := config.R.XLen(context.Background(), key).Result()
		if err == nil && streamLen > 0 {
			fmt.Printf("%s: 已检测到实时 Redis Stream 样本 task_id=%s sample_key=%s stream_len=%d\n", contextName, taskID, key, streamLen)
			return nil
		}
		if err != nil {
			fmt.Printf("%s: 等待实时 Redis Stream 样本失败 key=%s task_id=%s err=%v\n", contextName, key, taskID, err)
		}
		lastLen = streamLen
		lastErr = err
		if !time.Now().Before(nextDiagnosticAt) {
			logRealtimeSampleStreamDiagnostics(contextName, taskID, key, lastLen, false)
			nextDiagnosticAt = time.Now().Add(5 * time.Second)
		}
		time.Sleep(250 * time.Millisecond)
	}
	logRealtimeSampleStreamDiagnostics(contextName, taskID, key, lastLen, true)
	if lastErr != nil {
		return fmt.Errorf("%s: 等待实时 Redis Stream 样本失败 key=%s task_id=%s err=%w", contextName, key, taskID, lastErr)
	}
	return fmt.Errorf("%s: 等待实时 Redis Stream 样本超时 key=%s task_id=%s last_len=%d", contextName, key, taskID, lastLen)
}

// logRealtimeSampleStreamDiagnostics 输出实时 FMU 样本等待失败时的关键链路状态；includeRecords 为 true 时额外查询 TIC 和数据库记录。
func logRealtimeSampleStreamDiagnostics(contextName, taskID, sampleKey string, streamLen int64, includeRecords bool) {
	ctx := context.Background()
	redisOptions := config.R.Options()
	fmt.Printf("%s: 实时 Redis Stream 诊断 task_id=%s sample_key=%s stream_len=%d redis_addr=%s redis_db=%d\n", contextName, taskID, sampleKey, streamLen, redisOptions.Addr, redisOptions.DB)

	logRedisKeyDiagnostics(ctx, contextName, sampleKey)
	logRealtimeSubscriptionDiagnostics(ctx, contextName, taskID)
	if !includeRecords {
		return
	}
	logRealtimeTaskDiagnostics(contextName, taskID)
	logRealtimeDatabaseDiagnostics(contextName, taskID)
}

// logRedisKeyDiagnostics 输出样本 Stream key 的 Redis 类型和过期时间。
func logRedisKeyDiagnostics(ctx context.Context, contextName, key string) {
	keyType, typeErr := config.R.Type(ctx, key).Result()
	ttl, ttlErr := config.R.TTL(ctx, key).Result()
	if typeErr != nil || ttlErr != nil {
		fmt.Printf("%s: Redis 样本 key 状态 key=%s type=%s type_err=%v ttl=%s ttl_err=%v\n", contextName, key, keyType, typeErr, ttl, ttlErr)
		return
	}
	fmt.Printf("%s: Redis 样本 key 状态 key=%s type=%s ttl=%s\n", contextName, key, keyType, ttl)
}

// logRealtimeSubscriptionDiagnostics 检查后端写入的实时变量订阅 key，确认订阅变量是否已落到同一个 Redis 实例。
func logRealtimeSubscriptionDiagnostics(ctx context.Context, contextName, taskID string) {
	if spaceID := strings.TrimSpace(os.Getenv("TEST_SPACE_ID")); spaceID != "" {
		expectedKey := fmt.Sprintf("realtime:fmu:%s:%s:subscribed", spaceID, taskID)
		if logRealtimeSubscriptionKey(ctx, contextName, expectedKey, "expected") {
			return
		}
	}

	pattern := fmt.Sprintf("realtime:fmu:*:%s:subscribed", taskID)
	var cursor uint64
	keys := make([]string, 0)
	seen := make(map[string]struct{})
	const maxScanPages = 50
	for scanPages := 0; scanPages < maxScanPages; scanPages++ {
		batch, nextCursor, err := config.R.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			fmt.Printf("%s: Redis 实时订阅 key 扫描失败 pattern=%s cursor=%d err=%v\n", contextName, pattern, cursor, err)
			return
		}
		for _, key := range batch {
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			keys = append(keys, key)
		}
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
	if len(keys) == 0 {
		fmt.Printf("%s: Redis 实时订阅 key 不存在 pattern=%s final_cursor=%d\n", contextName, pattern, cursor)
		return
	}
	if cursor != 0 {
		fmt.Printf("%s: Redis 实时订阅 key 扫描达到上限 pattern=%s next_cursor=%d max_pages=%d matched=%d\n", contextName, pattern, cursor, maxScanPages, len(keys))
	}
	for _, key := range keys {
		logRealtimeSubscriptionKey(ctx, contextName, key, "scan")
	}
}

// logRealtimeSubscriptionKey 输出单个实时变量订阅 key 的 TTL 和变量值；返回值表示该 key 是否存在或被 Redis 成功读取到。
func logRealtimeSubscriptionKey(ctx context.Context, contextName, key, source string) bool {
	value, getErr := config.R.Get(ctx, key).Result()
	if errors.Is(getErr, redis.Nil) {
		fmt.Printf("%s: Redis 实时订阅 key 不存在 source=%s key=%s\n", contextName, source, key)
		return false
	}
	if getErr != nil {
		fmt.Printf("%s: Redis 实时订阅 key 读取失败 source=%s key=%s err=%v\n", contextName, source, key, getErr)
		return true
	}
	ttl, ttlErr := config.R.TTL(ctx, key).Result()
	fmt.Printf("%s: Redis 实时订阅 key source=%s key=%s ttl=%s ttl_err=%v value=%s\n", contextName, source, key, ttl, ttlErr, truncateRealtimeLogValue(value, 300))
	return true
}

// logRealtimeTaskDiagnostics 查询 TIC 中的实时任务状态，辅助判断样本超时是任务未运行还是输出未写入。
func logRealtimeTaskDiagnostics(contextName, taskID string) {
	defer func() {
		if recovered := recover(); recovered != nil {
			fmt.Printf("%s: TIC 实时任务诊断不可用 task_id=%s panic=%v\n", contextName, taskID, recovered)
		}
	}()

	task := simulateService.GetTaskInformation(taskID)
	if task == nil {
		fmt.Printf("%s: TIC 未查询到实时任务 task_id=%s\n", contextName, taskID)
		return
	}
	fmt.Printf("%s: TIC 实时任务 task_id=%s status=%s progress=%s task_type=%s model=%s package_id=%s start=%v stop=%v interval=%v result=%s parent_ids=%v export_variables=%v\n",
		contextName,
		taskID,
		task.Status,
		task.ProgressRate,
		task.TaskType,
		task.ModelName,
		task.PackageId,
		task.StartTime,
		task.StopTime,
		task.OutputInterval,
		task.ResultAddress,
		task.ParentId,
		task.ExportVariables,
	)
}

// logRealtimeDatabaseDiagnostics 查询实时仿真的 case 和实验记录，辅助确认 task_id 与数据库记录是否一致。
func logRealtimeDatabaseDiagnostics(contextName, taskID string) {
	var cases []DataBase.YssimExperimentCase
	if err := config.DB.Where("task_id = ?", taskID).Find(&cases).Error; err != nil {
		fmt.Printf("%s: 查询实时 case 记录失败 task_id=%s err=%v\n", contextName, taskID, err)
		return
	}
	if len(cases) == 0 {
		fmt.Printf("%s: 未查询到实时 case 记录 task_id=%s\n", contextName, taskID)
		return
	}

	experimentIDs := make(map[string]struct{}, len(cases))
	for _, item := range cases {
		fmt.Printf("%s: 实时 case 记录 case_id=%s experiment_id=%s task_batch_id=%s result_path=%s multiple_run=%v\n", contextName, item.ID, item.ExperimentId, item.TaskBatchId, item.SimulateModelResultPath, item.MultipleRunSim)
		experimentIDs[item.ExperimentId] = struct{}{}
	}

	for experimentID := range experimentIDs {
		var record DataBase.YssimExperimentRecord
		if err := config.DB.Where("id = ?", experimentID).First(&record).Error; err != nil {
			fmt.Printf("%s: 查询实时实验记录失败 experiment_id=%s err=%v\n", contextName, experimentID, err)
			continue
		}
		fmt.Printf("%s: 实时实验记录 experiment_id=%s package_id=%s model=%s task_batch_id=%s interactive=%v simulate_type=%s start_time=%s stop_time=%s interval=%s task_created_time=%d export_variables=%s\n",
			contextName,
			record.ID,
			record.PackageId,
			record.ModelName,
			record.TaskBatchId,
			record.Interactive,
			record.SimulateType,
			record.StartTime,
			record.StopTime,
			record.Interval,
			record.TaskCreatedTime,
			truncateRealtimeLogValue(string(record.ExportVariables), 300),
		)
	}
}

// truncateRealtimeLogValue 截断较长的 JSON 或 Redis value，避免诊断日志过大。
func truncateRealtimeLogValue(value string, maxLen int) string {
	value = strings.TrimSpace(value)
	if maxLen <= 0 || len(value) <= maxLen {
		return value
	}
	return value[:maxLen] + "..."
}
