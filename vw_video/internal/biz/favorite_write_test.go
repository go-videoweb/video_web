package biz

import (
	"context"
	"errors"
	"fmt"
	"testing"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	"vw_video/internal/data/dal/model"
)

type favoriteTxKey struct{}

type localFavoriteRepo struct {
	InteractRepo
	beginErr                                                           error
	t                                                                  *testing.T
	owner                                                              int64
	relation                                                           *model.FavoriteVideo
	count, status                                                      int64
	failAt                                                             string
	transactions, commits, rollbacks, creates, deletes                 int
	metadataCalls, videoReads, relationReads, statusWrites, countCalls int
	versionFailures                                                    int
	commitErr                                                          error
	afterRollback                                                      func(*localFavoriteRepo)
}

// failure 校验仓储接收了事务回调的上下文，并按步骤注入失败。
func (r *localFavoriteRepo) failure(ctx context.Context, step string) error {
	r.t.Helper()
	if ctx.Value(favoriteTxKey{}) != r {
		r.t.Fatal("repository escaped local transaction")
	}
	if r.failAt == step {
		return errors.New(step)
	}
	return nil
}

// GetFavoriteMetadata 模拟事务外用户 RPC，不允许在持有事务时查询用户服务。
func (r *localFavoriteRepo) GetFavoriteMetadata(ctx context.Context, id int64) (*FavoriteMetadata, error) {
	r.metadataCalls++
	if ctx.Value(favoriteTxKey{}) != nil {
		r.t.Fatal("RPC inside transaction")
	}
	if r.failAt == "rpc" {
		return nil, errors.New("rpc")
	}
	return &FavoriteMetadata{FavoriteID: id, UserID: r.owner, IsPrivate: true}, nil
}

// WithTx 仅模拟业务步骤的原子性；真实 GORM 生命周期另由 data 测试验证。
func (r *localFavoriteRepo) WithTx(ctx context.Context, fn func(context.Context) error) error {
	r.transactions++
	if r.beginErr != nil {
		return r.beginErr
	}
	before, count, state := r.relation, r.count, r.status
	err := fn(context.WithValue(ctx, favoriteTxKey{}, r))
	if err == nil && r.failAt == "commit" {
		err = errors.New("commit")
	}
	if err == nil && r.commitErr != nil {
		err = r.commitErr
	}
	if err != nil {
		r.relation, r.count, r.status = before, count, state
		r.rollbacks++
		if r.afterRollback != nil {
			r.afterRollback(r)
		}
		return err
	}
	r.commits++
	return nil
}

// GetPublisherIdByVideoId 模拟普通视频读取，检查每次事务重试均重新访问视频。
func (r *localFavoriteRepo) GetPublisherIdByVideoId(ctx context.Context, id int64) (int64, error) {
	r.videoReads++
	if id != 9 {
		r.t.Fatal("wrong video ID")
	}
	return 7, r.failure(ctx, "video")
}

// GetFavoriteRelation 返回现有关系；无关系不初始化。
func (r *localFavoriteRepo) GetFavoriteRelation(ctx context.Context, _, _ int64) (*model.FavoriteVideo, error) {
	r.relationReads++
	return r.relation, r.failure(ctx, "read")
}

// CreateFavoriteRelation 验证新 ID 在业务层生成，并保存本次关系。
func (r *localFavoriteRepo) CreateFavoriteRelation(ctx context.Context, row *model.FavoriteVideo) error {
	if err := r.failure(ctx, "create"); err != nil {
		return err
	}
	if row.ID <= 0 || row.UserID != 7 || row.VideoID != 9 || row.FavoritesID != 13 {
		r.t.Fatal("invalid generated relation")
	}
	r.creates++
	r.relation = row
	return nil
}

// DeleteFavoriteRelation 只删除业务层选中的记录，检查没有丢失代际 ID。
func (r *localFavoriteRepo) DeleteFavoriteRelation(ctx context.Context, row *model.FavoriteVideo) error {
	if err := r.failure(ctx, "delete"); err != nil {
		return err
	}
	if r.relation != row {
		r.t.Fatal("deleting wrong generation")
	}
	r.deletes++
	r.relation = nil
	return nil
}

