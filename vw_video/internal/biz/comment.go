package biz

import (
	"context"
	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"google.golang.org/protobuf/types/known/timestamppb"
	"math"
	"strings"
	"unicode/utf8"
	"util/dbutil/mysqlutil"
	"util/iputil"
	commentv1 "vw_video/api/v1/comment"
	"vw_video/internal/data/dal/model"
)

const maxCommentContentRunes = 5000

// VideoCommentRepo 定义平铺评论、批量引用及个人点赞的存储边界。
type VideoCommentRepo interface {
	GetCommentPage(ctx context.Context, query *CommentQuery) ([]*model.Comment, int64, error)
	GetCommentReferences(ctx context.Context, videoID, rootID int64, ids []int64) ([]*model.Comment, error)
	GetCommentLikes(ctx context.Context, userID, videoID int64, ids []int64) (map[int64]bool, error)
	GetRootIDByParentID(ctx context.Context, videoID, parentID int64) (int64, error)
	InsertComment(ctx context.Context, comment *model.Comment) (int64, error)
	IncrementCommentCntReplies(ctx context.Context, rootID int64) error
	AddVideoHotAndCntCommentsByComment(ctx context.Context, videoID int64) error
	AddVideoHotAndCntCommentsByReply(ctx context.Context, videoID int64) error
	GetCommentInfo(ctx context.Context, commentID int64) (*model.Comment, error)
	SetCommentLike(ctx context.Context, userID, videoID, commentID int64, liked bool) (*commentv1.UpvoteCommentResp, error)
}

// VideoCommentUsecase 负责两层评论读取与本地事务写入。
type VideoCommentUsecase struct {
	repo VideoCommentRepo
	tx   mysqlutil.Transaction
	log  *log.Helper
}

// NewVideoCommentUsecase 装配评论仓储与事务管理器，不连接外部服务。
func NewVideoCommentUsecase(repo VideoCommentRepo, tx mysqlutil.Transaction, logger log.Logger) *VideoCommentUsecase {
	return &VideoCommentUsecase{repo: repo, tx: tx, log: log.NewHelper(logger)}
}

// CreateComment 验证回复归属后创建评论，整楼回复数和视频计数在同一事务内更新。
// 输入：parentID=-1 表示主评论；其他 ID 必须指向同视频下的有效评论。
func (u *VideoCommentUsecase) CreateComment(ctx context.Context, videoID, parentID, publisherID int64, content, ipAddr string) (int64, error) {
	invalid := kerrors.BadRequest("INVALID_COMMENT", "评论参数无效")
	if videoID <= 0 || publisherID <= 0 {
		return 0, invalid
	}
	// 主评论使用 -1；回复必须明确指定有效的直接回复对象。
	if parentID != -1 && parentID <= 0 {
		return 0, invalid
	}
	if strings.TrimSpace(content) == "" || utf8.RuneCountInString(content) > maxCommentContentRunes {
		return 0, invalid
	}
	country, city, err := iputil.GetCountryAndCity(ipAddr)
	if err != nil {
		return 0, err
	}
	if country == "" {
		country = "未知地区"
	}
	var id int64
	err = u.tx.WithTx(ctx, func(txctx context.Context) error {
		rootID, err := u.repo.GetRootIDByParentID(txctx, videoID, parentID)
		if err != nil {
			return err
		}
		row := &model.Comment{
			VideoID:     videoID,
			ParentID:    parentID,
			RootID:      rootID,
			PublisherID: publisherID,
			Content:     content,
			IPLocation:  strings.TrimSpace(country + " " + city),
		}
		id, err = u.repo.InsertComment(txctx, row)
		if err != nil {
			return err
		}
		if parentID == -1 {
			return u.repo.AddVideoHotAndCntCommentsByComment(txctx, videoID)
		}
		if err = u.repo.IncrementCommentCntReplies(txctx, rootID); err != nil {
			return err
		}
		return u.repo.AddVideoHotAndCntCommentsByReply(txctx, videoID)
	})
	return id, err
}

// GetCommentList 返回固定三十条主评论及个人点赞状态，不附带回复。
func (u *VideoCommentUsecase) GetCommentList(ctx context.Context, req *commentv1.GetCommentListReq) (*commentv1.GetCommentListResp, error) {
	q, err := NewCommentQuery(req.VideoId, 0, req.UserId, req.SortBy, req.Order, req.Cursor)
	if err != nil {
		return nil, err
	}
	items, total, next, more, err := u.readCommentPage(ctx, q)
	if err != nil {
		return nil, err
	}
	if total > math.MaxInt32 {
		return nil, kerrors.InternalServer("COMMENT_COUNT_OVERFLOW", "评论总数超出响应范围")
	}
	return &commentv1.GetCommentListResp{
		Comments:   items,
		TotalCount: int32(total),
		NextCursor: next,
		HasMore:    more,
	}, nil
}

