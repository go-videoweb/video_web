package biz

import (
	"context"
	stderr "errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	"util/dbutil/mysqlutil"
	"util/getid"
	"util/helper"
	"util/helper/file"
	videoinfov1 "vw_video/api/v1/videoinfo"
	"vw_video/cmd/gorm_gen/methods"
	"vw_video/internal/data/dal/model"
	"vw_video/internal/pkg/ecode/errdef"

	"github.com/go-kratos/kratos/v2/log"
	grpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	emptypb "google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gorm.io/gorm"
)

const (
	videoUploadWaitingCover = "waiting_cover"
	videoUploadWaitingVideo = "waiting_video"
	videoUploadUploading    = "uploading"
	videoUploadProcessing   = "processing"
	videoUploadCompleted    = "completed"
	videoUploadFailed       = "failed"

	videoProcessingTimeout  = 30 * time.Minute
	videoUploadWriteTimeout = 3 * time.Second
)

// VideoInfoRepo 定义公共视频信息、上传状态、个人状态和最近观看时间的仓储能力。
type VideoInfoRepo interface {
	GetVideoInfoById(ctx context.Context, videoId int64) (*model.Video, error)
	GetVideoListByClass(ctx context.Context, class []string, pageNum int32, pageSize int32) ([]*model.Video, error)
	GetVideoFilePathById(ctx context.Context, videoId int64) (string, error)
	AddVideoCntView(ctx context.Context, videoId int64) error
	SetCoverPath(ctx context.Context, videoId int64, coverPath string) error
	// CreateBasicVideoInfo 插入一条视频元信息，沿用 ctx 中的事务。
	CreateBasicVideoInfo(ctx context.Context, video *model.Video) error
	// CreateVideoUploadStatus 插入一条上传状态，沿用 ctx 中的事务。
	CreateVideoUploadStatus(ctx context.Context, uploadStatus *model.VideoUploadStatus) error
	// GetVideoUploadStatus 按视频 ID 读取上传状态，缺少记录时返回 gorm.ErrRecordNotFound。
	GetVideoUploadStatus(ctx context.Context, videoId int64) (*model.VideoUploadStatus, error)
	// UpdateVideoUploadStatus 仅更新处于指定阶段的记录，返回是否命中。
	UpdateVideoUploadStatus(ctx context.Context, videoId int64, from, to, errorMessage string, updatedTime time.Time) (bool, error)
	// UpdateVideoMediaInfo 保存处理完成的原片路径、时长及大小。
	UpdateVideoMediaInfo(ctx context.Context, videoId int64, videoFilePath, duration string, size int64) error
	GetPublisherIdByVideoId(CTX context.Context, videoId int64) (publisherId int64, err error)
	GetBarragesByVideoId(ctx context.Context, videoId int64) ([]*model.Barrage, error)
	// GetUserVideoStatus 只读个人互动字段，无记录返回默认位掩码；收藏归属另行查询。
	GetUserVideoStatus(ctx context.Context, videoId int64, userId int64) (int64, error)
	// GetUserFavorite 从有效关系读取收藏夹及关系 ID，无记录时两个 ID 均为零。
	GetUserFavorite(ctx context.Context, userId, videoId int64) (int64, int64, error)
	// GetFavoriteVideos 在权限校验后分页读取有效视频，返回相同筛选范围的总数。
	GetFavoriteVideos(ctx context.Context, userId, favoritesId int64, pageNum, pageSize int32) ([]*FavoriteVideo, int64, error)
	// UpdateUserVideoHistory 在上下文事务内更新最近观看时间，不覆盖互动或收藏状态。
	UpdateUserVideoHistory(ctx context.Context, videoId int64, userId int64) error
}

type VideoInfoUsecase struct {
	repo   VideoInfoRepo
	tx     mysqlutil.Transaction
	logger *log.Helper
}

func NewVideoInfoUsecase(repo VideoInfoRepo, tx mysqlutil.Transaction, logger log.Logger) *VideoInfoUsecase {
	return &VideoInfoUsecase{
		repo:   repo,
		tx:     tx,
		logger: log.NewHelper(logger),
	}
}