// UpdateFavoriteCount 模拟计数变更，并可在关系已修改后失败。
func (r *localFavoriteRepo) UpdateFavoriteCount(ctx context.Context, _, delta int64) error {
	r.countCalls++
	if err := r.failure(ctx, "count"); err != nil {
		return err
	}
	if r.versionFailures != 0 {
		if r.versionFailures > 0 {
			r.versionFailures--
		}
		return fmt.Errorf("count update: %w", ErrFavoriteVersionConflict)
	}
	r.count += delta
	return nil
}

// SetFavoriteStatus 模拟只写收藏状态，并可在关系和计数已变更后失败。
func (r *localFavoriteRepo) SetFavoriteStatus(ctx context.Context, _, _ int64, value bool) error {
	r.statusWrites++
	if err := r.failure(ctx, "status"); err != nil {
		return err
	}
	r.status = 0
	if value {
		r.status = 1
	}
	return nil
}

// TestFavoriteLocalTransaction 验证新增、取消、幂等、跨夹、旧 ID 拒绝和每个失败阶段的原子性。
func TestFavoriteLocalTransaction(t *testing.T) {
	for _, mode := range []string{"collect", "repeat-collect", "cancel", "repeat-cancel", "other-folder", "old-id", "owner", "rpc", "video", "read", "create", "delete", "count", "status", "commit"} {
		t.Run(mode, func(t *testing.T) {
			r := &localFavoriteRepo{t: t, owner: 7, count: 3}
			cmd := &FavoriteCommand{UserID: 7, VideoID: 9, FavoriteID: 13, Favorite: 1}
			if mode == "repeat-collect" || mode == "cancel" || mode == "other-folder" || mode == "old-id" || mode == "delete" {
				r.relation = &model.FavoriteVideo{ID: 23, UserID: 7, VideoID: 9, FavoritesID: 13}
				r.status = 1
			}
			if mode == "cancel" || mode == "repeat-cancel" || mode == "old-id" || mode == "delete" {
				cmd.Favorite, cmd.RecordID = -1, 23
			}
			if mode == "old-id" {
				cmd.RecordID = 22
			}
			if mode == "other-folder" {
				r.relation.FavoritesID = 14
			}
			if mode == "owner" {
				r.owner = 8
			}
			switch mode {
			case "rpc", "video", "read", "create", "delete", "count", "status", "commit":
				r.failAt = mode
			}
			before, count, state := r.relation, r.count, r.status
			uc := &InteractUsecase{repo: r, tx: r}
			result, err := uc.SetVideoFavorite(context.Background(), cmd)
			failed := r.failAt != "" || mode == "other-folder" || mode == "old-id" || mode == "owner"
			if failed {
				if err == nil || result != nil || r.relation != before || r.count != count || r.status != state || r.commits != 0 {
					t.Fatalf("partial success: %v %+v", err, r)
				}
				if (mode == "owner" || mode == "rpc") && r.transactions != 0 {
					t.Fatal("rejected metadata opened transaction")
				}
				if r.transactions > 1 {
					t.Fatal("non-version failure retried")
				}
				return
			}
			if err != nil || r.commits != 1 {
				t.Fatalf("operation failed: %v", err)
			}
			if (mode == "repeat-collect" || mode == "repeat-cancel") && r.statusWrites != 0 {
				t.Fatal("idempotent operation rewrote status")
			}
			if cmd.Favorite == 1 {
				if !result.IsFavorited || result.RecordID <= 0 || result.RecordID != r.relation.ID || result.FavoriteID != 13 || r.status != 1 {
					t.Fatal("favorite state/ID mismatch")
				}
				want := int64(4)
				if mode == "repeat-collect" {
					want = 3
					if result.RecordID != 23 {
						t.Fatal("duplicate changed record ID")
					}
				}
				if r.count != want {
					t.Fatal("duplicate count")
				}
			} else if result.IsFavorited || result.RecordID != 0 || result.FavoriteID != 0 || r.relation != nil || r.status != 0 || (mode == "cancel" && r.count != 2) || (mode == "repeat-cancel" && r.count != 3) {
				t.Fatal("cancel mismatch")
			}
		})
	}
}

