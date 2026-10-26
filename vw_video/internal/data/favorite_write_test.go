package data

import (
	"context"
	"database/sql/driver"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	drivermysql "github.com/go-sql-driver/mysql"
	favorv1 "vw_user/api/v1/favorites"
	"vw_video/internal/biz"
	"vw_video/internal/data/dal/model"
	"vw_video/internal/data/dal/query"
)

// namedValues 提取离线 SQL 驱动收到的有序参数，不依赖 SQL 字符串插值。
func namedValues(args []driver.NamedValue) []interface{} {
	values := make([]interface{}, len(args))
	for i, arg := range args {
		values[i] = arg.Value
	}
	return values
}

// TestFavoriteAccessRPCOffline 验证公开可读但不可写，异常元信息和 RPC 故障一律拒绝。
func TestFavoriteAccessRPCOffline(t *testing.T) {
	fault := errors.New("rpc failure")
	for _, tc := range []struct {
		name  string
		row   *favorv1.GetFavoriteInfoResp
		err   error
		write bool
		code  int32
	}{
		{name: "本人私密", row: &favorv1.GetFavoriteInfoResp{FavoriteId: 13, UserId: 7, IsPrivate: true}, write: true},
		{name: "公开读取", row: &favorv1.GetFavoriteInfoResp{FavoriteId: 13, UserId: 8}},
		{name: "公开写入", row: &favorv1.GetFavoriteInfoResp{FavoriteId: 13, UserId: 8}, write: true, code: 404},
		{name: "私密读取", row: &favorv1.GetFavoriteInfoResp{FavoriteId: 13, UserId: 8, IsPrivate: true}, code: 404},
		{name: "空元信息", code: 500},
		{name: "错误ID", row: &favorv1.GetFavoriteInfoResp{FavoriteId: 14, UserId: 7}, code: 500},
		{name: "缺所有者", row: &favorv1.GetFavoriteInfoResp{FavoriteId: 13}, code: 500},
		{name: "RPC故障", err: fault, code: 500},
		{name: "不存在", err: kerrors.NotFound("FAVORITE_NOT_FOUND", "missing"), code: 404},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			calls := 0
			d := &Data{favoriteClient: &favoriteClientStub{getInfo: func(request context.Context, req *favorv1.GetFavoriteInfoReq) (*favorv1.GetFavoriteInfoResp, error) {
				calls++
				if request != ctx || req.FavoriteId != 13 {
					t.Fatal("RPC parameters changed")
				}
				return tc.row, tc.err
			}}}
			err := d.requireFavoriteAccess(ctx, 7, 13, tc.write)
			if (tc.code == 0 && err != nil) || (tc.code != 0 && (err == nil || kerrors.FromError(err).Code != tc.code)) {
				t.Fatalf("unexpected permission: %v", err)
			}
			if tc.err != nil && !errors.Is(err, tc.err) {
				t.Fatal("RPC error lost")
			}
			if err := d.requireFavoriteAccess(ctx, 0, 13, tc.write); err == nil || calls != 1 {
				t.Fatal("invalid user reached RPC")
			}
		})
	}
	if err := (*Data)(nil).requireFavoriteAccess(context.Background(), 7, 13, true); err == nil {
		t.Fatal("nil client accepted")
	}
}