// GetVideoInfoById 获取视频公共详情，并按身份处理个人数据；弹幕改用分段读取接口。
// 输入：ctx 为请求上下文；videoId 为目标视频 ID；userId 为 0 表示游客，正数须由可信调用方验证。
// 输出：游客返回公共详情，不包含个人互动状态；登录用户额外返回个人互动状态。
// userId 为负数时返回 InvalidArgument，且不访问仓储；查询或更新失败时返回 nil 和包装后的业务错误。
// 游客和登录用户均沿用增加播放量和热度的行为，只有登录用户更新最近观看时间，不保存摘要副本。
func (uc *VideoInfoUsecase) GetVideoInfoById(ctx context.Context, videoId, userId int64) (*videoinfov1.GetVideoInfoResp, error) {
	if userId < 0 {
		return nil, status.Error(codes.InvalidArgument, "user_id must not be negative")
	}
	var ret = new(videoinfov1.GetVideoInfoResp)
	err := uc.tx.WithTx(ctx, func(ctx context.Context) error {
		videoDetail, err := uc.repo.GetVideoInfoById(ctx, videoId)
		if err != nil {
			if stderr.Is(err, gorm.ErrRecordNotFound) {
				return errdef.ErrVideoNotFound
			}
			return err
		}

		ret.VideoDetail = &videoinfov1.VideoMetaInfo{
			VideoId:       videoDetail.VideoID,
			Title:         videoDetail.Title,
			Description:   videoDetail.Description,
			Classes:       strings.Split(videoDetail.Class, methods.Separator),
			Tags:          strings.Split(videoDetail.Tags, methods.Separator),
			PublisherId:   videoDetail.PublisherID,
			PublisherName: videoDetail.PublisherName,
			Duration:      videoDetail.Duration,
			Hot:           videoDetail.Hot,
			Records: &videoinfov1.VideoMetaInfo_Records{
				CntBarrages:  uint32(videoDetail.CntBarrages),
				CntShares:    uint32(videoDetail.CntShares),
				CntViewed:    uint32(videoDetail.CntViewed),
				CntFavorited: uint32(videoDetail.CntFavorited),
			},
			VideoPath:   videoDetail.VideoPath,
			CoverPath:   videoDetail.CoverPath,
			Size:        videoDetail.Size,
			CreatedTime: timestamppb.New(videoDetail.CreatedAt),
		}

		// 游客不访问个人状态仓储，避免读取或初始化 user_id=0 的互动记录。
		if userId > 0 {
			status, err := uc.repo.GetUserVideoStatus(ctx, videoId, userId)
			if err != nil {
				return err
			}
			favoriteID, recordID, err := uc.repo.GetUserFavorite(ctx, userId, videoId)
			if err != nil {
				return err
			}
			isFavorited := int32(-1)
			if favoriteID > 0 {
				isFavorited = 1
			}
			ret.UserVideoStatus = &videoinfov1.GetVideoInfoResp_UserVideoStatus{
				IsFavorited:      isFavorited,
				FavoriteId:       favoriteID,
				FavoriteRecordId: recordID,
				IsShared:         checkIsShared(status),
				IsUpvoted:        checkIsUpvoted(status),
				IsThrownShells:   checkIsThrownShell(status),
			}
		}

		// 公共统计不依赖登录身份，必须在游客提前返回前更新。
		err = uc.repo.AddVideoCntView(ctx, videoId)
		if err != nil {
			return err
		}

		// 游客没有个人观看历史，不写入 user_id=0 的历史记录。
		if userId == 0 {
			return nil
		}
		err = uc.repo.UpdateUserVideoHistory(ctx, videoId, userId)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, helper.HandleError(errdef.ErrGetVideoInfoFailed, err)
	}
	return ret, nil
}

func (uc *VideoInfoUsecase) GetVideoListByClass(ctx context.Context, class []string, pageNum int32, pageSize int32) ([]*model.Video, error) {
	videoList, err := uc.repo.GetVideoListByClass(ctx, class, pageNum, pageSize)
	if err != nil {
		return nil, helper.HandleError(errdef.ErrGetVideoListFailed, err)
	}
	return videoList, nil
}

// UploadVideoInfo 创建视频元信息和待上传封面的初始状态，事务成功提交后才返回视频 ID。
func (uc *VideoInfoUsecase) UploadVideoInfo(ctx context.Context, info *videoinfov1.VideoMetaInfo) (int64, error) {
	videoInfo := &model.Video{
		VideoID:       getid.GetID(),
		Title:         info.Title,
		Description:   info.Description,
		Class:         strings.Join(info.Classes, methods.Separator),
		Tags:          strings.Join(info.Tags, methods.Separator),
		PublisherID:   info.PublisherId,
		PublisherName: info.PublisherName,
		Duration:      info.Duration,
	}
	err := uc.tx.WithTx(ctx, func(txCtx context.Context) error {
		if err := uc.repo.CreateBasicVideoInfo(txCtx, videoInfo); err != nil {
			return err
		}
		// updated_time 不是 GORM 的默认更新时间列，显式写入初始值。
		now := time.Now()
		return uc.repo.CreateVideoUploadStatus(txCtx, &model.VideoUploadStatus{
			VideoID:     videoInfo.VideoID,
			CreatedAt:   now,
			UpdatedTime: now,
			Status:      videoUploadWaitingCover,
		})
	})
	if err != nil {
		return 0, helper.HandleError(errdef.ErrUploadVideoInfoFailed, err)
	}
	return videoInfo.VideoID, nil
}

// UploadVideoFile 校验发布者并接收原片，落盘后启动后台 DASH 任务并发送回执。
// 接收失败时清理本次原片，后台任务启动后由任务管理文件和最终状态。
func (uc *VideoInfoUsecase) UploadVideoFile(stream grpc.ClientStreamingServer[videoinfov1.UploadVideoFileReq, emptypb.Empty]) error {
	uc.logger.Info("receiving the video file")
	req, err := stream.Recv()
	if err == io.EOF {
		return errdef.ErrEmptyVideoUploadFile
	}
	if err != nil {
		return helper.HandleError(errdef.ErrUploadVideoFileFailed, err)
	}
	meta := req.GetVideoMetadata()
	if meta == nil {
		return errdef.ErrInvalidVideoUploadStream
	}
	videoId, publisherId := meta.VideoId, meta.PublisherId
	ctx := stream.Context()
	if err := uc.checkVideoUploadOwner(ctx, videoId, publisherId); err != nil {
		return err
	}
	filename := meta.VideoFileName
	if !filepath.IsLocal(filename) || strings.ContainsAny(filename, "/\\:") ||
		strings.TrimRight(filename, ". ") != filename {
		return errdef.ErrInvalidVideoFileName
	}
	if err := uc.beginVideoFileUpload(ctx, videoId); err != nil {
		return err
	}

	videoFilePath := filepath.Join(
		resourcePath,
		strconv.FormatInt(publisherId, 10),
		videoPath,
		strconv.FormatInt(videoId, 10),
		filename,
	)
	if err := uc.receiveVideoFile(stream, videoId, videoFilePath); err != nil {
		return err
	}
	// 即使回执传输失败，已经落盘的原片仍由后台任务继续处理。
	go uc.processVideoFile(videoId, videoFilePath)
	return stream.SendAndClose(&emptypb.Empty{})
}

// receiveVideoFile 在占用 uploading 阶段后接收原片，并在关闭文件后推进到 processing。
// 接收或状态更新失败时记录失败状态，并仅清理本次创建的原片。
func (uc *VideoInfoUsecase) receiveVideoFile(stream grpc.ClientStreamingServer[videoinfov1.UploadVideoFileReq, emptypb.Empty], videoId int64, videoFilePath string) (retErr error) {
	videoFile, err := file.CreateFile(videoFilePath, os.ModePerm)
	defer func() {
		if retErr == nil {
			return
		}
		if videoFile != nil {
			_ = videoFile.Close()
		}
		if uc.failVideoUpload(videoId, videoUploadUploading, "接收原片失败", retErr) && videoFile != nil {
			_ = os.Remove(videoFilePath)
		}
	}()
	if err != nil {
		return helper.HandleError(errdef.ErrUploadVideoFileFailed, err)
	}

	hasData := false
	for {
		req, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return helper.HandleError(errdef.ErrUploadVideoFileFailed, err)
		}
		data, ok := req.GetVideoFile().(*videoinfov1.UploadVideoFileReq_VideoFileData)
		if !ok {
			return errdef.ErrInvalidVideoUploadStream
		}
		if _, err := videoFile.Write(data.VideoFileData); err != nil {
			return helper.HandleError(errdef.ErrUploadVideoFileFailed, err)
		}
		hasData = hasData || len(data.VideoFileData) > 0
	}
	if !hasData {
		return errdef.ErrEmptyVideoUploadFile
	}
	if err := videoFile.Close(); err != nil {
		return helper.HandleError(errdef.ErrUploadVideoFileFailed, err)
	}
	writeCtx, cancel := context.WithTimeout(context.Background(), videoUploadWriteTimeout)
	defer cancel()
	if err := uc.changeVideoUploadStatus(writeCtx, videoId, videoUploadUploading, videoUploadProcessing, ""); err != nil {
		return helper.HandleError(errdef.ErrUploadVideoFileFailed, err)
	}
	return nil
}

