package data

import (
	"context"
	"errors"
	"time"
	commentv1 "vw_video/api/v1/comment"
	"vw_video/internal/biz"
	"vw_video/internal/data/dal/model"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	drivermysql "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
)

// requireVideo 验证视频仍有效，读取不增加播放量或写入观看历史。
func requireVideo(ctx context.Context, videoID int64) error {
	q := getQuery(ctx).Video
	_, err := q.WithContext(ctx).Select(q.VideoID).Where(q.VideoID.Eq(videoID)).First()
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return kerrors.NotFound("VIDEO_NOT_FOUND", "视频不存在")
	}
	return err
}

// GetCommentPage 按已校验的时间或热度游标读取，多取一条判断后续页。
// 查询与计数均限定视频和楼层，回复按 root_id 平铺且排除主评论。
func (v *videoCommentRepo) GetCommentPage(ctx context.Context, q *biz.CommentQuery) ([]*model.Comment, int64, error) {
	if err := requireVideo(ctx, q.VideoID); err != nil {
		return nil, 0, err
	}
	comment := getQuery(ctx).Comment
	query := comment.WithContext(ctx).Where(comment.VideoID.Eq(q.VideoID))
	if q.RootID == 0 {
		query = query.Where(comment.ParentID.Eq(-1))
	} else {
		query = query.Where(comment.RootID.Eq(q.RootID), comment.CommentID.Neq(q.RootID))
	}

	// 总数只受视频和楼层范围影响，不附加当前页的游标条件。
	total, err := query.Count()
	if err != nil {
		return nil, 0, err
	}
	if c := q.After; c != nil {
		// 游标保存上一批末条的 (排序值, ID)，下一批只取严格排在它后面的记录。
		// 例如热度倒序：(点赞数 < 游标点赞数) OR (点赞数相等 AND ID < 游标 ID)。
		// 时间正序使用 >，时间倒序使用 <；相等排序值依赖 ID，避免同一时刻的记录漏读。
		// 两个 OR 分支整体作为外层视频、楼层范围的 AND 条件，不能扩大查询范围。
		switch {
		case q.SortBy == "cnt_upvote":
			lowerLikes := comment.WithContext(ctx).Where(comment.CntUpvote.Lt(c.Likes))
			sameLikesAfterID := comment.WithContext(ctx).Where(
				comment.CntUpvote.Eq(c.Likes),
				comment.CommentID.Lt(c.ID),
			)
			query = query.Where(lowerLikes.Or(sameLikesAfterID))
		case q.Order == "asc":
			laterTime := comment.WithContext(ctx).Where(comment.CreatedAt.Gt(c.CreatedAt))
			sameTimeAfterID := comment.WithContext(ctx).Where(
				comment.CreatedAt.Eq(c.CreatedAt),
				comment.CommentID.Gt(c.ID),
			)
			query = query.Where(laterTime.Or(sameTimeAfterID))
		default:
			earlierTime := comment.WithContext(ctx).Where(comment.CreatedAt.Lt(c.CreatedAt))
			sameTimeAfterID := comment.WithContext(ctx).Where(
				comment.CreatedAt.Eq(c.CreatedAt),
				comment.CommentID.Lt(c.ID),
			)
			query = query.Where(earlierTime.Or(sameTimeAfterID))
		}
	}

	// 只选择已定义的类型化字段，不把 HTTP 排序参数拼进 SQL。
	switch {
	case q.SortBy == "cnt_upvote":
		query = query.Order(comment.CntUpvote.Desc(), comment.CommentID.Desc())
	case q.Order == "asc":
		query = query.Order(comment.CreatedAt.Asc(), comment.CommentID.Asc())
	default:
		query = query.Order(comment.CreatedAt.Desc(), comment.CommentID.Desc())
	}
	rows, err := query.Limit(biz.CommentPageSize + 1).Find()
	return rows, total, err
}

