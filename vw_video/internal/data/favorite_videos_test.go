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
	"vw_video/internal/data/dal/model"
	"vw_video/internal/data/dal/query"
)

// offlineFavoriteDB 为 SQL 契约测试提供无网络连接的 MySQL DryRun 上下文。
func offlineFavoriteDB(t *testing.T) (*gorm.DB, context.Context) {
	t.Helper()
	db, err := gorm.Open(mysql.New(mysql.Config{DSN: "offline:offline@tcp(127.0.0.1:1)/offline", SkipInitializeWithVersion: true}), &gorm.Config{DryRun: true, DisableAutomaticPing: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	return db, context.WithValue(context.Background(), transactionKey{}, &query.QueryTx{Query: query.Use(db)})
}

// TestFavoriteVideosQueryOffline 验证访问筛选、软删除、排序分页、摘要完整性和各阶段错误，无真实数据库访问。
func TestFavoriteVideosQueryOffline(t *testing.T) {
	for _, tc := range []struct {
		name          string
		failAt        int
		denied, empty bool
	}{
		{name: "正常分页"}, {name: "空页", empty: true}, {name: "私密或不存在", denied: true},
		{name: "权限查询故障", failAt: 1}, {name: "计数故障", failAt: 2}, {name: "列表故障", failAt: 3},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, ctx := offlineFavoriteDB(t)
			original := db.Callback().Query().Get("gorm:query")
			var sqls []string
			var args [][]interface{}
			wantErr := errors.New("查询失败")
			created, collected := time.Unix(100, 0), time.Unix(200, 0)
			if err := db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
				original(tx)
				sqls = append(sqls, tx.Statement.SQL.String())
				args = append(args, append([]interface{}{}, tx.Statement.Vars...))
				stage := len(sqls)
				if tc.failAt == stage {
					tx.AddError(wantErr)
					return
				}
				if stage == 1 {
					if tc.denied {
						tx.AddError(gorm.ErrRecordNotFound)
						return
					}
					reflect.ValueOf(tx.Statement.Dest).Elem().FieldByName("FavoritesID").SetInt(9)
				} else if stage == 2 {
					*tx.Statement.Dest.(*int64) = 3
					// COUNT 返回一行聚合结果，模拟驱动时也需提供行数，避免 GORM 用 RowsAffected 覆盖计数。
					tx.RowsAffected = 1
				} else if stage == 3 && !tc.empty {
					*tx.Statement.Dest.(*[]*favoriteVideoRow) = []*favoriteVideoRow{
						{Video: model.Video{VideoID: 42, Title: "摘要", CreatedAt: created}, CollectedAt: &collected},
						{Video: model.Video{VideoID: 43, Title: "历史空收藏时间", CreatedAt: created}},
					}
				}
			}); err != nil {
				t.Fatal(err)
			}
			rows, total, err := (&videoInfoRepo{}).GetFavoriteVideos(ctx, 7, 9, 2, 2)
			if !strings.Contains(sqls[0], "favorites_id = ? AND (user_id = ? OR is_private = ?)") || strings.Contains(sqls[0], "deleted_at") || !reflect.DeepEqual(args[0], []interface{}{int64(9), int64(7), 1, 1}) {
				t.Fatalf("访问条件错误：%s %#v", sqls[0], args[0])
			}
			if tc.denied {
				if err == nil || kerrors.FromError(err).Code != 404 || len(sqls) != 1 || rows != nil {
					t.Fatal("无权查询泄露内容或计数")
				}
				return
			}
			if tc.failAt != 0 {
				if !errors.Is(err, wantErr) || len(sqls) != tc.failAt || rows != nil {
					t.Fatalf("查询错误被吞掉：%v", err)
				}
				return
			}
			if err != nil || total != 3 || len(sqls) != 3 {
				t.Fatalf("分页查询失败：total=%d, queries=%d, err=%v", total, len(sqls), err)
			}
			for _, sqlText := range sqls[1:] {
				for _, fragment := range []string{"JOIN video AS v", "JOIN favorites AS f", "fv.favorites_id = ?", "fv.deleted_at IS NULL", "v.deleted_at IS NULL", "f.user_id = ? OR f.is_private = ?"} {
					if !strings.Contains(sqlText, fragment) {
						t.Fatalf("查询缺少筛选 %s：%s", fragment, sqlText)
					}
				}
			}
			if strings.Contains(sqls[1], "ORDER BY") || !strings.Contains(sqls[2], "fv.created_at DESC, fv.id DESC") || !strings.Contains(sqls[2], "LIMIT ? OFFSET ?") {
				t.Fatalf("分页排序错误：%v", sqls)
			}
			if !reflect.DeepEqual(args[1], []interface{}{int64(9), int64(7), 1}) || !reflect.DeepEqual(args[2], []interface{}{int64(9), int64(7), 1, 2, 2}) {
				t.Fatalf("内容筛选或分页参数错误：%#v", args)
			}
			if tc.empty {
				if rows == nil || len(rows) != 0 {
					t.Fatal("空页没有返回空数组")
				}
				return
			}
			if len(rows) != 2 || rows[0].Video.VideoID != 42 || rows[0].Video.Title != "摘要" || !rows[0].Video.CreatedAt.Equal(created) || !rows[0].CollectedAt.Equal(collected) || rows[1].CollectedAt != nil {
				t.Fatal("摘要或两种时间映射错误")
			}
		})
	}
}

// TestFavoriteRelationQueryOffline 验证旧表结构下按所有者和视频查归属，空关系、冲突和数据库错误明确返回。
func TestFavoriteRelationQueryOffline(t *testing.T) {
	for _, tc := range []struct {
		name string
		ids  []int64
		fail bool
	}{
		{name: "未收藏"}, {name: "已收藏", ids: []int64{91}}, {name: "历史多归属", ids: []int64{91, 92}}, {name: "数据库故障", fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, ctx := offlineFavoriteDB(t)
			original := db.Callback().Query().Get("gorm:query")
			wantErr := errors.New("归属查询失败")
			if err := db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
				original(tx)
				sqlText := tx.Statement.SQL.String()
				for _, fragment := range []string{"JOIN favorites AS f", "f.user_id = ? AND fv.video_id = ? AND fv.deleted_at IS NULL", "LIMIT ?"} {
					if !strings.Contains(sqlText, fragment) {
						t.Fatalf("归属筛选错误：%s", sqlText)
					}
				}
				if strings.Contains(sqlText, "fv.user_id") || !reflect.DeepEqual(tx.Statement.Vars, []interface{}{int64(7), int64(42), 2}) {
					t.Fatal("使用了新表字段或错误身份")
				}
				if tc.fail {
					tx.AddError(wantErr)
					return
				}
				*tx.Statement.Dest.(*[]int64) = tc.ids
			}); err != nil {
				t.Fatal(err)
			}
			id, err := (&videoInfoRepo{}).GetUserFavoriteID(ctx, 7, 42)
			if tc.fail {
				if !errors.Is(err, wantErr) || id != 0 {
					t.Fatal("故障被吞掉")
				}
				return
			}
			if len(tc.ids) > 1 {
				if err == nil || kerrors.FromError(err).Code != 409 || id != 0 {
					t.Fatal("冲突被静默选择")
				}
				return
			}
			want := int64(0)
			if len(tc.ids) == 1 {
				want = tc.ids[0]
			}
			if err != nil || id != want {
				t.Fatalf("归属结果错误：%d %v", id, err)
			}
		})
	}
}
