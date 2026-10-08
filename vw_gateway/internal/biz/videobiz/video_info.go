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
	GetVideoFile(ctx context.Context, videoId int64) (*videoinfov1.FileResp, error)
	GetVideoMpd(ctx context.Context, videoId int64) (*videoinfov1.FileResp, error)
	GetVideoSegment(ctx context.Context, segmentPath string) (*videoinfov1.FileResp, error)
	GetVideoCover(ctx context.Context, videoId int64) (*videoinfov1.FileResp, error)
	UploadVideoInfo(ctx context.Context, info *domain.VideoDetail) (int64, error)
	UploadVideoCover(ctx context.Context, publisherId, videoId int64, fh *multipart.FileHeader) error
	UploadVideoFile(publisherId int64, videoId int64, fh *multipart.FileHeader) error
}

type VideoInfoUsecase struct {
	repo VideoInfoRepo
	log  *log.Helper
}

func NewVideoInfoUsecase(repo VideoInfoRepo, logger log.Logger) *VideoInfoUsecase {
	return &VideoInfoUsecase{
		repo: repo,
		log:  log.NewHelper(logger),
	}
}

func (uc *VideoInfoUsecase) GetVideoDetail(ctx context.Context, videoId, userId int64) (*domain.VideoDetail, error) {
	resp, err := uc.repo.GetVideoInfo(ctx, videoId, userId)
	if err != nil {
		return nil, helper.HandleError(verr.ErrGetVideoListFailed, err)
	}
	return resp, nil
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

	// Upload big file asynchronously, to avoid the request timeout
	// TODO: use message queue to handle big file upload.
	go func() {
		err := uc.repo.UploadVideoFile(userId, videoId, fh)
		if err != nil {
			uc.log.Error(err)
		}
	}()
	return nil
}

func (uc *VideoInfoUsecase) DownloadVideo(ctx context.Context, videoId int64) (*videoinfov1.FileResp, error) {
	return uc.repo.GetVideoFile(ctx, videoId)
}

func (uc *VideoInfoUsecase) GetMpd(ctx context.Context, videoId int64) (*videoinfov1.FileResp, error) {
	return uc.repo.GetVideoMpd(ctx, videoId)
}

func (uc *VideoInfoUsecase) GetSegment(ctx context.Context, segmentPath string) (*videoinfov1.FileResp, error) {
	return uc.repo.GetVideoSegment(ctx, segmentPath)
}

func (uc *VideoInfoUsecase) GetVideoCover(ctx context.Context, videoId int64) (*videoinfov1.FileResp, error) {
	return uc.repo.GetVideoCover(ctx, videoId)
}
