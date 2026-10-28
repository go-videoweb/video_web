package data

import (
	"context"
	"errors"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"reflect"
	"strings"
	"testing"
	"time"
	"vw_user/internal/data/dal/model"
	"vw_user/internal/data/dal/query"
)

// offlineFollowDB 构造无网络的 MySQL DryRun，上下文显式绑定隔离查询器。
func offlineFollowDB(t *testing.T) (*gorm.DB, context.Context) {
	t.Helper()
	db, err := gorm.Open(mysql.New(mysql.Config{DSN: "offline:offline@tcp(127.0.0.1:1)/offline", SkipInitializeWithVersion: true}), &gorm.Config{DryRun: true, DisableAutomaticPing: true, SkipDefaultTransaction: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pool.Close() })
	return db, context.WithValue(context.Background(), transactionKey{}, &query.QueryTx{Query: query.Use(db)})
}

// TestFollowWriteSQLOffline 验证双向关系、精确删除、默认标记和非负计数 SQL 均保留同一请求事务。
func TestFollowWriteSQLOffline(t *testing.T) {
	for _, mode := range []string{"双向新增", "双向删除", "默认切换", "列表删除", "移动", "关注数", "零行"} {
		t.Run(mode, func(t *testing.T) {
			db, ctx := offlineFollowDB(t)
			var texts []string
			var args [][]interface{}
			for _, event := range []string{"create", "update", "delete"} {
				capture := func(tx *gorm.DB) {
					texts = append(texts, tx.Statement.SQL.String())
					args = append(args, append([]interface{}{}, tx.Statement.Vars...))
					tx.RowsAffected = 1
					if mode == "零行" {
						tx.RowsAffected = 0
					}
				}
				switch event {
				case "create":
					db.Callback().Create().After("gorm:create").Register("follow:capture", capture)
				case "update":
					db.Callback().Update().After("gorm:update").Register("follow:capture", capture)
				case "delete":
					db.Callback().Delete().After("gorm:delete").Register("follow:capture", capture)
				}
			}
			r := &followRepo{}
			var err error
			switch mode {
			case "双向新增":
				err = r.CreateFollowPair(ctx, &model.UserFollow{UserID: 7, FollowUserID: 9, FollowlistID: 13})
			case "双向删除":
				err = r.DeleteFollowPairs(ctx, 7, []int64{9})
			case "默认切换":
				err = r.SetDefaultFollowList(ctx, 7, 14)
			case "列表删除":
				err = r.DeleteFollowLists(ctx, 7, []int64{14})
			case "移动":
				err = r.MoveFollowee(ctx, 7, 9, 14)
			case "关注数", "零行":
				err = r.ChangeFollowCount(ctx, 7, -1)
			}
			if mode == "零行" {
				if err == nil {
					t.Fatal("零行写入被接受")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			joined := strings.Join(texts, " ")
			if strings.Contains(joined, "cnt_fans") || strings.Contains(joined, " OR ") {
				t.Fatal("写入旧粉丝字段或错误范围", joined)
			}
			switch mode {
			case "双向新增":
				if len(texts) != 2 || !strings.Contains(texts[0], "INSERT INTO `user_follows`") || !strings.Contains(texts[1], "INSERT INTO `user_fans`") || !strings.Contains(texts[1], "`follower_id`") {
					t.Fatal(texts)
				}
				if !strings.Contains(texts[0], "ON DUPLICATE KEY UPDATE") || !strings.Contains(texts[1], "`deleted_at`=VALUES(`deleted_at`)") {
					t.Fatal("重关注无法恢复软删除记录", texts)
				}
			case "双向删除":
				if len(texts) != 2 || !strings.HasPrefix(texts[0], "DELETE FROM `user_follows`") || !strings.Contains(texts[0], "user_id = ? AND follow_user_id IN (?) AND deleted_at IS NULL") || !strings.Contains(texts[1], "follower_id = ? AND user_id IN (?)") {
					t.Fatal(texts)
				}
				if !reflect.DeepEqual(args[0], []interface{}{int64(7), int64(9)}) || !reflect.DeepEqual(args[1], args[0]) {
					t.Fatal("双向删除目标错误", args)
				}
			case "默认切换":
				if len(texts) != 2 || !strings.Contains(texts[0], "user_id = ? AND is_default = ?") || !strings.Contains(texts[1], "user_id = ? AND list_id = ?") {
					t.Fatal(texts)
				}
				if args[0][0] != false || args[1][0] != true {
					t.Fatal("默认标记写反", args)
				}
			case "列表删除":
				if !strings.Contains(joined, "user_id = ? AND list_id IN (?) AND is_default = ?") {
					t.Fatal(texts)
				}
			case "移动":
				if !strings.Contains(joined, "`followlist_id`=?") || strings.Contains(joined, "cnt_follows") || strings.Contains(joined, "user_fans") {
					t.Fatal(texts)
				}
			case "关注数":
				if !strings.Contains(joined, "cnt_follows + ?") || !strings.Contains(joined, "cnt_follows >= ?") {
					t.Fatal(texts)
				}
			}
		})
	}
}

// TestFollowPageSQLOffline 验证计数与分页过滤一致、稳定排序、摘要重组、空页及各阶段错误透传。
func TestFollowPageSQLOffline(t *testing.T) {
	for _, mode := range []string{"全部", "分组", "空页", "计数失败", "关系失败", "摘要失败"} {
		t.Run(mode, func(t *testing.T) {
			db, ctx := offlineFollowDB(t)
			fault := errors.New("读取失败")
			var texts []string
			original := db.Callback().Query().Get("gorm:query")
			db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
				original(tx)
				// GORM 在编译有效用户子查询时也调用 Query 回调，但不会访问数据库。
				if _, subquery := tx.Statement.Dest.(*model.User); subquery {
					return
				}
				if tx.Statement.Dest == nil {
					return
				}
				texts = append(texts, tx.Statement.SQL.String())
				stage := len(texts)
				if (mode == "计数失败" && stage == 1) || (mode == "关系失败" && stage == 2) || (mode == "摘要失败" && stage == 3) {
					tx.AddError(fault)
					return
				}
				switch dest := tx.Statement.Dest.(type) {
				case *int64:
					*dest = 2
					tx.RowsAffected = 1
				case *[]*model.UserFollow:
					if mode != "空页" {
						*dest = []*model.UserFollow{{UserID: 7, FollowUserID: 10, FollowlistID: 14}, {UserID: 7, FollowUserID: 9, FollowlistID: 13}}
					}
				case *[]*model.User:
					*dest = []*model.User{{UserID: 9, Username: "second", Birthday: time.Now()}, {UserID: 10, Username: "first", Birthday: time.Now()}}
				default:
					t.Fatalf("未知结果类型：%T", dest)
				}
			})
			list := int64(0)
			if mode == "分组" {
				list = 14
			}
			page, err := (&followRepo{}).GetFolloweesPage(ctx, 7, list, 2, 20)
			if strings.HasSuffix(mode, "失败") {
				if !errors.Is(err, fault) || page != nil {
					t.Fatal("错误被忽略", err)
				}
				return
			}
			if err != nil || page.Total != 2 {
				t.Fatal("分页错误", page, err)
			}
			for _, text := range texts {
				if strings.Contains(text, "JOIN") || strings.Contains(text, "cnt_fans") {
					t.Fatal(text)
				}
			}
			for _, text := range texts[:2] {
				if !strings.Contains(text, "follow_user_id IN (SELECT") || !strings.Contains(text, "`user`.`deleted_at` IS NULL") || !strings.Contains(text, "`user_follows`.`deleted_at` IS NULL") {
					t.Fatal("过滤未在分页前执行", text)
				}
				if strings.Contains(text, "followlist_id = ?") != (list > 0) {
					t.Fatal("全部关注被当作默认分组", text)
				}
			}
			if !strings.Contains(texts[1], "ORDER BY created_at DESC,follow_user_id DESC") || !strings.Contains(texts[1], "LIMIT ? OFFSET ?") {
				t.Fatal("分页不稳定", texts[1])
			}
			if mode == "空页" {
				if len(texts) != 2 || len(page.Users) != 0 {
					t.Fatal("空页仍读取摘要")
				}
			} else if len(page.Users) != 2 || page.Users[0].Id != 10 || page.Users[0].FollowListID != 14 || page.Users[1].Id != 9 {
				t.Fatal("顺序或身份丢失", page.Users)
			}
		})
	}
}

