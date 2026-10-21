package data

import (
	"context"
	"strings"
	"testing"

	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"vw_user/internal/data/dal/query"
)

// TestFavoriteWriteOwnerSQL 使用 DryRun 核对修改删除 SQL 均限定所有者，且更新不会改变归属。
// 按文件运行并排除 test_helper.go，避免该历史初始化访问真实数据库。
func TestFavoriteWriteOwnerSQL(t *testing.T) {
	db, err := gorm.Open(mysql.New(mysql.Config{DSN: "offline:offline@tcp(127.0.0.1:1)/offline", SkipInitializeWithVersion: true}), &gorm.Config{DryRun: true, DisableAutomaticPing: true, SkipDefaultTransaction: true, Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	ctx := context.WithValue(context.Background(), transactionKey{}, &query.QueryTx{Query: query.Use(db)})
	var updateSQL, deleteSQL string
	originalUpdate := db.Callback().Update().Get("gorm:update")
	if err := db.Callback().Update().Replace("gorm:update", func(tx *gorm.DB) { originalUpdate(tx); updateSQL = tx.Statement.SQL.String() }); err != nil {
		t.Fatal(err)
	}
	originalDelete := db.Callback().Delete().Get("gorm:delete")
	if err := db.Callback().Delete().Replace("gorm:delete", func(tx *gorm.DB) { originalDelete(tx); deleteSQL = tx.Statement.SQL.String(); tx.RowsAffected = 1 }); err != nil {
		t.Fatal(err)
	}
	repo := &favoritesRepo{}
	description := ""
	if err := repo.UpdateFavorites(ctx, map[string]any{"user_id": int64(7), "favorites_id": int64(9), "description": &description}); err != nil {
		t.Fatal(err)
	}
	if err := repo.DeleteFavorites(ctx, 7, 9); err != nil {
		t.Fatal(err)
	}
	for _, sqlText := range []string{updateSQL, deleteSQL} {
		if !strings.Contains(sqlText, "`favorites`.`favorites_id` = ?") || !strings.Contains(sqlText, "`favorites`.`user_id` = ?") {
			t.Fatalf("写入缺少归属筛选：%s", sqlText)
		}
	}
	setClause := strings.Split(strings.Split(updateSQL, " SET ")[1], " WHERE ")[0]
	if strings.Contains(setClause, "user_id") || strings.Contains(setClause, "favorites_id") || !strings.Contains(setClause, "description") {
		t.Fatalf("更新改变归属或丢失零值字段：%s", updateSQL)
	}
}
