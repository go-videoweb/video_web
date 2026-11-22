package biz

import (
	"context"
	"errors"
	"io"
	"math"
	"testing"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
)

type favoritePageRepo struct {
	VideoInfoRepo
	userID, favoriteID int64
	page, size         int32
	calls              int
	err                error
}

func (r *favoritePageRepo) GetFavoriteVideos(_ context.Context, userID, favoriteID int64, page, size int32) ([]*FavoriteVideo, int64, error) {
	r.userID, r.favoriteID, r.page, r.size = userID, favoriteID, page, size
	r.calls++
	return nil, 0, r.err
}

// TestFavoritePageValidation 验证身份、分页默认值、上限和偏移，非法请求不访问仓储，权限错误不被包装。
func TestFavoritePageValidation(t *testing.T) {
	for _, tc := range []struct {
		name               string
		userID, favoriteID int64
		page, size         int32
		invalid            bool
	}{
		{name: "默认分页", userID: 7, favoriteID: 9},
		{name: "指定分页", userID: 7, favoriteID: 9, page: 2, size: 3},
		{name: "游客", favoriteID: 9, invalid: true},
		{name: "非法收藏夹", userID: 7, favoriteID: -1, invalid: true},
		{name: "负页码", userID: 7, favoriteID: 9, page: -1, invalid: true},
		{name: "负数量", userID: 7, favoriteID: 9, size: -1, invalid: true},
		{name: "数量超限", userID: 7, favoriteID: 9, size: 101, invalid: true},
		{name: "偏移溢出", userID: 7, favoriteID: 9, page: math.MaxInt32, size: 100, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &favoritePageRepo{}
			uc := NewVideoInfoUsecase(repo, nil, log.NewStdLogger(io.Discard))
			_, _, page, size, err := uc.GetFavoriteVideos(context.Background(), tc.userID, tc.favoriteID, tc.page, tc.size)
			if tc.invalid {
				if err == nil || repo.calls != 0 {
					t.Fatal("非法请求访问了仓储")
				}
				return
			}
			wantPage, wantSize := tc.page, tc.size
			if wantPage == 0 {
				wantPage = 1
			}
			if wantSize == 0 {
				wantSize = 20
			}
			if err != nil || repo.calls != 1 || repo.userID != 7 || repo.favoriteID != 9 || page != wantPage || size != wantSize || repo.page != page || repo.size != size {
				t.Fatal("分页参数或身份传递错误")
			}
			repo.err = kerrors.NotFound("FAVORITE_NOT_FOUND", "收藏夹不存在")
			_, _, _, _, err = uc.GetFavoriteVideos(context.Background(), 7, 9, 1, 20)
			if !errors.Is(err, repo.err) {
				t.Fatal("权限错误语义被改变")
			}
		})
	}
}

// TestDetailFavoriteUsesRelation 验证收藏状态由关系与所属 ID 一起确定，不能沿用过期的 MongoDB 收藏位。
func TestDetailFavoriteUsesRelation(t *testing.T) {
	for _, favoriteID := range []int64{0, 9007199254740993} {
		repo := &optionalViewerRepo{favoriteID: favoriteID}
		uc := NewVideoInfoUsecase(repo, optionalViewerTransaction{}, log.NewStdLogger(io.Discard))
		resp, err := uc.GetVideoInfoById(context.Background(), 42, 7)
		if err != nil {
			t.Fatal(err)
		}
		want := int32(-1)
		if favoriteID > 0 {
			want = 1
		}
		if resp.UserVideoStatus.IsFavorited != want || resp.UserVideoStatus.FavoriteId != favoriteID || resp.UserVideoStatus.IsUpvoted != 1 {
			t.Fatal("收藏状态与所属 ID 不一致或改变了其他状态")
		}
		wantRecord := int64(0)
		if favoriteID > 0 {
			wantRecord = favoriteID + 1
		}
		if resp.UserVideoStatus.FavoriteRecordId != wantRecord {
			t.Fatal("收藏记录 ID 丢失")
		}
	}
}
