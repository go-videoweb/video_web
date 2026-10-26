package video

import (
	"context"
	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	videoinfov1 "vw_gateway/api/v1/video/videoinfo"
	"vw_gateway/internal/biz/videobiz"
	"vw_gateway/internal/domain"
	"vw_gateway/internal/pkg/ecode/errdef/uerr"
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

// GetVideoInfo 使用鉴权上下文中的身份获取视频详情。
// 输入：ctx 的 user_id 为鉴权确认的 int64 用户 ID；req 提供视频 ID，不从请求参数接受用户身份。
// 输出：成功时返回视频详情响应；下游获取失败时返回 nil 和原错误。
// 身份缺失、类型不符或非正数时传入 user_id=0，由下游跳过个人互动状态和观看历史操作。
// 个人状态仅向已验证的登录访问者返回，游客始终不返回下游个人状态。
func (info *InfoService) GetVideoInfo(ctx context.Context, req *videoinfov1.GetVideoInfoReq) (*videoinfov1.GetVideoInfoResp, error) {
	userId, ok := ctx.Value(userIdKey).(int64)
	if !ok || userId <= 0 {
		userId = 0
	}
	detail, err := info.videoInfo.GetVideoDetail(ctx, req.VideoId, userId)
	if err != nil {
		return nil, err
	}
	resp := &videoinfov1.GetVideoInfoResp{
		VideoDetail: domainToVideoMetaInfo(detail),
	}
	if state := detail.UserVideoStatus; userId > 0 && state != nil {
		resp.UserVideoStatus = &videoinfov1.UserVideoStatus{
			IsUpvoted: state.IsUpvoted, IsFavorited: state.IsFavorited,
			IsShared: state.IsShared, IsThrownShells: state.IsThrownShells, FavoriteId: state.FavoriteId,
			FavoriteRecordId: state.FavoriteRecordId,
		}
	}
	return resp, nil
}

// GetFavoriteVideos 使用鉴权身份读取收藏夹分页内容，身份缺失时不调用下游。
// 摘要字段和收藏时间分别映射，保留视频发布时间；空页返回空数组和下游总数。
func (info *InfoService) GetFavoriteVideos(ctx context.Context, req *videoinfov1.GetFavoriteVideosReq) (*videoinfov1.GetFavoriteVideosResp, error) {
	userId, ok := ctx.Value(userIdKey).(int64)
	if !ok || userId <= 0 {
		return nil, uerr.ErrTokenInvalid
	}
	result, err := info.videoInfo.GetFavoriteVideos(ctx, userId, req.FavoritesId, req.PageNum, req.PageSize)
	if err != nil {
		return nil, err
	}
	resp := &videoinfov1.GetFavoriteVideosResp{
		Total: result.Total, PageNum: result.PageNum, PageSize: result.PageSize,
		Videos: make([]*videoinfov1.FavoriteVideoItem, len(result.Videos)),
	}
	for i, item := range result.Videos {
		v := item.Video
		resp.Videos[i] = &videoinfov1.FavoriteVideoItem{
			CollectedAt: item.CollectedAt,
			Video: &videoinfov1.VideoSummary{
				VideoId: v.VideoId, Title: v.Title, Description: v.Description, Duration: v.Duration,
				PublisherName: v.PublisherName, CoverPath: v.CoverPath, VideoPath: v.VideoPath,
				CntBarrages: v.CntBarrages, CntViewed: v.CntViewed, CreatedTime: v.CreatedTime,
			},
		}
	}
	return resp, nil
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

// GetSegments 按视频 ID 和分片名转发下载请求，拒绝旧的路径参数。
// 输入：req 提供 video_id 和 segment_name；路由中的 user_id 不作为身份或存储位置使用。
// 输出：分片文件响应；旧 segment_path 非空时返回参数错误，不调用下游。
func (info *InfoService) GetSegments(ctx context.Context, req *videoinfov1.ProvideSegmentsReq) (*videoinfov1.FileResp, error) {
	if req.SegmentPath != "" {
		return nil, kerrors.BadRequest("SEGMENT_PATH_UNSUPPORTED", "use video_id and segment_name instead of segment_path")
	}
	resp, err := info.videoInfo.GetSegment(ctx, req.VideoId, req.SegmentName)
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
		CreatedTime:   detail.CreatedTime,
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
		Duration:    detail.Duration,
		CoverPath:   detail.CoverPath,
		CreatedTime: detail.CreatedTime,
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
		Description:   detail.Description,
		VideoPath:     detail.VideoPath,
		CreatedTime:   detail.CreatedTime,
	}
}
