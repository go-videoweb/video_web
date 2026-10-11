package domain

import (
	"google.golang.org/protobuf/types/known/timestamppb"
	videoinfov1 "vw_video/api/v1/videoinfo"
)

type Records struct {
	CntBarrages  uint32
	CntShares    uint32
	CntViewed    uint32
	CntFavorites uint32
}
type VideoDetail struct {
	VideoId       int64
	PublisherId   int64
	PublisherName string
	Title         string
	Description   string
	VideoPath     string
	Classes       []string
	Tags          []string
	Hot           int64
	Records       *Records
	Duration      string
	CoverPath     string
	CreatedTime   *timestamppb.Timestamp
}

type VideoSummary struct {
	VideoId       int64
	CntBarrages   int64
	CntViewed     int64
	Title         string
	Duration      string
	PublisherName string
	CoverPath     string
	Description   string
	VideoPath     string
	CreatedTime   *timestamppb.Timestamp
}

// NewVideoDetail creates a new VideoDetail object from video grpc service GetVideoInfo's response.
func NewVideoDetail(videoInfo *videoinfov1.GetVideoInfoResp) *VideoDetail {
	return &VideoDetail{
		VideoId:       videoInfo.VideoDetail.VideoId,
		PublisherId:   videoInfo.VideoDetail.PublisherId,
		PublisherName: videoInfo.VideoDetail.PublisherName,
		Title:         videoInfo.VideoDetail.Title,
		Description:   videoInfo.VideoDetail.Description,
		VideoPath:     videoInfo.VideoDetail.VideoPath,
		Classes:       videoInfo.VideoDetail.Classes,
		Tags:          videoInfo.VideoDetail.Tags,
		Hot:           videoInfo.VideoDetail.Hot,
		Records: &Records{
			CntBarrages:  videoInfo.VideoDetail.Records.CntBarrages,
			CntShares:    videoInfo.VideoDetail.Records.CntShares,
			CntViewed:    videoInfo.VideoDetail.Records.CntViewed,
			CntFavorites: videoInfo.VideoDetail.Records.CntFavorited,
		},
		Duration:    videoInfo.VideoDetail.Duration,
		CoverPath:   videoInfo.VideoDetail.CoverPath,
		CreatedTime: videoInfo.VideoDetail.CreatedTime,
	}
}
