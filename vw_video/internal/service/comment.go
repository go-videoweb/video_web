package service

import (
	"context"
	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"google.golang.org/protobuf/types/known/emptypb"
	commentv1 "vw_video/api/v1/comment"
	"vw_video/internal/biz"
)

type VideoCommentService struct {
	commentv1.UnimplementedVideoCommentServer
	vcUsecase *biz.VideoCommentUsecase
	log       *log.Helper
}

func NewVideoCommentService(vcUsecase *biz.VideoCommentUsecase, logger log.Logger) *VideoCommentService {
	return &VideoCommentService{
		vcUsecase: vcUsecase,
		log:       log.NewHelper(logger),
	}
}

func (s *VideoCommentService) PublishComment(ctx context.Context, req *commentv1.PublishCommentReq) (*commentv1.PublishCommentResp, error) {
	commentId, err := s.vcUsecase.CreateComment(ctx, req.VideoId, req.ParentId, req.PublisherId, req.Content, req.IpAddr)
	if err != nil {
		return nil, err
	}
	return &commentv1.PublishCommentResp{CommentId: commentId}, nil
}

// GetCommentList 返回游标主评论页，不附带回复。
func (s *VideoCommentService) GetCommentList(ctx context.Context, req *commentv1.GetCommentListReq) (*commentv1.GetCommentListResp, error) {
	return s.vcUsecase.GetCommentList(ctx, req)
}

// GetCommentReplies 返回指定主评论的平铺回复页与直接引用。
func (s *VideoCommentService) GetCommentReplies(ctx context.Context, req *commentv1.GetCommentRepliesReq) (*commentv1.GetCommentRepliesResp, error) {
	return s.vcUsecase.GetCommentReplies(ctx, req)
}

// UpvoteComment 使用本地事务设置明确的点赞状态，不调用用户服务。
func (s *VideoCommentService) UpvoteComment(ctx context.Context, req *commentv1.UpvoteCommentReq) (*commentv1.UpvoteCommentResp, error) {
	return s.vcUsecase.UpvoteComment(ctx, req)
}

// UpvoteCommentRevert 拒绝旧补偿请求，避免修改本地事务已经提交的点赞。
func (s *VideoCommentService) UpvoteCommentRevert(ctx context.Context, req *commentv1.UpvoteCommentReq) (*emptypb.Empty, error) {
	return nil, kerrors.New(501, "COMMENT_SAGA_REMOVED", "评论点赞已改为本地事务，不支持旧补偿调用")
}

func (s *VideoCommentService) CheckIfUserUpvotedComment(ctx context.Context, req *commentv1.CheckIfUserUpvotedCommentReq) (*commentv1.CheckIfUserUpvotedCommentResp, error) {
	isUpvoted, err := s.vcUsecase.CheckIfUserUpvotedComment(ctx, req.CommentId, req.UserId)
	if err != nil {
		return nil, err
	}
	return &commentv1.CheckIfUserUpvotedCommentResp{IsUpvoted: isUpvoted}, nil
}

func (s *VideoCommentService) GetPublisherId(ctx context.Context, req *commentv1.GetPublisherIdReq) (*commentv1.GetPublisherIdResp, error) {
	id, err := s.vcUsecase.GetPublisherId(ctx, req.CommentId)
	if err != nil {
		return nil, err
	}
	return &commentv1.GetPublisherIdResp{PublisherId: id}, nil
}