// UploadVideoCover 接收封面，在一个事务中保存路径并推进上传状态，提交成功后发送回执。
func (uc *VideoInfoUsecase) UploadVideoCover(stream grpc.ClientStreamingServer[videoinfov1.UploadVideoCoverReq, emptypb.Empty]) error {
	var (
		videoId   int64
		coverFile *os.File
		hasData   bool
		committed bool
	)

	defer func() {
		if coverFile != nil {
			_ = coverFile.Close()
			if !committed {
				_ = os.Remove(coverFile.Name())
			}
		}
	}()

	for {
		req, err := stream.Recv()
		if err == io.EOF { // 传输完成
			break
		}
		if err != nil {
			return helper.HandleError(errdef.ErrUploadVideoCoverFailed, err)
		}

		switch reqData := req.CoverFile.(type) {
		case *videoinfov1.UploadVideoCoverReq_CoverMetadata:
			meta := reqData.CoverMetadata
			if coverFile != nil || meta == nil {
				return errdef.ErrInvalidVideoUploadStream
			}
			videoId = meta.VideoId
			ctx := stream.Context()
			if err = uc.checkVideoUploadOwner(ctx, videoId, meta.PublisherId); err != nil {
				return err
			}
			uploadStatus, err := uc.repo.GetVideoUploadStatus(ctx, videoId)
			if stderr.Is(err, gorm.ErrRecordNotFound) {
				return errdef.ErrVideoCoverUploadStage
			}
			if err != nil {
				return helper.HandleError(errdef.ErrUploadVideoCoverFailed, err)
			}
			if uploadStatus.Status != videoUploadWaitingCover {
				return errdef.ErrVideoCoverUploadStage
			}
			ext := filepath.Ext(meta.CoverFileName)
			switch ext {
			case ".jpg", ".jpeg", ".png", ".jfif":
			default:
				return errdef.ErrInvalidVideoCoverFormat
			}
			coverDir := filepath.Join(
				resourcePath,
				strconv.FormatInt(meta.PublisherId, 10),
				videoPath,
				strconv.FormatInt(videoId, 10),
				"covers")
			if err = os.MkdirAll(coverDir, os.ModePerm); err != nil {
				return helper.HandleError(errdef.ErrUploadVideoCoverFailed, err)
			}
			// 每次尝试使用独立文件。只有事务提交的路径可被下载，失败请求只删除自己的文件。
			coverFile, err = os.CreateTemp(coverDir, coverName+"-*"+ext)
			if err != nil {
				return helper.HandleError(errdef.ErrUploadVideoCoverFailed, err)
			}
		case *videoinfov1.UploadVideoCoverReq_CoverFileData:
			if coverFile == nil {
				return errdef.ErrInvalidVideoUploadStream
			}
			if _, err = coverFile.Write(reqData.CoverFileData); err != nil {
				return helper.HandleError(errdef.ErrUploadVideoCoverFailed, err)
			}
			hasData = hasData || len(reqData.CoverFileData) > 0
		default:
			return errdef.ErrInvalidVideoUploadStream
		}
	}
	if !hasData {
		return errdef.ErrEmptyVideoUploadFile
	}
	if err := coverFile.Close(); err != nil {
		return helper.HandleError(errdef.ErrUploadVideoCoverFailed, err)
	}

	absCoverPath, err := filepath.Abs(coverFile.Name())
	if err != nil {
		return helper.HandleError(errdef.ErrUploadVideoCoverFailed, err)
	}
	ctx, cancel := context.WithTimeout(stream.Context(), 3*time.Second)
	defer cancel()
	err = uc.tx.WithTx(ctx, func(txCtx context.Context) error {
		// 再检查一次旧状态，防止两个通过流开始校验的请求都提交封面。
		updated, err := uc.repo.UpdateVideoUploadStatus(txCtx, videoId, videoUploadWaitingCover, videoUploadWaitingVideo, "", time.Now())
		if err != nil {
			return err
		}
		if !updated {
			return errdef.ErrVideoCoverUploadStage
		}
		return uc.repo.SetCoverPath(txCtx, videoId, absCoverPath)
	})
	if err != nil {
		if stderr.Is(err, errdef.ErrVideoCoverUploadStage) {
			return err
		}
		return helper.HandleError(errdef.ErrUploadVideoCoverFailed, err)
	}
	committed = true
	return stream.SendAndClose(&emptypb.Empty{})
}

