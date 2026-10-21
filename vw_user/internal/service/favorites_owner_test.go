package service

import (
	"context"
	"errors"
	"io"
	"testing"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"gorm.io/gorm"
	favorv1 "vw_user/api/v1/favorites"
	"vw_user/internal/biz"
	"vw_user/internal/data/dal/model"
)

type favoriteOwnerRepo struct {
	biz.FavoritesRepo
	owner, writeUser, writeID int64
	metadataErr               error
	empty                     bool
	checks, updates, deletes  int
	update                    map[string]any
}

func (r *favoriteOwnerRepo) GetFavoritesMetadata(context.Context, int64) (*model.Favorite, error) {
	return &model.Favorite{FavoritesID: 9, UserID: r.owner}, r.metadataErr
}
func (r *favoriteOwnerRepo) CheckIfFavoritesEmpty(context.Context, int64) (bool, error) {
	r.checks++
	return r.empty, nil
}
func (r *favoriteOwnerRepo) DeleteFavorites(_ context.Context, userID, favoriteID int64) error {
	r.deletes++
	r.writeUser, r.writeID = userID, favoriteID
	return nil
}
func (r *favoriteOwnerRepo) UpdateFavorites(_ context.Context, values map[string]any) error {
	r.updates++
	r.update = values
	return nil
}

type favoriteOwnerTx struct{}

func (favoriteOwnerTx) WithTx(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

// TestFavoriteOwnerMutations 验证公开或私密收藏夹均只能由本人修改删除，非空规则及零值属性仍有效。
func TestFavoriteOwnerMutations(t *testing.T) {
	for _, tc := range []struct {
		name           string
		actor, owner   int64
		missing, empty bool
		wantCode       int32
	}{
		{name: "本人空收藏夹", actor: 7, owner: 7, empty: true},
		{name: "其他用户", actor: 8, owner: 7, empty: true, wantCode: 404},
		{name: "不存在", actor: 7, missing: true, empty: true, wantCode: 404},
		{name: "缺少身份", owner: 7, empty: true, wantCode: 400},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &favoriteOwnerRepo{owner: tc.owner, empty: tc.empty}
			if tc.missing {
				repo.metadataErr = gorm.ErrRecordNotFound
			}
			logger := log.NewStdLogger(io.Discard)
			svc := NewFavoritesService(biz.NewFavoritesUsecase(repo, favoriteOwnerTx{}, logger), logger)
			name, description, privacy := "", "", int32(-1)
			modified, err := svc.ModifyFavorites(context.Background(), &favorv1.ModifyFavoritesReq{UserId: tc.actor, FavoritesId: 9, FavoritesName: &name, Description: &description, IsPrivate: &privacy})
			if tc.wantCode != 0 {
				if err == nil || modified != nil || kerrors.FromError(err).Code != tc.wantCode || repo.updates != 0 {
					t.Fatalf("越权修改未拒绝：%v", err)
				}
			} else if err != nil || repo.updates != 1 || repo.update["user_id"] != int64(7) || repo.update["favorites_id"] != int64(9) || *repo.update["description"].(*string) != "" || *repo.update["is_private"].(*int32) != -1 {
				t.Fatalf("归属或零值更新错误：%v", err)
			}
			deleted, err := svc.DeleteFavorites(context.Background(), &favorv1.DeleteFavoritesReq{UserId: tc.actor, FavoritesId: 9})
			if tc.wantCode != 0 {
				if err == nil || deleted != nil || kerrors.FromError(err).Code != tc.wantCode || repo.deletes != 0 || repo.checks != 0 {
					t.Fatalf("越权删除或泄露非空状态：%v", err)
				}
			} else if err != nil || repo.deletes != 1 || repo.writeUser != 7 || repo.writeID != 9 {
				t.Fatalf("合法删除身份错误：%v", err)
			}
		})
	}
}

// TestFavoriteOwnerFailure 验证数据库故障不变成无权成功，非空收藏夹不得删除，非法隐私值不得写入。
func TestFavoriteOwnerFailure(t *testing.T) {
	logger := log.NewStdLogger(io.Discard)
	repo := &favoriteOwnerRepo{owner: 7}
	svc := NewFavoritesService(biz.NewFavoritesUsecase(repo, favoriteOwnerTx{}, logger), logger)
	if resp, err := svc.DeleteFavorites(context.Background(), &favorv1.DeleteFavoritesReq{UserId: 7, FavoritesId: 9}); err == nil || resp != nil || repo.deletes != 0 {
		t.Fatal("非空收藏夹被删除")
	}
	wantErr := errors.New("数据库故障")
	repo.metadataErr = wantErr
	if resp, err := svc.ModifyFavorites(context.Background(), &favorv1.ModifyFavoritesReq{UserId: 7, FavoritesId: 9}); !errors.Is(err, wantErr) || resp != nil || repo.updates != 0 {
		t.Fatal("数据库故障被吞掉")
	}
	privacy := int32(0)
	repo.metadataErr = nil
	if resp, err := svc.ModifyFavorites(context.Background(), &favorv1.ModifyFavoritesReq{UserId: 7, FavoritesId: 9, IsPrivate: &privacy}); err == nil || resp != nil || repo.updates != 0 {
		t.Fatal("非法隐私值被写入")
	}
}
