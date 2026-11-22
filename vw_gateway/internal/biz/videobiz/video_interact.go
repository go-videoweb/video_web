package videobiz

import (
	"context"
	"net/url"
	"strconv"
	"strings"

	interactv1 "vw_gateway/api/v1/video/video_interact"
	"vw_gateway/internal/conf"
	"vw_gateway/internal/pkg/ecode/errdef"
)

type InteractRepo interface {
	FavoriteVideo(ctx context.Context, userId int64, req *interactv1.VideoFavoriteReq) (*interactv1.VideoFavoriteResp, error)
	UpvoteVideo(ctx context.Context, userId int64, videoId int64) (bool, error)
	UpvoteBarrage(ctx context.Context, userID int64, req *interactv1.VideoUpvoteBarrageReq) (*interactv1.VideoUpvoteBarrageResp, error)
	GetBarrages(ctx context.Context, userID int64, req *interactv1.GetBarragesReq) (*interactv1.GetBarragesResp, error)
	SendBarrage(ctx context.Context, userId int64, req *interactv1.VideoSendBarrageReq) error
	RecordVideoShare(ctx context.Context, userId, videoId int64) error
	ThrowShells(ctx context.Context, userId int64, videoId int64, count int32) error
}

type InteractUsecase struct {
	repo             InteractRepo
	publicSiteOrigin string
}

func NewInteractUsecase(repo InteractRepo, service *conf.Service) *InteractUsecase {
	return &InteractUsecase{
		repo:             repo,
		publicSiteOrigin: service.GetPublicSiteOrigin(),
	}
}

// Favorite 只转交鉴权身份和明确操作，不在网关判断收藏状态或组合业务步骤。
func (u *InteractUsecase) Favorite(ctx context.Context, userId int64, req *interactv1.VideoFavoriteReq) (*interactv1.VideoFavoriteResp, error) {
	return u.repo.FavoriteVideo(ctx, userId, req)
}

func (u *InteractUsecase) Upvote(ctx context.Context, userId int64, videoId int64) (bool, error) {
	return u.repo.UpvoteVideo(ctx, userId, videoId)
}

// UpvoteBarrage 直接转发目标状态，不预读旧状态或创建 Saga。
func (u *InteractUsecase) UpvoteBarrage(ctx context.Context, userId int64, req *interactv1.VideoUpvoteBarrageReq) (*interactv1.VideoUpvoteBarrageResp, error) {
	return u.repo.UpvoteBarrage(ctx, userId, req)
}

// GetBarrages 透传播放区间及分页游标，不复用视频详情接口。
func (u *InteractUsecase) GetBarrages(ctx context.Context, userID int64, req *interactv1.GetBarragesReq) (*interactv1.GetBarragesResp, error) {
	return u.repo.GetBarrages(ctx, userID, req)
}

func (u *InteractUsecase) SendBarrage(ctx context.Context, userId int64, req *interactv1.VideoSendBarrageReq) error {
	return u.repo.SendBarrage(ctx, userId, req)
}

// Share 生成公开观看页链接，并将分享行为交给视频服务记录。
// 配置无效时在调用视频服务前返回错误，避免记录一次无法返回链接的分享。
func (u *InteractUsecase) Share(ctx context.Context, userId, videoId int64) (string, error) {
	origin, err := url.Parse(strings.TrimSpace(u.publicSiteOrigin))
	if err != nil {
		return "", errdef.ErrInvalidPublicSiteOrigin
	}
	if origin.Scheme != "http" && origin.Scheme != "https" {
		return "", errdef.ErrInvalidPublicSiteOrigin
	}
	if origin.Hostname() == "" || origin.User != nil {
		return "", errdef.ErrInvalidPublicSiteOrigin
	}
	if path := origin.EscapedPath(); path != "" && path != "/" {
		return "", errdef.ErrInvalidPublicSiteOrigin
	}
	if origin.ForceQuery || origin.RawQuery != "" {
		return "", errdef.ErrInvalidPublicSiteOrigin
	}
	if origin.Fragment != "" {
		return "", errdef.ErrInvalidPublicSiteOrigin
	}
	if err := u.repo.RecordVideoShare(ctx, userId, videoId); err != nil {
		return "", err
	}
	origin.Path = "/watch/" + strconv.FormatInt(videoId, 10)
	return origin.String(), nil
}

func (u *InteractUsecase) ThrowShells(ctx context.Context, userId int64, videoId int64, count int32) error {
	return u.repo.ThrowShells(ctx, userId, videoId, count)
}