// checkVideoUploadOwner 校验视频 ID 和调用者身份，确认调用者为实际发布者。
func (uc *VideoInfoUsecase) checkVideoUploadOwner(ctx context.Context, videoId, publisherId int64) error {
	if videoId <= 0 || publisherId <= 0 {
		return errdef.ErrInvalidVideoUploadIDs
	}
	ownerId, err := uc.repo.GetPublisherIdByVideoId(ctx, videoId)
	if stderr.Is(err, gorm.ErrRecordNotFound) {
		return errdef.ErrVideoNotFound
	}
	if err != nil {
		return err
	}
	if ownerId != publisherId {
		return errdef.ErrVideoUploadForbidden
	}
	return nil
}

// beginVideoFileUpload 仅允许待传原片的请求占用上传状态，阻止并发请求写入同一视频目录。
func (uc *VideoInfoUsecase) beginVideoFileUpload(ctx context.Context, videoId int64) error {
	uploadStatus, err := uc.repo.GetVideoUploadStatus(ctx, videoId)
	if stderr.Is(err, gorm.ErrRecordNotFound) {
		return errdef.ErrVideoFileUploadStage
	}
	if err != nil {
		return helper.HandleError(errdef.ErrUploadVideoFileFailed, err)
	}
	switch uploadStatus.Status {
	case videoUploadWaitingVideo:
		err := uc.changeVideoUploadStatus(ctx, videoId, videoUploadWaitingVideo, videoUploadUploading, "")
		if err != nil && !stderr.Is(err, errdef.ErrVideoFileUploadStage) {
			return helper.HandleError(errdef.ErrUploadVideoFileFailed, err)
		}
		return err
	case videoUploadWaitingCover:
		return errdef.ErrVideoCoverRequired
	default:
		return errdef.ErrVideoFileUploadStage
	}
}

