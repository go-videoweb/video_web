package data

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"fmt"
	klog "github.com/go-kratos/kratos/v2/log"
	drivermysql "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"io"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	comments "vw_video/api/v1/comment"
	interact "vw_video/api/v1/interact"
	"vw_video/internal/biz"
	"vw_video/internal/data/dal/model"
)

type socialSQLStep struct {
	text     string
	contains []string
	columns  []string
	rows     [][]driver.Value
	affected int64
	err      error
	args     []driver.NamedValue
}

// TestSendBarrageSecondsAndRollback 验证零秒入库与视频计数处于同一事务，版本冲突回滚弹幕。
func TestSendBarrageSecondsAndRollback(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(fmt.Sprintf("version-conflict-%v", conflict), func(t *testing.T) {
			insert := &socialSQLStep{contains: []string{"INSERT INTO `barrages`", "publish_time"}, affected: 1}
			video := &socialSQLStep{contains: []string{"FROM `video`", "video_id", "deleted_at"}, columns: []string{"video_id", "cnt_barrages", "hot", "version"}, rows: [][]driver.Value{{int64(9), int64(3), int64(5), int64(1)}}}
			update := &socialSQLStep{contains: []string{"UPDATE `video`", "cnt_barrages", "version", "video_id", "deleted_at"}, affected: 1}
			if conflict {
				update.affected = 0
			}
			d, state := newSocialSQL(t, socialVideoStep(), insert, video, update)
			uc := biz.NewInteractUseCase(&interactRepo{}, nil, d, klog.NewStdLogger(io.Discard))
			err := uc.SendBarrage(context.Background(), &interact.VideoSendBarrageReq{PublisherId: 7, VideoId: 9, Content: "弹幕", PositionSecond: 0, Color: "#ffffff"})
			if (err != nil) != conflict {
				t.Fatalf("发送结果错误：%v", err)
			}
			if conflict {
				if state.rollbacks != 1 || state.commits != 0 {
					t.Fatal("版本冲突未回滚")
				}
			} else if state.commits != 1 || state.rollbacks != 0 {
				t.Fatal("发送未一起提交")
			}
			values := namedValues(insert.args)
			zero := false
			for _, value := range values {
				if value == int64(0) {
					zero = true
				}
			}
			if !zero {
				t.Fatal("零秒位置未保存")
			}
		})
	}
}

