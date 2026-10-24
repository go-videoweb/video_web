package videodata

import (
	"context"
	"github.com/go-kratos/kratos/v2/log"
	vcommentv1 "vw_gateway/api/v1/video/video_comment"
	"vw_gateway/internal/biz/videobiz"
	vcommentGRPC "vw_video/api/v1/comment"
)

type videoCommentRepo struct {
	data *Data
	log  *log.Helper
}

func NewVideoCommentRepo(data *Data, logger log.Logger) videobiz.VideoCommentRepo {
	return &videoCommentRepo{
		data: data,
		log:  log.NewHelper(logger),
	}
}

func (v *videoCommentRepo) PublishVideoComment(ctx context.Context, publisherId int64, videoId int64, parentId int64, content string, ipAddr string) (int64, error) {
	resp, err := v.data.videoCommentClient.PublishComment(ctx, &vcommentGRPC.PublishCommentReq{
		VideoId:     videoId,
		ParentId:    parentId,
		PublisherId: publisherId,
		Content:     content,
		IpAddr:      ipAddr,
	})
	if err != nil {
		return 0, err
	}
	return resp.CommentId, nil
}

func (v *videoCommentRepo) GetCommentList(ctx context.Context, videoId int64, pageNum int32, pageSize int32, sortBy string, order string) (*vcommentv1.GetCommentListResp, error) {
	resp, err := v.data.videoCommentClient.GetCommentList(ctx, &vcommentGRPC.GetCommentListReq{
		VideoId:  videoId,
		PageNum:  pageNum,
		PageSize: pageSize,
		SortBy:   sortBy,
		Order:    order,
	})
	if err != nil {
		return nil, err
	}

	return &vcommentv1.GetCommentListResp{
		Comments:   convert(resp.Comments),
		TotalCount: resp.TotalCount,
	}, nil
}

func (v *videoCommentRepo) GetCommentReplies(ctx context.Context, commentId int64, pageNum int32, pageSize int32, sortBy string, order string) (*vcommentv1.GetCommentRepliesResp, error) {
	resp, err := v.data.videoCommentClient.GetCommentReplies(ctx, &vcommentGRPC.GetCommentRepliesReq{
		CommentId: commentId,
		PageNum:   pageNum,
		PageSize:  pageSize,
		SortBy:    sortBy,
		Order:     order,
	})
	if err != nil {
		return nil, err
	}

	return &vcommentv1.GetCommentRepliesResp{
		Replies: convert(resp.Replies),
	}, nil
}

// UpvoteComment 在创建 Saga 或修改计数前拒绝尚未启用的个人点赞。
func (v *videoCommentRepo) UpvoteComment(ctx context.Context, commentId int64, userId, publisherId int64, isUpvoted bool) error {
	return interactionNotSupported()
}

func (v *videoCommentRepo) CheckIfUserUpvotedComment(ctx context.Context, commentId, userId int64) (bool, error) {
	return false, interactionNotSupported()
}

func (v *videoCommentRepo) GetPublisherId(ctx context.Context, commentId int64) (int64, error) {
	resp, err := v.data.videoCommentClient.GetPublisherId(ctx, &vcommentGRPC.GetPublisherIdReq{
		CommentId: commentId,
	})
	if err != nil {
		return 0, err
	}
	return resp.PublisherId, nil
}

func convert(comments []*vcommentGRPC.CommentInfo) []*vcommentv1.CommentInfo {
	resp := make([]*vcommentv1.CommentInfo, len(comments))
	for i, comment := range comments {
		resp[i] = &vcommentv1.CommentInfo{
			CommentId:   comment.CommentId,
			PublisherId: comment.PublisherId,
			RootId:      comment.RootId,
			Content:     comment.Content,
			CreatedAt:   comment.CreatedAt,
			UpvoteCount: comment.UpvoteCount,
			IpAddress:   comment.IpAddress,
			CntReplies:  comment.CntReplies,
		}
	}
	return resp
}
