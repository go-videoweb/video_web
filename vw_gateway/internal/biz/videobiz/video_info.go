package videobiz

import (
	"context"
	stderr "errors"
	"github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	khttp "github.com/go-kratos/kratos/v2/transport/http"
	"mime/multipart"
	"net/http"
	"util/helper"
	"util/helper/file"
	videoinfov1 "vw_gateway/api/v1/video/videoinfo"
	"vw_gateway/internal/domain"
	"vw_gateway/internal/pkg/ecode/errdef/uerr"
	"vw_gateway/internal/pkg/ecode/errdef/verr"
	videoinfov2 "vw_video/api/v1/videoinfo"
)

type VideoInfoRepo interface {
	GetVideoInfo(ctx context.Context, videoId, userId int64) (*domain.VideoDetail, error)
	GetVideoList(ctx context.Context, class []string, num int32, size int32) ([]*videoinfov2.VideoSummary, error)
	// GetFavoriteVideos 传递可信访问者身份，读取收藏夹视频分页结果。
	GetFavoriteVideos(ctx context.Context, userId, favoritesId int64, pageNum, pageSize int32) (*videoinfov2.GetFavoriteVideosResp, error)
	GetVideoFile(ctx context.Context, videoId int64) (*videoinfov1.FileResp, error)
	GetVideoMpd(ctx context.Context, videoId int64) (*videoinfov1.FileResp, error)
	GetVideoSegment(ctx context.Context, videoId int64, segmentName string) (*videoinfov1.FileResp, error)
	GetVideoCover(ctx context.Context, videoId int64) (*videoinfov1.FileResp, error)
	UploadVideoInfo(ctx context.Context, info *domain.VideoDetail) (int64, error)
	UploadVideoCover(ctx context.Context, publisherId, videoId int64, fh *multipart.FileHeader) error
	UploadVideoFile(ctx context.Context, publisherId int64, videoId int64, fh *multipart.FileHeader) error
	// GetVideoUploadStatus 使用已鉴权的用户 ID 向视频服务查询上传状态。
	GetVideoUploadStatus(ctx context.Context, videoId, userId int64) (*videoinfov2.GetVideoUploadStatusResp, error)
}

type VideoInfoUsecase struct {
	repo VideoInfoRepo
}

func NewVideoInfoUsecase(repo VideoInfoRepo, _ log.Logger) *VideoInfoUsecase {
	return &VideoInfoUsecase{
		repo: repo,
	}
}

func (uc *VideoInfoUsecase) GetVideoDetail(ctx context.Context, videoId, userId int64) (*domain.VideoDetail, error) {
	resp, err := uc.repo.GetVideoInfo(ctx, videoId, userId)
	if err != nil {
		return nil, helper.HandleError(verr.ErrGetVideoListFailed, err)
	}
	return resp, nil
}

// GetFavoriteVideos 转发收藏夹分页查询，保留权限、参数和下游错误的原始语义。
func (uc *VideoInfoUsecase) GetFavoriteVideos(ctx context.Context, userId, favoritesId int64, pageNum, pageSize int32) (*videoinfov2.GetFavoriteVideosResp, error) {
	return uc.repo.GetFavoriteVideos(ctx, userId, favoritesId, pageNum, pageSize)
}

// GetVideoUploadStatus 转发投稿状态查询，由视频服务校验发布者身份。
func (uc *VideoInfoUsecase) GetVideoUploadStatus(ctx context.Context, videoId, userId int64) (*videoinfov2.GetVideoUploadStatusResp, error) {
	return uc.repo.GetVideoUploadStatus(ctx, videoId, userId)
}

func (uc *VideoInfoUsecase) GetVideoList(ctx context.Context, class []string, num int32, size int32) ([]*domain.VideoSummary, error) {
	if class == nil || len(class) == 0 {
		class = []string{"default"}
	}
	resp, err := uc.repo.GetVideoList(ctx, class, num, size)
	if err != nil {
		return nil, helper.HandleError(verr.ErrGetVideoListFailed, err)
	}
	var videoList []*domain.VideoSummary
	for _, v := range resp {
		videoList = append(videoList, &domain.VideoSummary{
			VideoId:       v.VideoId,
			CntBarrages:   v.CntBarrages,
			CntViewed:     v.CntViewed,
			Title:         v.Title,
			Duration:      v.Duration,
			PublisherName: v.PublisherName,
			CoverPath:     v.CoverPath,
			Description:   v.Description,
			VideoPath:     v.VideoPath,
			CreatedTime:   v.CreatedTime,
		})
	}
	return videoList, nil
}

func (uc *VideoInfoUsecase) UploadVideoInfo(ctx context.Context, info *domain.VideoDetail) (int64, error) {
	return uc.repo.UploadVideoInfo(ctx, info)
}

func (uc *VideoInfoUsecase) UploadVideoCover(ctx context.Context, userId, videoId int64) error {
	httpReq, ok := khttp.RequestFromServerContext(ctx)
	if !ok {
		return helper.HandleError(
			errors.New(http.StatusInternalServerError, "invalid context", "服务器内部错误"),
			stderr.New("khttp.RequestFromServerContext(ctx) failed"))
	}
	fh, err := file.FormFile(httpReq, "cover")
	if err != nil {
		return helper.HandleError(uerr.ErrFormFileFiled, err)
	}

	err = file.CheckIfPictureValid(fh)
	if err != nil {
		return helper.HandleError(uerr.ErrUploadAvatarFailed, err)
	}
	return uc.repo.UploadVideoCover(ctx, userId, videoId, fh)
}

// UploadVideoFile 等待视频服务确认原片落盘，并透传上传失败。
func (uc *VideoInfoUsecase) UploadVideoFile(ctx context.Context, userId, videoId int64) error {
	// Parse form data and get video file
	httpReq, ok := khttp.RequestFromServerContext(ctx)
	if !ok {
		return helper.HandleError(
			errors.New(http.StatusInternalServerError, "invalid context", "服务器内部错误"),
			stderr.New("khttp.RequestFromServerContext(ctx) failed"))
	}
	fh, err := file.FormFile(httpReq, "file")
	if err != nil {
		return helper.HandleError(uerr.ErrFormFileFiled, err)
	}

	err = file.CheckIfVideoValid(fh)
	if err != nil {
		return helper.HandleError(uerr.ErrUploadAvatarFailed, err)
	}

	return uc.repo.UploadVideoFile(ctx, userId, videoId, fh)
}

func (uc *VideoInfoUsecase) DownloadVideo(ctx context.Context, videoId int64) (*videoinfov1.FileResp, error) {
	return uc.repo.GetVideoFile(ctx, videoId)
}

func (uc *VideoInfoUsecase) GetMpd(ctx context.Context, videoId int64) (*videoinfov1.FileResp, error) {
	return uc.repo.GetVideoMpd(ctx, videoId)
}

// GetSegment 按视频 ID 和分片名获取 DASH 分片，不接受服务器路径。
// 输入：ctx 传递取消和超时；videoId 指定视频；segmentName 为 MPD 中的单个文件名。
// 输出：网关临时文件响应或下游错误，路径和文件名校验由视频服务执行。
func (uc *VideoInfoUsecase) GetSegment(ctx context.Context, videoId int64, segmentName string) (*videoinfov1.FileResp, error) {
	return uc.repo.GetVideoSegment(ctx, videoId, segmentName)
}

func (uc *VideoInfoUsecase) GetVideoCover(ctx context.Context, videoId int64) (*videoinfov1.FileResp, error) {
	return uc.repo.GetVideoCover(ctx, videoId)
}
