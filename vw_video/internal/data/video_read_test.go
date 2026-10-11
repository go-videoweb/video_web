package data

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	utilCtx "util/context"
	"vw_video/internal/data/dal/model"
	"vw_video/internal/data/dal/query"
)

// TestVideoListQueryOffline 使用 DryRun 验证完整模型查询、分类筛选、排序分页及错误传播，不连接数据库。
func TestVideoListQueryOffline(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := "success"
		if fail {
			name = "query error"
		}
		t.Run(name, func(t *testing.T) {
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
			var sqlText string
			var vars []interface{}
			calls := 0
			wantErr := errors.New("offline query failed")
			row := &model.Video{VideoID: 42, Description: "description", VideoPath: "/video.mp4", CreatedAt: time.Date(2024, 6, 9, 10, 11, 12, 0, time.UTC)}
			err = db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
				originalQuery(tx)
				calls++
				sqlText, vars = tx.Statement.SQL.String(), tx.Statement.Vars
				if fail {
					tx.AddError(wantErr)
					return
				}
				dest, ok := tx.Statement.Dest.(*[]*model.Video)
				if !ok {
					t.Fatalf("unexpected query destination: %T", tx.Statement.Dest)
				}
				*dest = []*model.Video{row}
			})
			if err != nil {
				t.Fatal(err)
			}
			ctx := utilCtx.WithValue(context.Background(), transactionKey{}, &query.QueryTx{Query: query.Use(db)})
			rows, err := (&videoInfoRepo{}).GetVideoListByClass(ctx, []string{"music", "animation"}, 2, 3)
			if calls != 1 || !strings.Contains(sqlText, "SELECT * FROM") || !strings.Contains(sqlText, "`class` IN (?,?)") || !strings.Contains(sqlText, "`hot` DESC") || !strings.Contains(sqlText, "LIMIT ? OFFSET ?") {
				t.Fatalf("classification, ordering or pagination changed: %s", sqlText)
			}
			if !reflect.DeepEqual(vars, []interface{}{"music", "animation", 3, 3}) {
				t.Fatalf("unexpected query arguments: %#v", vars)
			}
			if fail {
				if !errors.Is(err, wantErr) {
					t.Fatalf("query error was swallowed: %v", err)
				}
			} else if err != nil || len(rows) != 1 || rows[0] != row {
				t.Fatal("query must return complete video models")
			}
		})
	}
}
