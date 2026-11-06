package data

import (
	"context"
	stderr "errors"
	"reflect"
	"strings"
	"testing"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	drivermysql "github.com/go-sql-driver/mysql"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"vw_user/internal/data/dal/model"
	"vw_user/internal/data/dal/query"
)

// TestAccountSQL 验证邮箱查询与密码更新都使用上下文事务，拒绝多归属、零行更新并传播 SQL 故障。
// 必须按文件运行并排除 test_helper.go，不连接真实 MySQL。
func TestAccountSQL(t *testing.T) {
	for _, scenario := range []string{"success", "missing", "multiple", "read error", "zero rows", "write error"} {
		t.Run(scenario, func(t *testing.T) {
			db, err := gorm.Open(mysql.New(mysql.Config{DSN: "offline:offline@tcp(127.0.0.1:1)/offline", SkipInitializeWithVersion: true}),
				&gorm.Config{DryRun: true, DisableAutomaticPing: true, SkipDefaultTransaction: true, Logger: logger.Default.LogMode(logger.Silent)})
			if err != nil {
				t.Fatal(err)
			}
			sqlDB, _ := db.DB()
			t.Cleanup(func() { _ = sqlDB.Close() })
			failure := stderr.New("offline database failure")
			var readSQL, updateSQL string
			var updateArgs []any
			originalRead := db.Callback().Query().Get("gorm:query")
			if err := db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
				originalRead(tx)
				readSQL = tx.Statement.SQL.String()
				if scenario == "read error" {
					tx.AddError(failure)
					return
				}
				if rows, ok := tx.Statement.Dest.(*[]*model.User); ok {
					if scenario != "missing" {
						*rows = []*model.User{{UserID: 7, Email: "one@example.com"}}
					}
					if scenario == "multiple" {
						*rows = append(*rows, &model.User{UserID: 8, Email: "one@example.com"})
					}
				}
				if count, ok := tx.Statement.Dest.(*int64); ok {
					*count = 1
					if scenario == "missing" {
						*count = 0
					}
					// COUNT 聚合始终返回一行，不能让 GORM 用零行结果覆盖注入的计数。
					tx.RowsAffected = 1
				}
			}); err != nil {
				t.Fatal(err)
			}
			originalUpdate := db.Callback().Update().Get("gorm:update")
			if err := db.Callback().Update().Replace("gorm:update", func(tx *gorm.DB) {
				originalUpdate(tx)
				updateSQL, updateArgs = tx.Statement.SQL.String(), tx.Statement.Vars
				if scenario == "write error" {
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
			repo := &userInfoRepo{}
			user, err := repo.GetUserByEmail(ctx, "one@example.com")
			if !strings.Contains(readSQL, "`user`.`email` = ? AND `user`.`deleted_at` IS NULL LIMIT ?") {
				t.Fatalf("邮箱范围或软删除条件缺失: %s", readSQL)
			}
			switch scenario {
			case "read error":
				if !stderr.Is(err, failure) {
					t.Fatal("读取错误未透传")
				}
			case "missing", "multiple":
				if err == nil || user != nil {
					t.Fatal("非唯一邮箱归属仍返回用户")
				}
			default:
				if err != nil || user.UserID != 7 {
					t.Fatalf("邮箱定位失败: %v", err)
				}
			}
			err = repo.ResetPasswordByEmail(ctx, 7, "one@example.com", "new-hash")
			if !strings.Contains(updateSQL, "WHERE `user`.`user_id` = ? AND `user`.`email` = ? AND `user`.`deleted_at` IS NULL") || !strings.Contains(updateSQL, "`version`=`user`.`version`+?") {
				t.Fatalf("更新范围或版本缺失: %s", updateSQL)
			}
			if len(updateArgs) < 2 || !reflect.DeepEqual(updateArgs[len(updateArgs)-2:], []any{int64(7), "one@example.com"}) {
				t.Fatalf("更新目标参数错误: %#v", updateArgs)
			}
			if scenario == "write error" {
				if !stderr.Is(err, failure) {
					t.Fatal("写入错误未透传")
				}
			} else if scenario == "zero rows" {
				if kerrors.Code(err) != 409 {
					t.Fatal("零行更新未拒绝")
				}
			} else if err != nil {
				t.Fatal(err)
			}
			err = repo.CheckEmailConflict(ctx, "one@example.com", 7)
			if strings.Contains(readSQL, "deleted_at") || !strings.Contains(readSQL, "`user`.`user_id` <> ?") {
				t.Fatalf("唯一性未包含软删除或未排除本人: %s", readSQL)
			}
			if scenario == "missing" && err != nil {
				t.Fatal("未占用邮箱被拒绝")
			}
			if scenario != "read error" && scenario != "missing" && kerrors.Code(err) != 409 {
				t.Fatal("邮箱冲突未拒绝")
			}
		})
	}
}

// TestAccountDuplicateWrite 验证唯一键并发冲突转为业务错误，其他数据库错误仍原样保留。
func TestAccountDuplicateWrite(t *testing.T) {
	err := accountWriteError(&drivermysql.MySQLError{Number: 1062, Message: "private index details"})
	if kerrors.Code(err) != 409 || strings.Contains(err.Error(), "private index") {
		t.Fatal("唯一键冲突暴露原始信息")
	}
	failure := stderr.New("connection lost")
	if !stderr.Is(accountWriteError(failure), failure) {
		t.Fatal("非唯一键错误被替换")
	}
}
