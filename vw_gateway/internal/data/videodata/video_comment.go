package videodata

import (
	"context"
	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	vcommentv1 "vw_gateway/api/v1/video/video_comment"
	"vw_gateway/internal/biz/videobiz"
	rpc "vw_video/api/v1/comment"
)

type videoCommentRepo struct {
	data *Data
	log  *log.Helper
}

// NewVideoCommentRepo 装配视频评论 RPC 客户端。
func NewVideoCommentRepo(data *Data, logger log.Logger) videobiz.VideoCommentRepo {
	return &videoCommentRepo{data: data, log: log.NewHelper(logger)}
}

// PublishVideoComment 透传可信发布者身份与评论内容。
func (v *videoCommentRepo) PublishVideoComment(ctx context.Context, userID, videoID, parentID int64, content, ip string) (int64, error) {
	resp, err := v.data.videoCommentClient.PublishComment(ctx, &rpc.PublishCommentReq{
		PublisherId: userID,
		VideoId:     videoID,
		ParentId:    parentID,
		Content:     content,
		IpAddr:      ip,
	})
	if err != nil {
		return 0, err
	}
	if resp == nil {
		return 0, kerrors.InternalServer("INVALID_VIDEO_RESPONSE", "视频服务未返回评论结果")
	}
	return resp.CommentId, nil
}

// GetCommentList 透传游标并保留个人状态与总数，不隐式加载回复。
func (v *videoCommentRepo) GetCommentList(ctx context.Context, userID int64, req *vcommentv1.GetCommentListReq) (*vcommentv1.GetCommentListResp, error) {
	resp, err := v.data.videoCommentClient.GetCommentList(ctx, &rpc.GetCommentListReq{
		VideoId: req.VideoId,
		UserId:  userID,
		Cursor:  commentCursorToRPC(req.Cursor),
		SortBy:  req.SortBy,
		Order:   req.Order,
	})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, kerrors.InternalServer("INVALID_VIDEO_RESPONSE", "视频服务未返回评论列表")
	}
	return &vcommentv1.GetCommentListResp{
		Comments:   convert(resp.Comments),
		TotalCount: resp.TotalCount,
		NextCursor: commentCursorFromRPC(resp.NextCursor),
		HasMore:    resp.HasMore,
	}, nil
}

// GetCommentReplies 保留视频和主评论范围，返回平铺条目及引用。
func (v *videoCommentRepo) GetCommentReplies(ctx context.Context, userID int64, req *vcommentv1.GetCommentRepliesReq) (*vcommentv1.GetCommentRepliesResp, error) {
	resp, err := v.data.videoCommentClient.GetCommentReplies(ctx, &rpc.GetCommentRepliesReq{
		VideoId:   req.VideoId,
		CommentId: req.CommentId,
		UserId:    userID,
		Cursor:    commentCursorToRPC(req.Cursor),
		SortBy:    req.SortBy,
		Order:     req.Order,
	})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, kerrors.InternalServer("INVALID_VIDEO_RESPONSE", "视频服务未返回回复列表")
	}
	return &vcommentv1.GetCommentRepliesResp{
		Replies:    convert(resp.Replies),
		NextCursor: commentCursorFromRPC(resp.NextCursor),
		HasMore:    resp.HasMore,
	}, nil
}

// commentCursorToRPC 逐字段传递结构化游标，nil 表示首批，不补造空游标。
func commentCursorToRPC(cursor *vcommentv1.CommentCursor) *rpc.CommentCursor {
	if cursor == nil {
		return nil
	}
	return &rpc.CommentCursor{
		VideoId:     cursor.VideoId,
		RootId:      cursor.RootId,
		SortBy:      cursor.SortBy,
		Order:       cursor.Order,
		CommentId:   cursor.CommentId,
		CreatedAt:   cursor.CreatedAt,
		UpvoteCount: cursor.UpvoteCount,
	}
}

// commentCursorFromRPC 保留所有分页位置和范围字段，nil 表示无下一批。
func commentCursorFromRPC(cursor *rpc.CommentCursor) *vcommentv1.CommentCursor {
	if cursor == nil {
		return nil
	}
	return &vcommentv1.CommentCursor{
		VideoId:     cursor.VideoId,
		RootId:      cursor.RootId,
		SortBy:      cursor.SortBy,
		Order:       cursor.Order,
		CommentId:   cursor.CommentId,
		CreatedAt:   cursor.CreatedAt,
		UpvoteCount: cursor.UpvoteCount,
	}
}

// UpvoteComment 单次调用视频服务，不调用用户计数接口或提交 Saga。
func (v *videoCommentRepo) UpvoteComment(ctx context.Context, userID int64, req *vcommentv1.UpvoteCommentReq) (*vcommentv1.UpvoteCommentResp, error) {
	resp, err := v.data.videoCommentClient.UpvoteComment(ctx, &rpc.UpvoteCommentReq{
		CommentId: req.CommentId,
		VideoId:   req.VideoId,
		UserId:    userID,
		Liked:     req.Liked,
	})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, kerrors.InternalServer("INVALID_VIDEO_RESPONSE", "视频服务未返回点赞结果")
	}
	return &vcommentv1.UpvoteCommentResp{
		StatusCode:  200,
		Message:     "操作成功",
		IsUpvoted:   resp.IsUpvoted,
		UpvoteCount: resp.UpvoteCount,
	}, nil
}

// convert 映射当前页及一层引用，完整正文不截断，空列表返回空数组。
func convert(comments []*rpc.CommentInfo) []*vcommentv1.CommentInfo {
	result := make([]*vcommentv1.CommentInfo, 0, len(comments))
	for _, row := range comments {
		item := &vcommentv1.CommentInfo{
			CommentId:   row.CommentId,
			PublisherId: row.PublisherId,
			RootId:      row.RootId,
			ParentId:    row.ParentId,
			Content:     row.Content,
			CreatedAt:   row.CreatedAt,
			UpvoteCount: row.UpvoteCount,
			IpAddress:   row.IpAddress,
			CntReplies:  row.CntReplies,
			IsUpvoted:   row.IsUpvoted,
		}
		if ref := row.Reference; ref != nil {
			item.Reference = &vcommentv1.CommentReference{
				CommentId:   ref.CommentId,
				PublisherId: ref.PublisherId,
				Content:     ref.Content,
				Unavailable: ref.Unavailable,
			}
		}
		result = append(result, item)
	}
	return result
}
