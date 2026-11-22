package data

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/callbacks"
	"gorm.io/gorm/logger"
	"vw_user/internal/data/dal/model"
	"vw_user/internal/data/dal/query"
)

var repoTransactionDriverID atomic.Int64

type repoTransactionDriver struct {
	begins, commits, rollbacks, committed int
	failTable                             string
	fault                                 error
	inserts                               []string
	insertArgs                            [][]driver.NamedValue
	beginErr, commitErr                   error
}

type repoTransactionConn struct {
	state              *repoTransactionDriver
	active             bool
	pending, savepoint int
}

type repoTransactionStub struct{ conn *repoTransactionConn }

type repoTransactionResult struct{}

// LastInsertId 提供 GORM 创建自增模型所需的测试返回值，不分配真实数据库 ID。
func (repoTransactionResult) LastInsertId() (int64, error) { return 1, nil }

// RowsAffected 表示一次模拟插入成功；测试使用语句计数，不用于断言批量插入行数。
func (repoTransactionResult) RowsAffected() (int64, error) { return 1, nil }

// Open 返回内存事务连接，忽略 DSN，不建立网络连接。
func (d *repoTransactionDriver) Open(string) (driver.Conn, error) {
	return &repoTransactionConn{state: d}, nil
}

// Close 释放无外部资源的测试连接，始终成功。
func (c *repoTransactionConn) Close() error { return nil }

// Prepare 拒绝预处理回退，测试只接受已知的事务 SQL。
func (c *repoTransactionConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("离线测试不支持预处理")
}

// Begin 创建内存事务，拒绝同一连接重复开启事务。
func (c *repoTransactionConn) Begin() (driver.Tx, error) {
	if c.state.beginErr != nil {
		return nil, c.state.beginErr
	}
	if c.active {
		return nil, errors.New("连接已有事务")
	}
	c.active, c.pending = true, 0
	c.state.begins++
	return &repoTransactionStub{conn: c}, nil
}

// ExecContext 模拟注册插入及保存点，拒绝事务外写入，可按表注入失败。
// 输出：模拟影响行数或错误；pending 仅在提交时计入已提交语句数，不访问真实数据。
func (c *repoTransactionConn) ExecContext(_ context.Context, text string, args []driver.NamedValue) (driver.Result, error) {
	if !c.active {
		return nil, errors.New("仓储写入逃逸到事务外")
	}
	switch {
	case strings.HasPrefix(text, "SAVEPOINT "):
		c.savepoint = c.pending
	case strings.HasPrefix(text, "ROLLBACK TO SAVEPOINT "):
		c.pending = c.savepoint
	case strings.HasPrefix(text, "INSERT INTO "):
		c.state.inserts = append(c.state.inserts, text)
		c.state.insertArgs = append(c.state.insertArgs, append([]driver.NamedValue{}, args...))
		if c.state.failTable != "" && strings.HasPrefix(text, "INSERT INTO `"+c.state.failTable+"`") {
			return nil, c.state.fault
		}
		c.pending++
	default:
		return nil, fmt.Errorf("不支持的测试 SQL：%s", text)
	}
	return repoTransactionResult{}, nil
}

// Commit 发布本事务的写入计数，关闭活动事务。
func (tx *repoTransactionStub) Commit() error {
	c := tx.conn
	if c.state.commitErr != nil {
		return c.state.commitErr
	}
	c.state.commits++
	c.state.committed += c.pending
	c.active, c.pending = false, 0
	return nil
}

// Rollback 丢弃本事务未提交写入，不影响之前提交的计数。
func (tx *repoTransactionStub) Rollback() error {
	tx.conn.state.rollbacks++
	tx.conn.active, tx.conn.pending = false, 0
	return nil
}

// offlineRepoTransactionDB 构造隔离的内存 SQL 驱动并临时设置默认查询入口。
// 输出：GORM 连接和驱动状态；测试结束关闭连接并还原全局 Query，不读取本机配置。
func offlineRepoTransactionDB(t *testing.T) (*gorm.DB, *repoTransactionDriver) {
	t.Helper()
	state := &repoTransactionDriver{}
	name := fmt.Sprintf("user-repo-tx-%d", repoTransactionDriverID.Add(1))
	sql.Register(name, state)
	pool, err := sql.Open(name, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: pool, SkipInitializeWithVersion: true}), &gorm.Config{
		DisableAutomaticPing: true, SkipDefaultTransaction: true, Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	saved := *query.Q
	query.SetDefault(db)
	t.Cleanup(func() { *query.Q = saved })
	return db, state
}

