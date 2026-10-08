package video

import (
	"context"
	"github.com/go-kratos/kratos/v2/log"
	videoinfov1 "vw_gateway/api/v1/video/videoinfo"
	"vw_gateway/internal/biz/videobiz"
	"vw_gateway/internal/domain"
)

type InfoService struct {
	videoinfov1.UnimplementedVideoInfoServer
	videoInfo *videobiz.VideoInfoUsecase
	log       *log.Helper
}

func NewVideoInfoService(videoInfo *videobiz.VideoInfoUsecase, logger log.Logger) *InfoService {
	return &InfoService{
		videoInfo: videoInfo,
		log:       log.NewHelper(log.With(logger, "module", "video_info")),
	}
}

func (info *InfoService) GetVideoInfo(ctx context.Context, req *videoinfov1.GetVideoInfoReq) (*videoinfov1.GetVideoInfoResp, error) {
	userId := ctx.Value(userIdKey).(int64)
	detail, err := info.videoInfo.GetVideoDetail(ctx, req.VideoId, userId)
	if err != nil {
		return nil, err
	}
	return &videoinfov1.GetVideoInfoResp{
		VideoDetail: domainToVideoMetaInfo(detail),
	}, nil
}

func (info *InfoService) GetVideoList(ctx context.Context, req *videoinfov1.GetVideoListReq) (*videoinfov1.GetVideoListResp, error) {
	videosSummary, err := info.videoInfo.GetVideoList(ctx, req.Class, req.PageNum, req.PageSize)
	if err != nil {
		return nil, err
	}
	ret := &videoinfov1.GetVideoListResp{
		VideoSummary: make([]*videoinfov1.VideoSummary, len(videosSummary)),
	}
	for i, summary := range videosSummary {
		ret.VideoSummary[i] = domainToVideoSummary(summary)
	}
	return ret, nil
}

func (info *InfoService) UploadVideoInfo(ctx context.Context, req *videoinfov1.UploadVideoInfoReq) (*videoinfov1.UploadVideoInfoResp, error) {
	videoID, err := info.videoInfo.UploadVideoInfo(ctx, videoMetaInfoToDomain(req.VideoInfo))
	if err != nil {
		return nil, err
	}
	return &videoinfov1.UploadVideoInfoResp{
		VideoId: videoID,
		Resp: &videoinfov1.CommonResp{
			StatusCode: 200,
			Message:    "success",
		}}, nil
}

func (info *InfoService) UploadVideoCover(ctx context.Context, req *videoinfov1.UploadVideoCoverReq) (*videoinfov1.UploadVideoCoverResp, error) {
	err := info.videoInfo.UploadVideoCover(ctx, req.UserId, req.VideoId)
	if err != nil {
		return nil, err
	}
	return &videoinfov1.UploadVideoCoverResp{
		Resp: &videoinfov1.CommonResp{
			StatusCode: 200,
			Message:    "success",
		}}, nil
}

func (info *InfoService) UploadVideoFile(ctx context.Context, req *videoinfov1.UploadVideoFileReq) (*videoinfov1.UploadVideoFileResp, error) {
	err := info.videoInfo.UploadVideoFile(ctx, req.UserId, req.VideoId)
	if err != nil {
		return nil, err
	}
	return &videoinfov1.UploadVideoFileResp{
		Resp: &videoinfov1.CommonResp{
			StatusCode: 200,
			Message:    "success",
		}}, nil
}

func (info *InfoService) DownloadVideo(ctx context.Context, req *videoinfov1.DownloadVideoReq) (*videoinfov1.FileResp, error) {
	resp, err := info.videoInfo.DownloadVideo(ctx, req.VideoId)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (info *InfoService) GetMpd(ctx context.Context, req *videoinfov1.ProvideMpdReq) (*videoinfov1.FileResp, error) {
	resp, err := info.videoInfo.GetMpd(ctx, req.VideoId)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (info *InfoService) GetSegments(ctx context.Context, req *videoinfov1.ProvideSegmentsReq) (*videoinfov1.FileResp, error) {
	resp, err := info.videoInfo.GetSegment(ctx, req.SegmentPath)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func (info *InfoService) GetVideoCover(ctx context.Context, req *videoinfov1.GetVideoCoverReq) (*videoinfov1.FileResp, error) {
	resp, err := info.videoInfo.GetVideoCover(ctx, req.VideoId)
	if err != nil {
		return nil, err
	}
	return resp, nil
}

func videoMetaInfoToDomain(detail *videoinfov1.VideoMetaInfo) *domain.VideoDetail {
	dv := &domain.VideoDetail{
		VideoId:       detail.VideoId,
		PublisherId:   detail.PublisherId,
		PublisherName: detail.PublisherName,
		Title:         detail.Title,
		Description:   detail.Description,
		VideoPath:     detail.VideoPath,
		Classes:       detail.Classes,
		Tags:          detail.Tags,
		Hot:           detail.Hot,
		Duration:      detail.Duration,
		CoverPath:     detail.CoverPath,
	}

	if detail.Records != nil {
		dv.Records = &domain.Records{
			CntBarrages:  detail.Records.CntBarrages,
			CntShares:    detail.Records.CntShares,
			CntViewed:    detail.Records.CntViewed,
			CntFavorites: detail.Records.CntFavorites,
		}
	}
	return dv
}

func domainToVideoMetaInfo(detail *domain.VideoDetail) *videoinfov1.VideoMetaInfo {
	return &videoinfov1.VideoMetaInfo{
		VideoId:       detail.VideoId,
		PublisherId:   detail.PublisherId,
		PublisherName: detail.PublisherName,
		Title:         detail.Title,
		Description:   detail.Description,
		VideoPath:     detail.VideoPath,
		Classes:       detail.Classes,
		Tags:          detail.Tags,
		Hot:           detail.Hot,
		Records: &videoinfov1.VideoMetaInfo_Records{
			CntBarrages:  detail.Records.CntBarrages,
			CntShares:    detail.Records.CntShares,
			CntViewed:    detail.Records.CntViewed,
			CntFavorites: detail.Records.CntFavorites,
		},
		Duration:  detail.Duration,
		CoverPath: detail.CoverPath,
	}
}

func domainToVideoSummary(detail *domain.VideoSummary) *videoinfov1.VideoSummary {
	return &videoinfov1.VideoSummary{
		VideoId:       detail.VideoId,
		CntBarrages:   detail.CntBarrages,
		CntViewed:     detail.CntViewed,
		Title:         detail.Title,
		Duration:      detail.Duration,
		PublisherName: detail.PublisherName,
		CoverPath:     detail.CoverPath,
	}
}
