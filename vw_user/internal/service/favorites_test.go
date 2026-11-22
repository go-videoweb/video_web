package service

import (
	"context"
	"errors"
	"io"
	"testing"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	favorv1 "vw_user/api/v1/favorites"
	"vw_user/internal/biz"
	"vw_user/internal/data/dal/model"
	"vw_user/internal/pkg/ecode/errdef"
)

type favoriteStore struct {
	biz.FavoritesRepo
	rows        []*model.Favorite
	checkErr    error
	createErr   error
	listErr     error
	conflict    bool
	createCalls int
	listCalls   int
}

func (r *favoriteStore) CheckFavoritesName(_ context.Context, userId int64, name string) (bool, error) {
	return !r.conflict, r.checkErr
}

func (r *favoriteStore) CreateFavorites(_ context.Context, favorite *model.Favorite) error {
	r.createCalls++
	if r.createErr != nil {
		return r.createErr
	}
	r.rows = append(r.rows, favorite)
	return nil
}

func (r *favoriteStore) GetFavoritesList(_ context.Context, userId int64) ([]*model.Favorite, error) {
	r.listCalls++
	if r.listErr != nil {
		return nil, r.listErr
	}
	var rows []*model.Favorite
	for _, row := range r.rows {
		if row.UserID == userId {
			rows = append(rows, row)
		}
	}
	return rows, nil
}

// TestFavoriteCreateThenList 验证创建返回实际保存的 ID，并可按用户回读名称、说明和公开或私密状态。
func TestFavoriteCreateThenList(t *testing.T) {
	repo := &favoriteStore{rows: []*model.Favorite{{FavoritesID: 99, UserID: 8, FavoritesName: "其他用户的私密收藏夹", IsPrivate: -1}}}
	logger := log.NewStdLogger(io.Discard)
	svc := NewFavoritesService(biz.NewFavoritesUsecase(repo, nil, logger), logger)
	for _, privacy := range []int32{1, -1, 0} {
		created, err := svc.CreateFavorites(context.Background(), &favorv1.CreateFavoritesReq{
			UserId: 7, FavoritesName: "收藏夹", Description: "说明", IsPrivate: privacy,
		})
		if err != nil || created == nil || created.StatusCode != 200 || created.FavoriteId <= 0 {
			t.Fatalf("创建未返回有效 ID：%v %v", created, err)
		}
		saved := repo.rows[len(repo.rows)-1]
		if saved.FavoritesID != created.FavoriteId || saved.UserID != 7 || saved.FavoritesName != "收藏夹" || saved.Description != "说明" {
			t.Fatal("创建响应和保存的数据不一致")
		}
		if privacy == 0 && saved.IsPrivate != 1 {
			t.Fatal("省略隐私标记时应保留默认公开行为")
		}
	}
	list, err := svc.GetFavoritesList(context.Background(), &favorv1.GetFavoritesListReq{UserId: 7})
	if err != nil || list == nil || len(list.FavoriteList) != 3 {
		t.Fatalf("创建后无法读取全部收藏夹：%v %v", list, err)
	}
	for i, favorite := range list.FavoriteList {
		if favorite.FavoriteId != repo.rows[i+1].FavoritesID || favorite.FavoriteName != "收藏夹" || favorite.Description != "说明" || favorite.IsPrivate != (i == 1) {
			t.Fatalf("列表字段或私密标记转换错误：%v", favorite)
		}
	}
	empty, err := svc.GetFavoritesList(context.Background(), &favorv1.GetFavoritesListReq{UserId: 9})
	if err != nil || empty == nil || empty.FavoriteList == nil || len(empty.FavoriteList) != 0 {
		t.Fatalf("无收藏夹应返回空数组：%v %v", empty, err)
	}
}

// TestFavoriteCreateFailureReturnsNoID 验证重名、查重或保存失败时不返回成功 ID，也不重复写入。
func TestFavoriteCreateFailureReturnsNoID(t *testing.T) {
	for _, tc := range []struct {
		name        string
		repo        favoriteStore
		privacy     int32
		createCalls int
	}{
		{name: "查重失败", repo: favoriteStore{checkErr: errors.New("查询失败")}, privacy: 1},
		{name: "同名收藏夹", repo: favoriteStore{conflict: true}, privacy: 1},
		{name: "写入失败", repo: favoriteStore{createErr: errors.New("写入失败")}, privacy: 1, createCalls: 1},
		{name: "非法私密标记", privacy: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			logger := log.NewStdLogger(io.Discard)
			svc := NewFavoritesService(biz.NewFavoritesUsecase(&tc.repo, nil, logger), logger)
			resp, err := svc.CreateFavorites(context.Background(), &favorv1.CreateFavoritesReq{UserId: 7, FavoritesName: "收藏夹", IsPrivate: tc.privacy})
			if err == nil || resp != nil || tc.repo.createCalls != tc.createCalls || len(tc.repo.rows) != 0 {
				t.Fatalf("失败时应无成功响应且不重复写入：%v %v", resp, err)
			}
		})
	}
}

// TestFavoriteListFailure 验证非法用户不查询仓储，数据库故障返回专用业务错误而非空列表。
func TestFavoriteListFailure(t *testing.T) {
	repo := &favoriteStore{listErr: errors.New("数据库不可用")}
	logger := log.NewStdLogger(io.Discard)
	svc := NewFavoritesService(biz.NewFavoritesUsecase(repo, nil, logger), logger)
	for _, id := range []int64{0, -1} {
		resp, err := svc.GetFavoritesList(context.Background(), &favorv1.GetFavoritesListReq{UserId: id})
		if err == nil || resp != nil || repo.listCalls != 0 {
			t.Fatal("非法用户触发了查询")
		}
	}
	resp, err := svc.GetFavoritesList(context.Background(), &favorv1.GetFavoritesListReq{UserId: 7})
	if err == nil || resp != nil || repo.listCalls != 1 || kerrors.FromError(err).Code != errdef.ErrGetFavoritesListFailed.Code {
		t.Fatalf("数据库故障被吞掉：%v %v", resp, err)
	}
}
