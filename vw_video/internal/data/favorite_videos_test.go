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

// TestFavoriteVideosQueryOffline 验证分步权限、删除过滤先于分页、摘要顺序与权限复核，不连接数据库。
func TestFavoriteVideosQueryOffline(t *testing.T) {
	for _, tc := range []struct {
		name                                                string
		owner, privacy                                      int64
		failAt                                              int
		denied, missing, empty, revoked, removed, lostVideo bool
	}{
		{name: "本人私密", owner: 7, privacy: -1},
		{name: "本人公开", owner: 7, privacy: 1},
		{name: "他人公开", owner: 8, privacy: 1},
		{name: "他人私密", owner: 8, privacy: -1, denied: true},
		{name: "异常隐私标记", owner: 8, denied: true},
		{name: "不存在", missing: true},
		{name: "空页", owner: 7, empty: true},
		{name: "查询期间转私密", owner: 8, privacy: 1, revoked: true},
		{name: "查询期间删除收藏夹", owner: 7, removed: true},
		{name: "摘要读取前删除视频", owner: 7, lostVideo: true},
		{name: "权限查询故障", owner: 7, failAt: 1},
		{name: "计数故障", owner: 7, failAt: 2},
		{name: "关系分页故障", owner: 7, failAt: 3},
		{name: "摘要查询故障", owner: 7, failAt: 4},
		{name: "权限复核故障", owner: 7, failAt: 5},
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
				// 子查询只参与 SQL 构建，没有结果接收对象，不计作一次数据库往返。
				if tx.Statement.Dest == nil {
					return
				}
				sqls = append(sqls, tx.Statement.SQL.String())
				args = append(args, append([]interface{}{}, tx.Statement.Vars...))
				stage := len(sqls)
				if tc.failAt == stage {
					tx.AddError(wantErr)
					return
				}
				switch {
				case stage == 1 || stage == 5 || (tc.empty && stage == 4):
					if tc.missing || (stage > 1 && tc.removed) {
						tx.AddError(gorm.ErrRecordNotFound)
						return
					}
					favorite := reflect.ValueOf(tx.Statement.Dest).Elem()
					favorite.FieldByName("UserID").SetInt(tc.owner)
					privacy := tc.privacy
					if stage > 1 && tc.revoked {
						privacy = -1
					}
					favorite.FieldByName("IsPrivate").SetInt(privacy)
				case stage == 2:
					*tx.Statement.Dest.(*int64) = 3
					// COUNT 返回一行聚合结果，避免 GORM 用 RowsAffected 覆盖计数。
					tx.RowsAffected = 1
				case stage == 3 && !tc.empty:
					*tx.Statement.Dest.(*[]*favoriteVideoRow) = []*favoriteVideoRow{
						{VideoID: 42, CollectedAt: &collected}, {VideoID: 43},
					}
				case stage == 4:
					videos := []*model.Video{{VideoID: 42, Title: "摘要", CreatedAt: created}}
					if !tc.lostVideo {
						videos = append([]*model.Video{{VideoID: 43, Title: "历史空收藏时间", CreatedAt: created}}, videos...)
					}
					*tx.Statement.Dest.(*[]*model.Video) = videos
				}
			}); err != nil {
				t.Fatal(err)
			}
			rows, total, err := (&videoInfoRepo{}).GetFavoriteVideos(ctx, 7, 9, 2, 2)
			wantAccessSQL := "SELECT user_id, is_private FROM `favorites` WHERE favorites_id = ? LIMIT ?"
			if sqls[0] != wantAccessSQL || !reflect.DeepEqual(args[0], []interface{}{int64(9), 1}) {
				t.Fatalf("权限查询未限定目标收藏夹：%s %#v", sqls[0], args[0])
			}
			for _, sqlText := range sqls {
				if strings.Contains(sqlText, "JOIN") || strings.Contains(sqlText, " OR ") {
					t.Fatalf("分步查询意外包含 JOIN 或权限 OR：%s", sqlText)
				}
			}
			if tc.denied || tc.missing || tc.revoked || tc.removed {
				wantQueries := 1
				if tc.revoked || tc.removed {
					wantQueries = 5
				}
				if err == nil || kerrors.FromError(err).Code != 404 || len(sqls) != wantQueries || rows != nil || total != 0 {
					t.Fatalf("无权响应泄露内容或计数：%d %v %v", total, rows, err)
				}
				return
			}
			if tc.failAt != 0 {
				if !errors.Is(err, wantErr) || len(sqls) != tc.failAt || rows != nil || total != 0 {
					t.Fatalf("查询错误被吞掉：%v", err)
				}
				return
			}
			wantQueries := 5
			if tc.empty {
				wantQueries = 4
			}
			if err != nil || total != 3 || len(sqls) != wantQueries {
				t.Fatalf("分页查询失败：total=%d, queries=%d, err=%v", total, len(sqls), err)
			}
			filter := " FROM `favorite_video` WHERE (favorites_id = ? AND deleted_at IS NULL) AND video_id IN (SELECT video_id FROM `video` WHERE deleted_at IS NULL)"
			if sqls[1] != "SELECT count(*)"+filter || sqls[2] != "SELECT video_id, created_at AS collected_at"+filter+" ORDER BY created_at DESC, id DESC LIMIT ? OFFSET ?" {
				t.Fatalf("完整 SQL 的筛选分组、删除过滤或分页错误：%v", sqls)
			}
			if !reflect.DeepEqual(args[1], []interface{}{int64(9)}) || !reflect.DeepEqual(args[2], []interface{}{int64(9), 2, 2}) {
				t.Fatalf("内容筛选或分页参数错误：%#v", args)
			}
			if sqls[len(sqls)-1] != wantAccessSQL || !reflect.DeepEqual(args[len(args)-1], args[0]) {
				t.Fatal("返回前未重新校验同一收藏夹权限")
			}
			if tc.empty {
				if rows == nil || len(rows) != 0 {
					t.Fatal("空页没有返回空数组")
				}
				return
			}
			if sqls[3] != "SELECT * FROM `video` WHERE video_id IN (?,?) AND `video`.`deleted_at` IS NULL" || !reflect.DeepEqual(args[3], []interface{}{int64(42), int64(43)}) {
				t.Fatalf("摘要未按本页 ID 批量读取：%s %#v", sqls[3], args[3])
			}
			wantRows := 2
			if tc.lostVideo {
				wantRows = 1
			}
			if len(rows) != wantRows || rows[0].Video.VideoID != 42 || rows[0].Video.Title != "摘要" || !rows[0].Video.CreatedAt.Equal(created) || !rows[0].CollectedAt.Equal(collected) {
				t.Fatal("摘要、收藏顺序或两种时间映射错误")
			}
			if !tc.lostVideo && (rows[1].Video.VideoID != 43 || rows[1].CollectedAt != nil) {
				t.Fatal("摘要数据库返回乱序或历史空时间未正确处理")
			}
		})
	}
}