// TestRegisterQueryTransactionOffline 验证注册独立调用原子提交，嵌入外层事务时不另开或提前提交。
// 内存驱动只验证事务边界和语句计数，不替代 MySQL 约束集成测试。
func TestRegisterQueryTransactionOffline(t *testing.T) {
	for _, mode := range []string{"独立提交", "独立失败", "外层提交", "外层回滚"} {
		t.Run(mode, func(t *testing.T) {
			db, state := offlineRepoTransactionDB(t)
			fault := errors.New("注册事务失败")
			if mode == "独立失败" {
				state.failTable, state.fault = model.TableNameFavorite, fault
			}
			var expected gorm.ConnPool
			original := db.Callback().Create().Get("gorm:create")
			db.Callback().Create().Replace("gorm:create", func(tx *gorm.DB) {
				if expected != nil && tx.Statement.ConnPool != expected {
					t.Fatal("注册没有使用上下文中的事务连接")
				}
				original(tx)
			})
			err := db.Error
			if strings.HasPrefix(mode, "外层") {
				err = db.Transaction(func(tx *gorm.DB) error {
					expected = tx.Statement.ConnPool
					ctx := context.WithValue(context.Background(), transactionKey{}, &query.QueryTx{Query: query.Use(tx)})
					if err := (&userIdentityRepo{}).CreatRecordsForRegister(ctx, &model.User{UserID: 7}); err != nil {
						return err
					}
					if state.commits != 0 || state.committed != 0 {
						t.Fatal("注册提前提交了外层事务")
					}
					if mode == "外层回滚" {
						return fault
					}
					return nil
				})
			} else {
				err = (&userIdentityRepo{}).CreatRecordsForRegister(context.Background(), &model.User{UserID: 7})
			}
			failed := mode == "独立失败" || mode == "外层回滚"
			wantTables := []string{model.TableNameUser, model.TableNameFollowList, model.TableNameFavorite, model.TableNameUserLevel}
			if mode == "独立失败" {
				wantTables = wantTables[:3]
			}
			if len(state.inserts) != len(wantTables) {
				t.Fatalf("注册写入步骤错误：%v", state.inserts)
			}
			for i, table := range wantTables {
				if !strings.HasPrefix(state.inserts[i], "INSERT INTO `"+table+"`") {
					t.Fatalf("注册写入表错误：want=%s SQL=%s", table, state.inserts[i])
				}
				if table == model.TableNameFollowList {
					found := false
					for _, arg := range state.insertArgs[i] {
						if arg.Value == true {
							found = true
						}
					}
					if !strings.Contains(state.inserts[i], "`is_default`") || !found {
						t.Fatal("注册未创建默认关注列表")
					}
				}
			}
			if failed {
				if !errors.Is(err, fault) || state.begins != 1 || state.commits != 0 || state.rollbacks != 1 || state.committed != 0 {
					t.Fatalf("注册失败未回滚：err=%v state=%+v", err, state)
				}
			} else if err != nil || state.begins != 1 || state.commits != 1 || state.rollbacks != 0 || state.committed != 4 {
				t.Fatalf("注册未在单个事务提交四组写入：err=%v state=%+v", err, state)
			}
		})
	}
}

// TestUserReadsQueryTransactionOffline 验证登录经验和粉丝查询使用传入事务，而非全局普通连接。
func TestUserReadsQueryTransactionOffline(t *testing.T) {
	for _, mode := range []string{"经验读取失败", "粉丝", "空粉丝", "粉丝读取失败", "用户读取失败"} {
		t.Run(mode, func(t *testing.T) {
			db, _ := offlineRepoTransactionDB(t)
			tx := db.Begin()
			if tx.Error != nil {
				t.Fatal(tx.Error)
			}
			defer tx.Rollback()
			ctx := context.WithValue(context.Background(), transactionKey{}, &query.QueryTx{Query: query.Use(tx)})
			fault := errors.New("读取失败")
			calls := 0
			db.Callback().Query().Replace("gorm:query", func(db *gorm.DB) {
				calls++
				if db.Statement.ConnPool != tx.Statement.ConnPool {
					t.Fatal("读取绕过了上下文事务")
				}
				callbacks.BuildQuerySQL(db)
				if mode == "经验读取失败" || mode == "粉丝读取失败" || (mode == "用户读取失败" && calls == 2) {
					db.AddError(fault)
					return
				}
				if calls == 1 {
					text := db.Statement.SQL.String()
					if !strings.Contains(text, "`user_fans`") || !strings.Contains(text, "`follower_id`") || !strings.Contains(text, "`deleted_at` IS NULL") {
						t.Fatalf("粉丝查询表、字段或软删除筛选错误：%s", text)
					}
					if mode != "空粉丝" {
						*db.Statement.Dest.(*[]int64) = []int64{9}
					}
				} else if mode != "空粉丝" && calls == 2 {
					*db.Statement.Dest.(*[]*model.User) = []*model.User{{UserID: 9, Username: "fan"}}
				} else if mode != "空粉丝" {
					*db.Statement.Dest.(*[]fanCountRow) = []fanCountRow{{UserID: 9, Count: 3}}
				}
			})
			if mode == "经验读取失败" {
				if err := (&userIdentityRepo{}).AddExpForLogin(ctx, 7); !errors.Is(err, fault) || calls != 1 {
					t.Fatalf("经验读取未使用事务或未透传错误：%v calls=%d", err, calls)
				}
				return
			}
			users, err := (&userInfoRepo{}).GetUserFansByUserID(ctx, 7)
			if mode == "粉丝读取失败" || mode == "用户读取失败" {
				if !errors.Is(err, fault) {
					t.Fatalf("粉丝查询错误未透传：%v", err)
				}
			} else if err != nil || (mode == "粉丝" && len(users) != 1) || (mode == "空粉丝" && len(users) != 0) {
				t.Fatalf("粉丝查询结果错误：%+v %v", users, err)
			}
			if mode == "粉丝" && users[0].CntFans != 3 {
				t.Fatal("粉丝摘要未使用粉丝表聚合数量")
			}
		})
	}
}
