package data

import (
	"context"
	"github.com/go-kratos/kratos/v2/log"
	"gorm.io/gen/field"
	"vw_video/internal/biz"
	"vw_video/internal/data/dal/model"
	"vw_video/internal/data/dal/query"
)

type videoCommentRepo struct {
	data *Data
	log  *log.Helper
}

func NewVideoCommentRepo(data *Data, logger log.Logger) biz.VideoCommentRepo {
	return &videoCommentRepo{
		data: data,
		log:  log.NewHelper(logger),
	}
}

const separator = "::"

var orderMap map[string]field.Expr

func initOrderMap() {
	orderMap = map[string]field.Expr{
		"created_at" + separator + "asc":  query.Comment.CreatedAt.Asc(),
		"created_at" + separator + "desc": query.Comment.CreatedAt.Desc(),
		"cnt_upvote" + separator + "desc": query.Comment.CntUpvote.Desc(),
	}
}

func (v *videoCommentRepo) GetRootComment(ctx context.Context, videoId int64, sortBy, order string, pageNum, pageSize int32) ([]*model.Comment, error) {
	comment := getQuery(ctx).Comment
	commentDo := comment.WithContext(ctx)
	ret, err := commentDo.
		Where(comment.VideoID.Eq(videoId), comment.ParentID.Eq(-1)).
		Order(orderMap[sortBy+separator+order]).
		Offset(int((pageNum - 1) * pageSize)).Limit(int(pageSize)).
		Find()
	if err != nil {
		return nil, err
	}
	return ret, nil
}

func (v *videoCommentRepo) GetReplyComments(ctx context.Context, parentId int64, sortBy, order string, pageNum, pageSize int32) ([]*model.Comment, error) {
	comment := getQuery(ctx).Comment
	commentDo := comment.WithContext(ctx)
	ret, err := commentDo.
		Where(comment.ParentID.Eq(parentId)).
		Order(orderMap[sortBy+separator+order]).
		Offset(int((pageNum - 1) * pageSize)).
		Limit(int(pageSize)).
		Find()
	if err != nil {
		return nil, err
	}
	return ret, nil
}

func (v *videoCommentRepo) GetRepliesByParentID(ctx context.Context, parentId int64, sortBy, order string, pageNum, pageSize int32) ([]*model.Comment, error) {
	comment := getQuery(ctx).Comment
	commentDo := comment.WithContext(ctx)

	ret, err := commentDo.
		Debug().
		Where(comment.ParentID.Eq(parentId)).
		Order(orderMap[sortBy+separator+order]).
		Offset(int((pageNum - 1) * pageSize)).
		Limit(int(pageSize)).
		Find()
	if err != nil {
		return nil, err
	}
	return ret, nil
}

func (v *videoCommentRepo) InsertComment(ctx context.Context, newComment *model.Comment) (int64, error) {
	comment := getQuery(ctx).Comment
	commentDo := comment.WithContext(ctx)
	err := commentDo.Create(newComment)
	if err != nil {
		return 0, err
	}

	// If the comment is a root comment, its root_id is -1 now.
	// We need to update root_id as the same as the comment_id.
	if newComment.ParentID == -1 {
		_, err = commentDo.Where(comment.CommentID.Eq(newComment.CommentID)).Updates(map[string]any{
			"root_id": newComment.CommentID,
		})
		if err != nil {
			return 0, err
		}
	}
	return newComment.CommentID, nil
}

func (v *videoCommentRepo) GetRootIDByParentID(ctx context.Context, videoId, parentId int64) (int64, error) {
	// parent_id == -1 indicates that the comment is a root comment., so its root_id is -1.
	if parentId == -1 {
		return -1, nil
	}

	comment := getQuery(ctx).Comment
	commentDo := comment.WithContext(ctx)
	ret, err := commentDo.Select(comment.RootID).Where(comment.VideoID.Eq(videoId), comment.CommentID.Eq(parentId)).First()
	if err != nil {
		return 0, err
	}

	return ret.RootID, nil
}