// GetCommentReferences 一次读取本页直接回复对象，仅返回同视频同楼的有效正文。
func (v *videoCommentRepo) GetCommentReferences(ctx context.Context, videoID, rootID int64, ids []int64) ([]*model.Comment, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	q := getQuery(ctx).Comment
	return q.WithContext(ctx).Where(q.VideoID.Eq(videoID), q.RootID.Eq(rootID), q.CommentID.In(ids...)).Find()
}

// GetCommentLikes 批量返回本页有效点赞关系，空页不访问状态表。
func (v *videoCommentRepo) GetCommentLikes(ctx context.Context, userID, videoID int64, ids []int64) (map[int64]bool, error) {
	states := make(map[int64]bool, len(ids))
	if len(ids) == 0 {
		return states, nil
	}
	q := getQuery(ctx).UserCommentLike
	rows, err := q.WithContext(ctx).Where(q.UserID.Eq(userID), q.VideoID.Eq(videoID), q.CommentID.In(ids...)).Find()
	for _, row := range rows {
		states[row.CommentID] = true
	}
	return states, err
}

// isDuplicateLike 只识别唯一键冲突，不将外键、连接等数据库错误当成重复点赞。
func isDuplicateLike(err error) bool {
	if errors.Is(err, gorm.ErrDuplicatedKey) {
		return true
	}
	var duplicate *drivermysql.MySQLError
	if !errors.As(err, &duplicate) {
		return false
	}
	return duplicate.Number == 1062
}

// SetCommentLike 在调用方事务内维护关系与计数，重复请求不再次加减。
// 计数使用原子表达式并同步递增乐观版本；失败交给业务层回滚关系变更。
func (v *videoCommentRepo) SetCommentLike(ctx context.Context, userID, videoID, commentID int64, liked bool) (*commentv1.UpvoteCommentResp, error) {
	if err := requireVideo(ctx, videoID); err != nil {
		return nil, err
	}
	q := getQuery(ctx).Comment
	row, err := q.WithContext(ctx).Where(q.VideoID.Eq(videoID), q.CommentID.Eq(commentID)).First()
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, kerrors.NotFound("COMMENT_NOT_FOUND", "评论不存在")
	}
	if err != nil {
		return nil, err
	}
	relation := getQuery(ctx).UserCommentLike
	changed := false
	if liked {
		err = relation.WithContext(ctx).Create(&model.UserCommentLike{UserID: userID, VideoID: videoID, CommentID: commentID, CreatedAt: time.Now()})
		if err != nil && !isDuplicateLike(err) {
			return nil, err
		}
		changed = err == nil
	} else {
		result, err := relation.WithContext(ctx).Where(relation.UserID.Eq(userID), relation.VideoID.Eq(videoID), relation.CommentID.Eq(commentID)).Delete()
		if err != nil {
			return nil, err
		}
		if result.RowsAffected > 1 {
			return nil, kerrors.Conflict("COMMENT_LIKE_CONFLICT", "点赞关系异常")
		}
		changed = result.RowsAffected == 1
	}
	if changed {
		delta := int64(1)
		if !liked {
			delta = -1
		}
		db := q.WithContext(ctx).UnderlyingDB().Model(&model.Comment{}).Where("video_id = ? AND comment_id = ?", videoID, commentID)
		if !liked {
			db = db.Where("cnt_upvote > 0")
		}
		result := db.Updates(map[string]any{"cnt_upvote": gorm.Expr("cnt_upvote + ?", delta)})
		if result.Error != nil {
			return nil, result.Error
		}
		if result.RowsAffected != 1 {
			return nil, kerrors.Conflict("COMMENT_LIKE_CONFLICT", "评论已变化或点赞计数异常")
		}
		row, err = q.WithContext(ctx).Where(q.CommentID.Eq(commentID), q.VideoID.Eq(videoID)).First()
		if err != nil {
			return nil, err
		}
	}
	return &commentv1.UpvoteCommentResp{IsUpvoted: liked, UpvoteCount: row.CntUpvote}, nil
}
