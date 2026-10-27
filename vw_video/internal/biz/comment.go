package biz

import (
	"context"
	"github.com/go-kratos/kratos/v2/log"
	"google.golang.org/protobuf/types/known/timestamppb"
	"util/dbutil/mysqlutil"
	"util/iputil"
	commentv1 "vw_video/api/v1/comment"
	"vw_video/internal/data/dal/model"
)

// VideoCommentRepo 定义评论读写及保留的个人点赞接口，后者在模型确定前明确拒绝执行。
type VideoCommentRepo interface {
	GetRootComment(ctx context.Context, videoId int64, sortBy, order string, pageNum, pageSize int32) ([]*model.Comment, error)
	GetReplyComments(ctx context.Context, parentId int64, sortBy, order string, pageNum, pageSize int32) ([]*model.Comment, error)
	GetRepliesByParentID(ctx context.Context, parentId int64, sortBy, order string, pageNum, pageSize int32) ([]*model.Comment, error)
	GetRootIDByParentID(ctx context.Context, videoId, parentId int64) (int64, error)
	InsertComment(ctx context.Context, comment *model.Comment) (int64, error)
	IncrementCommentCntReplies(ctx context.Context, commentId int64) error
	IncrementCommentCntUpvote(ctx context.Context, commentId int64) error
	DecrementCommentCntUpvote(ctx context.Context, commentId int64) error
	AddVideoHotAndCntCommentsByComment(ctx context.Context, videoId int64) error
	AddVideoHotAndCntCommentsByReply(ctx context.Context, videoId int64) error
	GetCommentInfo(ctx context.Context, commentId int64) (*model.Comment, error)

	/* 个人点赞功能暂未启用 */
	// CommentSetUpvoted 评论个人点赞设计确定后实现。
	CommentSetUpvoted(ctx context.Context, commentId int64, userId int64) error

	// CommentCancelUpvoted 评论个人点赞设计确定后实现。
	CommentCancelUpvoted(ctx context.Context, commentId int64, userId int64) error

	// CheckIfUserUpvotedComment Check if a user has upvoted a comment.
	CheckIfUserUpvotedComment(ctx context.Context, commentId int64, userId int64) (bool, error)
}

type VideoCommentUsecase struct {
	repo VideoCommentRepo
	tx   mysqlutil.Transaction
	log  *log.Helper
}

func NewVideoCommentUsecase(repo VideoCommentRepo, tx mysqlutil.Transaction, logger log.Logger) *VideoCommentUsecase {
	return &VideoCommentUsecase{
		repo: repo,
		tx:   tx,
		log:  log.NewHelper(logger),
	}
}

func (u *VideoCommentUsecase) CreateComment(ctx context.Context, videoId, parentId, publisherId int64, content string, ipAddr string) (int64, error) {
	country, city, err := iputil.GetCountryAndCity(ipAddr)
	if err != nil {
		return 0, err
	}
	if country == "" {
		country = "未知地区"
	}

	var newCommentId int64
	err = u.tx.WithTx(ctx, func(ctx context.Context) error {
		// 1. Get root id by parent id.
		rootId, err := u.repo.GetRootIDByParentID(ctx, videoId, parentId)
		if err != nil {
			return err
		}

		// 2. Insert comment.
		comment := &model.Comment{
			VideoID:     videoId,
			ParentID:    parentId,
			RootID:      rootId,
			PublisherID: publisherId,
			Content:     content,
			IPLocation:  country + " " + city,
		}
		newCommentId, err = u.repo.InsertComment(ctx, comment)
		if err != nil {
			return err
		}

		// 3. Increment cnt_replies of parent comment.
		err = u.repo.IncrementCommentCntReplies(ctx, parentId)
		if err != nil {
			return err
		}

		// 4. Increment hot and cnt_comments of video.
		if parentId == -1 { // root comment.
			err = u.repo.AddVideoHotAndCntCommentsByComment(ctx, videoId)
			if err != nil {
				return err
			}
		} else {
			err = u.repo.AddVideoHotAndCntCommentsByReply(ctx, videoId)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return -1, err
	}

	return newCommentId, nil
}

func (u *VideoCommentUsecase) GetCommentList(ctx context.Context, videoId int64, sortBy, order string, page, pageSize int32) ([]*commentv1.CommentInfo, error) {
	// 1. Get root comments. Which is the comments to the video.
	comments, err := u.repo.GetRootComment(ctx, videoId, sortBy, order, page, pageSize)
	if err != nil {
		return nil, err
	}

	// 2. Padding resp with root comments.
	result := paddingResp(comments...)
	return result, nil
}

func (u *VideoCommentUsecase) GetCommentReplies(ctx context.Context, commentId int64, sortBy, order string, pageNum, pageSize int32) ([]*commentv1.CommentInfo, error) {
	// 1. Get replies of this comment.
	replies, err := u.repo.GetRepliesByParentID(ctx, commentId, sortBy, order, pageNum, pageSize)
	if err != nil {
		return nil, err
	}

	// 2. Padding resp with replies.
	result := paddingResp(replies...)
	return result, nil
}

// UpvoteComment 在个人评论状态设计确定前拒绝点赞及取消操作。
// 输入：保留现有用户、评论和原点赞标记；输出：未支持错误，不开启事务或修改计数。
func (u *VideoCommentUsecase) UpvoteComment(ctx context.Context, commentId, userId int64, isUpvoted bool) error {
	return ErrInteractionNotSupported
}

// CheckIfUserUpvotedComment 在个人评论状态设计确定前拒绝读取。
// 输入：保留现有用户/评论 ID；输出：false 和未支持错误，false 不代表查询成功。
func (u *VideoCommentUsecase) CheckIfUserUpvotedComment(ctx context.Context, commentId, userId int64) (bool, error) {
	return false, ErrInteractionNotSupported
}

func (u *VideoCommentUsecase) GetPublisherId(ctx context.Context, commentId int64) (int64, error) {
	comment, err := u.repo.GetCommentInfo(ctx, commentId)
	if err != nil {
		return 0, err
	}
	return comment.PublisherID, nil
}

func paddingResp(comments ...*model.Comment) []*commentv1.CommentInfo {
	result := make([]*commentv1.CommentInfo, len(comments))
	for i, comment := range comments {
		result[i] = &commentv1.CommentInfo{
			CommentId:   comment.CommentID,
			PublisherId: comment.PublisherID,
			RootId:      comment.RootID,
			Content:     comment.Content,
			CreatedAt:   timestamppb.New(comment.CreatedAt),
			UpvoteCount: comment.CntUpvote,
			IpAddress:   comment.IPLocation,
			CntReplies:  uint32(comment.CntReplies),
		}
	}
	return result
}
