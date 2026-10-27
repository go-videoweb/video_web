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
	VideoId         int64
	PublisherId     int64
	PublisherName   string
	Title           string
	Description     string
	VideoPath       string
	Classes         []string
	Tags            []string
	Hot             int64
	Records         *Records
	Duration        string
	CoverPath       string
	CreatedTime     *timestamppb.Timestamp
	UserVideoStatus *UserVideoStatus
}

// UserVideoStatus 保存仅属于当前访问者的互动状态，游客使用 nil。
type UserVideoStatus struct {
	IsUpvoted        int32
	IsFavorited      int32
	IsShared         int32
	IsThrownShells   int32
	FavoriteId       int64
	FavoriteRecordId int64
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

// NewVideoDetail 映射视频 RPC 的公共详情与可选个人状态，不将个人状态混入公共元信息。
func NewVideoDetail(videoInfo *videoinfov1.GetVideoInfoResp) *VideoDetail {
	detail := &VideoDetail{
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
	if state := videoInfo.UserVideoStatus; state != nil {
		detail.UserVideoStatus = &UserVideoStatus{
			IsUpvoted: state.IsUpvoted, IsFavorited: state.IsFavorited,
			IsShared: state.IsShared, IsThrownShells: state.IsThrownShells, FavoriteId: state.FavoriteId,
			FavoriteRecordId: state.FavoriteRecordId,
		}
	}
	return detail
}
