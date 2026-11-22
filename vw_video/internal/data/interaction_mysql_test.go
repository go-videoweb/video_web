package data

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/go-kratos/kratos/v2/config"
	"github.com/go-kratos/kratos/v2/config/file"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"vw_video/internal/biz"
	"vw_video/internal/conf"
	"vw_video/internal/data/dal/model"
	"vw_video/internal/data/dal/query"
)

// TestVideoStateMySQLRollback 仅在显式启用时连接本机配置；所有样例写入始终回滚。
func TestVideoStateMySQLRollback(t *testing.T) {
	if os.Getenv("VIDEOWEB_MYSQL_ROLLBACK_TEST") != "1" {
		t.Skip("未启用真实 MySQL 回滚验证")
	}
	c := config.New(config.WithSource(file.NewSource("../../configs/config.yaml")))
	defer c.Close()
	if err := c.Load(); err != nil {
		t.Fatal(err)
	}
	var cfg conf.Bootstrap
	if err := c.Scan(&cfg); err != nil {
		t.Fatal(err)
	}
	m := cfg.Data.Mysql
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local", m.User, m.Password, m.Host, m.Port, m.Database)
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	pool, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	userID, videoID := time.Now().UnixNano(), time.Now().UnixNano()
	var before int64
	if err := db.Model(&model.UserVideoStatus{}).Unscoped().Where("user_id = ? AND video_id = ?", userID, videoID).Count(&before).Error; err != nil {
		t.Fatal(err)
	}
	if before != 0 {
		t.Fatal("测试 ID 已存在，拒绝使用")
	}
	tx := db.Begin()
	if tx.Error != nil {
		t.Fatal(tx.Error)
	}
	defer func() {
		if err := tx.Rollback().Error; err != nil {
			t.Error(err)
		}
		var remaining int64
		if err := db.Model(&model.UserVideoStatus{}).Unscoped().Where("user_id = ? AND video_id = ?", userID, videoID).Count(&remaining).Error; err != nil || remaining != 0 {
			t.Errorf("回滚后仍残留测试记录：%d %v", remaining, err)
		}
	}()
	ctx := context.WithValue(context.Background(), transactionKey{}, &query.QueryTx{Query: query.Use(tx)})
	row, err := readUserVideoStatus(ctx, interactionDB(ctx), userID, videoID)
	if err != nil || videoStatusMask(row) != 0 {
		t.Fatalf("无记录读取错误：%+v %v", row, err)
	}
	if err := (&videoInfoRepo{}).UpdateUserVideoHistory(ctx, videoID, userID); err != nil {
		t.Fatal(err)
	}
	row, err = readUserVideoStatus(ctx, interactionDB(ctx), userID, videoID)
	if err != nil || row.LastWatchTime == nil {
		t.Fatalf("观看时间写入失败：%+v %v", row, err)
	}
	watched := *row.LastWatchTime
	repo := &interactRepo{}
	changes := []*biz.VideoStatusPatch{
		{IsLike: &biz.StatusChange{Before: 0, After: 1}},
		{IsShared: &biz.StatusChange{Before: 0, After: 1}},
		{ShellsCount: &biz.StatusChange{Before: 0, After: 1}},
		{ShellsCount: &biz.StatusChange{Before: 1, After: 2}},
		{ShellsCount: &biz.StatusChange{Before: 2, After: 1}},
		{IsShared: &biz.StatusChange{Before: 1, After: 0}},
	}
	for _, patch := range changes {
		if err := repo.ApplyUserVideoStatus(ctx, tx.Statement.ConnPool.(*sql.Tx), userID, videoID, patch); err != nil {
			t.Fatal(err)
		}
	}
	row, err = readUserVideoStatus(ctx, interactionDB(ctx), userID, videoID)
	if err != nil || row.IsLike != 1 || row.IsShared != 0 || row.ShellsCount != 1 || !row.LastWatchTime.Equal(watched) {
		t.Fatalf("局部更新或补偿覆盖其他状态：%+v %v", row, err)
	}
	if err := repo.ApplyUserVideoStatus(ctx, tx.Statement.ConnPool.(*sql.Tx), userID, videoID, &biz.VideoStatusPatch{IsLike: &biz.StatusChange{Before: 0, After: 1}}); err == nil {
		t.Fatal("过期的互动状态未拒绝")
	}
	if err := (&videoInfoRepo{}).UpdateUserVideoHistory(ctx, videoID, userID); err != nil {
		t.Fatal(err)
	}
	row, err = readUserVideoStatus(ctx, interactionDB(ctx), userID, videoID)
	if err != nil || row.IsLike != 1 || row.ShellsCount != 1 || row.LastWatchTime.Before(watched) {
		t.Fatalf("观看 upsert 覆盖互动或回退时间：%+v %v", row, err)
	}
}
