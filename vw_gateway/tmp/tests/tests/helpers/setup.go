package helpers

import (
	"fmt"
	"os"
	"time"
)

// TestConfig 测试配置
type TestConfig struct {
	BaseURL    string
	Timeout    time.Duration
	RetryCount int
	RetryDelay time.Duration
	Username   string
	SpaceID    string
	Token      string
}

// DefaultTestConfig 默认测试配置
func DefaultTestConfig() *TestConfig {
	port := os.Getenv("PORT")

	// 默认请求 147 后端测试环境服务，避免本机后端与远端 MAT/FMU/上传模型目录不同步。
	baseURL := fmt.Sprintf("http://192.168.121.147:%s", port)

	return &TestConfig{
		BaseURL:    baseURL,
		Timeout:    120 * time.Second,
		RetryCount: 3,
		RetryDelay: 1 * time.Second,
		Username:   os.Getenv("TEST_USERNAME"),
		SpaceID:    os.Getenv("TEST_SPACE_ID"),
	}
}

// WaitForServer 等待服务启动
func WaitForServer(baseURL string, maxRetries int) error {
	client := NewAPIClient(baseURL)

	for i := 0; i < maxRetries; i++ {
		_, err := client.GetSysRootModel()
		if err == nil {
			fmt.Printf("✓ Server is ready at %s\n", baseURL)
			return nil
		}

		if i < maxRetries-1 {
			fmt.Printf("⏳ Waiting for server... (attempt %d/%d)\n", i+1, maxRetries)
			time.Sleep(2 * time.Second)
		}
	}

	return fmt.Errorf("server not ready after %d retries", maxRetries)
}

// CleanupTestData 清理测试数据
func CleanupTestData(client *APIClient, modelNames []string) error {
	for _, modelName := range modelNames {
		if _, err := client.UnLoadModel(modelName); err != nil {
			fmt.Printf("Warning: failed to unload model %s: %v\n", modelName, err)
		}
	}
	return nil
}