// TestFavoriteLocalQueryOffline 验证全部收藏 SQL 使用 WithTx 的上下文，不逃逸到默认连接。
func TestFavoriteLocalQueryOffline(t *testing.T) {
	for _, mode := range []string{"video", "read", "create", "delete", "increment", "decrement", "status"} {
		t.Run(mode, func(t *testing.T) {
			globalDB, global := offlineExplicitQueryDB(t)
			saved := *query.Q
			query.SetDefault(globalDB)
			defer func() { *query.Q = saved }()
			db, state := offlineExplicitQueryDB(t)
			d := &Data{mysql: db}
			repo := &interactRepo{data: d}
			row := &model.FavoriteVideo{ID: 23, UserID: 7, FavoritesID: 13, VideoID: 9}
			if mode == "read" {
				state.favoriteRow = row
			}
			ctx := context.WithValue(context.Background(), transactionContextKey{}, "request")
			err := d.WithTx(ctx, func(ctx context.Context) error {
				switch mode {
				case "video":
					_, err := repo.GetPublisherIdByVideoId(ctx, 9)
					return err
				case "read":
					got, err := repo.GetFavoriteRelation(ctx, 7, 9)
					if err == nil && (got.ID != 23 || got.UserID != 7 || got.VideoID != 9) {
						t.Fatal("relation ID lost")
					}
					return err
				case "create":
					return repo.CreateFavoriteRelation(ctx, row)
				case "delete":
					return repo.DeleteFavoriteRelation(ctx, row)
				case "increment":
					return repo.UpdateFavoriteCount(ctx, 9, 1)
				case "decrement":
					return repo.UpdateFavoriteCount(ctx, 9, -1)
				default:
					return repo.SetFavoriteStatus(ctx, 7, 9, true)
				}
			})
			if err != nil || state.commits != 1 || state.rollbacks != 0 || global.queries != 0 || global.writes != 0 {
				t.Fatalf("transaction escaped: %v %+v", err, state)
			}
			for i, sql := range state.queryTexts {
				if strings.Contains(sql, "FOR UPDATE") || strings.Contains(sql, "FOR SHARE") {
					t.Fatalf("unexpected explicit lock: %s", sql)
				}
				if strings.Contains(sql, "`favorites`") {
					t.Fatal("shared user table queried")
				}
				if mode == "read" {
					if !reflect.DeepEqual(namedValues(state.queryArgs[i]), []interface{}{int64(7), int64(9), int64(1)}) {
						t.Fatal("wrong relation filter")
					}
				}
			}
			if mode == "delete" {
				want := "DELETE FROM `favorite_video` WHERE `favorite_video`.`id` = ? AND `favorite_video`.`user_id` = ? AND `favorite_video`.`favorites_id` = ? AND `favorite_video`.`video_id` = ? AND `favorite_video`.`deleted_at` IS NULL"
				if state.writeTables[0] != want || !reflect.DeepEqual(namedValues(state.writeArgs[0]), []interface{}{int64(23), int64(7), int64(13), int64(9)}) {
					t.Fatalf("unsafe delete: %s", state.writeTables[0])
				}
			}
			if mode == "status" {
				sql := state.writeTables[0]
				parts := strings.Split(sql, " ON DUPLICATE KEY UPDATE ")
				if len(parts) != 2 || state.queries != 0 || state.writes != 1 {
					t.Fatalf("status is not a single upsert: %s", sql)
				}
				if !strings.Contains(parts[1], "`is_favor`") || !strings.Contains(parts[1], "`deleted_at`") || !strings.Contains(parts[1], "`updated_at`") {
					t.Fatal("favorite status not persisted")
				}
				for _, field := range []string{"is_like", "is_dislike", "is_shared", "shells_count", "last_watch_time", "created_at", "user_id", "video_id"} {
					if strings.Contains(parts[1], field) {
						t.Fatalf("overwrote unrelated status: %s", sql)
					}
				}
			}
			if mode == "increment" || mode == "decrement" {
				sql := state.writeTables[0]
				if !strings.Contains(sql, "`version` = ?") || !strings.Contains(sql, "`version`=`version`+1") {
					t.Fatal("version guard lost")
				}
				if mode == "decrement" && !strings.Contains(sql, "`video`.`cnt_favorited` >= ?") {
					t.Fatal("counter underflow guard lost")
				}
			}
		})
	}
}

// TestFavoriteOptimisticRetryOffline 使用真实 GORM 事务和离线驱动验证重试重新读取版本与计数。
func TestFavoriteOptimisticRetryOffline(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(map[bool]string{false: "收藏", true: "取消"}[cancel], func(t *testing.T) {
			db, state := offlineExplicitQueryDB(t)
			count := int64(3)
			state.videoCount, state.countConflicts = &count, 1
			d := offlineFavoriteData()
			d.mysql = db
			metadataCalls := 0
			d.favoriteClient = &favoriteClientStub{getInfo: func(ctx context.Context, req *favorv1.GetFavoriteInfoReq) (*favorv1.GetFavoriteInfoResp, error) {
				metadataCalls++
				if ctx.Value(transactionKey{}) != nil {
					t.Fatal("RPC entered local transaction")
				}
				return &favorv1.GetFavoriteInfoResp{FavoriteId: req.FavoriteId, UserId: 7}, nil
			}}
			cmd := &biz.FavoriteCommand{UserID: 7, VideoID: 9, FavoriteID: 13, Favorite: 1}
			if cancel {
				cmd.Favorite, cmd.RecordID = -1, 23
				state.favoriteRow = &model.FavoriteVideo{ID: 23, UserID: 7, FavoritesID: 13, VideoID: 9}
			}
			logger := log.NewStdLogger(io.Discard)
			ctx := context.WithValue(context.Background(), transactionContextKey{}, "request")
			result, err := biz.NewInteractUseCase(NewInteractRepo(d, logger), nil, d, logger).SetVideoFavorite(ctx, cmd)
			if err != nil || result == nil || state.begins != 2 || state.rollbacks != 1 || state.commits != 1 || state.writes != 5 || state.queries != 6 || metadataCalls != 1 {
				t.Fatalf("retry escaped whole transaction: %v %+v", err, state)
			}
			wantCount := int64(5)
			if cancel {
				wantCount = 3
			}
			if got := namedValues(state.writeArgs[3])[0]; got != wantCount {
				t.Fatalf("retry reused old counter: got=%v want=%d", got, wantCount)
			}
			if state.videoVersion != 2 {
				t.Fatal("version was not refreshed")
			}
			for _, text := range state.queryTexts {
				if strings.Contains(text, "FOR UPDATE") || strings.Contains(text, "FOR SHARE") || strings.Contains(text, "user_video_status") {
					t.Fatalf("explicit lock or status preread remains: %s", text)
				}
			}
		})
	}
}