// TestFansCountOffline 验证按有效粉丝表计数，用户信息不再查询 user.cnt_fans，统计故障正常返回。
func TestFansCountOffline(t *testing.T) {
	for _, mode := range []string{"按ID", "按名称", "计数失败"} {
		t.Run(mode, func(t *testing.T) {
			db, ctx := offlineFollowDB(t)
			var texts []string
			fault := errors.New("计数失败")
			original := db.Callback().Query().Get("gorm:query")
			db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
				original(tx)
				texts = append(texts, tx.Statement.SQL.String())
				switch dest := tx.Statement.Dest.(type) {
				case *model.User:
					*dest = model.User{UserID: 9, Username: "target"}
				case *int64:
					if mode == "计数失败" {
						tx.AddError(fault)
					} else {
						*dest = 42
						tx.RowsAffected = 1
					}
				default:
					t.Fatalf("未知结果：%T", dest)
				}
			})
			r := &userInfoRepo{}
			info, err := r.GetUserInfoByUserId(ctx, 9)
			if mode == "按名称" {
				texts = nil
				info, err = r.GetUserInfoByUsername(ctx, "target")
			}
			if mode == "计数失败" {
				if !errors.Is(err, fault) || info != nil {
					t.Fatal("粉丝计数故障未透传", err)
				}
				return
			}
			if err != nil || info.CntFans != 42 || len(texts) != 2 {
				t.Fatal("用户粉丝数错误", info, err, texts)
			}
			if strings.Contains(strings.Join(texts, " "), "cnt_fans") || !strings.Contains(texts[1], "`user_fans`") || !strings.Contains(texts[1], "`deleted_at` IS NULL") {
				t.Fatal("未统计有效粉丝表", texts)
			}
		})
	}
}

