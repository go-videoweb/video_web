package biz

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dtm-labs/dtm/client/dtmgrpc/dtmgimp"
	"google.golang.org/grpc/metadata"
	interactv1 "vw_video/api/v1/interact"
)

var barrierDriverID atomic.Int64

// barrierStore 只模拟 SQL 事务提交与唯一键，实际屏障算法仍运行项目依赖的 DTM SDK。
type barrierStore struct {
	markers                    map[string]bool
	writes, commits, rollbacks int
}
type barrierDriver struct{ store *barrierStore }
type barrierConn struct {
	store *barrierStore
	tx    *barrierTx
}
type barrierTx struct {
	conn    *barrierConn
	markers map[string]bool
	writes  int
}

// Open 返回共享内存屏障数据的测试连接，忽略 DSN，不建立网络连接。
func (d *barrierDriver) Open(string) (driver.Conn, error) { return &barrierConn{store: d.store}, nil }

// Close 关闭无外部资源的测试连接，始终成功。
func (c *barrierConn) Close() error { return nil }

// Prepare 拒绝预处理回退，确保测试实际走 ExecContext 的事务检查。
func (c *barrierConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("测试不接受预处理回退")
}

// Begin 复制已提交屏障状态，后续写入只留在本次事务中直至提交。
func (c *barrierConn) Begin() (driver.Tx, error) {
	tx := &barrierTx{conn: c, markers: map[string]bool{}}
	for k, v := range c.store.markers {
		tx.markers[k] = v
	}
	c.tx = tx
	return tx, nil
}

// ExecContext 模拟屏障唯一键插入和业务更新，只允许在活动事务内执行已知 SQL。
// 输入：query 与参数由 DTM SDK 或测试仓储提供；输出：模拟影响行数，未知 SQL/事务外写入报错。
func (c *barrierConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	if c.tx == nil {
		return nil, errors.New("写入逃逸到事务外")
	}
	if strings.HasPrefix(strings.ToLower(query), "insert") && strings.Contains(query, "barrier") {
		if len(args) < 5 {
			return nil, errors.New("屏障参数缺失")
		}
		key := fmt.Sprint(args[1].Value, "/", args[2].Value, "/", args[3].Value, "/", args[4].Value)
		if c.tx.markers[key] {
			return driver.RowsAffected(0), nil
		}
		c.tx.markers[key] = true
		return driver.RowsAffected(1), nil
	}
	if query != "UPDATE test_video_state" {
		return nil, fmt.Errorf("意外 SQL：%s", query)
	}
	c.tx.writes++
	return driver.RowsAffected(1), nil
}

// Commit 发布本次屏障标记和写入数量，并记录一次提交；不访问真实数据库。
func (tx *barrierTx) Commit() error {
	tx.conn.store.markers = tx.markers
	tx.conn.store.writes += tx.writes
	tx.conn.store.commits++
	tx.conn.tx = nil
	return nil
}

// Rollback 丢弃本次未提交标记和写入，保留先前已提交的数据。
func (tx *barrierTx) Rollback() error {
	tx.conn.store.rollbacks++
	tx.conn.tx = nil
	return nil
}

type barrierStatusRepo struct {
	InteractRepo
	t        *testing.T
	db       *sql.DB
	opened   *sql.Tx
	calls    int
	failOnce bool
}

// GetSqlTx 开启并记录测试 SQL 事务，供后续检查屏障和业务是否复用同一事务。
// 输出：事务；开启失败时立即结束当前测试，不连接真实数据库。
func (r *barrierStatusRepo) GetSqlTx() *sql.Tx {
	tx, err := r.db.Begin()
	if err != nil {
		r.t.Fatal(err)
	}
	r.opened = tx
	return tx
}

// ApplyUserVideoStatus 验证传入事务身份后模拟一次业务写入，可按需注入一次失败。
// 输入：tx 必须等于 GetSqlTx 返回的事务；输出：模拟写入结果，不解释具体状态字段。
func (r *barrierStatusRepo) ApplyUserVideoStatus(ctx context.Context, tx *sql.Tx, _, _ int64, _ *VideoStatusPatch) error {
	if tx != r.opened {
		r.t.Fatal("业务写入没有复用屏障事务")
	}
	r.calls++
	if _, err := tx.ExecContext(ctx, "UPDATE test_video_state"); err != nil {
		return err
	}
	if r.failOnce {
		r.failOnce = false
		return errors.New("模拟写入失败")
	}
	return nil
}

// statusBarrierContext 构造固定 GID/分支的入站 DTM 元数据，供重复及补偿调用测试复用。
// 输入：op 为 action 或 compensate；输出：仅含测试元数据的上下文，不发出 RPC。
func statusBarrierContext(op string) context.Context {
	ctx := dtmgimp.TransInfo2Ctx(context.Background(), "offline-state", "saga", "01", op, "unused")
	md, _ := metadata.FromOutgoingContext(ctx)
	return metadata.NewIncomingContext(context.Background(), md)
}

// TestVideoStateSQLBarrier 验证重复调用、空补偿、防悬挂和失败回滚，不连接基础设施。
func TestVideoStateSQLBarrier(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		ops                      []string
		fail                     bool
		calls, writes, rollbacks int
	}{
		{"重复正向", []string{"action", "action"}, false, 1, 1, 0},
		{"重复补偿", []string{"action", "compensate", "compensate"}, false, 2, 2, 0},
		{"空补偿和悬挂", []string{"compensate", "action"}, false, 0, 0, 0},
		{"写入失败后重试", []string{"action", "action"}, true, 2, 1, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &barrierStore{markers: map[string]bool{}}
			name := fmt.Sprintf("offline-video-barrier-%d", barrierDriverID.Add(1))
			sql.Register(name, &barrierDriver{store: store})
			db, err := sql.Open(name, "")
			if err != nil {
				t.Fatal(err)
			}
			defer db.Close()
			db.SetMaxOpenConns(1)
			repo := &barrierStatusRepo{t: t, db: db, failOnce: tc.fail}
			uc := &InteractUsecase{repo: repo}
			req := &interactv1.SetUserVideoStatusReq{UserId: 7, VideoId: 9, Statuses: &interactv1.SetUserVideoStatusReq_Statuses{IsShared: 1}}
			for n, op := range tc.ops {
				if op == "compensate" {
					err = uc.SetUserVideoStatusRevert(statusBarrierContext(op), 7, 9, req)
				} else {
					err = uc.SetUserVideoStatus(statusBarrierContext(op), 7, 9, req)
				}
				if tc.fail && n == 0 {
					if err == nil {
						t.Fatal("失败没有触发回滚")
					}
					continue
				}
				if err != nil {
					t.Fatal(err)
				}
			}
			if repo.calls != tc.calls || store.writes != tc.writes || store.rollbacks != tc.rollbacks {
				t.Fatalf("屏障执行错误：calls=%d store=%+v", repo.calls, store)
			}
			repo.opened = nil
			if err := uc.SetUserVideoStatus(context.Background(), 7, 9, req); err == nil || repo.opened != nil {
				t.Fatal("缺失 DTM 元数据仍开启事务")
			}
		})
	}
}
