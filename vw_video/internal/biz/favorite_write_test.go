package biz

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"testing"
)

type favoriteBarrierRepo struct {
	*barrierStatusRepo
	relationErr error
}

// writeFavorite 验证业务沿用屏障事务和目标视频，并模拟一次可回滚的写入。
func (r *favoriteBarrierRepo) writeFavorite(ctx context.Context, tx *sql.Tx, videoID int64) error {
	if tx != r.opened || videoID != 9 {
		r.t.Fatal("收藏写入丢失事务或视频 ID")
	}
	_, err := tx.ExecContext(ctx, "UPDATE test_video_state")
	return err
}

// AddVideoCntFavorite 模拟正向收藏计数更新。
func (r *favoriteBarrierRepo) AddVideoCntFavorite(ctx context.Context, tx *sql.Tx, videoID int64) error {
	return r.writeFavorite(ctx, tx, videoID)
}

// DecrementVideoCntFavorite 模拟取消收藏计数更新。
func (r *favoriteBarrierRepo) DecrementVideoCntFavorite(ctx context.Context, tx *sql.Tx, videoID int64) error {
	return r.writeFavorite(ctx, tx, videoID)
}

// AddUserFavoriteVideo 校验身份和收藏夹传递，错误应使之前的计数一并回滚。
func (r *favoriteBarrierRepo) AddUserFavoriteVideo(ctx context.Context, tx *sql.Tx, userID, favoriteID, videoID int64) error {
	if userID != 7 || favoriteID != 13 {
		r.t.Fatal("收藏身份或目标收藏夹错误")
	}
	if err := r.writeFavorite(ctx, tx, videoID); err != nil {
		return err
	}
	return r.relationErr
}

// DeleteUserFavoriteVideo 复用同一组事务及身份断言验证取消操作。
func (r *favoriteBarrierRepo) DeleteUserFavoriteVideo(ctx context.Context, tx *sql.Tx, userID, favoriteID, videoID int64) error {
	return r.AddUserFavoriteVideo(ctx, tx, userID, favoriteID, videoID)
}

// TestFavoriteSagaOffline 验证第一批保留屏障幂等，关系失败回滚计数，并传递用户身份。
func TestFavoriteSagaOffline(t *testing.T) {
	for _, flag := range []int32{1, -1} {
		for _, fail := range []bool{false, true} {
			t.Run(fmt.Sprintf("操作%d_失败%t", flag, fail), func(t *testing.T) {
				store := &barrierStore{markers: map[string]bool{}}
				name := fmt.Sprintf("offline-favorite-%d", barrierDriverID.Add(1))
				sql.Register(name, &barrierDriver{store: store})
				db, err := sql.Open(name, "")
				if err != nil {
					t.Fatal(err)
				}
				defer db.Close()
				repo := &favoriteBarrierRepo{barrierStatusRepo: &barrierStatusRepo{t: t, db: db}}
				if fail {
					repo.relationErr = errors.New("关系写入失败")
				}
				uc := &InteractUsecase{repo: repo}
				for n := 0; n < 2; n++ {
					err = uc.FavoriteVideo(statusBarrierContext("action"), 7, 9, 13, flag)
					if (err != nil) != fail {
						t.Fatalf("收藏结果不符合预期：%v", err)
					}
				}
				if fail {
					if store.writes != 0 || store.rollbacks != 2 {
						t.Fatalf("失败未整体回滚：%+v", store)
					}
				} else if store.writes != 2 {
					t.Fatalf("重复调用未被屏障拦截：%+v", store)
				}
				repo.opened = nil
				if err := uc.FavoriteVideo(context.Background(), 0, 9, 13, flag); err == nil || repo.opened != nil {
					t.Fatal("非法用户仍开启事务")
				}
			})
		}
	}
}
