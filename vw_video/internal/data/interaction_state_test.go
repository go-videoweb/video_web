package data

import (
	"errors"
	"strings"
	"testing"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	"gorm.io/gorm"
	"vw_video/internal/biz"
	"vw_video/internal/data/dal/model"
)

// TestVideoStateReadOffline 验证缺失状态只返回默认值，错误透传，收藏副本不进入掩码。
func TestVideoStateReadOffline(t *testing.T) {
	for _, missing := range []bool{false, true} {
		db, ctx := offlineFavoriteDB(t)
		queries, writes := 0, 0
		original := db.Callback().Query().Get("gorm:query")
		db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
			original(tx)
			queries++
			if missing {
				tx.AddError(gorm.ErrRecordNotFound)
				return
			}
			*tx.Statement.Dest.(*model.UserVideoStatus) = model.UserVideoStatus{IsLike: 1, IsFavor: 1, IsShared: 1, ShellsCount: 2}
		})
		db.Callback().Create().Before("gorm:create").Register("test:no-initialize", func(*gorm.DB) { writes++ })
		row, err := readUserVideoStatus(ctx, db, 7, 9)
		want := biz.UpvoteStatus | biz.ShareStatus | biz.ThrowShellStatus
		if missing {
			want = 0
		}
		if err != nil || queries != 1 || writes != 0 || videoStatusMask(row) != want {
			t.Fatalf("状态读取不符合约定：row=%+v queries=%d writes=%d err=%v", row, queries, writes, err)
		}
		if _, err := readUserVideoStatus(ctx, db, 0, 9); err == nil || queries != 1 {
			t.Fatal("游客触发个人状态查询")
		}
		fault := errors.New("读取失败")
		db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) { tx.AddError(fault) })
		if _, err := readUserVideoStatus(ctx, db, 7, 9); !errors.Is(err, fault) {
			t.Fatal("数据库错误被吞掉")
		}
	}
}

// TestLastWatchTimeUpsertOffline 验证观看时间冲突更新不覆盖点赞、分享和贝壳数量。
func TestLastWatchTimeUpsertOffline(t *testing.T) {
	db, ctx := offlineFavoriteDB(t)
	db.Config.SkipDefaultTransaction = true
	var text string
	var row *model.UserVideoStatus
	original := db.Callback().Create().Get("gorm:create")
	db.Callback().Create().Replace("gorm:create", func(tx *gorm.DB) {
		original(tx)
		text = tx.Statement.SQL.String()
		row = tx.Statement.Dest.(*model.UserVideoStatus)
	})
	if err := (&videoInfoRepo{}).UpdateUserVideoHistory(ctx, 9, 7); err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(text, " ON DUPLICATE KEY UPDATE ")
	if len(parts) != 2 || row == nil || row.LastWatchTime == nil || row.UserID != 7 || row.VideoID != 9 {
		t.Fatalf("观看记录未按用户视频 upsert：%s %+v", text, row)
	}
	for _, forbidden := range []string{"is_like", "is_shared", "shells_count", "is_favor"} {
		if strings.Contains(parts[1], forbidden) {
			t.Fatalf("观看操作覆盖互动字段：%s", parts[1])
		}
	}
	if !strings.Contains(parts[1], "GREATEST") || !strings.Contains(parts[1], "last_watch_time") {
		t.Fatal("最近观看时间可能被旧请求回退")
	}
	text = ""
	if err := (&videoInfoRepo{}).UpdateUserVideoHistory(ctx, 9, 0); err == nil || text != "" {
		t.Fatal("游客写入观看记录")
	}
}

// TestPartialVideoStatusUpdateOffline 验证行锁、预期旧值和局部更新，避免无关字段被覆盖。
func TestPartialVideoStatusUpdateOffline(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		db, _ := offlineFavoriteDB(t)
		db.Config.SkipDefaultTransaction = true
		var selectSQL, updateSQL string
		query := db.Callback().Query().Get("gorm:query")
		db.Callback().Query().Replace("gorm:query", func(tx *gorm.DB) {
			query(tx)
			selectSQL = tx.Statement.SQL.String()
			row := tx.Statement.Dest.(*model.UserVideoStatus)
			row.ShellsCount = 2
			if conflict {
				row.IsShared = 1
			}
		})
		update := db.Callback().Update().Get("gorm:update")
		db.Callback().Update().Replace("gorm:update", func(tx *gorm.DB) { update(tx); updateSQL = tx.Statement.SQL.String() })
		err := applyVideoStatusPatch(db, 7, 9, &biz.VideoStatusPatch{IsShared: &biz.StatusChange{Before: 0, After: 1}})
		if !strings.Contains(selectSQL, "FOR UPDATE") || !strings.Contains(selectSQL, "user_id = ? AND video_id = ?") {
			t.Fatalf("未锁定目标状态：%s", selectSQL)
		}
		if conflict {
			if err == nil || kerrors.FromError(err).Code != 409 || updateSQL != "" {
				t.Fatalf("并发状态未拒绝：%v %s", err, updateSQL)
			}
			continue
		}
		if err != nil || !strings.Contains(updateSQL, "`is_shared`=?") {
			t.Fatalf("分享更新失败：%v %s", err, updateSQL)
		}
		for _, name := range []string{"is_like", "shells_count", "last_watch_time", "is_favor"} {
			if strings.Contains(updateSQL, name) {
				t.Fatalf("更新了无关字段：%s", updateSQL)
			}
		}
	}
}
