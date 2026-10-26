package data

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"testing"
	"time"
	favorv1 "vw_user/api/v1/favorites"
	"vw_video/internal/data/dal/model"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	drivermysql "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

// TestFavoriteWriteOffline 验证收藏写入保存操作人、跨夹约束、精准取消以及错误透传；不连接数据库。
func TestFavoriteWriteOffline(t *testing.T) {
	for _, mode := range []string{"收藏", "取消", "同夹重复", "其他收藏夹", "历史软删除", "重复键", "无匹配", "SQL故障", "RPC故障", "他人收藏夹"} {
		t.Run(mode, func(t *testing.T) {
			db, state := offlineExplicitQueryDB(t)
			txDB := db.Begin()
			defer txDB.Rollback()
			tx := txDB.Statement.ConnPool.(*sql.Tx)
			ctx := context.WithValue(context.Background(), transactionContextKey{}, "request")
			data := offlineFavoriteData()
			fault := errors.New("离线故障")
			wantCode := 0
			switch mode {
			case "同夹重复", "其他收藏夹", "历史软删除":
				state.favoriteRow = &model.FavoriteVideo{ID: 21, UserID: 7, FavoritesID: 13, VideoID: 9}
				if mode == "其他收藏夹" {
					state.favoriteRow.FavoritesID = 14
				}
				if mode == "历史软删除" {
					state.favoriteRow.DeletedAt = gorm.DeletedAt{Time: time.Now(), Valid: true}
				}
				wantCode = 409
			case "重复键":
				state.failWrite = &drivermysql.MySQLError{Number: 1062}
				wantCode = 409
			case "无匹配":
				zero := int64(0)
				state.affected = &zero
				wantCode = 404
			case "SQL故障":
				state.failWrite = fault
			case "RPC故障", "他人收藏夹":
				data.favoriteClient = &favoriteClientStub{getInfo: func(request context.Context, req *favorv1.GetFavoriteInfoReq) (*favorv1.GetFavoriteInfoResp, error) {
					if request.Value(transactionContextKey{}) != "request" || req.FavoriteId != 13 {
						t.Fatal("权限 RPC 参数错误")
					}
					if mode == "RPC故障" {
						return nil, fault
					}
					return &favorv1.GetFavoriteInfoResp{FavoriteId: 13, UserId: 8}, nil
				}}
				if mode == "他人收藏夹" {
					wantCode = 404
				}
			}
			repo := &interactRepo{data: data}
			var err error
			if mode == "取消" || mode == "无匹配" {
				err = repo.DeleteUserFavoriteVideo(ctx, tx, 7, 13, 9)
			} else {
				err = repo.AddUserFavoriteVideo(ctx, tx, 7, 13, 9)
			}
			switch {
			case wantCode != 0:
				if err == nil || kerrors.FromError(err).Code != int32(wantCode) {
					t.Fatalf("错误分类不符：%v", err)
				}
			case mode == "RPC故障" || mode == "SQL故障":
				if !errors.Is(err, fault) {
					t.Fatalf("故障未透传：%v", err)
				}
			default:
				if err != nil {
					t.Fatal(err)
				}
			}
			if mode == "收藏" {
				if len(state.writeTables) != 1 || !strings.HasPrefix(state.writeTables[0], "INSERT INTO `favorite_video`") || !strings.Contains(state.writeTables[0], "`user_id`") {
					t.Fatalf("身份未写入关系：%v", state.writeTables)
				}
				found := false
				for _, arg := range state.writeArgs[0] {
					if arg.Value == int64(7) {
						found = true
					}
				}
				if !found {
					t.Fatal("插入参数缺少用户 ID")
				}
				text := strings.Join(state.queryTexts, " ")
				if !strings.Contains(text, "`favorite_video`.`user_id` = ?") || !strings.Contains(text, "`favorite_video`.`video_id` = ?") {
					t.Fatalf("关系检查未限定用户和视频：%s", text)
				}
			}
			if mode == "取消" || mode == "无匹配" {
				if len(state.writeTables) != 1 {
					t.Fatalf("取消写入次数错误：%v", state.writeTables)
				}
				text := state.writeTables[0]
				if !strings.HasPrefix(text, "DELETE FROM") || strings.Contains(text, " OR ") {
					t.Fatalf("不是精准硬删除：%s", text)
				}
				for _, part := range []string{"`user_id` = ?", "`favorites_id` = ?", "`video_id` = ?", "`deleted_at` IS NULL"} {
					if !strings.Contains(text, part) {
						t.Fatalf("缺少删除条件 %s：%s", part, text)
					}
				}
				args := state.writeArgs[0]
				if len(args) != 3 || args[0].Value != int64(7) || args[1].Value != int64(13) || args[2].Value != int64(9) {
					t.Fatalf("取消参数错误：%+v", args)
				}
			}
			if mode == "同夹重复" || mode == "其他收藏夹" || mode == "历史软删除" || mode == "RPC故障" || mode == "他人收藏夹" {
				if state.writes != 0 {
					t.Fatal("失败后仍写入关系")
				}
			}
			if state.commits != 0 || state.rollbacks != 0 {
				t.Fatal("仓储擅自结束屏障事务")
			}
		})
	}
}
