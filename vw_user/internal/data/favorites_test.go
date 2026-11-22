package data

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"vw_user/internal/data/dal/model"
	"vw_user/internal/data/dal/query"
)

// TestFavoritesListQueryOffline 使用 DryRun 核对用户隔离、完整字段、稳定排序和错误传播，不连接数据库。
// 按文件运行并排除 test_helper.go，避免该历史文件的初始化连接真实数据库。
func TestFavoritesListQueryOffline(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(map[bool]string{false: "成功", true: "查询失败"}[fail], func(t *testing.T) {
			db, err := gorm.Open(mysql.New(mysql.Config{
				DSN: "offline:offline@tcp(127.0.0.1:1)/offline", SkipInitializeWithVersion: true,
			}), &gorm.Config{DryRun: true, DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
			if err != nil {
				t.Fatal(err)
			}
			sqlDB, err := db.DB()
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = sqlDB.Close() })
			originalQuery := db.Callback().Query().Get("gorm:query")
			wantErr := errors.New("查询失败")
			row := &model.Favorite{FavoritesID: 9007199254740993, UserID: 7, FavoritesName: "私密收藏夹", IsPrivate: -1}
			var sqlText string
			var vars []interface{}
			calls := 0
			if err := db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
				originalQuery(tx)
				calls++
				sqlText, vars = tx.Statement.SQL.String(), tx.Statement.Vars
				if tx.Statement.Context.Value("request") != "favorites" {
					t.Fatal("查询丢失请求上下文")
				}
				if fail {
					tx.AddError(wantErr)
					return
				}
				dest, ok := tx.Statement.Dest.(*[]*model.Favorite)
				if !ok {
					t.Fatalf("查询结果类型错误：%T", tx.Statement.Dest)
				}
				*dest = []*model.Favorite{row}
			}); err != nil {
				t.Fatal(err)
			}
			ctx := context.WithValue(context.Background(), transactionKey{}, &query.QueryTx{Query: query.Use(db)})
			ctx = context.WithValue(ctx, "request", "favorites")
			rows, err := (&favoritesRepo{}).GetFavoritesList(ctx, 7)
			if calls != 1 || !strings.Contains(sqlText, "SELECT * FROM `favorites`") || !strings.Contains(sqlText, "`favorites`.`user_id` = ?") || !strings.Contains(sqlText, "ORDER BY `favorites`.`favorites_id` ASC") || !reflect.DeepEqual(vars, []interface{}{int64(7)}) {
				t.Fatalf("用户隔离或排序错误：%s %#v", sqlText, vars)
			}
			if fail {
				if !errors.Is(err, wantErr) {
					t.Fatalf("数据库错误被吞掉：%v", err)
				}
			} else if err != nil || len(rows) != 1 || rows[0] != row {
				t.Fatal("查询未返回完整收藏夹记录")
			}
		})
	}
}