// TestFollowReadScopeOffline 验证用户行串行入口、本人列表和有效关系读取均保留目标筛选。
func TestFollowReadScopeOffline(t *testing.T) {
	db, ctx := offlineFollowDB(t)
	var texts []string
	var args [][]interface{}
	original := db.Callback().Query().Get("gorm:query")
	db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
		original(tx)
		texts = append(texts, tx.Statement.SQL.String())
		args = append(args, append([]interface{}{}, tx.Statement.Vars...))
	})
	repo := &followRepo{}
	if err := repo.LockFollowOwner(ctx, 7); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(texts[0], "FOR UPDATE") || !strings.Contains(texts[0], "user_id = ?") || args[0][0] != int64(7) {
		t.Fatal("用户写入缺少串行入口", texts, args)
	}
	if _, err := repo.GetFollowLists(ctx, 7); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(texts[1], "user_id = ?") || !strings.Contains(texts[1], "ORDER BY is_default DESC,list_id ASC") {
		t.Fatal("本人列表范围或排序错误", texts)
	}
	if _, err := repo.GetFollowRelation(ctx, 7, 9); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(texts[2], "user_id = ? AND follow_user_id = ?") || !strings.Contains(texts[2], "`deleted_at` IS NULL") {
		t.Fatal("关注状态读取范围错误", texts)
	}
}
