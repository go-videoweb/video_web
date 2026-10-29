package data

import (
	"context"
	"errors"
	"testing"
	"vw_user/internal/data/dal/model"
)

// TestUserWithTxOffline 验证本轮关注使用的事务包装传播开启、业务、提交错误并保留 panic。
func TestUserWithTxOffline(t *testing.T) {
	for _, mode := range []string{"成功", "开启失败", "业务失败", "提交失败", "panic"} {
		t.Run(mode, func(t *testing.T) {
			db, state := offlineRepoTransactionDB(t)
			d := &Data{mysql: db}
			fault := errors.New(mode)
			calls := 0
			if mode == "开启失败" {
				state.beginErr = fault
			}
			if mode == "提交失败" {
				state.commitErr = fault
			}
			var recovered any
			var err error
			func() {
				defer func() { recovered = recover() }()
				err = d.WithTx(context.Background(), func(ctx context.Context) error {
					calls++
					if err := getQuery(ctx).FollowList.WithContext(ctx).Create(&model.FollowList{ListID: 13, UserID: 7, IsDefault: true}); err != nil {
						return err
					}
					if mode == "业务失败" {
						return fault
					}
					if mode == "panic" {
						panic(fault)
					}
					return nil
				})
			}()
			if mode == "开启失败" {
				if !errors.Is(err, fault) || calls != 0 {
					t.Fatal("开启失败仍执行业务", err)
				}
				return
			}
			if mode == "panic" {
				if recovered != fault || state.rollbacks != 1 || state.committed != 0 {
					t.Fatal("panic 未回滚或未传播")
				}
				return
			}
			if mode == "成功" {
				if err != nil || state.committed != 1 || state.commits != 1 {
					t.Fatal("事务未成功提交", err)
				}
				return
			}
			if !errors.Is(err, fault) || state.committed != 0 {
				t.Fatal("事务错误被吞掉", err)
			}
			if mode == "业务失败" && state.rollbacks != 1 {
				t.Fatal("业务失败未回滚")
			}
		})
	}
}