// IncrementCommentCntReplies 使用上下文选定的连接调整回复数，负 ID 表示无需更新。
func (v *videoCommentRepo) IncrementCommentCntReplies(ctx context.Context, commentId int64) error {
	// This case indicated that the child comment is a root comment, so we don't need to increment the cnt_replies field.
	if commentId < 0 {
		return nil
	}
	comment := getQuery(ctx).Comment
	commentDo, commentModel, err := addCommentModel(ctx, commentId)
	if err != nil {
		return err
	}
	_, err = commentDo.Where(comment.CommentID.Eq(commentId)).Updates(
		&model.Comment{
			CntReplies: commentModel.CntReplies + 1,
		})
	return nil
}

func (v *videoCommentRepo) IncrementCommentCntUpvote(ctx context.Context, commentId int64) error {
	comment := getQuery(ctx).Comment
	commentDo, commentModel, err := addCommentModel(ctx, commentId)
	if err != nil {
		return err
	}
	_, err = commentDo.
		Where(comment.CommentID.Eq(commentId)).
		Updates(&model.Comment{
			CntUpvote: commentModel.CntUpvote + 1,
		})
	return err
}

func (v *videoCommentRepo) DecrementCommentCntUpvote(ctx context.Context, commentId int64) error {
	comment := getQuery(ctx).Comment
	commentDo, commentModel, err := addCommentModel(ctx, commentId)
	if err != nil {
		return err
	}
	_, err = commentDo.
		Where(comment.CommentID.Eq(commentId)).
		Updates(map[string]any{
			"cnt_upvote": commentModel.CntUpvote - 1,
		})
	return err
}

// AddVideoHotAndCntCommentsByComment 使用上下文选定的连接调整评论数和热度。
func (v *videoCommentRepo) AddVideoHotAndCntCommentsByComment(ctx context.Context, videoId int64) error {
	video := getQuery(ctx).Video
	videoDo, videoModel, err := addVideoModel(ctx, videoId)
	if err != nil {
		return err
	}
	_, err = videoDo.Where(video.VideoID.Eq(videoId)).Updates(
		map[string]any{
			"cnt_comments": videoModel.CntComments + 1,
			"hot":          videoModel.Hot + HotEachReply,
		})
	return nil
}

// AddVideoHotAndCntCommentsByReply 使用上下文选定的连接调整回复关联视频的评论数和热度。
func (v *videoCommentRepo) AddVideoHotAndCntCommentsByReply(ctx context.Context, videoId int64) error {
	video := getQuery(ctx).Video
	videoDo, videoModel, err := addVideoModel(ctx, videoId)
	if err != nil {
		return err
	}
	_, err = videoDo.Where(video.VideoID.Eq(videoId)).Updates(
		map[string]any{
			"cnt_comments": videoModel.CntComments + 1,
			"hot":          videoModel.Hot + HotEachReply,
		})
	return nil
}

func (v *videoCommentRepo) GetCommentInfo(ctx context.Context, commentId int64) (*model.Comment, error) {
	comment := getQuery(ctx).Comment
	commentDo := comment.WithContext(ctx)
	c, err := commentDo.Where(comment.CommentID.Eq(commentId)).First()
	if err != nil {
		return nil, err
	}
	return c, nil
}

// CommentSetUpvoted 暂不创建个人评论点赞关系。
// 输入：保留原用户/评论 ID；输出：未支持错误，不写入任何状态表。
func (v *videoCommentRepo) CommentSetUpvoted(ctx context.Context, commentId, userId int64) error {
	return biz.ErrInteractionNotSupported
}

// CommentCancelUpvoted 暂不删除个人评论点赞关系。
// 输入：保留原用户/评论 ID；输出：未支持错误，不修改点赞关系或计数。
func (v *videoCommentRepo) CommentCancelUpvoted(ctx context.Context, commentId, userId int64) error {
	return biz.ErrInteractionNotSupported
}

// CheckIfUserUpvotedComment 暂不读取个人评论点赞关系。
// 输入：保留原用户/评论 ID；输出：false 和未支持错误，不伪装成未点赞的成功响应。
func (v *videoCommentRepo) CheckIfUserUpvotedComment(ctx context.Context, commentId, userId int64) (bool, error) {
	return false, biz.ErrInteractionNotSupported
}
