package service

import (
	"context"
	"github.com/go-kratos/kratos/v2/log"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
	videoinfov1 "vw_video/api/v1/videoinfo"
	"vw_video/internal/biz"
)

type VideoInfoService struct {
	videoinfov1.UnimplementedVideoInfoServer
	info   *biz.VideoInfoUsecase
	logger *log.Helper
}

func NewVideoInfo(info *biz.VideoInfoUsecase, logger log.Logger) *VideoInfoService {
	return &VideoInfoService{
		info:   info,
		logger: log.NewHelper(logger),
	}
}

func (s *VideoInfoService) GetVideoInfo(ctx context.Context, req *videoinfov1.GetVideoInfoReq) (*videoinfov1.GetVideoInfoResp, error) {
	return s.info.GetVideoInfoById(ctx, req.VideoId, req.UserId)
}

func (s *VideoInfoService) GetVideoList(ctx context.Context, req *videoinfov1.GetVideoListReq) (*videoinfov1.GetVideoListResp, error) {
	//spanCtx := trace.SpanContextFromContext(ctx)
	//fmt.Println("[video grpc service GetVideoList] Span ID:", spanCtx.SpanID().String())
	//fmt.Println("[video grpc service GetVideoList] Trace ID:", spanCtx.TraceID().String())
	videoSummary, err := s.info.GetVideoListByClass(ctx, req.Class, req.PageNum, req.PageSize)
	if err != nil {
		return nil, err
	}
	resp := &videoinfov1.GetVideoListResp{
		VideoSummary: make([]*videoinfov1.VideoSummary, len(videoSummary)),
	}
	for i, vs := range videoSummary {
		resp.VideoSummary[i] = &videoinfov1.VideoSummary{
			VideoId:       vs.VideoId,
			CntBarrages:   vs.CntBarrages,
			Title:         vs.Title,
			Duration:      vs.Duration,
			PublisherName: vs.PublisherName,
			CntViewed:     vs.CntViewed,
			CoverPath:     vs.CoverPath,
		}
	}
	return resp, nil
}

func (s *VideoInfoService) UploadVideoInfo(ctx context.Context, req *videoinfov1.UploadVideoInfoReq) (*videoinfov1.UploadVideoInfoResp, error) {
	videoID, err := s.info.UploadVideoInfo(ctx, req.VideoInfo)
	if err != nil {
		return nil, err
	}
	return &videoinfov1.UploadVideoInfoResp{VideoId: videoID}, nil
}

func (s *VideoInfoService) UploadVideoFile(stream grpc.ClientStreamingServer[videoinfov1.UploadVideoFileReq, emptypb.Empty]) error {
	return s.info.UploadVideoFile(stream)
}

func (s *VideoInfoService) UploadVideoCover(stream grpc.ClientStreamingServer[videoinfov1.UploadVideoCoverReq, emptypb.Empty]) error {
	return s.info.UploadVideoCover(stream)
}

func (s *VideoInfoService) GetVideoFile(req *videoinfov1.GetVideoFileReq, stream grpc.ServerStreamingServer[videoinfov1.GetVideoFileResp]) error {
	return s.info.GetVideoFile(req.VideoId, stream)
}

func (s *VideoInfoService) GetVideoMpd(req *videoinfov1.GetVideoMpdReq, stream grpc.ServerStreamingServer[videoinfov1.GetVideoMpdResp]) error {
	return s.info.GetVideoMpd(req.VideoId, stream)
}

func (s *VideoInfoService) GetVideoSegments(req *videoinfov1.GetVideoSegmentReq, stream grpc.ServerStreamingServer[videoinfov1.GetVideoSegmentResp]) error {
	return s.info.GetVideoSegment(req.VideoSegmentPath, stream)
}

func (s *VideoInfoService) GetVideoCover(req *videoinfov1.GetVideoCoverReq, stream grpc.ServerStreamingServer[videoinfov1.GetVideoCoverResp]) error {
	return s.info.GetVideoCover(req.VideoId, stream)
}

func (s *VideoInfoService) GetPublisherIdByVideoId(ctx context.Context, req *videoinfov1.GetPublisherIdByVideoIdReq) (*videoinfov1.GetPublisherIdByVideoIdResp, error) {
	publisherId, err := s.info.GetPublisherIdByVideoId(ctx, req.VideoId)
	if err != nil {
		return nil, err
	}
	return &videoinfov1.GetPublisherIdByVideoIdResp{
		PublisherId: publisherId,
	}, nil
}

func (s *VideoInfoService) AddVideoCntShared(ctx context.Context, req *videoinfov1.AddVideoCntSharedReq) (*emptypb.Empty, error) {
	err := s.info.UpdateVideoCntShare(ctx, req.VideoId, req.IsCompensation)
	if err != nil {
		return nil, err
	}
	return nil, nil
}
func (s *VideoInfoService) AddVideoCntSharedRevert(ctx context.Context, req *videoinfov1.AddVideoCntSharedReq) (*emptypb.Empty, error) {
	req.IsCompensation = !req.IsCompensation
	return s.AddVideoCntShared(ctx, req)
}
