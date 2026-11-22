package data

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"sync/atomic"
	"testing"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

var transactionDriverID atomic.Int64

type transactionDriver struct {
	fail               error
	beginFail          error
	commits, rollbacks int
}
type transactionConn struct{ state *transactionDriver }
type transactionStub struct{ state *transactionDriver }

// transactionContextKey 用于验证事务上下文仍保留请求信息。
type transactionContextKey struct{}

// Open 返回本地事务测试连接，忽略 DSN，不建立网络连接。
func (d *transactionDriver) Open(string) (driver.Conn, error) { return &transactionConn{state: d}, nil }

// Close 关闭不持有外部资源的测试连接，始终成功。
func (c *transactionConn) Close() error { return nil }

// Prepare 拒绝 SQL 执行，确保测试仅验证事务生命周期。
func (c *transactionConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("测试不执行 SQL")
}

// Begin 创建测试事务，或返回注入的开启错误以验证业务回调不会执行。
func (c *transactionConn) Begin() (driver.Tx, error) {
	if c.state.beginFail != nil {
		return nil, c.state.beginFail
	}
	return &transactionStub{state: c.state}, nil
}

// Commit 记录提交次数并返回注入的错误，不修改真实数据。
func (t *transactionStub) Commit() error { t.state.commits++; return t.state.fail }

// Rollback 记录回滚次数，始终成功，不访问数据库。
func (t *transactionStub) Rollback() error { t.state.rollbacks++; return nil }

// TestVideoLocalTransactionOffline 验证上下文传递、成功提交、开启/业务/提交错误和 panic 回滚。
func TestVideoLocalTransactionOffline(t *testing.T) {
	for _, mode := range []string{"提交", "开启失败", "业务错误", "提交失败", "panic"} {
		t.Run(mode, func(t *testing.T) {
			state := &transactionDriver{}
			fault := errors.New("事务失败")
			if mode == "提交失败" {
				state.fail = fault
			}
			if mode == "开启失败" {
				state.beginFail = fault
			}
			name := fmt.Sprintf("video-tx-%d", transactionDriverID.Add(1))
			sql.Register(name, state)
			pool, err := sql.Open(name, "")
			if err != nil {
				t.Fatal(err)
			}
			defer pool.Close()
			db, err := gorm.Open(mysql.New(mysql.Config{Conn: pool, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
			if err != nil {
				t.Fatal(err)
			}
			var recovered any
			called := false
			requestContext := context.WithValue(context.Background(), transactionContextKey{}, "request-value")
			func() {
				defer func() { recovered = recover() }()
				err = (&Data{mysql: db}).WithTx(requestContext, func(ctx context.Context) error {
					called = true
					if ctx.Value(transactionContextKey{}) != "request-value" {
						t.Fatal("事务上下文丢失请求信息")
					}
					if _, ok := interactionDB(ctx).Statement.ConnPool.(*sql.Tx); !ok {
						t.Fatal("仓储没有使用本地事务")
					}
					if mode == "panic" {
						panic("测试回滚")
					}
					if mode == "业务错误" {
						return fault
					}
					return nil
				})
			}()
			if mode != "开启失败" && !called {
				t.Fatal("开启成功后没有执行业务回调")
			}
			if mode != "panic" && recovered != nil {
				t.Fatalf("出现非预期 panic：%v", recovered)
			}
			switch mode {
			case "提交":
				if err != nil || state.commits != 1 || state.rollbacks != 0 {
					t.Fatalf("提交错误：%v %+v", err, state)
				}
			case "开启失败":
				if !errors.Is(err, fault) || called || state.commits != 0 || state.rollbacks != 0 {
					t.Fatalf("开启失败未直接返回：%v called=%v %+v", err, called, state)
				}
			case "业务错误":
				if !errors.Is(err, fault) || state.rollbacks != 1 || state.commits != 0 {
					t.Fatalf("业务错误未回滚：%v %+v", err, state)
				}
			case "提交失败":
				if !errors.Is(err, fault) || state.commits != 1 {
					t.Fatalf("提交错误被吞掉：%v", err)
				}
			case "panic":
				if recovered != "测试回滚" || state.rollbacks != 1 || state.commits != 0 {
					t.Fatalf("panic 未回滚：%v %+v", recovered, state)
				}
			}
		})
	}
}
