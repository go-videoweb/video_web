package biz

import (
	"context"
	"errors"
	"io"
	"testing"

	"github.com/go-kratos/kratos/v2/log"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	"vw_video/internal/data/dal/model"
)

type optionalViewerRepo struct {
	VideoInfoRepo
	detailCalls, barrageCalls, statusCalls, historyCalls, viewCalls int
	statusUser, historyUser                                         int64
	favoriteCalls                                                   int
	favoriteID                                                      int64
	errAt                                                           string
}

func (r *optionalViewerRepo) fail(at string) error {
	if r.errAt == at {
		return errors.New(at + " failed")
	}
	return nil
}
func (r *optionalViewerRepo) GetVideoInfoById(context.Context, int64) (*model.Video, error) {
	r.detailCalls++
	return &model.Video{VideoID: 42, Title: "public detail"}, r.fail("detail")
}
func (r *optionalViewerRepo) GetBarragesByVideoId(context.Context, int64) ([]*model.Barrage, error) {
	r.barrageCalls++
	return []*model.Barrage{{BarrageID: 9, Content: "public barrage"}}, r.fail("barrage")
}
func (r *optionalViewerRepo) GetUserVideoStatus(_ context.Context, _, userID int64) (int64, error) {
	r.statusCalls++
	r.statusUser = userID
	return UpvoteStatus | FavoriteStatus, r.fail("status")
}

func (r *optionalViewerRepo) GetUserFavoriteID(_ context.Context, userID, videoID int64) (int64, error) {
	r.favoriteCalls++
	return r.favoriteID, r.fail("favorite")
}
func (r *optionalViewerRepo) AddVideoCntView(context.Context, int64) error {
	r.viewCalls++
	return r.fail("view")
}
func (r *optionalViewerRepo) UpdateUserVideoHistory(_ context.Context, _, userID int64, _ *model.VideoSummary) error {
	r.historyCalls++
	r.historyUser = userID
	return r.fail("history")
}

type optionalViewerTransaction struct{}

func (optionalViewerTransaction) WithTx(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

// TestVideoDetailOptionalViewer 验证游客保留公共详情、弹幕和播放统计，但不访问个人状态或观看历史仓储。
// 登录用户应读取个人状态并写入历史，负用户 ID 应在访问仓储前被拒绝，实际访问的仓储错误不得被吞掉。
func TestVideoDetailOptionalViewer(t *testing.T) {
	for _, tc := range []struct {
		name   string
		userID int64
		errAt  string
	}{
		{name: "anonymous"}, {name: "signed in", userID: 7}, {name: "negative ID", userID: -1},
		{name: "anonymous skips broken personal store", errAt: "status"},
		{name: "anonymous skips history", errAt: "history"},
		{name: "public detail failure", errAt: "detail"},
		{name: "public barrage failure", errAt: "barrage"},
		{name: "view failure", errAt: "view"},
		{name: "signed in status failure", userID: 7, errAt: "status"},
		{name: "signed in favorite failure", userID: 7, errAt: "favorite"},
		{name: "anonymous skips favorite store", errAt: "favorite"},
		{name: "signed in history failure", userID: 7, errAt: "history"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &optionalViewerRepo{errAt: tc.errAt, favoriteID: 91}
			uc := NewVideoInfoUsecase(repo, optionalViewerTransaction{}, log.NewStdLogger(io.Discard))
			resp, err := uc.GetVideoInfoById(context.Background(), 42, tc.userID)
			if tc.userID < 0 {
				if status.Code(err) != codes.InvalidArgument || repo.detailCalls != 0 {
					t.Fatal("negative ID was not rejected before storage")
				}
				return
			}
			wantError := tc.errAt != "" && !(tc.userID == 0 && (tc.errAt == "status" || tc.errAt == "history" || tc.errAt == "favorite"))
			if wantError {
				if err == nil || resp != nil {
					t.Fatal("storage failure was swallowed")
				}
				if tc.errAt != "history" && repo.historyCalls != 0 {
					t.Fatal("history written after earlier failure")
				}
				return
			}
			if err != nil || resp == nil || resp.VideoDetail.VideoId != 42 || len(resp.Barrages) != 1 {
				t.Fatalf("public data unavailable: %v", err)
			}
			if repo.viewCalls != 1 {
				t.Fatal("existing view count behavior changed")
			}
			if tc.userID == 0 {
				if resp.UserVideoStatus != nil || repo.statusCalls != 0 || repo.historyCalls != 0 || repo.favoriteCalls != 0 {
					t.Fatal("anonymous viewer accessed personal data")
				}
			} else if resp.UserVideoStatus == nil || resp.UserVideoStatus.IsUpvoted != 1 || resp.UserVideoStatus.IsFavorited != 1 || resp.UserVideoStatus.FavoriteId != 91 || repo.favoriteCalls != 1 || repo.statusCalls != 1 || repo.historyCalls != 1 || repo.statusUser != 7 || repo.historyUser != 7 {
				t.Fatal("authenticated behavior changed")
			}
		})
	}
}
