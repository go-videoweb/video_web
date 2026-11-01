package data

import (
	"context"
	"errors"
	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"gorm.io/gorm"
	"vw_video/internal/biz"
	"vw_video/internal/data/dal/model"
)

type videoCommentRepo struct {
	data *Data
	log  *log.Helper
}

// NewVideoCommentRepo 装配评论仓储，所有 SQL 经 getQuery 取得请求连接。
func NewVideoCommentRepo(data *Data, logger log.Logger) biz.VideoCommentRepo {
	return &videoCommentRepo{data: data, log: log.NewHelper(logger)}
}

// InsertComment 保存评论；主评论写入自身 root_id，与创建事务一起提交。
func (v *videoCommentRepo) InsertComment(ctx context.Context, row *model.Comment) (int64, error) {
	q := getQuery(ctx).Comment
	if err := q.WithContext(ctx).Create(row); err != nil {
		return 0, err
	}
	if row.ParentID == -1 {
		result, err := q.WithContext(ctx).Where(q.CommentID.Eq(row.CommentID)).Updates(map[string]any{"root_id": row.CommentID})
		if err != nil {
			return 0, err
		}
		if result.RowsAffected != 1 {
			return 0, kerrors.Conflict("COMMENT_ROOT_CHANGED", "主评论创建失败")
		}
	}
	return row.CommentID, nil
}

// GetRootIDByParentID 校验视频、直接回复对象及所属主评论均有效，不允许跨视频回复。
// 输入：parentID=-1 表示主评论；输出：主评论创建前返回 -1，否则返回整楼根 ID。
func (v *videoCommentRepo) GetRootIDByParentID(ctx context.Context, videoID, parentID int64) (int64, error) {
	if err := requireVideo(ctx, videoID); err != nil {
		return 0, err
	}
	if parentID == -1 {
		return -1, nil
	}
	q := getQuery(ctx).Comment
	parent, err := q.WithContext(ctx).Where(q.VideoID.Eq(videoID), q.CommentID.Eq(parentID)).First()
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, kerrors.NotFound("COMMENT_NOT_FOUND", "回复对象不存在")
	}
	if err != nil {
		return 0, err
	}
	root, err := q.WithContext(ctx).Where(q.VideoID.Eq(videoID), q.CommentID.Eq(parent.RootID), q.ParentID.Eq(-1)).First()
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, kerrors.NotFound("COMMENT_NOT_FOUND", "主评论不存在")
	}
	if err != nil {
		return 0, err
	}
	if root.RootID != root.CommentID {
		return 0, kerrors.Conflict("INVALID_COMMENT_ROOT", "评论楼层关系异常")
	}
	return root.CommentID, nil
}

// IncrementCommentCntReplies 原子增加整楼回复数，不再更新直接父回复的计数。
func (v *videoCommentRepo) IncrementCommentCntReplies(ctx context.Context, rootID int64) error {
	q := getQuery(ctx).Comment
	result, err := q.WithContext(ctx).Where(q.CommentID.Eq(rootID), q.ParentID.Eq(-1)).Updates(map[string]any{"cnt_replies": gorm.Expr("cnt_replies + 1")})
	if err != nil {
		return err
	}
	if result.RowsAffected != 1 {
		return kerrors.Conflict("COMMENT_ROOT_CHANGED", "主评论已变化")
	}
	return nil
}

// AddVideoHotAndCntCommentsByComment 在调用方事务内增加视频评论数和热度。
func (v *videoCommentRepo) AddVideoHotAndCntCommentsByComment(ctx context.Context, videoID int64) error {
	return incrementCommentVideoCount(ctx, videoID)
}

// AddVideoHotAndCntCommentsByReply 在调用方事务内增加视频总评论数和回复热度。
func (v *videoCommentRepo) AddVideoHotAndCntCommentsByReply(ctx context.Context, videoID int64) error {
	return incrementCommentVideoCount(ctx, videoID)
}

// incrementCommentVideoCount 使用原子表达式维护计数及版本，更新失败交给事务回滚。
func incrementCommentVideoCount(ctx context.Context, videoID int64) error {
	q := getQuery(ctx).Video
	result, err := q.WithContext(ctx).Where(q.VideoID.Eq(videoID)).Updates(map[string]any{
		"cnt_comments": gorm.Expr("cnt_comments + 1"),
		"hot":          gorm.Expr("hot + ?", HotEachReply),
	})
	if err != nil {
		return err
	}
	if result.RowsAffected != 1 {
		return kerrors.Conflict("VIDEO_CHANGED", "视频已变化")
	}
	return nil
}

// GetCommentInfo 返回有效评论，软删除或不存在统一返回 404。
func (v *videoCommentRepo) GetCommentInfo(ctx context.Context, commentID int64) (*model.Comment, error) {
	q := getQuery(ctx).Comment
	row, err := q.WithContext(ctx).Where(q.CommentID.Eq(commentID)).First()
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, kerrors.NotFound("COMMENT_NOT_FOUND", "评论不存在")
	}
	return row, err
}