// TestFavoriteVersionRetry 验证版本冲突重试整个事务，关系及版本重读，用户 RPC 不重复调用。
func TestFavoriteVersionRetry(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		for _, failures := range []int{1, 2, -1} {
			t.Run(fmt.Sprintf("cancel=%v/conflicts=%d", cancel, failures), func(t *testing.T) {
				r := &localFavoriteRepo{t: t, owner: 7, count: 3, versionFailures: failures}
				cmd := &FavoriteCommand{UserID: 7, VideoID: 9, FavoriteID: 13, Favorite: 1}
				if cancel {
					cmd.Favorite, cmd.RecordID = -1, 23
					r.status = 1
					r.relation = &model.FavoriteVideo{ID: 23, UserID: 7, VideoID: 9, FavoritesID: 13}
				}
				before := r.relation
				result, err := (&InteractUsecase{repo: r, tx: r}).SetVideoFavorite(context.Background(), cmd)
				attempts := failures + 1
				if failures == -1 {
					attempts = 3
				}
				if r.transactions != attempts || r.videoReads != attempts || r.relationReads != attempts || r.countCalls != attempts || r.metadataCalls != 1 {
					t.Fatalf("transaction or RPC retry mismatch: %+v", r)
				}
				if failures == -1 {
					if !errors.Is(err, ErrFavoriteVersionConflict) || result != nil || r.rollbacks != 3 || r.commits != 0 || r.relation != before || r.count != 3 || r.statusWrites != 0 {
						t.Fatal("retry limit/rollback failed")
					}
				} else {
					if err != nil || result == nil || r.commits != 1 || r.rollbacks != failures || r.statusWrites != 1 {
						t.Fatalf("retry did not commit once: %+v %v", r, err)
					}
					want := int64(4)
					if cancel {
						want = 2
					}
					if r.count != want || (!cancel && (r.relation == nil || r.status != 1)) || (cancel && (r.relation != nil || r.status != 0)) {
						t.Fatal("retry duplicated or lost business change")
					}
				}
			})
		}
	}
}

// TestFavoriteRetryRechecksRelation 验证重试重新判断关系，不能沿用上次事务生成的结果或旧记录 ID。
func TestFavoriteRetryRechecksRelation(t *testing.T) {
	r := &localFavoriteRepo{t: t, owner: 7, count: 3, status: 1, versionFailures: 1,
		relation: &model.FavoriteVideo{ID: 23, UserID: 7, VideoID: 9, FavoritesID: 13}}
	r.afterRollback = func(r *localFavoriteRepo) {
		r.relation = &model.FavoriteVideo{ID: 24, UserID: 7, VideoID: 9, FavoritesID: 13}
		r.count = 8
	}
	cmd := &FavoriteCommand{UserID: 7, VideoID: 9, FavoriteID: 13, Favorite: -1, RecordID: 23}
	result, err := (&InteractUsecase{repo: r, tx: r}).SetVideoFavorite(context.Background(), cmd)
	if err == nil || kerrors.FromError(err).Reason != "FAVORITE_RECORD_CHANGED" || result != nil || r.transactions != 2 || r.count != 8 || r.relation.ID != 24 || r.status != 1 {
		t.Fatal("retry acted on stale relation")
	}
}

