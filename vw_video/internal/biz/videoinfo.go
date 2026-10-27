package biz

import (
	"context"
	"database/sql"
	stderr "errors"
	"fmt"
	"github.com/dtm-labs/dtm/client/dtmcli"
	"github.com/dtm-labs/dtm/client/dtmgrpc"
	"github.com/go-kratos/kratos/v2/log"
	grpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	emptypb "google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gorm.io/gorm"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
	utilCtx "util/context"
	"util/dbutil/mysqlutil"
	"util/ffmpeg"
	"util/getid"
	"util/helper"
	"util/helper/file"
	videoinfov1 "vw_video/api/v1/videoinfo"
	"vw_video/cmd/gorm_gen/methods"
	"vw_video/internal/data/dal/model"
	"vw_video/internal/pkg/ecode/errdef"
)

// VideoInfoRepo 定义公共视频信息、个人状态和最近观看时间的仓储能力。
type VideoInfoRepo interface {
	/* Used for dtm */
	GetSqlTx() *sql.Tx

	GetVideoInfoById(ctx context.Context, videoId int64) (*model.Video, error)
	GetVideoListByClass(ctx context.Context, class []string, pageNum int32, pageSize int32) ([]*model.Video, error)
	GetVideoFilePathById(ctx context.Context, videoId int64) (string, error)
	AddVideoCntView(ctx context.Context, videoId int64) error
	SetCoverPath(ctx context.Context, videoId int64, coverPath string) error
	CreateBasicVideoInfo(ctx context.Context, video *model.Video) error
	UpdateVideoDurationSizeInfo(ctx context.Context, videoId int64, duration string, size int64) error
	GetPublisherIdByVideoId(CTX context.Context, videoId int64) (publisherId int64, err error)
	UpdateVideoFilePath(ctx context.Context, videoId int64, videoFilePath string) error
	GetBarragesByVideoId(ctx context.Context, videoId int64) ([]*model.Barrage, error)
	UpdateVideoCntShare(ctx context.Context, tx *sql.Tx, videoID int64) error
	UpdateVideoCntShareCompensation(ctx context.Context, tx *sql.Tx, videoID int64) error
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

// GetVideoInfoById 获取视频公共详情和弹幕，并按身份处理个人数据。
// 输入：ctx 为请求上下文；videoId 为目标视频 ID；userId 为 0 表示游客，正数须由可信调用方验证。
// 输出：游客返回公共详情和弹幕，不包含个人互动状态；登录用户额外返回个人互动状态。
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

		barrages, err := uc.repo.GetBarragesByVideoId(ctx, videoId)
		if err != nil {
			return err
		}

		ret.Barrages = make([]*videoinfov1.GetVideoInfoResp_BarrageInfo, 0)
		for _, barrage := range barrages {
			ret.Barrages = append(ret.Barrages, &videoinfov1.GetVideoInfoResp_BarrageInfo{
				BarrageId: barrage.BarrageID,
				Color:     barrage.Color,
				Content:   barrage.Content,
				Time:      strings.Join([]string{barrage.Hour, barrage.Minute, barrage.Second}, ":"),
			})
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
	err := uc.repo.CreateBasicVideoInfo(ctx, videoInfo)
	if err != nil {
		return 0, helper.HandleError(errdef.ErrUploadVideoInfoFailed, err)
	}
	return videoInfo.VideoID, nil
}

func (uc *VideoInfoUsecase) UploadVideoFile(stream grpc.ClientStreamingServer[videoinfov1.UploadVideoFileReq, emptypb.Empty]) error {
	uc.logger.Info("receiving the video file")
	var (
		retErr        error
		videoId       int64
		videoFilePath string
		videoFile     *os.File
	)

	defer func() {
		if videoFile != nil {
			_ = videoFile.Close()
			if retErr != nil {
				err := os.RemoveAll(filepath.Dir(videoFilePath))
				// TODO: use message queue to retry later
				if err != nil {

				}
			}
		}
	}()

	for {
		req, err := stream.Recv()
		if err == io.EOF { // 传输完成
			uc.logger.Infof("receive the video file %s complete.", videoFilePath)
			break
		}
		if err != nil {
			retErr = helper.HandleError(errdef.ErrUploadVideoFileFailed, err)
			return retErr
		}

		switch reqData := req.VideoFile.(type) {
		// 1. Handle the metadata of the video file
		// 1.1 Receive the metadata of the video file
		case *videoinfov1.UploadVideoFileReq_VideoMetadata:
			videoId = reqData.VideoMetadata.VideoId
			publisherId := reqData.VideoMetadata.PublisherId

			// 1.2 Create video file, which path is computed by the video id and publisher(user) id.
			videoFilePath = filepath.Join(
				resourcePath,
				strconv.FormatInt(publisherId, 10),
				videoPath,
				strconv.FormatInt(videoId, 10),
				reqData.VideoMetadata.VideoFileName)
			videoFile, err = file.CreateFile(videoFilePath, os.ModePerm)
			if err != nil {
				retErr = helper.HandleError(errdef.ErrUploadVideoFileFailed, err)
				return retErr
			}
			uc.logger.Info("create video file success")
		case *videoinfov1.UploadVideoFileReq_VideoFileData:
			// 2. Handle the data of the video cover file
			fileContent := reqData.VideoFileData
			_, err = videoFile.Write(fileContent)
			if err != nil {
				retErr = helper.HandleError(errdef.ErrUploadVideoFileFailed, err)
				return retErr
			}
		}
	}
	err := stream.SendAndClose(&emptypb.Empty{})
	if err != nil {
		retErr = helper.HandleError(errdef.ErrUploadVideoFileFailed, err)
		return retErr
	}

	// 3. Handle the video file: make DASH segments files.
	// 3.1 Make DASH directory, which path is computed by the video file path.
	_ = os.MkdirAll(filepath.Dir(videoFilePath)+"/"+dashPath, os.ModePerm)

	// 3.2 Make DASH segments files.
	err = ffmpeg.MakeDASH(videoFilePath, filepath.Dir(videoFilePath)+"/"+dashPath, dashName)
	if err != nil {
		retErr = helper.HandleError(errdef.ErrUploadVideoFileFailed, err)
		return retErr
	}
	uc.logger.Info("make DASH segments files success.")

	// 4. Update the video info record
	// 4.1 Get the duration of the video file.
	ctx, cancel := utilCtx.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	err = uc.tx.WithTx(ctx, func(ctx context.Context) error {
		duration, err := ffmpeg.GetVideoDuration(videoFilePath)
		if err != nil {
			return err
		}
		durationStr, _ := helper.SecondToTime(duration)

		// 4.2 Get the size of the video file.
		size, err := file.GetFileSize(videoFilePath)
		if err != nil {
			return err
		}

		// 4.3 Set the video size and duration information of the video info record.
		ctx, cancel := utilCtx.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		err = uc.repo.UpdateVideoDurationSizeInfo(ctx, videoId, durationStr, size)
		if err != nil {
			return err
		}

		// 4.4 Set video_path of the video info record.
		err = uc.repo.UpdateVideoFilePath(ctx, videoId, videoFilePath)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		retErr = helper.HandleError(errdef.ErrUploadVideoFileFailed, err)
		return retErr
	}

	uc.logger.Info("update video size and duration information success.")
	uc.logger.Info("upload video file success.")
	return nil
}

func (uc *VideoInfoUsecase) UploadVideoCover(stream grpc.ClientStreamingServer[videoinfov1.UploadVideoCoverReq, emptypb.Empty]) error {
	var (
		retErr        error
		coverFilePath string
		videoId       int64
		coverFile     *os.File
	)

	defer func() {
		if coverFile != nil {
			_ = coverFile.Close()
			if retErr != nil {
				_ = os.Remove(coverFilePath)
			}
		}
	}()

	for {
		req, err := stream.Recv()
		if err == io.EOF { // 传输完成
			break
		}
		if err != nil {
			retErr = helper.HandleError(errdef.ErrUploadVideoCoverFailed, err)
			return retErr
		}

		switch reqData := req.CoverFile.(type) {
		case *videoinfov1.UploadVideoCoverReq_CoverMetadata:
			// 1. Handle the metadata of the video cover file
			// 1.1 Receive the metadata of the video cover file
			videoId = reqData.CoverMetadata.VideoId
			publisherId := reqData.CoverMetadata.PublisherId

			// 1.2 Create cover file, which path is computed by the video id and publisher(user) id.
			coverFilePath = filepath.Join(
				resourcePath,
				strconv.FormatInt(publisherId, 10),
				videoPath,
				strconv.FormatInt(videoId, 10),
				coverName+filepath.Ext(reqData.CoverMetadata.CoverFileName))
			coverFile, err = file.CreateFile(coverFilePath, os.ModePerm)
			if err != nil {
				retErr = helper.HandleError(errdef.ErrUploadVideoCoverFailed, err)
				return retErr
			}
		case *videoinfov1.UploadVideoCoverReq_CoverFileData:
			// 2. Handle the data of the video cover file
			fileContent := reqData.CoverFileData
			_, err = coverFile.Write(fileContent)
			if err != nil {
				retErr = helper.HandleError(errdef.ErrUploadVideoCoverFailed, err)
				return retErr
			}
		}
	}
	err := stream.SendAndClose(&emptypb.Empty{})
	if err != nil {
		retErr = helper.HandleError(errdef.ErrUploadVideoCoverFailed, err)
		return retErr
	}

	// 3. Set the cover path of the video info record.
	absCoverPath, err := filepath.Abs(coverFilePath)
	if err != nil {
		retErr = helper.HandleError(errdef.ErrUploadVideoCoverFailed, err)
		return retErr
	}
	ctx, cancel := utilCtx.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	err = uc.repo.SetCoverPath(ctx, videoId, absCoverPath)
	if err != nil {
		retErr = helper.HandleError(errdef.ErrUploadVideoCoverFailed, err)
		return retErr
	}
	return nil
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

// GetVideoCover 从实际发布者的视频目录查找封面并发送下载流。
// 输入：videoId 必须为正数；stream.Context() 用于仓储查询和取消传递。
// 输出：真实封面文件名及内容；参数、查询、查找或传输失败时返回错误。
func (uc *VideoInfoUsecase) GetVideoCover(videoId int64, stream grpc.ServerStreamingServer[videoinfov1.GetVideoCoverResp]) error {
	if videoId <= 0 {
		return status.Error(codes.InvalidArgument, "video_id must be positive")
	}
	publisherId, err := uc.repo.GetPublisherIdByVideoId(stream.Context(), videoId)
	if err != nil {
		return helper.HandleError(errdef.ErrGetVideoCoverFailed, err)
	}
	if publisherId <= 0 {
		return errdef.ErrVideoNotFound
	}
	coverDirPath := filepath.Join(
		resourcePath,
		strconv.FormatInt(publisherId, 10),
		videoPath,
		strconv.FormatInt(videoId, 10))
	coverFilePath, err := file.NewFileSearcher().Find(coverDirPath, coverName)
	if err != nil {
		return helper.HandleError(errdef.ErrGetVideoCoverFailed, err)
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

func (uc *VideoInfoUsecase) UpdateVideoCntShare(ctx context.Context, videoId int64, isCompensation bool) error {
	barrier, err := dtmgrpc.BarrierFromGrpc(ctx)
	if err != nil {
		return helper.HandleGrpcError(codes.Aborted, dtmcli.ResultFailure, err)
	}

	err = barrier.Call(uc.repo.GetSqlTx(), func(tx *sql.Tx) error {
		if isCompensation {
			return uc.repo.UpdateVideoCntShareCompensation(ctx, tx, videoId)
		} else {
			return uc.repo.UpdateVideoCntShare(ctx, tx, videoId)
		}
	})
	if err != nil {
		return helper.HandleGrpcError(codes.Aborted, dtmcli.ResultFailure, err)
	}

	return nil
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
