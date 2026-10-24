package data

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"vw_video/internal/biz"
	"vw_video/internal/data/dal/query"
)

var explicitQueryDriverID atomic.Int64

type explicitQueryDriver struct {
	queries, writes, commits, rollbacks int
	failRead, failWrite, failVersion    error
	writeTables                         []string
}

type explicitQueryConn struct{ state *explicitQueryDriver }
type explicitQueryTx struct{ state *explicitQueryDriver }
type explicitQueryResult struct{}
type explicitQueryRows struct {
	columns []string
	values  []driver.Value
	read    bool
}

// Open 返回内存 SQL 连接，不使用 DSN 或建立网络连接。
func (d *explicitQueryDriver) Open(string) (driver.Conn, error) {
	return &explicitQueryConn{state: d}, nil
}

// Close 释放无外部资源的测试连接，始终成功。
func (*explicitQueryConn) Close() error { return nil }

// Prepare 拒绝预处理回退，测试仅支持已知 SQL。
func (*explicitQueryConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("离线驱动不支持预处理")
}

// Begin 开启计数事务，实际提交或回滚由调用方决定。
func (c *explicitQueryConn) Begin() (driver.Tx, error) {
	return &explicitQueryTx{state: c.state}, nil
}

// QueryContext 模拟适配器的版本读取及三种模型读取，校验业务查询保留请求上下文。
func (c *explicitQueryConn) QueryContext(ctx context.Context, text string, _ []driver.NamedValue) (driver.Rows, error) {
	c.state.queries++
	if text == "SELECT VERSION()" {
		return &explicitQueryRows{columns: []string{"version"}, values: []driver.Value{"8.0.32"}}, c.state.failVersion
	}
	if ctx.Value(transactionContextKey{}) != "request" {
		return nil, errors.New("业务查询丢失请求上下文")
	}
	if c.state.failRead != nil {
		return nil, c.state.failRead
	}
	switch {
	case strings.Contains(text, "FROM `video`"):
		return &explicitQueryRows{columns: []string{"video_id", "likes", "hot", "cnt_favorited", "cnt_shares", "shells", "version"}, values: []driver.Value{int64(9), int64(4), int64(10), int64(3), int64(2), int64(1), int64(1)}}, nil
	case strings.Contains(text, "FROM `barrages`"):
		return &explicitQueryRows{columns: []string{"barrage_id", "likes"}, values: []driver.Value{int64(11), int64(2)}}, nil
	case strings.Contains(text, "FROM `user_video_status`"):
		return &explicitQueryRows{columns: []string{"user_id", "video_id", "is_like", "is_shared", "shells_count"}, values: []driver.Value{int64(7), int64(9), int64(0), int64(0), int64(0)}}, nil
	default:
		return nil, fmt.Errorf("未知读取 SQL：%s", text)
	}
}

// ExecContext 记录模拟写入，校验上下文，可注入错误；不修改真实数据。
func (c *explicitQueryConn) ExecContext(ctx context.Context, text string, _ []driver.NamedValue) (driver.Result, error) {
	if ctx.Value(transactionContextKey{}) != "request" {
		return nil, errors.New("业务写入丢失请求上下文")
	}
	c.state.writes++
	c.state.writeTables = append(c.state.writeTables, text)
	return explicitQueryResult{}, c.state.failWrite
}

// Commit 仅记录调用次数，供测试排除仓储提前提交。
func (tx *explicitQueryTx) Commit() error { tx.state.commits++; return nil }

// Rollback 仅记录调用次数，供测试核对事务生命周期仍属于调用方。
func (tx *explicitQueryTx) Rollback() error { tx.state.rollbacks++; return nil }

// LastInsertId 为 GORM 创建模型提供模拟自增值。
func (explicitQueryResult) LastInsertId() (int64, error) { return 1, nil }

// RowsAffected 表示一次模拟写入成功，不用于验证真实数据库约束。
func (explicitQueryResult) RowsAffected() (int64, error) { return 1, nil }

// Columns 返回本次读取的固定模型列。
func (r *explicitQueryRows) Columns() []string { return r.columns }

