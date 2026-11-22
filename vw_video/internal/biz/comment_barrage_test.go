package biz

import (
	"context"
	"errors"
	"github.com/go-kratos/kratos/v2/log"
	"io"
	"strings"
	"testing"
	"time"
	comments "vw_video/api/v1/comment"
	interact "vw_video/api/v1/interact"
	"vw_video/internal/data/dal/model"
)

type commentPageRepo struct {
	VideoCommentRepo
	rows, parents []*model.Comment
	query         *CommentQuery
	ids           []int64
	fail          error
}

// GetCommentPage 记录楼层和排序范围，仅返回内存样例。
func (r *commentPageRepo) GetCommentPage(_ context.Context, q *CommentQuery) ([]*model.Comment, int64, error) {
	r.query = q
	return r.rows, 31, r.fail
}

// GetCommentReferences 返回直接回复对象，不要求对象存在于当前页。
func (r *commentPageRepo) GetCommentReferences(_ context.Context, videoID, rootID int64, ids []int64) ([]*model.Comment, error) {
	r.ids = ids
	return r.parents, nil
}

// GetCommentLikes 返回本批样例状态，不为其他对象创建记录。
func (r *commentPageRepo) GetCommentLikes(context.Context, int64, int64, []int64) (map[int64]bool, error) {
	return map[int64]bool{2: true}, nil
}

// GetCommentInfo 模拟有效主评论，不连接数据库。
func (r *commentPageRepo) GetCommentInfo(context.Context, int64) (*model.Comment, error) {
	return &model.Comment{CommentID: 1, RootID: 1, ParentID: -1, VideoID: 9}, nil
}

// TestCommentCursorScope 验证默认排序、完整游标往返和跨视频、楼层、排序拒绝。
func TestCommentCursorScope(t *testing.T) {
	for _, root := range []int64{0, 1} {
		q, err := NewCommentQuery(9, root, 7, "", "", nil)
		if err != nil {
			t.Fatal(err)
		}
		want := "desc"
		if root > 0 {
			want = "asc"
		}
		if q.Order != want {
			t.Fatal(q.Order)
		}
		cursor := q.NextCursor(&model.Comment{CommentID: 9007199254740993, CreatedAt: time.Date(2026, 10, 31, 12, 0, 0, 123000000, time.UTC), CntUpvote: 9})
		restored, err := NewCommentQuery(9, root, 7, "", "", cursor)
		if err != nil || restored.After.ID != 9007199254740993 {
			t.Fatalf("游标丢失 ID：%v", err)
		}
		for _, args := range []struct {
			video, root int64
			sort, order string
		}{{10, root, "", ""}, {9, root + 1, "", ""}, {9, root, "cnt_upvote", "desc"}} {
			if _, err := NewCommentQuery(args.video, args.root, 7, args.sort, args.order, cursor); err == nil {
				t.Fatal("跨范围游标未拒绝")
			}
		}
	}
	if _, err := NewCommentQuery(9, 0, 7, "", "", &comments.CommentCursor{}); err == nil {
		t.Fatal("显式空游标不能被当成首批")
	}
	if _, err := NewCommentQuery(9, 0, 7, "cnt_upvote", "asc", nil); err == nil {
		t.Fatal("无效排序未拒绝")
	}
}