// GetCommentReplies 按主评论读取整楼平铺回复，不局限于直接子回复。
func (u *VideoCommentUsecase) GetCommentReplies(ctx context.Context, req *commentv1.GetCommentRepliesReq) (*commentv1.GetCommentRepliesResp, error) {
	q, err := NewCommentQuery(req.VideoId, req.CommentId, req.UserId, req.SortBy, req.Order, req.Cursor)
	if err != nil {
		return nil, err
	}
	if req.CommentId <= 0 {
		return nil, kerrors.BadRequest("INVALID_ROOT_COMMENT", "必须提供主评论 ID")
	}
	root, err := u.repo.GetCommentInfo(ctx, req.CommentId)
	if err != nil {
		return nil, err
	}
	if root.VideoID != req.VideoId {
		return nil, kerrors.NotFound("COMMENT_NOT_FOUND", "主评论不存在")
	}
	if root.ParentID != -1 || root.RootID != root.CommentID {
		return nil, kerrors.NotFound("COMMENT_NOT_FOUND", "主评论不存在")
	}
	items, _, next, more, err := u.readCommentPage(ctx, q)
	if err != nil {
		return nil, err
	}
	return &commentv1.GetCommentRepliesResp{
		Replies:    items,
		NextCursor: next,
		HasMore:    more,
	}, nil
}

// readCommentPage 批量补齐本页点赞和直接引用；多读一条仅用于判断下一页。
func (u *VideoCommentUsecase) readCommentPage(ctx context.Context, q *CommentQuery) ([]*commentv1.CommentInfo, int64, *commentv1.CommentCursor, bool, error) {
	rows, total, err := u.repo.GetCommentPage(ctx, q)
	if err != nil {
		return nil, 0, nil, false, err
	}
	more := len(rows) > CommentPageSize
	if more {
		rows = rows[:CommentPageSize]
	}
	ids := make([]int64, 0, len(rows))
	parentIDs := make([]int64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.CommentID)
		if row.ParentID > 0 {
			parentIDs = append(parentIDs, row.ParentID)
		}
	}
	likes, err := u.repo.GetCommentLikes(ctx, q.UserID, q.VideoID, ids)
	if err != nil {
		return nil, 0, nil, false, err
	}
	parents, err := u.repo.GetCommentReferences(ctx, q.VideoID, q.RootID, parentIDs)
	if err != nil {
		return nil, 0, nil, false, err
	}
	refs := make(map[int64]*model.Comment, len(parents))
	for _, row := range parents {
		refs[row.CommentID] = row
	}
	items := make([]*commentv1.CommentInfo, 0, len(rows))
	for _, row := range rows {
		item := &commentv1.CommentInfo{
			CommentId:   row.CommentID,
			PublisherId: row.PublisherID,
			RootId:      row.RootID,
			ParentId:    row.ParentID,
			Content:     row.Content,
			CreatedAt:   timestamppb.New(row.CreatedAt),
			UpvoteCount: row.CntUpvote,
			IpAddress:   row.IPLocation,
			CntReplies:  uint32(row.CntReplies),
			IsUpvoted:   likes[row.CommentID],
		}
		if row.ParentID > 0 {
			// 引用不递归展开；不可见对象保留 ID，但不返回发布者或正文。
			item.Reference = &commentv1.CommentReference{CommentId: row.ParentID, Unavailable: true}
			if parent := refs[row.ParentID]; parent != nil {
				item.Reference.PublisherId = parent.PublisherID
				item.Reference.Content = parent.Content
				item.Reference.Unavailable = false
			}
		}
		items = append(items, item)
	}
	// 第 31 条只证明还有数据；以已返回的第 30 条作为续查起点，避免漏掉预读条目。
	// 不足一批或恰好一批且无预读条目时，返回 nil，表示没有下一批游标。
	var next *commentv1.CommentCursor
	if more {
		next = q.NextCursor(rows[len(rows)-1])
	}
	return items, total, next, more, nil
}

// UpvoteComment 在本地事务中设置明确目标，重复操作不重复调整计数。
func (u *VideoCommentUsecase) UpvoteComment(ctx context.Context, req *commentv1.UpvoteCommentReq) (*commentv1.UpvoteCommentResp, error) {
	invalid := kerrors.BadRequest("INVALID_COMMENT_LIKE", "必须提供身份、视频、评论及点赞目标状态")
	if req.UserId <= 0 || req.VideoId <= 0 {
		return nil, invalid
	}
	if req.CommentId <= 0 {
		return nil, invalid
	}
	if req.Liked == nil {
		return nil, invalid
	}
	var result *commentv1.UpvoteCommentResp
	err := u.tx.WithTx(ctx, func(txctx context.Context) error {
		var err error
		result, err = u.repo.SetCommentLike(txctx, req.UserId, req.VideoId, req.CommentId, req.GetLiked())
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, err
}

// CheckIfUserUpvotedComment 查询单条有效评论的个人状态，不创建记录。
func (u *VideoCommentUsecase) CheckIfUserUpvotedComment(ctx context.Context, commentID, userID int64) (bool, error) {
	if commentID <= 0 || userID <= 0 {
		return false, ErrInvalidInteractionIdentity
	}
	row, err := u.repo.GetCommentInfo(ctx, commentID)
	if err != nil {
		return false, err
	}
	states, err := u.repo.GetCommentLikes(ctx, userID, row.VideoID, []int64{commentID})
	return states[commentID], err
}

// GetPublisherId 读取有效评论的发布者，不返回已删除评论的信息。
func (u *VideoCommentUsecase) GetPublisherId(ctx context.Context, commentID int64) (int64, error) {
	row, err := u.repo.GetCommentInfo(ctx, commentID)
	if err != nil {
		return 0, err
	}
	return row.PublisherID, nil
}
