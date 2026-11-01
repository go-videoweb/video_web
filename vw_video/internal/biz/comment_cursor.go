package biz

import (
	"math"
	"time"
	commentv1 "vw_video/api/v1/comment"
	"vw_video/internal/data/dal/model"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	"google.golang.org/protobuf/types/known/timestamppb"
)

// CommentPageSize 是主评论和楼内回复统一的固定批次上限。
const CommentPageSize = 30

// CommentQuery 保存经过校验的排序、楼层范围和游标，不接受任意 SQL 排序表达式。
type CommentQuery struct {
	VideoID int64          // 目标视频 ID。
	RootID  int64          // 0 查询主评论，正数查询指定主评论下的整楼回复。
	UserID  int64          // 当前鉴权用户 ID，仅用于读取本批个人点赞状态。
	SortBy  string         // 已校验的排序字段：created_at 或 cnt_upvote。
	Order   string         // 已校验的排序方向：asc 或 desc。
	After   *CommentCursor // 上一批末条记录的位置；nil 表示首批。
}

// CommentCursor 绑定视频、楼层和排序；点赞数是翻页值，不构成排名快照。
type CommentCursor struct {
	VideoID   int64     `json:"video_id"`     // 原查询的视频 ID。
	RootID    int64     `json:"root_id"`      // 原查询的楼层，主评论列表为 0。
	SortBy    string    `json:"sort_by"`      // 原查询的排序字段。
	Order     string    `json:"order"`        // 原查询的排序方向。
	ID        int64     `json:"comment_id"`   // 上一批末条评论 ID，用于同值排序。
	CreatedAt time.Time `json:"created_at"`   // 末条评论创建时间，供时间排序比较。
	Likes     uint64    `json:"upvote_count"` // 末条评论读取时的点赞数，供热度排序比较。
}

// NewCommentQuery 校验游标与查询一致，主评论默认倒序、回复默认正序。
func NewCommentQuery(videoID, rootID, userID int64, sortBy, order string, cursor *commentv1.CommentCursor) (*CommentQuery, error) {
	invalid := kerrors.BadRequest("INVALID_COMMENT_QUERY", "评论范围、排序或游标无效")
	if videoID <= 0 || userID <= 0 {
		return nil, invalid
	}
	// rootID=0 表示查询主评论列表，正数表示读取指定楼层。
	if rootID < 0 {
		return nil, invalid
	}
	if sortBy == "" {
		sortBy = "created_at"
	}
	if order == "" {
		order = "desc"
		if rootID > 0 && sortBy == "created_at" {
			order = "asc"
		}
	}

	// 目前支持按评论时间和热度(按点赞数降序)进行排序
	if sortBy != "created_at" && sortBy != "cnt_upvote" {
		return nil, invalid
	}
	if order != "asc" && order != "desc" {
		return nil, invalid
	}
	if sortBy == "cnt_upvote" && order != "desc" {
		return nil, invalid
	}
	q := &CommentQuery{
		VideoID: videoID,
		RootID:  rootID,
		UserID:  userID,
		SortBy:  sortBy,
		Order:   order,
	}
	if cursor == nil {
		return q, nil
	}
	// 协议对象只在入口转换；仓储仍使用普通 Go 时间和明确字段进行比较。
	c := &CommentCursor{
		VideoID: cursor.VideoId,
		RootID:  cursor.RootId,
		SortBy:  cursor.SortBy,
		Order:   cursor.Order,
		ID:      cursor.CommentId,
		Likes:   cursor.UpvoteCount,
	}

	// 游标只能接着原查询使用，不能在切换视频、楼层或排序后继续翻页。
	if c.VideoID != videoID || c.RootID != rootID {
		return nil, invalid
	}
	if c.SortBy != sortBy || c.Order != order {
		return nil, invalid
	}

	// ID 用于打破同值排序的并列；点赞列是 MySQL INT UNSIGNED。
	if c.ID <= 0 {
		return nil, invalid
	}
	if c.Likes > math.MaxUint32 {
		return nil, invalid
	}
	if sortBy == "created_at" {
		// Timestamp.AsTime 会归一化非法值，因此必须先校验协议时间，不能直接转换。
		if cursor.CreatedAt == nil || cursor.CreatedAt.CheckValid() != nil {
			return nil, invalid
		}
		c.CreatedAt = cursor.CreatedAt.AsTime()
		if c.CreatedAt.IsZero() {
			return nil, invalid
		}
		// MySQL DATETIME 的有效年份为 1000-9999，不能将非法时间传入比较条件。
		year := c.CreatedAt.Year()
		if year < 1000 || year > 9999 {
			return nil, invalid
		}
	}
	q.After = c
	return q, nil
}

// NextCursor 从本批最后一条已返回评论构造下一批的位置，不进行字符串编解码。
// 不能使用第 31 条预读记录，否则下次的严格比较会跳过这条尚未返回的评论。
func (q *CommentQuery) NextCursor(row *model.Comment) *commentv1.CommentCursor {
	return &commentv1.CommentCursor{
		VideoId:     q.VideoID,
		RootId:      q.RootID,
		SortBy:      q.SortBy,
		Order:       q.Order,
		CommentId:   row.CommentID,
		CreatedAt:   timestamppb.New(row.CreatedAt),
		UpvoteCount: row.CntUpvote,
	}
}
