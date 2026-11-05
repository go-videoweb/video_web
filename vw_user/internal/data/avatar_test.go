package data

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"vw_user/internal/data/dal/model"
	"vw_user/internal/data/dal/query"
)

// TestAvatarRepoSQL 验证事务上下文、完整读取条件、旧路径比较更新及零行与 SQL 错误传播。
// 本测试必须按文件运行，排除会连接真实数据库的 test_helper.go。
func TestAvatarRepoSQL(t *testing.T) {
	for _, scenario := range []string{"success", "missing", "query error", "zero rows", "update error"} {
		t.Run(scenario, func(t *testing.T) {
			now := time.Date(2026, 11, 3, 12, 0, 0, 0, time.UTC)
			db, err := gorm.Open(mysql.New(mysql.Config{DSN: "offline:offline@tcp(127.0.0.1:1)/offline", SkipInitializeWithVersion: true}),
				&gorm.Config{DryRun: true, DisableAutomaticPing: true, SkipDefaultTransaction: true, Logger: logger.Default.LogMode(logger.Silent), NowFunc: func() time.Time { return now }})
			if err != nil {
				t.Fatal(err)
			}
			sqlDB, _ := db.DB()
			t.Cleanup(func() { _ = sqlDB.Close() })
			failure := errors.New("offline SQL failure")
			var readSQL, updateSQL string
			var readArgs, updateArgs []any
			originalRead := db.Callback().Query().Get("gorm:query")
			if err := db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
				originalRead(tx)
				readSQL, readArgs = tx.Statement.SQL.String(), tx.Statement.Vars
				if tx.Statement.Context.Value("avatar-test") != true {
					t.Fatal("读取丢失上下文")
				}
				switch scenario {
				case "missing":
					tx.AddError(gorm.ErrRecordNotFound)
				case "query error":
					tx.AddError(failure)
				default:
					tx.Statement.Dest.(*model.User).AvatarPath = "old.png"
				}
			}); err != nil {
				t.Fatal(err)
			}
			originalUpdate := db.Callback().Update().Get("gorm:update")
			if err := db.Callback().Update().Replace("gorm:update", func(tx *gorm.DB) {
				originalUpdate(tx)
				updateSQL, updateArgs = tx.Statement.SQL.String(), tx.Statement.Vars
				if tx.Statement.Context.Value("avatar-test") != true {
					t.Fatal("更新丢失上下文")
				}
				if scenario == "update error" {
					tx.AddError(failure)
					return
				}
				if scenario != "zero rows" {
					tx.RowsAffected = 1
				}
			}); err != nil {
				t.Fatal(err)
			}
			ctx := context.WithValue(context.Background(), transactionKey{}, &query.QueryTx{Query: query.Use(db)})
			ctx = context.WithValue(ctx, "avatar-test", true)
			repo := &fileRepo{}
			path, err := repo.GetAvatarPath(ctx, 7)
			expectedRead := "SELECT `user`.`avatar_path` FROM `user` WHERE `user`.`user_id` = ? AND `user`.`deleted_at` IS NULL ORDER BY `user`.`user_id` LIMIT ?"
			if readSQL != expectedRead || !reflect.DeepEqual(readArgs, []any{int64(7), 1}) {
				t.Fatalf("读取条件错误: %s %#v", readSQL, readArgs)
			}
			if scenario == "missing" {
				if kerrors.Code(err) != 404 {
					t.Fatal("用户缺失未映射为 404")
				}
				return
			}
			if scenario == "query error" {
				if !errors.Is(err, failure) {
					t.Fatal("读取错误被吞掉")
				}
				return
			}
			if err != nil || path != "old.png" {
				t.Fatalf("读取结果错误: %s %v", path, err)
			}
			err = repo.UpdateAvatarPath(ctx, 7, "old.png", "new.png")
			if !strings.Contains(updateSQL, "WHERE `user`.`user_id` = ? AND `user`.`avatar_path` = ? AND `user`.`deleted_at` IS NULL") {
				t.Fatalf("更新缺少旧路径或用户条件: %s", updateSQL)
			}
			if !strings.Contains(updateSQL, "`version`=`user`.`version`+?") {
				t.Fatalf("版本未递增: %s", updateSQL)
			}
			if len(updateArgs) < 4 || updateArgs[len(updateArgs)-2] != int64(7) || updateArgs[len(updateArgs)-1] != "old.png" {
				t.Fatalf("更新参数错误: %#v", updateArgs)
			}
			switch scenario {
			case "zero rows":
				if kerrors.Code(err) != 409 {
					t.Fatal("零行被报告成功")
				}
			case "update error":
				if !errors.Is(err, failure) {
					t.Fatal("更新错误被吞掉")
				}
			default:
				if err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
