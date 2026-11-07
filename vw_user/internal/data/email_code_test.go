package data

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/redis/go-redis/v9"
	"util/helper"
)

// newEmailCodeTestRepo 使用独立内存 Redis 执行真实 Lua；不连接配置中的 Redis 或业务库。
// 必须按文件运行 data 包测试并排除 test_helper.go 的真实 MySQL 初始化。
func newEmailCodeTestRepo(t *testing.T) (*captchaRepo, *miniredis.Miniredis) {
	t.Helper()
	server := miniredis.RunT(t)
	client := redis.NewClusterClient(&redis.ClusterOptions{
		Addrs: []string{server.Addr()}, MaxRetries: -1,
		ClusterSlots: func(context.Context) ([]redis.ClusterSlot, error) {
			return []redis.ClusterSlot{{Start: 0, End: 16383, Nodes: []redis.ClusterNode{{Addr: server.Addr()}}}}, nil
		},
	})
	t.Cleanup(func() { _ = client.Close() })
	return &captchaRepo{data: &Data{redis: client}}, server
}

// TestEmailCodeConsume 验证用途隔离、错误尝试保留正确码、过期和同码并发最多一次成功。
func TestEmailCodeConsume(t *testing.T) {
	repo, server := newEmailCodeTestRepo(t)
	ctx := context.Background()
	email := "one@example.com"
	if err := repo.SetCodeToCache(ctx, email, "123456", helper.EmailCodeReset, "192.0.2.1", 5*time.Minute); err != nil {
		t.Fatal(err)
	}
	if repo.ConsumeCode(ctx, email, helper.EmailCodeRegister, "123456") == nil || repo.ConsumeCode(ctx, email, helper.EmailCodeReset, "999999") == nil {
		t.Fatal("其他用途或错误验证码通过校验")
	}
	var successes atomic.Int32
	var group sync.WaitGroup
	for i := 0; i < 8; i++ {
		group.Add(1)
		go func() {
			defer group.Done()
			if repo.ConsumeCode(ctx, email, helper.EmailCodeReset, "123456") == nil {
				successes.Add(1)
			}
		}()
	}
	group.Wait()
	if successes.Load() != 1 {
		t.Fatalf("同码成功次数=%d", successes.Load())
	}
	server.FastForward(time.Minute)
	if err := repo.SetCodeToCache(ctx, email, "222222", helper.EmailCodeReset, "192.0.2.1", 5*time.Minute); err != nil {
		t.Fatal(err)
	}
	server.FastForward(5 * time.Minute)
	if repo.ConsumeCode(ctx, email, helper.EmailCodeReset, "222222") == nil {
		t.Fatal("过期验证码仍可使用")
	}
}

// TestEmailCodeBudgets 验证五次失败失效、重发不恢复旧码或重置小时预算，以及邮箱/IP 发送限额。
func TestEmailCodeBudgets(t *testing.T) {
	ctx := context.Background()
	t.Run("failed attempts and resend", func(t *testing.T) {
		repo, server := newEmailCodeTestRepo(t)
		for cycle := 0; cycle < 4; cycle++ {
			if err := repo.SetCodeToCache(ctx, "budget@example.com", "123456", helper.EmailCodeReset, "192.0.2.1", 5*time.Minute); err != nil {
				t.Fatal(err)
			}
			for i := 0; i < 5; i++ {
				if repo.ConsumeCode(ctx, "budget@example.com", helper.EmailCodeReset, "000000") == nil {
					t.Fatal("错误码通过校验")
				}
			}
			if server.Exists(emailCodeKey("budget@example.com", helper.EmailCodeReset)) {
				t.Fatal("五次错误后验证码未失效")
			}
			server.FastForward(time.Minute)
		}
		if err := repo.SetCodeToCache(ctx, "budget@example.com", "654321", helper.EmailCodeReset, "192.0.2.1", 5*time.Minute); err != nil {
			t.Fatal(err)
		}
		if err := repo.ConsumeCode(ctx, "budget@example.com", helper.EmailCodeReset, "654321"); kerrors.Code(err) != 429 {
			t.Fatalf("重发重置了验证预算: %v", err)
		}
	})
	t.Run("email cooldown and hourly sends", func(t *testing.T) {
		repo, server := newEmailCodeTestRepo(t)
		for i := 0; i < 5; i++ {
			if err := repo.SetCodeToCache(ctx, "send@example.com", "123456", helper.EmailCodeRegister, "192.0.2.1", 5*time.Minute); err != nil {
				t.Fatal(err)
			}
			if err := repo.SetCodeToCache(ctx, "send@example.com", "654321", helper.EmailCodeReset, "192.0.2.1", 5*time.Minute); kerrors.Code(err) != 429 {
				t.Fatal("跨用途绕过了冷却")
			}
			server.FastForward(time.Minute)
		}
		if err := repo.SetCodeToCache(ctx, "send@example.com", "123456", helper.EmailCodeReset, "192.0.2.1", 5*time.Minute); kerrors.Code(err) != 429 {
			t.Fatal("未限制邮箱小时发送次数")
		}
	})
	t.Run("resend replaces previous code", func(t *testing.T) {
		repo, server := newEmailCodeTestRepo(t)
		if err := repo.SetCodeToCache(ctx, "replace@example.com", "123456", helper.EmailCodeReset, "192.0.2.1", 5*time.Minute); err != nil {
			t.Fatal(err)
		}
		server.FastForward(time.Minute)
		if err := repo.SetCodeToCache(ctx, "replace@example.com", "654321", helper.EmailCodeReset, "192.0.2.1", 5*time.Minute); err != nil {
			t.Fatal(err)
		}
		if repo.ConsumeCode(ctx, "replace@example.com", helper.EmailCodeReset, "123456") == nil {
			t.Fatal("重发后旧码仍有效")
		}
		if err := repo.ConsumeCode(ctx, "replace@example.com", helper.EmailCodeReset, "654321"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("IP limit and session deletion", func(t *testing.T) {
		repo, _ := newEmailCodeTestRepo(t)
		for i := 0; i < 20; i++ {
			if err := repo.SetCodeToCache(ctx, fmt.Sprintf("u%d@example.com", i), "123456", helper.EmailCodeRegister, "192.0.2.1", 5*time.Minute); err != nil {
				t.Fatal(err)
			}
		}
		if err := repo.SetCodeToCache(ctx, "last@example.com", "123456", helper.EmailCodeReset, "192.0.2.1", 5*time.Minute); kerrors.Code(err) != 429 {
			t.Fatal("未限制IP小时发送次数")
		}
		if err := repo.data.redis.Set(ctx, "7", "old-access", time.Hour).Err(); err != nil {
			t.Fatal(err)
		}
		if err := (&userInfoRepo{data: repo.data}).InvalidateUserSession(ctx, 7); err != nil {
			t.Fatal(err)
		}
		if _, err := repo.data.redis.Get(ctx, "7").Result(); !errors.Is(err, redis.Nil) {
			t.Fatal("旧登录态未清除")
		}
	})
}