// TestFlatRepliesWithFullReference 验证三十条平铺回复、完整一层引用与已删除占位。
func TestFlatRepliesWithFullReference(t *testing.T) {
	r := &commentPageRepo{parents: []*model.Comment{{CommentID: 80, RootID: 1, VideoID: 9, PublisherID: 88, Content: strings.Repeat("完整正文", 100)}}}
	for n := 0; n < 31; n++ {
		r.rows = append(r.rows, &model.Comment{CommentID: int64(n + 2), RootID: 1, ParentID: 80, VideoID: 9, CreatedAt: time.Unix(1700000000+int64(n), 0)})
	}
	r.rows[1].ParentID = 81
	uc := NewVideoCommentUsecase(r, optionalViewerTransaction{}, log.NewStdLogger(io.Discard))
	resp, err := uc.GetCommentReplies(context.Background(), &comments.GetCommentRepliesReq{VideoId: 9, CommentId: 1, UserId: 7})
	if err != nil || len(resp.Replies) != 30 || !resp.HasMore || resp.NextCursor == nil {
		t.Fatalf("分页错误：%v %v", resp, err)
	}
	if !resp.Replies[0].IsUpvoted || resp.Replies[0].Reference.Content != r.parents[0].Content || resp.Replies[0].ParentId != 80 {
		t.Fatal("个人状态或完整引用丢失")
	}
	if !resp.Replies[1].Reference.Unavailable || resp.Replies[1].Reference.Content != "" {
		t.Fatal("不可见引用泄露正文")
	}
	if r.query.RootID != 1 || r.query.Order != "asc" {
		t.Fatal("未按整楼查询")
	}
	q, err := NewCommentQuery(9, 1, 7, "", "", resp.NextCursor)
	if err != nil || q.After.ID != 31 {
		t.Fatal("游标使用了额外预读条目")
	}
	r.rows = nil
	r.parents = nil
	empty, err := uc.GetCommentList(context.Background(), &comments.GetCommentListReq{VideoId: 9, UserId: 7})
	if err != nil || empty.Comments == nil || len(empty.Comments) != 0 || empty.HasMore || empty.NextCursor != nil {
		t.Fatal("空页契约错误")
	}
	r.fail = errors.New("storage failure")
	if _, err = uc.GetCommentList(context.Background(), &comments.GetCommentListReq{VideoId: 9, UserId: 7}); !errors.Is(err, r.fail) {
		t.Fatal("存储错误被吞掉")
	}
}

// TestBarrageCursorAndSendValidation 验证整数秒、同秒 ID、播放区间和发送参数边界。
func TestBarrageCursorAndSendValidation(t *testing.T) {
	req := &interact.GetBarragesReq{VideoId: 9, UserId: 7, StartSecond: 0, EndSecond: 60}
	q, err := NewBarrageQuery(req)
	if err != nil {
		t.Fatal(err)
	}
	req.Cursor = q.NextCursor(&model.Barrage{BarrageID: 9007199254740993, PublishTime: 0})
	got, err := NewBarrageQuery(req)
	if err != nil || got.After.ID != 9007199254740993 || got.After.Position != 0 {
		t.Fatal("同秒游标丢失")
	}
	req.StartSecond = 1
	if _, err = NewBarrageQuery(req); err == nil {
		t.Fatal("跨区间游标未拒绝")
	}
	for _, r := range []*interact.GetBarragesReq{{VideoId: 9, UserId: 7, StartSecond: -1, EndSecond: 60}, {VideoId: 9, UserId: 7, StartSecond: 60, EndSecond: 60}, {VideoId: 9, UserId: 7, EndSecond: 3601}} {
		if _, err = NewBarrageQuery(r); err == nil {
			t.Fatal("无效范围未拒绝")
		}
	}
	send := &interact.VideoSendBarrageReq{VideoId: 9, PublisherId: 7, PositionSecond: 0, Content: "弹幕", Color: "#ffffff"}
	if err = validateBarrageSend(send); err != nil {
		t.Fatal(err)
	}
	send.PositionSecond = -1
	if err = validateBarrageSend(send); err == nil {
		t.Fatal("负秒数未拒绝")
	}
}

// TestLikeCommandsRequireExplicitTarget 验证缺少目标状态时不访问事务或仓储。
func TestLikeCommandsRequireExplicitTarget(t *testing.T) {
	if _, err := (&VideoCommentUsecase{}).UpvoteComment(context.Background(), &comments.UpvoteCommentReq{UserId: 7, VideoId: 9, CommentId: 1}); err == nil {
		t.Fatal("缺少 liked 未拒绝")
	}
	if _, err := (&InteractUsecase{}).SetBarrageLike(context.Background(), &interact.SetBarrageLikeReq{UserId: 7, VideoId: 9, BarrageId: 1}); err == nil {
		t.Fatal("缺少 liked 未拒绝")
	}
}

type publishTimeBarrageRepo struct{ InteractRepo }