// TestReplyRootAndReferenceSQL 验证直接回复可指向楼内回复，但计数始终更新根评论。
func TestReplyRootAndReferenceSQL(t *testing.T) {
	parent := &socialSQLStep{contains: []string{"FROM `comment`", "video_id", "comment_id", "deleted_at"}, columns: []string{"comment_id", "root_id", "video_id", "parent_id"}, rows: [][]driver.Value{{int64(11), int64(1), int64(9), int64(10)}}}
	root := &socialSQLStep{contains: []string{"FROM `comment`", "video_id", "parent_id", "deleted_at"}, columns: []string{"comment_id", "root_id", "video_id", "parent_id"}, rows: [][]driver.Value{{int64(1), int64(1), int64(9), int64(-1)}}}
	update := &socialSQLStep{contains: []string{"UPDATE `comment`", "cnt_replies + 1", "parent_id", "deleted_at"}, affected: 1}
	refs := &socialSQLStep{contains: []string{"FROM `comment`", "root_id", "video_id", "comment_id", "deleted_at"}, columns: []string{"comment_id", "content"}, rows: [][]driver.Value{{int64(11), "完整正文"}}}
	d, _ := newSocialSQL(t, socialVideoStep(), parent, root, update, refs)
	err := d.WithTx(context.Background(), func(ctx context.Context) error {
		r := &videoCommentRepo{}
		id, err := r.GetRootIDByParentID(ctx, 9, 11)
		if err != nil {
			return err
		}
		if id != 1 {
			t.Fatal("未解析到整楼根 ID")
		}
		if err = r.IncrementCommentCntReplies(ctx, id); err != nil {
			return err
		}
		rows, err := r.GetCommentReferences(ctx, 9, 1, []int64{11})
		if err == nil && (len(rows) != 1 || rows[0].Content != "完整正文") {
			t.Fatal("完整引用丢失")
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	values := namedValues(update.args)
	foundRoot := false
	for _, value := range values {
		if value == int64(1) {
			foundRoot = true
		}
		if value == int64(11) {
			t.Fatal("回复计数仍更新直接父回复")
		}
	}
	if !foundRoot {
		t.Fatal("根计数缺少目标 ID")
	}
}

// TestSocialMissingTargetsRollback 验证失效或跨视频目标不会创建点赞关系。
func TestSocialMissingTargetsRollback(t *testing.T) {
	for _, kind := range []string{"comment", "barrage"} {
		t.Run(kind, func(t *testing.T) {
			table := "comment"
			if kind == "barrage" {
				table = "barrages"
			}
			target := &socialSQLStep{contains: []string{"FROM `" + table + "`", "video_id", "deleted_at"}, columns: []string{"video_id"}}
			d, state := newSocialSQL(t, socialVideoStep(), target)
			liked := true
			if kind == "comment" {
				_, err := biz.NewVideoCommentUsecase(&videoCommentRepo{}, d, klog.NewStdLogger(io.Discard)).UpvoteComment(context.Background(), &comments.UpvoteCommentReq{UserId: 7, VideoId: 9, CommentId: 11, Liked: &liked})
				if err == nil {
					t.Fatal("失效评论仍写入")
				}
			} else {
				_, err := biz.NewInteractUseCase(&interactRepo{}, nil, d, klog.NewStdLogger(io.Discard)).SetBarrageLike(context.Background(), &interact.SetBarrageLikeReq{UserId: 7, VideoId: 9, BarrageId: 11, Liked: &liked})
				if err == nil {
					t.Fatal("失效弹幕仍写入")
				}
			}
			if state.rollbacks != 1 || state.commits != 0 {
				t.Fatal("失败事务未回滚")
			}
		})
	}
}

// TestNewRootCommentAndVideoCount 验证主评论 root_id 回写以及视频计数的原子更新。
func TestNewRootCommentAndVideoCount(t *testing.T) {
	insert := &socialSQLStep{contains: []string{"INSERT INTO `comment`"}, affected: 1}
	root := &socialSQLStep{contains: []string{"UPDATE `comment`", "root_id", "comment_id", "deleted_at"}, affected: 1}
	video := &socialSQLStep{contains: []string{"UPDATE `video`", "cnt_comments + 1", "hot + ?", "version", "video_id", "deleted_at"}, affected: 1}
	d, _ := newSocialSQL(t, insert, root, video)
	err := d.WithTx(context.Background(), func(ctx context.Context) error {
		r := &videoCommentRepo{}
		id, err := r.InsertComment(ctx, &model.Comment{CommentID: 11, VideoID: 9, ParentID: -1, RootID: -1, Content: "主评论"})
		if err != nil {
			return err
		}
		if id != 11 {
			t.Fatal("创建 ID 丢失")
		}
		return r.AddVideoHotAndCntCommentsByComment(ctx, 9)
	})
	if err != nil {
		t.Fatal(err)
	}
}

type socialSQLDriver struct {
	steps                      []*socialSQLStep
	begins, commits, rollbacks int
	commitErr                  error
	t                          *testing.T
}
type socialSQLConn struct{ state *socialSQLDriver }
type socialSQLTx struct{ state *socialSQLDriver }
type socialSQLResult struct{ affected int64 }

// LastInsertId 模拟无自增 ID 关系表的 MySQL 插入结果。
func (r socialSQLResult) LastInsertId() (int64, error) { return 0, nil }

// RowsAffected 返回样例语句实际影响行数。
func (r socialSQLResult) RowsAffected() (int64, error) { return r.affected, nil }

type socialSQLRows struct {
	columns []string
	rows    [][]driver.Value
	at      int
}

var socialSQLID atomic.Int64

// Open 返回内存驱动连接，不使用 DSN 或访问网络。
func (d *socialSQLDriver) Open(string) (driver.Conn, error) { return &socialSQLConn{state: d}, nil }

// Prepare 拒绝未约定的预处理路径。
func (c *socialSQLConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("unexpected prepare")
}

// Close 释放无外部资源的离线连接。
func (c *socialSQLConn) Close() error { return nil }

// Begin 记录真实 database/sql 事务边界。
func (c *socialSQLConn) Begin() (driver.Tx, error) {
	c.state.begins++
	return &socialSQLTx{state: c.state}, nil
}

// Commit 可注入提交错误，验证业务不会提前报告成功。
func (tx *socialSQLTx) Commit() error { tx.state.commits++; return tx.state.commitErr }

// Rollback 记录写入失败后的回滚。
func (tx *socialSQLTx) Rollback() error { tx.state.rollbacks++; return nil }

// take 检查完整 SQL 中的必要条件及语句顺序，同时保留绑定参数供断言。
func (d *socialSQLDriver) take(text string, args []driver.NamedValue) (*socialSQLStep, error) {
	if len(d.steps) == 0 {
		d.t.Errorf("额外 SQL：%s", text)
		return nil, errors.New("unexpected SQL")
	}
	step := d.steps[0]
	d.steps = d.steps[1:]
	step.text = text
	step.args = append([]driver.NamedValue(nil), args...)
	for _, part := range step.contains {
		if !strings.Contains(strings.ToLower(text), strings.ToLower(part)) {
			d.t.Errorf("SQL 缺少 %q：%s", part, text)
		}
	}
	return step, step.err
}

// QueryContext 按预设行集执行查询，所有记录只存在于内存。
func (c *socialSQLConn) QueryContext(_ context.Context, text string, args []driver.NamedValue) (driver.Rows, error) {
	step, err := c.state.take(text, args)
	if err != nil {
		return nil, err
	}
	return &socialSQLRows{columns: step.columns, rows: step.rows}, nil
}

// ExecContext 返回指定影响行数，支持唯一键及计数更新失败注入。
func (c *socialSQLConn) ExecContext(_ context.Context, text string, args []driver.NamedValue) (driver.Result, error) {
	step, err := c.state.take(text, args)
	if err != nil {
		return nil, err
	}
	return socialSQLResult{affected: step.affected}, nil
}

// Columns 返回当前样例行集列名。
func (r *socialSQLRows) Columns() []string { return r.columns }

// Close 不持有网络或文件资源。
func (r *socialSQLRows) Close() error { return nil }

// Next 依次填充多行样例，结束时返回 EOF。
func (r *socialSQLRows) Next(dest []driver.Value) error {
	if r.at >= len(r.rows) {
		return io.EOF
	}
	copy(dest, r.rows[r.at])
	r.at++
	return nil
}

// newSocialSQL 为每个测试构造独立驱动，不设置全局默认查询器。
func newSocialSQL(t *testing.T, steps ...*socialSQLStep) (*Data, *socialSQLDriver) {
	t.Helper()
	state := &socialSQLDriver{t: t, steps: steps}
	name := fmt.Sprintf("social-offline-%d", socialSQLID.Add(1))
	sql.Register(name, state)
	pool, err := sql.Open(name, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = pool.Close()
		if len(state.steps) != 0 {
			t.Errorf("有 %d 条 SQL 未执行", len(state.steps))
		}
	})
	db, err := gorm.Open(mysql.New(mysql.Config{Conn: pool, SkipInitializeWithVersion: true}), &gorm.Config{DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	return &Data{mysql: db}, state
}

// socialVideoStep 提供有效视频样例，验证所有读取和写入均排除软删除视频。
func socialVideoStep() *socialSQLStep {
	return &socialSQLStep{contains: []string{"FROM `video`", "deleted_at", "video_id"}, columns: []string{"video_id"}, rows: [][]driver.Value{{int64(9)}}}
}

// socialCommentStep 提供同视频评论和计数样例。
func socialCommentStep(count int64) *socialSQLStep {
	return &socialSQLStep{contains: []string{"FROM `comment`", "video_id", "comment_id", "deleted_at"}, columns: []string{"comment_id", "video_id", "cnt_upvote", "version"}, rows: [][]driver.Value{{int64(11), int64(9), count, int64(1)}}}
}

// TestCommentLikeLocalTransaction 验证重复、取消、下溢、唯一键错误分类和提交失败。
func TestCommentLikeLocalTransaction(t *testing.T) {
	for _, mode := range []string{"like", "duplicate", "unlike", "absent", "counter-conflict", "write-failure", "foreign-key", "commit-failure"} {
		t.Run(mode, func(t *testing.T) {
			liked := mode != "unlike" && mode != "absent" && mode != "counter-conflict"
			relation := &socialSQLStep{contains: []string{"INSERT INTO `user_comment_like`"}, affected: 1}
			if !liked {
				relation.contains = []string{"DELETE FROM `user_comment_like`", "user_id", "video_id", "comment_id"}
			}
			changed := true
			if mode == "duplicate" {
				relation.err = &drivermysql.MySQLError{Number: 1062}
				changed = false
			}
			if mode == "absent" {
				relation.affected = 0
				changed = false
			}
			if mode == "foreign-key" {
				relation.err = &drivermysql.MySQLError{Number: 1452}
				changed = false
			}
			steps := []*socialSQLStep{socialVideoStep(), socialCommentStep(3), relation}
			failure := mode == "counter-conflict" || mode == "write-failure" || mode == "foreign-key" || mode == "commit-failure"
			if changed {
				update := &socialSQLStep{contains: []string{"UPDATE `comment`", "cnt_upvote + ?", "version", "video_id", "comment_id", "deleted_at"}, affected: 1}
				if !liked {
					update.contains = append(update.contains, "cnt_upvote > 0")
				}
				if mode == "counter-conflict" {
					update.affected = 0
				}
				if mode == "write-failure" {
					update.err = errors.New("write failed")
				}
				steps = append(steps, update)
				if mode != "counter-conflict" && mode != "write-failure" {
					count := int64(4)
					if !liked {
						count = 2
					}
					steps = append(steps, socialCommentStep(count))
				}
			}
			d, state := newSocialSQL(t, steps...)
			if mode == "commit-failure" {
				state.commitErr = errors.New("commit failed")
			}
			repo := &videoCommentRepo{data: d}
			uc := biz.NewVideoCommentUsecase(repo, d, klog.NewStdLogger(io.Discard))
			resp, err := uc.UpvoteComment(context.Background(), &comments.UpvoteCommentReq{UserId: 7, VideoId: 9, CommentId: 11, Liked: &liked})
			if (err != nil) != failure {
				t.Fatalf("事务结果错误：%v %v", resp, err)
			}
			if !failure && (resp.IsUpvoted != liked || state.commits != 1 || state.rollbacks != 0) {
				t.Fatal("目标或提交状态错误")
			}
			if failure && mode != "commit-failure" && (state.rollbacks != 1 || state.commits != 0) {
				t.Fatal("部分写入未回滚")
			}
			if state.begins != 1 {
				t.Fatal("未使用单个本地事务")
			}
			values := namedValues(relation.args)
			found := false
			for _, value := range values {
				if value == int64(7) {
					found = true
				}
			}
			if !found {
				t.Fatal("关系写入缺少用户身份")
			}
		})
	}
}

// TestCommentPageSQLScope 验证主评论与回复的完整 SQL、绑定参数及游标 OR 分组。
func TestCommentPageSQLScope(t *testing.T) {
	tests := []struct {
		name                    string
		rootID                  int64
		sortBy, order, operator string
	}{
		{name: "root-newest", sortBy: "created_at", order: "desc", operator: "<"},
		{name: "root-oldest", sortBy: "created_at", order: "asc", operator: ">"},
		{name: "root-hot", sortBy: "cnt_upvote", order: "desc", operator: "<"},
		{name: "reply-newest", rootID: 1, sortBy: "created_at", order: "desc", operator: "<"},
		{name: "reply-oldest", rootID: 1, sortBy: "created_at", order: "asc", operator: ">"},
		{name: "reply-hot", rootID: 1, sortBy: "cnt_upvote", order: "desc", operator: "<"},
	}
	for _, tc := range tests {
		for _, hasCursor := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/cursor=%v", tc.name, hasCursor), func(t *testing.T) {
				q, err := biz.NewCommentQuery(9, tc.rootID, 7, tc.sortBy, tc.order, nil)
				if err != nil {
					t.Fatal(err)
				}
				cursorTime := time.Date(2026, 10, 31, 1, 2, 3, 0, time.UTC)
				if hasCursor {
					q.After = &biz.CommentCursor{ID: 20, CreatedAt: cursorTime, Likes: 10}
				}

				scope := "`comment`.`video_id` = ? AND `comment`.`parent_id` = ?"
				scopeArgs := []any{int64(9), int64(-1)}
				if tc.rootID > 0 {
					scope = "`comment`.`video_id` = ? AND `comment`.`root_id` = ? AND `comment`.`comment_id` <> ?"
					scopeArgs = []any{int64(9), tc.rootID, tc.rootID}
				}
				countSQL := "SELECT count(*) FROM `comment` WHERE " + scope + " AND `comment`.`deleted_at` IS NULL"
				pageWhere := scope
				pageArgs := append([]any(nil), scopeArgs...)
				if hasCursor {
					column := "`comment`.`" + tc.sortBy + "`"
					pageWhere += " AND (" + column + " " + tc.operator + " ? OR (" + column + " = ? AND `comment`.`comment_id` " + tc.operator + " ?))"
					var value any = cursorTime
					if tc.sortBy == "cnt_upvote" {
						value = int64(10)
					}
					pageArgs = append(pageArgs, value, value, int64(20))
				}
				direction := strings.ToUpper(tc.order)
				pageSQL := "SELECT * FROM `comment` WHERE " + pageWhere + " AND `comment`.`deleted_at` IS NULL ORDER BY `comment`.`" + tc.sortBy + "` " + direction + ",`comment`.`comment_id` " + direction + " LIMIT ?"
				pageArgs = append(pageArgs, int64(31))
				count := &socialSQLStep{columns: []string{"count"}, rows: [][]driver.Value{{int64(100)}}}
				page := &socialSQLStep{columns: []string{"comment_id"}}
				d, _ := newSocialSQL(t, socialVideoStep(), count, page)
				err = d.WithTx(context.Background(), func(ctx context.Context) error {
					rows, total, err := (&videoCommentRepo{}).GetCommentPage(ctx, q)
					if total != 100 || len(rows) != 0 {
						t.Error("计数或空页错误")
					}
					return err
				})
				if err != nil {
					t.Fatal(err)
				}
				if count.text != countSQL {
					t.Fatalf("计数 SQL 错误：\ngot  %s\nwant %s", count.text, countSQL)
				}
				if !reflect.DeepEqual(namedValues(count.args), scopeArgs) {
					t.Fatalf("计数被游标过滤：%v", namedValues(count.args))
				}
				if page.text != pageSQL {
					t.Fatalf("分页 SQL 分组或排序错误：\ngot  %s\nwant %s", page.text, pageSQL)
				}
				if !reflect.DeepEqual(namedValues(page.args), pageArgs) {
					t.Fatalf("分页参数错误：\ngot  %v\nwant %v", namedValues(page.args), pageArgs)
				}
			})
		}
	}
}

// TestBarrageRangeSQLAndLikes 验证左闭右开区间、同秒游标、排序及批量个人状态。
func TestBarrageRangeSQLAndLikes(t *testing.T) {
	page := &socialSQLStep{contains: []string{"FROM `barrages`", "publish_time` >= ?", "publish_time` < ?", "publish_time` > ?", "barrage_id` > ?", "deleted_at", "ORDER BY `barrages`.`publish_time` ASC,`barrages`.`barrage_id` ASC"}, columns: []string{"barrage_id", "video_id", "publish_time"}, rows: [][]driver.Value{{int64(22), int64(9), int64(0)}}}
	likes := &socialSQLStep{contains: []string{"FROM `user_barrage_like`", "user_id", "video_id", "barrage_id"}, columns: []string{"barrage_id"}, rows: [][]driver.Value{{int64(22)}}}
	d, _ := newSocialSQL(t, socialVideoStep(), page, likes)
	err := d.WithTx(context.Background(), func(ctx context.Context) error {
		repo := &interactRepo{}
		rows, err := repo.GetBarragePage(ctx, &biz.BarrageQuery{VideoID: 9, UserID: 7, Start: 0, End: 60, After: &biz.BarrageCursor{Position: 0, ID: 21}})
		if err != nil {
			return err
		}
		if len(rows) != 1 {
			t.Fatal("分段读取错误")
		}
		states, err := repo.GetBarrageLikes(ctx, 7, 9, []int64{22})
		if !states[22] {
			t.Fatal("点赞状态丢失")
		}
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestBarrageLikeLocalTransaction 验证关系写入、原子计数、精确取消和更新失败回滚。
func TestBarrageLikeLocalTransaction(t *testing.T) {
	for _, mode := range []string{"like", "duplicate", "unlike", "absent", "failure"} {
		t.Run(mode, func(t *testing.T) {
			liked := mode != "unlike" && mode != "absent"
			row := func(count int64) *socialSQLStep {
				return &socialSQLStep{contains: []string{"FROM `barrages`", "video_id", "barrage_id", "deleted_at"}, columns: []string{"barrage_id", "video_id", "likes"}, rows: [][]driver.Value{{int64(11), int64(9), count}}}
			}
			relation := &socialSQLStep{contains: []string{"INSERT INTO `user_barrage_like`"}, affected: 1}
			if !liked {
				relation.contains = []string{"DELETE FROM `user_barrage_like`", "user_id", "video_id", "barrage_id"}
			}
			if mode == "duplicate" {
				relation.err = &drivermysql.MySQLError{Number: 1062}
			}
			if mode == "absent" {
				relation.affected = 0
			}
			steps := []*socialSQLStep{socialVideoStep(), row(3), relation}
			if mode != "duplicate" && mode != "absent" {
				update := &socialSQLStep{contains: []string{"UPDATE `barrages`", "likes + ?", "video_id", "barrage_id", "deleted_at"}, affected: 1}
				if !liked {
					update.contains = append(update.contains, "likes > 0")
				}
				if mode == "failure" {
					update.affected = 0
				}
				steps = append(steps, update)
				if mode != "failure" {
					steps = append(steps, row(4))
				}
			}
			d, state := newSocialSQL(t, steps...)
			uc := biz.NewInteractUseCase(&interactRepo{}, nil, d, klog.NewStdLogger(io.Discard))
			resp, err := uc.SetBarrageLike(context.Background(), &interact.SetBarrageLikeReq{UserId: 7, VideoId: 9, BarrageId: 11, Liked: &liked})
			if mode == "failure" {
				if err == nil || state.rollbacks != 1 || state.commits != 0 {
					t.Fatal("失败未回滚")
				}
			} else if err != nil || resp.IsUpvoted != liked || state.commits != 1 {
				t.Fatalf("点赞结果错误：%v %v", resp, err)
			}
		})
	}
}