// Close 结束不持有外部资源的读取。
func (*explicitQueryRows) Close() error { return nil }

// Next 返回唯一一行模拟数据，之后报告 EOF。
func (r *explicitQueryRows) Next(dest []driver.Value) error {
	if r.read {
		return io.EOF
	}
	copy(dest, r.values)
	r.read = true
	return nil
}

// offlineExplicitQueryDB 创建无网络的 GORM 连接及其独立调用计数，不修改全局查询器。
func offlineExplicitQueryDB(t *testing.T) (*gorm.DB, *explicitQueryDriver) {
	t.Helper()
	state := &explicitQueryDriver{}
	name := fmt.Sprintf("explicit-query-%d", explicitQueryDriverID.Add(1))
	sql.Register(name, state)
	pool, err := sql.Open(name, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: pool, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return db, state
}

// TestVideoExplicitQueryOffline 验证全部显式事务仓储使用目标事务，而非默认连接或 ctx 中另一事务。
func TestVideoExplicitQueryOffline(t *testing.T) {
	type queryCase struct {
		name, table string
		run         func(context.Context, *sql.Tx) error
	}
	i, v := &interactRepo{}, &videoInfoRepo{}
	cases := []queryCase{
		{"点赞", "video", func(ctx context.Context, tx *sql.Tx) error { return i.AddVideoUpvote(ctx, tx, 9) }},
		{"取消点赞", "video", func(ctx context.Context, tx *sql.Tx) error { return i.DecrementVideoUpvote(ctx, tx, 9) }},
		{"收藏计数", "video", func(ctx context.Context, tx *sql.Tx) error { return i.AddVideoCntFavorite(ctx, tx, 9) }},
		{"取消收藏计数", "video", func(ctx context.Context, tx *sql.Tx) error { return i.DecrementVideoCntFavorite(ctx, tx, 9) }},
		{"收藏关系", "favorite_video", func(ctx context.Context, tx *sql.Tx) error { return i.AddUserFavoriteVideo(ctx, tx, 13, 9) }},
		{"取消收藏关系", "favorite_video", func(ctx context.Context, tx *sql.Tx) error { return i.DeleteUserFavoriteVideo(ctx, tx, 13, 9) }},
		{"贝壳计数", "video", func(ctx context.Context, tx *sql.Tx) error { return i.UpdateVideoShells(ctx, tx, 9, 1) }},
		{"弹幕点赞", "barrages", func(ctx context.Context, tx *sql.Tx) error { return i.AddVideoBarrageUpvote(ctx, tx, 11) }},
		{"取消弹幕点赞", "barrages", func(ctx context.Context, tx *sql.Tx) error { return i.DecrementVideoBarrageUpvote(ctx, tx, 11) }},
		{"分享计数", "video", func(ctx context.Context, tx *sql.Tx) error { return v.UpdateVideoCntShare(ctx, tx, 9) }},
		{"分享补偿", "video", func(ctx context.Context, tx *sql.Tx) error { return v.UpdateVideoCntShareCompensation(ctx, tx, 9) }},
		{"热度", "video", func(ctx context.Context, tx *sql.Tx) error { return v.SetVideoHot(ctx, tx, 9, 1) }},
		{"个人状态", "user_video_status", func(ctx context.Context, tx *sql.Tx) error {
			return i.ApplyUserVideoStatus(ctx, tx, 7, 9, &biz.VideoStatusPatch{IsLike: &biz.StatusChange{Before: 0, After: 1}})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			globalDB, global := offlineExplicitQueryDB(t)
			saved := *query.Q
			query.SetDefault(globalDB)
			t.Cleanup(func() { *query.Q = saved })
			contextDB, other := offlineExplicitQueryDB(t)
			contextTx := contextDB.Begin()
			defer contextTx.Rollback()
			explicitDB, target := offlineExplicitQueryDB(t)
			explicitTx := explicitDB.Begin()
			defer explicitTx.Rollback()
			tx := explicitTx.Statement.ConnPool.(*sql.Tx)
			request := context.WithValue(context.Background(), transactionContextKey{}, "request")
			ctx := context.WithValue(request, transactionKey{}, &query.QueryTx{Query: query.Use(contextTx)})
			originalQuery := getQuery(ctx)
			if err := tc.run(ctx, tx); err != nil {
				t.Fatal(err)
			}
			if target.queries < 2 || target.writes == 0 || target.commits != 0 || target.rollbacks != 0 || getQuery(ctx) != originalQuery {
				t.Fatalf("目标事务被切换、提前结束或污染原上下文：%+v", target)
			}
			for _, text := range target.writeTables {
				if !strings.Contains(text, "`"+tc.table+"`") {
					t.Fatalf("写入表发生变化：%s", text)
				}
				if tc.table == "video" && (!strings.Contains(text, "`version` = ?") || !strings.Contains(text, "`version`=`version`+1")) {
					t.Fatalf("视频更新缺少乐观锁条件或版本递增：%s", text)
				}
			}
			if global.queries != 0 || global.writes != 0 || other.queries != 0 || other.writes != 0 {
				t.Fatal("SQL 逃逸到普通连接或上下文中的其他事务")
			}
			if err := tc.run(ctx, nil); err == nil {
				t.Fatal("空事务被静默接受")
			}
		})
	}
}

// TestVideoExplicitQueryErrorsOffline 验证适配、读取、更新、取消和已结束事务的错误均返回，不切换连接重试。
func TestVideoExplicitQueryErrorsOffline(t *testing.T) {
	for _, mode := range []string{"适配", "读取", "写入", "取消", "已回滚"} {
		t.Run(mode, func(t *testing.T) {
			db, state := offlineExplicitQueryDB(t)
			txDB := db.Begin()
			defer txDB.Rollback()
			tx := txDB.Statement.ConnPool.(*sql.Tx)
			fault := errors.New("测试 SQL 失败")
			ctx := context.WithValue(context.Background(), transactionContextKey{}, "request")
			want := fault
			switch mode {
			case "适配":
				state.failVersion = fault
			case "读取":
				state.failRead = fault
			case "写入":
				state.failWrite = fault
			case "取消":
				cancelled, cancel := context.WithCancel(ctx)
				cancel()
				ctx, want = cancelled, context.Canceled
			case "已回滚":
				txDB.Rollback()
				want = sql.ErrTxDone
			}
			err := (&interactRepo{}).AddVideoUpvote(ctx, tx, 9)
			if !errors.Is(err, want) {
				t.Fatalf("错误未透传：got=%v want=%v", err, want)
			}
			if mode != "写入" && state.writes != 0 {
				t.Fatal("失败后仍执行业务写入")
			}
			if state.commits != 0 {
				t.Fatal("仓储擅自提交事务")
			}
		})
	}
}

// TestVideoContextQueryVersionOffline 验证普通上下文事务的结构体更新同样保留读取版本条件。
func TestVideoContextQueryVersionOffline(t *testing.T) {
	for _, name := range []string{"播放量", "封面", "弹幕数"} {
		t.Run(name, func(t *testing.T) {
			db, state := offlineExplicitQueryDB(t)
			txDB := db.Begin()
			defer txDB.Rollback()
			request := context.WithValue(context.Background(), transactionContextKey{}, "request")
			ctx := context.WithValue(request, transactionKey{}, &query.QueryTx{Query: query.Use(txDB)})
			var err error
			switch name {
			case "播放量":
				err = (&videoInfoRepo{}).AddVideoCntView(ctx, 9)
			case "封面":
				err = (&videoInfoRepo{}).SetCoverPath(ctx, 9, "cover.jpg")
			case "弹幕数":
				err = (&interactRepo{}).AddVideoCntBarrage(ctx, 9)
			}
			if err != nil || state.queries != 1 || state.writes != 1 || state.commits != 0 || state.rollbacks != 0 {
				t.Fatalf("上下文事务的模型更新异常：err=%v state=%+v", err, state)
			}
			text := state.writeTables[0]
			if !strings.Contains(text, "`version` = ?") || !strings.Contains(text, "`version`=`version`+1") {
				t.Fatalf("结构体更新缺少版本条件或版本递增：%s", text)
			}
		})
	}
}