// GetVideoFile 查询视频文件并发送下载流。
// 输入：videoId 为目标视频；stream.Context() 传递请求取消和超时。
// 输出：查询或传输错误；文件在流发送结束后关闭。
func (uc *VideoInfoUsecase) GetVideoFile(videoId int64, stream grpc.ServerStreamingServer[videoinfov1.GetVideoFileResp]) error {
	videoFilePath, err := uc.repo.GetVideoFilePathById(stream.Context(), videoId)
	if err != nil {
		return helper.HandleError(errdef.ErrGetVideoFileFailed, err)
	}
	// 1. Send the video file name to the client.
	//err = stream.Send(&videoinfov1.GetVideoFileResp{
	//	VideoFile: &videoinfov1.FileResp{
	//		File: &videoinfov1.FileResp_Filename{
	//			Filename: url.PathEscape(filepath.Base(videoFilePath)),
	//		},
	//	},
	//})
	//if err != nil {
	//	return helper.HandleError(errdef.ErrGetVideoFileFailed, err)
	//}
	//
	//// 2. Send the video file data to the client.
	//videoFile, err := os.Open(videoFilePath)
	//if err != nil {
	//	return helper.HandleError(errdef.ErrGetVideoFileFailed, err)
	//}
	//defer videoFile.Close()
	//buf := make([]byte, 1*mb)
	//for {
	//	v, err := videoFile.Read(buf)
	//	if err == io.EOF {
	//		break
	//	}
	//	if err != nil {
	//		return helper.HandleError(errdef.ErrGetVideoFileFailed, err)
	//	}
	//	err = stream.Send(&videoinfov1.GetVideoFileResp{
	//		VideoFile: &videoinfov1.FileResp{
	//			File: &videoinfov1.FileResp_FileData{
	//				FileData: buf[:v],
	//			},
	//		},
	//	})
	//	if err != nil {
	//		return helper.HandleError(errdef.ErrGetVideoFileFailed, err)
	//	}
	//}

	filenameMsgFn := func(filename string) *videoinfov1.GetVideoFileResp {
		return &videoinfov1.GetVideoFileResp{
			VideoFile: &videoinfov1.FileResp{
				File: &videoinfov1.FileResp_Filename{
					Filename: filename,
				},
			},
		}
	}
	fileDataMsgFn := func(data []byte) *videoinfov1.GetVideoFileResp {
		return &videoinfov1.GetVideoFileResp{
			VideoFile: &videoinfov1.FileResp{
				File: &videoinfov1.FileResp_FileData{
					FileData: data,
				},
			},
		}
	}
	err = sendFileStream(stream, videoFilePath, filenameMsgFn, fileDataMsgFn)
	return err
}