// GetBarragePage 返回按真实数据库字段保存的播放秒数，不连接数据库。
func (publishTimeBarrageRepo) GetBarragePage(context.Context, *BarrageQuery) ([]*model.Barrage, error) {
	return []*model.Barrage{{BarrageID: 11, VideoID: 9, PublishTime: 37}}, nil
}

// GetBarrageLikes 提供本批个人状态，确认时间字段对齐不影响状态映射。
func (publishTimeBarrageRepo) GetBarrageLikes(context.Context, int64, int64, []int64) (map[int64]bool, error) {
	return map[int64]bool{11: true}, nil
}

// TestBarragePublishTimeMapping 验证存储 publish_time 同时映射到响应位置和翻页游标。
func TestBarragePublishTimeMapping(t *testing.T) {
	uc := NewInteractUseCase(publishTimeBarrageRepo{}, nil, optionalViewerTransaction{}, log.NewStdLogger(io.Discard))
	resp, err := uc.GetBarrages(context.Background(), &interact.GetBarragesReq{VideoId: 9, UserId: 7, StartSecond: 0, EndSecond: 60})
	if err != nil || len(resp.Barrages) != 1 || resp.Barrages[0].PositionSecond != 37 || !resp.Barrages[0].IsUpvoted {
		t.Fatalf("publish_time 映射错误：%v %v", resp, err)
	}
	q, err := NewBarrageQuery(&interact.GetBarragesReq{VideoId: 9, UserId: 7, StartSecond: 0, EndSecond: 60})
	if err != nil {
		t.Fatal(err)
	}
	cursor := q.NextCursor(&model.Barrage{BarrageID: 11, PublishTime: 37})
	restored, err := NewBarrageQuery(&interact.GetBarragesReq{VideoId: 9, UserId: 7, StartSecond: 0, EndSecond: 60, Cursor: cursor})
	if err != nil || restored.After.Position != 37 || restored.After.ID != 11 {
		t.Fatalf("游标未使用 publish_time：%v %v", restored, err)
	}
}

// barrageBatchRepo 提供可控的同秒弹幕批次，不访问真实数据库。
type barrageBatchRepo struct {
	InteractRepo                  // 未使用的方法不提供替代实现。
	rows         []*model.Barrage // 仓储已按播放位置和 ID 排序的结果，最多含一条预读数据。
}

// GetBarragePage 返回测试设置的批次，供业务层执行截断和下一游标构造。
func (r barrageBatchRepo) GetBarragePage(context.Context, *BarrageQuery) ([]*model.Barrage, error) {
	return r.rows, nil
}

// GetBarrageLikes 不提供个人点赞，验证翻页本身不会依赖状态预读结果。
func (r barrageBatchRepo) GetBarrageLikes(context.Context, int64, int64, []int64) (map[int64]bool, error) {
	return map[int64]bool{}, nil
}

// TestBarrageNextCursorUsesReturnedRow 验证预读条目不成为游标，恰好满批和空批均无下一游标。
func TestBarrageNextCursorUsesReturnedRow(t *testing.T) {
	for _, size := range []int{0, BarragePageSize, BarragePageSize + 1} {
		rows := make([]*model.Barrage, size)
		for n := range rows {
			rows[n] = &model.Barrage{VideoID: 9, BarrageID: int64(n + 1), PublishTime: 0}
		}
		uc := NewInteractUseCase(barrageBatchRepo{rows: rows}, nil, optionalViewerTransaction{}, log.NewStdLogger(io.Discard))
		response, err := uc.GetBarrages(context.Background(), &interact.GetBarragesReq{VideoId: 9, UserId: 7, EndSecond: 60})
		if err != nil {
			t.Fatal(err)
		}
		if size <= BarragePageSize {
			if response.HasMore || response.NextCursor != nil || len(response.Barrages) != size || response.Barrages == nil {
				t.Fatalf("末批错误，size=%d: %v", size, response)
			}
			continue
		}
		if len(response.Barrages) != BarragePageSize || !response.HasMore || response.NextCursor == nil {
			t.Fatal("预读结果未正确截断")
		}
		if response.NextCursor.BarrageId != int64(BarragePageSize) || response.NextCursor.PositionSecond != 0 {
			t.Fatal("游标错误地使用预读条目或丢失零秒位置")
		}
	}
}