// TestFavoriteCountErrorsOffline 验证已知下溢和缺失版本不是可重试冲突，不提交关系或状态。
func TestFavoriteCountErrorsOffline(t *testing.T) {
	for _, mode := range []string{"underflow", "missing-version"} {
		t.Run(mode, func(t *testing.T) {
			db, state := offlineExplicitQueryDB(t)
			d := offlineFavoriteData()
			d.mysql = db
			cmd := &biz.FavoriteCommand{UserID: 7, VideoID: 9, FavoriteID: 13, Favorite: 1}
			if mode == "underflow" {
				state.videoCount = new(int64)
				state.favoriteRow = &model.FavoriteVideo{ID: 23, UserID: 7, FavoritesID: 13, VideoID: 9}
				cmd.Favorite, cmd.RecordID = -1, 23
			} else {
				state.missingVideoVersion = true
			}
			logger := log.NewStdLogger(io.Discard)
			ctx := context.WithValue(context.Background(), transactionContextKey{}, "request")
			result, err := biz.NewInteractUseCase(NewInteractRepo(d, logger), nil, d, logger).SetVideoFavorite(ctx, cmd)
			if err == nil || errors.Is(err, biz.ErrFavoriteVersionConflict) || result != nil || state.begins != 1 || state.rollbacks != 1 || state.commits != 0 || state.writes != 1 {
				t.Fatalf("invalid count retried or partially committed: %v %+v", err, state)
			}
			want := "FAVORITE_COUNT_CONFLICT"
			if mode == "missing-version" {
				want = "INVALID_VIDEO_VERSION"
			}
			if kerrors.FromError(err).Reason != want {
				t.Fatalf("wrong error classification: %v", err)
			}
		})
	}
}

// TestFavoriteStatusIdentityOffline 验证局部 upsert 不会为无效身份初始化个人数据。
func TestFavoriteStatusIdentityOffline(t *testing.T) {
	for _, ids := range [][2]int64{{0, 9}, {-1, 9}, {7, 0}} {
		if err := (&interactRepo{}).SetFavoriteStatus(context.Background(), ids[0], ids[1], true); !errors.Is(err, biz.ErrInvalidInteractionIdentity) {
			t.Fatal("invalid identity accepted")
		}
	}
}

// TestFavoriteLocalFailuresOffline 验证真实 GORM 事务在关系、计数、状态失败时回滚，提交失败不返回成功。
func TestFavoriteLocalFailuresOffline(t *testing.T) {
	for _, mode := range []string{"success", "relation", "count", "status", "commit", "duplicate", "zero-create", "zero-count", "zero-delete"} {
		t.Run(mode, func(t *testing.T) {
			db, state := offlineExplicitQueryDB(t)
			d := offlineFavoriteData()
			d.mysql = db
			fault := errors.New("injected SQL failure")
			switch mode {
			case "relation":
				state.failWrite, state.failWriteAt = fault, 1
			case "count":
				state.failWrite, state.failWriteAt = fault, 2
			case "status":
				state.failWrite, state.failWriteAt = fault, 3
			case "commit":
				state.failCommit = fault
			case "duplicate":
				state.failWrite, state.failWriteAt = &drivermysql.MySQLError{Number: 1062}, 1
			case "zero-create", "zero-delete":
				state.affected = new(int64)
				state.affectedWriteAt = 1
			case "zero-count":
				state.countConflicts = -1
			}
			cmd := &biz.FavoriteCommand{UserID: 7, VideoID: 9, FavoriteID: 13, Favorite: 1}
			if mode == "zero-delete" {
				cmd.Favorite, cmd.RecordID = -1, 23
				state.favoriteRow = &model.FavoriteVideo{ID: 23, UserID: 7, FavoritesID: 13, VideoID: 9}
			}
			logger := log.NewStdLogger(io.Discard)
			uc := biz.NewInteractUseCase(NewInteractRepo(d, logger), nil, d, logger)
			ctx := context.WithValue(context.Background(), transactionContextKey{}, "request")
			result, err := uc.SetVideoFavorite(ctx, cmd)
			if mode == "success" {
				if err != nil || result.RecordID <= 0 || state.commits != 1 || state.writes != 3 {
					t.Fatalf("local favorite failed: %v %+v", err, state)
				}
				if !strings.Contains(state.writeTables[0], "`id`") {
					t.Fatal("generated relation ID not inserted")
				}
			} else {
				if err == nil || result != nil {
					t.Fatal("failed transaction returned success")
				}
				attempts := 1
				if mode == "zero-count" {
					attempts = 3
				}
				if state.begins != attempts {
					t.Fatalf("unexpected retry count: %d", state.begins)
				}
				if mode != "commit" && (state.rollbacks != attempts || state.commits != 0) {
					t.Fatal("failed transaction not rolled back")
				}
				if mode == "commit" && (!errors.Is(err, fault) || state.commits != 1) {
					t.Fatal("commit error lost")
				}
				if (mode == "duplicate" || strings.HasPrefix(mode, "zero-")) && kerrors.FromError(err).Code != 409 {
					t.Fatalf("conflict not reported: %v", err)
				}
			}
		})
	}
}