// TestFavoriteRelationQueryOffline 验证先查本人收藏夹再限定视频及有效关系，冲突和错误明确返回。
func TestFavoriteRelationQueryOffline(t *testing.T) {
	for _, tc := range []struct {
		name           string
		favorites, ids []int64
		failAt         int
	}{
		{name: "没有收藏夹"},
		{name: "未收藏", favorites: []int64{91, 92}},
		{name: "已收藏", favorites: []int64{91, 92}, ids: []int64{91}},
		{name: "历史多归属", favorites: []int64{91, 92}, ids: []int64{91, 92}},
		{name: "收藏夹查询故障", failAt: 1},
		{name: "关系查询故障", favorites: []int64{91, 92}, failAt: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, ctx := offlineFavoriteDB(t)
			original := db.Callback().Query().Get("gorm:query")
			wantErr := errors.New("归属查询失败")
			calls := 0
			if err := db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
				original(tx)
				calls++
				wantSQL := "SELECT `favorites_id` FROM `favorites` WHERE user_id = ?"
				wantArgs := []interface{}{int64(7)}
				if calls == 2 {
					wantSQL = "SELECT `favorites_id` FROM `favorite_video` WHERE favorites_id IN (?,?) AND video_id = ? AND deleted_at IS NULL ORDER BY id DESC LIMIT ?"
					wantArgs = []interface{}{int64(91), int64(92), int64(42), 2}
				}
				if tx.Statement.SQL.String() != wantSQL || !reflect.DeepEqual(tx.Statement.Vars, wantArgs) {
					t.Fatalf("归属查询范围错误：%s %#v", tx.Statement.SQL.String(), tx.Statement.Vars)
				}
				if calls == tc.failAt {
					tx.AddError(wantErr)
					return
				}
				ids := tc.favorites
				if calls == 2 {
					ids = tc.ids
				}
				*tx.Statement.Dest.(*[]int64) = ids
			}); err != nil {
				t.Fatal(err)
			}
			id, err := (&videoInfoRepo{}).GetUserFavoriteID(ctx, 7, 42)
			if tc.failAt != 0 {
				if !errors.Is(err, wantErr) || calls != tc.failAt || id != 0 {
					t.Fatal("故障被吞掉或继续访问数据库")
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
			wantCalls := 2
			if len(tc.favorites) == 0 {
				wantCalls = 1
			}
			if err != nil || id != want || calls != wantCalls {
				t.Fatalf("归属结果错误：%d %v, calls=%d", id, err, calls)
			}
		})
	}
}
