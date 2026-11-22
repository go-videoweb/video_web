package videobiz

import (
	"context"
	"github.com/go-kratos/kratos/v2/log"
	khttp "github.com/go-kratos/kratos/v2/transport/http"
	"util/iputil"
	vcommentv1 "vw_gateway/api/v1/video/video_comment"
	"vw_gateway/internal/pkg/ecode/errdef"
)

// VideoCommentRepo 仅转发视频服务的评论命令与查询，不在网关组合业务。
type VideoCommentRepo interface {
	PublishVideoComment(ctx context.Context, userID, videoID, parentID int64, content, ip string) (int64, error)
	GetCommentList(ctx context.Context, userID int64, req *vcommentv1.GetCommentListReq) (*vcommentv1.GetCommentListResp, error)
	GetCommentReplies(ctx context.Context, userID int64, req *vcommentv1.GetCommentRepliesReq) (*vcommentv1.GetCommentRepliesResp, error)
	UpvoteComment(ctx context.Context, userID int64, req *vcommentv1.UpvoteCommentReq) (*vcommentv1.UpvoteCommentResp, error)
}

// VideoCommentUsecase 提供评论调用入口，身份由 service 从鉴权上下文取得。
type VideoCommentUsecase struct {
	repo VideoCommentRepo
	log  *log.Helper
}

// NewVideoCommentUsecase 装配网关评论适配器。
func NewVideoCommentUsecase(repo VideoCommentRepo, logger log.Logger) *VideoCommentUsecase {
	return &VideoCommentUsecase{repo: repo, log: log.NewHelper(logger)}
}

// CommentVideo 提取请求 IP 后透传发布命令，不在网关写入状态。
func (u *VideoCommentUsecase) CommentVideo(ctx context.Context, publisherID, videoID, parentID int64, content string) (int64, error) {
	request, ok := khttp.RequestFromServerContext(ctx)
	if !ok {
		return 0, errdef.INTERNAL_ERROR
	}
	ip, err := iputil.GetClientIp(request)
	if err != nil {
		return 0, errdef.INTERNAL_ERROR
	}
	return u.repo.PublishVideoComment(ctx, publisherID, videoID, parentID, content, ip)
}

// GetCommentList 转发当前用户与主评论游标查询。
func (u *VideoCommentUsecase) GetCommentList(ctx context.Context, userID int64, req *vcommentv1.GetCommentListReq) (*vcommentv1.GetCommentListResp, error) {
	return u.repo.GetCommentList(ctx, userID, req)
}

// GetCommentReplies 转发当前用户与指定楼层的游标查询。
func (u *VideoCommentUsecase) GetCommentReplies(ctx context.Context, userID int64, req *vcommentv1.GetCommentRepliesReq) (*vcommentv1.GetCommentRepliesResp, error) {
	return u.repo.GetCommentReplies(ctx, userID, req)
}

// UpvoteComment 直接传递目标状态，不预读状态或创建 Saga。
func (u *VideoCommentUsecase) UpvoteComment(ctx context.Context, userID int64, req *vcommentv1.UpvoteCommentReq) (*vcommentv1.UpvoteCommentResp, error) {
	return u.repo.UpvoteComment(ctx, userID, req)
}
