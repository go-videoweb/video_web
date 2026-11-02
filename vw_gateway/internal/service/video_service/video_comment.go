package video

import (
	"context"
	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	api "vw_gateway/api/v1/video/video_comment"
	"vw_gateway/internal/biz/videobiz"
)

type CommentService struct {
	api.UnimplementedVideoCommentServer
	vcUsecase *videobiz.VideoCommentUsecase
	log       *log.Helper
}

// NewVideoCommentService 装配评论 HTTP/RPC 适配入口。
func NewVideoCommentService(usecase *videobiz.VideoCommentUsecase, logger log.Logger) *CommentService {
	return &CommentService{vcUsecase: usecase, log: log.NewHelper(logger)}
}

// interactionUserID 仅接受鉴权上下文中的正用户 ID，不读取请求中的身份字段。
func interactionUserID(ctx context.Context) (int64, error) {
	id, ok := ctx.Value(userIdKey).(int64)
	if !ok || id <= 0 {
		return 0, kerrors.Unauthorized("INVALID_USER_ID", "请先登录")
	}
	return id, nil
}

// CommentVideo 使用鉴权身份发布主评论或回复。
func (s *CommentService) CommentVideo(ctx context.Context, req *api.CommentVideoReq) (*api.CommentVideoResp, error) {
	id, err := interactionUserID(ctx)
	if err != nil {
		return nil, err
	}
	commentID, err := s.vcUsecase.CommentVideo(ctx, id, req.VideoId, req.ParentId, req.Content)
	if err != nil {
		return nil, err
	}
	return &api.CommentVideoResp{CommentId: commentID}, nil
}

// GetCommentList 传递当前用户用于批量读取个人点赞状态。
func (s *CommentService) GetCommentList(ctx context.Context, req *api.GetCommentListReq) (*api.GetCommentListResp, error) {
	id, err := interactionUserID(ctx)
	if err != nil {
		return nil, err
	}
	return s.vcUsecase.GetCommentList(ctx, id, req)
}

// GetCommentReplies 传递视频与楼层范围，不丢弃游标信息。
func (s *CommentService) GetCommentReplies(ctx context.Context, req *api.GetCommentRepliesReq) (*api.GetCommentRepliesResp, error) {
	id, err := interactionUserID(ctx)
	if err != nil {
		return nil, err
	}
	return s.vcUsecase.GetCommentReplies(ctx, id, req)
}

// UpvoteComment 传递明确目标状态，缺少操作标志时拒绝调用下游。
func (s *CommentService) UpvoteComment(ctx context.Context, req *api.UpvoteCommentReq) (*api.UpvoteCommentResp, error) {
	id, err := interactionUserID(ctx)
	if err != nil {
		return nil, err
	}
	if req.Liked == nil {
		return nil, kerrors.BadRequest("INVALID_COMMENT_LIKE", "必须提供 liked")
	}
	return s.vcUsecase.UpvoteComment(ctx, id, req)
}
