package service

import (
	"context"
	interactv1 "vw_video/api/v1/interact"
	"vw_video/internal/biz"

	"github.com/go-kratos/kratos/v2/log"
	"google.golang.org/protobuf/types/known/emptypb"
)

type InteractService struct {
	interactv1.UnimplementedVideoInteractServer
	interactUsecase *biz.InteractUsecase
	log             *log.Helper
}

func NewInteractService(interactUsecase *biz.InteractUsecase, logger log.Logger) *InteractService {
	return &InteractService{
		interactUsecase: interactUsecase,
		log:             log.NewHelper(logger),
	}
}

func (s *InteractService) VideoUpvote(ctx context.Context, req *interactv1.VideoUpvoteReq) (*emptypb.Empty, error) {
	err := s.interactUsecase.UpvoteVideo(ctx, req.VideoId, req.UpvoteFlag)
	if err != nil {
		return nil, err
	}
	return nil, nil
}
func (s *InteractService) VideoUpvoteRevert(ctx context.Context, req *interactv1.VideoUpvoteReq) (*emptypb.Empty, error) {
	req.UpvoteFlag = -req.UpvoteFlag
	return s.VideoUpvote(ctx, req)
}

func (s *InteractService) VideoFavorite(ctx context.Context, req *interactv1.VideoFavoriteReq) (*emptypb.Empty, error) {
	err := s.interactUsecase.FavoriteVideo(ctx, req.VideoId, req.FavoriteId, req.Favorite)
	if err != nil {
		return nil, err
	}
	return nil, nil
}
func (s *InteractService) VideoFavoriteRevert(ctx context.Context, req *interactv1.VideoFavoriteReq) (*emptypb.Empty, error) {
	req.Favorite = -req.Favorite
	return s.VideoFavorite(ctx, req)
}

func (s *InteractService) VideoThrowShells(ctx context.Context, req *interactv1.VideoThrowShellsReq) (*emptypb.Empty, error) {
	err := s.interactUsecase.UpdateVideoShells(ctx, req.VideoId, req.Shells)
	if err != nil {
		return nil, err
	}
	return nil, nil
}
func (s *InteractService) VideoThrowShellsRevert(ctx context.Context, req *interactv1.VideoThrowShellsReq) (*emptypb.Empty, error) {
	err := s.interactUsecase.UpdateVideoShells(ctx, req.VideoId, -req.Shells)
	if err != nil {
		return nil, err
	}
	return nil, nil
}

func (s *InteractService) VideoShare(ctx context.Context, req *interactv1.VideoShareReq) (*interactv1.VideoShareResp, error) {
	uri, err := s.interactUsecase.ShareVideo(ctx, req.VideoId)
	if err != nil {
		return nil, err
	}

	return &interactv1.VideoShareResp{
		Uri: uri,
	}, nil
}

func (s *InteractService) VideoSendBarrage(ctx context.Context, req *interactv1.VideoSendBarrageReq) (*emptypb.Empty, error) {
	err := s.interactUsecase.SendBarrage(ctx, req)
	if err != nil {
		return nil, err
	}
	return nil, nil
}

func (s *InteractService) BarrageUpvote(ctx context.Context, req *interactv1.UpvoteBarrageReq) (*emptypb.Empty, error) {
	err := s.interactUsecase.UpvoteBarrage(ctx, req.BarrageId, req.Upvote)
	if err != nil {
		return nil, err
	}
	return nil, nil
}
func (s *InteractService) BarrageUpvoteRevert(ctx context.Context, req *interactv1.UpvoteBarrageReq) (*emptypb.Empty, error) {
	return s.BarrageUpvote(ctx, req)
}

func (s *InteractService) GetUserVideoStatus(ctx context.Context, req *interactv1.GetUserVideoStatusReq) (*interactv1.GetUserVideoStatusResp, error) {
	status, shellsCount, err := s.interactUsecase.GetUserVideoStatus(ctx, req.UserId, req.VideoId)
	if err != nil {
		return nil, err
	}

	return &interactv1.GetUserVideoStatusResp{
		Status:      status,
		ShellsCount: shellsCount,
	}, nil
}

func (s *InteractService) GetUserBarrageStatus(ctx context.Context, req *interactv1.GetUserBarrageStatusReq) (*interactv1.GetUserBarrageStatusResp, error) {
	status, err := s.interactUsecase.GetUserBarrageStatus(ctx, req.UserId, req.BarrageId)
	if err != nil {
		return nil, err
	}

	return &interactv1.GetUserBarrageStatusResp{
		Status: status,
	}, nil
}

func (s *InteractService) SetUserVideoStatus(ctx context.Context, req *interactv1.SetUserVideoStatusReq) (*emptypb.Empty, error) {
	err := s.interactUsecase.SetUserVideoStatus(ctx, req.UserId, req.VideoId, req)
	if err != nil {
		return nil, err
	}
	return nil, nil
}
func (s *InteractService) SetUserVideoStatusRevert(ctx context.Context, req *interactv1.SetUserVideoStatusReq) (*emptypb.Empty, error) {
	err := s.interactUsecase.SetUserVideoStatusRevert(ctx, req.UserId, req.VideoId, req)
	if err != nil {
		return nil, err
	}
	return nil, nil
}

func (s *InteractService) SetUserBarrageStatus(ctx context.Context, req *interactv1.SetUserBarrageStatusReq) (*emptypb.Empty, error) {
	err := s.interactUsecase.SetUserBarrageStatus(ctx, req.UserId, req.BarrageId, req)
	if err != nil {
		return nil, err
	}
	return nil, nil
}
func (s *InteractService) SetUserBarrageStatusRevert(ctx context.Context, req *interactv1.SetUserBarrageStatusReq) (*emptypb.Empty, error) {
	err := s.interactUsecase.SetUserBarrageStatusRevert(ctx, req.UserId, req.BarrageId, req)
	if err != nil {
		return nil, err
	}
	return nil, nil
}