// GetVideoMpd 从实际发布者的视频目录读取 DASH 清单。
// 输入：videoId 必须为正数；stream.Context() 用于查询发布者并传递取消和超时。
// 输出：依次发送清单文件名和内容；参数、查询或传输失败时返回错误。
func (uc *VideoInfoUsecase) GetVideoMpd(videoId int64, stream grpc.ServerStreamingServer[videoinfov1.GetVideoMpdResp]) error {
	if videoId <= 0 {
		return status.Error(codes.InvalidArgument, "video_id must be positive")
	}
	publisherId, err := uc.repo.GetPublisherIdByVideoId(stream.Context(), videoId)
	if err != nil {
		return helper.HandleError(errdef.ErrGetVideoMpdFailed, err)
	}
	if publisherId <= 0 {
		return errdef.ErrVideoNotFound
	}
	mpdPath := filepath.Join(
		resourcePath,
		strconv.FormatInt(publisherId, 10),
		videoPath,
		strconv.FormatInt(videoId, 10),
		dashPath,
		dashName)

	filenameMsgFn := func(filename string) *videoinfov1.GetVideoMpdResp {
		return &videoinfov1.GetVideoMpdResp{
			MpdFile: &videoinfov1.FileResp{
				File: &videoinfov1.FileResp_Filename{
					Filename: filename,
				},
			},
		}
	}

	fileDataMsgFn := func(data []byte) *videoinfov1.GetVideoMpdResp {
		return &videoinfov1.GetVideoMpdResp{
			MpdFile: &videoinfov1.FileResp{
				File: &videoinfov1.FileResp_FileData{
					FileData: data,
				},
			},
		}
	}
	err = sendFileStream(stream, mpdPath, filenameMsgFn, fileDataMsgFn)
	if err != nil {
		return helper.HandleError(errdef.ErrGetVideoMpdFailed, err)
	}
	return nil
}

var dashSegmentNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]*\.m4s$`)

// GetVideoSegment 在实际发布者的视频 DASH 目录中定位并发送分片。
// 输入：videoId 为正数；segmentName 为单个 .m4s 文件名，仅允许 ASCII 字母、数字、下划线和连字符作为主文件名。
// 输出：依次发送真实文件名和内容；非法参数在访问仓储前拒绝，查询、路径或传输错误向上传递。
// 不接受客户端路径，并校验符号链接解析后的文件仍位于目标 DASH 目录；上下文沿用下载流。
func (uc *VideoInfoUsecase) GetVideoSegment(videoId int64, segmentName string, stream grpc.ServerStreamingServer[videoinfov1.GetVideoSegmentResp]) error {
	if videoId <= 0 || len(segmentName) > 255 || !filepath.IsLocal(segmentName) || !dashSegmentNamePattern.MatchString(segmentName) {
		return status.Error(codes.InvalidArgument, "invalid video_id or DASH segment name")
	}
	publisherId, err := uc.repo.GetPublisherIdByVideoId(stream.Context(), videoId)
	if err != nil {
		return helper.HandleError(errdef.ErrGetVideoSegmentFailed, err)
	}
	if publisherId <= 0 {
		return errdef.ErrVideoNotFound
	}
	dir := filepath.Join(resourcePath, strconv.FormatInt(publisherId, 10), videoPath, strconv.FormatInt(videoId, 10), dashPath)
	realDir, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return helper.HandleError(errdef.ErrGetVideoSegmentFailed, err)
	}
	segmentPath, err := filepath.EvalSymlinks(filepath.Join(realDir, segmentName))
	if err != nil {
		return helper.HandleError(errdef.ErrGetVideoSegmentFailed, err)
	}
	rel, err := filepath.Rel(realDir, segmentPath)
	if err != nil || !filepath.IsLocal(rel) {
		return status.Error(codes.PermissionDenied, "DASH segment resolves outside the video directory")
	}
	filenameMsgFn := func(filename string) *videoinfov1.GetVideoSegmentResp {
		return &videoinfov1.GetVideoSegmentResp{
			SegmentFile: &videoinfov1.FileResp{
				File: &videoinfov1.FileResp_Filename{
					Filename: filename,
				},
			},
		}
	}

	fileDataMsgFn := func(data []byte) *videoinfov1.GetVideoSegmentResp {
		return &videoinfov1.GetVideoSegmentResp{
			SegmentFile: &videoinfov1.FileResp{
				File: &videoinfov1.FileResp_FileData{
					FileData: data,
				},
			},
		}
	}
	err = sendFileStream(stream, segmentPath, filenameMsgFn, fileDataMsgFn)
	if err != nil {
		return helper.HandleError(errdef.ErrGetVideoSegmentFailed, err)
	}
	return nil
}

// GetVideoCover 根据已提交的封面路径发送下载流，旧记录没有路径时沿用发布者目录查找。
// 输入：videoId 必须为正数；stream.Context() 用于仓储查询和取消传递。
// 输出：真实封面文件名及内容；参数、查询、查找或传输失败时返回错误。
func (uc *VideoInfoUsecase) GetVideoCover(videoId int64, stream grpc.ServerStreamingServer[videoinfov1.GetVideoCoverResp]) error {
	if videoId <= 0 {
		return status.Error(codes.InvalidArgument, "video_id must be positive")
	}
	video, err := uc.repo.GetVideoInfoById(stream.Context(), videoId)
	if err != nil {
		return helper.HandleError(errdef.ErrGetVideoCoverFailed, err)
	}
	if video.PublisherID <= 0 {
		return errdef.ErrVideoNotFound
	}
	coverFilePath := video.CoverPath
	if coverFilePath == "" {
		coverDirPath := filepath.Join(
			resourcePath,
			strconv.FormatInt(video.PublisherID, 10),
			videoPath,
			strconv.FormatInt(videoId, 10))
		coverFilePath, err = file.NewFileSearcher().Find(coverDirPath, coverName)
		if err != nil {
			return helper.HandleError(errdef.ErrGetVideoCoverFailed, err)
		}
	}

	filenameMsgFn := func(filename string) *videoinfov1.GetVideoCoverResp {
		return &videoinfov1.GetVideoCoverResp{
			CoverFile: &videoinfov1.FileResp{
				File: &videoinfov1.FileResp_Filename{
					Filename: filename,
				},
			},
		}
	}

	fileDataMsgFn := func(data []byte) *videoinfov1.GetVideoCoverResp {
		return &videoinfov1.GetVideoCoverResp{
			CoverFile: &videoinfov1.FileResp{
				File: &videoinfov1.FileResp_FileData{
					FileData: data,
				},
			},
		}
	}
	err = sendFileStream(stream, coverFilePath, filenameMsgFn, fileDataMsgFn)
	if err != nil {
		return helper.HandleError(errdef.ErrGetVideoCoverFailed, err)
	}
	return nil
}

func (uc *VideoInfoUsecase) GetPublisherIdByVideoId(ctx context.Context, videoId int64) (int64, error) {
	return uc.repo.GetPublisherIdByVideoId(ctx, videoId)
}

// sendFileStream 以文件名、数据块的顺序发送本地普通文件。
// 输入：stream 提供取消上下文；filePath 来自可信的业务定位；两个回调分别构造元信息和数据消息。
// 输出：文件打开、读取、取消或任一发送失败时返回错误，成功返回 nil；文件始终关闭。
// 先打开文件再发送元信息，避免不存在的文件产生成功响应；每轮读取前检查取消状态。
func sendFileStream[resT any, streamT grpc.ServerStreamingServer[resT]](
	stream streamT,
	filePath string,
	fileMetaMsgFn func(filename string) *resT,
	fileDataMsgFn func(data []byte) *resT,
) error {
	if err := stream.Context().Err(); err != nil {
		return err
	}
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("not a regular file")
	}
	if err := stream.Send(fileMetaMsgFn(filepath.Base(filePath))); err != nil {
		return err
	}

	buf := make([]byte, 1*mb)
	for {
		if err := stream.Context().Err(); err != nil {
			return err
		}
		n, err := f.Read(buf)
		if n > 0 {
			if sendErr := stream.Send(fileDataMsgFn(buf[:n])); sendErr != nil {
				return sendErr
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}
