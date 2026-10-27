package videobiz

import (
	"context"
	"github.com/go-kratos/kratos/v2/log"
	interactv1 "vw_gateway/api/v1/video/video_interact"
)

type InteractRepo interface {
	FavoriteVideo(ctx context.Context, userId int64, req *interactv1.VideoFavoriteReq) (*interactv1.VideoFavoriteResp, error)
	UpvoteVideo(ctx context.Context, userId int64, videoId int64) (bool, error)
	UpvoteBarrage(ctx context.Context, userId int64, barrageId int64) error
	SendBarrage(ctx context.Context, userId int64, req *interactv1.VideoSendBarrageReq) error
	ShareVideo(ctx context.Context, userId int64, videoId int64) (string, error)
	ThrowShells(ctx context.Context, userId int64, videoId int64, count int32) error
}

type InteractUsecase struct {
	repo InteractRepo
	log  *log.Helper
}

func NewInteractUsecase(repo InteractRepo, logger log.Logger) *InteractUsecase {
	return &InteractUsecase{
		repo: repo,
		log:  log.NewHelper(logger),
	}
}

// Favorite 只转交鉴权身份和明确操作，不在网关判断收藏状态或组合业务步骤。
func (u *InteractUsecase) Favorite(ctx context.Context, userId int64, req *interactv1.VideoFavoriteReq) (*interactv1.VideoFavoriteResp, error) {
	return u.repo.FavoriteVideo(ctx, userId, req)
}

func (u *InteractUsecase) Upvote(ctx context.Context, userId int64, videoId int64) (bool, error) {
	return u.repo.UpvoteVideo(ctx, userId, videoId)
}

func (u *InteractUsecase) UpvoteBarrage(ctx context.Context, userId int64, barrageId int64) error {
	return u.repo.UpvoteBarrage(ctx, userId, barrageId)
}

func (u *InteractUsecase) SendBarrage(ctx context.Context, userId int64, req *interactv1.VideoSendBarrageReq) error {
	return u.repo.SendBarrage(ctx, userId, req)
}

func (u *InteractUsecase) Share(ctx context.Context, id int64, id2 int64) (string, error) {
	return u.repo.ShareVideo(ctx, id, id2)
}

func (u *InteractUsecase) ThrowShells(ctx context.Context, userId int64, videoId int64, count int32) error {
	return u.repo.ThrowShells(ctx, userId, videoId, count)
}