// TestFavoriteRetryDoesNotReplayCommit 验证提交阶段即使返回相同错误类型，也不能重放已执行的业务。
func TestFavoriteRetryDoesNotReplayCommit(t *testing.T) {
	for _, commitErr := range []error{errors.New("connection lost during commit"), ErrFavoriteVersionConflict} {
		r := &localFavoriteRepo{t: t, owner: 7, count: 3, commitErr: commitErr}
		cmd := &FavoriteCommand{UserID: 7, VideoID: 9, FavoriteID: 13, Favorite: 1}
		result, err := (&InteractUsecase{repo: r, tx: r}).SetVideoFavorite(context.Background(), cmd)
		if !errors.Is(err, commitErr) || result != nil || r.transactions != 1 || r.creates != 1 {
			t.Fatal("commit failure automatically retried")
		}
	}
}

// TestFavoriteRetryDoesNotReplayBegin 验证开启事务失败时不执行回调，即使错误类型相同也不重试。
func TestFavoriteRetryDoesNotReplayBegin(t *testing.T) {
	r := &localFavoriteRepo{t: t, owner: 7, beginErr: ErrFavoriteVersionConflict}
	cmd := &FavoriteCommand{UserID: 7, VideoID: 9, FavoriteID: 13, Favorite: 1}
	result, err := (&InteractUsecase{repo: r, tx: r}).SetVideoFavorite(context.Background(), cmd)
	if !errors.Is(err, ErrFavoriteVersionConflict) || result != nil || r.transactions != 1 || r.videoReads != 0 || r.creates != 0 {
		t.Fatal("begin failure retried or executed business")
	}
}

// TestFavoriteRetryCancellation 验证取消会在下次尝试前停止，不继续开启新事务。
func TestFavoriteRetryCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r := &localFavoriteRepo{t: t, owner: 7, count: 3, versionFailures: -1}
	r.afterRollback = func(*localFavoriteRepo) { cancel() }
	cmd := &FavoriteCommand{UserID: 7, VideoID: 9, FavoriteID: 13, Favorite: 1}
	result, err := (&InteractUsecase{repo: r, tx: r}).SetVideoFavorite(ctx, cmd)
	if !errors.Is(err, context.Canceled) || result != nil || r.transactions != 1 || r.rollbacks != 1 {
		t.Fatal("canceled request retried")
	}
}

// TestFavoriteRecordGeneration 验证取消后重新收藏产生新 ID，旧取消请求不能删除新关系。
func TestFavoriteRecordGeneration(t *testing.T) {
	r := &localFavoriteRepo{t: t, owner: 7}
	uc := &InteractUsecase{repo: r, tx: r}
	cmd := &FavoriteCommand{UserID: 7, VideoID: 9, FavoriteID: 13, Favorite: 1}
	first, err := uc.SetVideoFavorite(context.Background(), cmd)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Favorite, cmd.RecordID = -1, first.RecordID
	if _, err := uc.SetVideoFavorite(context.Background(), cmd); err != nil {
		t.Fatal(err)
	}
	cmd.Favorite, cmd.RecordID = 1, 0
	second, err := uc.SetVideoFavorite(context.Background(), cmd)
	if err != nil || second.RecordID == first.RecordID {
		t.Fatal("relation generation reused")
	}
	cmd.Favorite, cmd.RecordID = -1, first.RecordID
	if _, err := uc.SetVideoFavorite(context.Background(), cmd); err == nil || kerrors.FromError(err).Code != 409 || r.relation.ID != second.RecordID || r.count != 1 || r.status != 1 {
		t.Fatal("old cancellation affected new favorite")
	}
}

// TestFavoriteInvalidCommand 验证旧式省略操作、身份无效、错误记录 ID 均在访问仓储前拒绝。
func TestFavoriteInvalidCommand(t *testing.T) {
	for _, cmd := range []*FavoriteCommand{nil, {}, {UserID: 7, VideoID: 9, FavoriteID: 13}, {UserID: 7, VideoID: 9, FavoriteID: 13, Favorite: -1}, {UserID: 7, VideoID: 9, FavoriteID: 13, Favorite: 1, RecordID: 23}} {
		if _, err := (&InteractUsecase{}).SetVideoFavorite(context.Background(), cmd); err == nil || kerrors.FromError(err).Code != 400 {
			t.Fatal("invalid command accepted")
		}
	}
}
