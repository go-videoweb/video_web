package biz

import (
	"context"
	"database/sql"
	stderr "errors"
	"github.com/dtm-labs/dtm/client/dtmcli"
	"github.com/dtm-labs/dtm/client/dtmgrpc"
	"github.com/go-kratos/kratos/v2/log"
	"go.mongodb.org/mongo-driver/mongo"
	grpc "google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
	emptypb "google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
	"gorm.io/gorm"
	"io"
	"os"
	"path/filepath"
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

type VideoInfoRepo interface {
	/* Used for dtm */
	GetSqlTx() *sql.Tx
	GetMongoClient() *mongo.Client
	GetDBAndCollection() (db, collection string)

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
	GetUserVideoStatus(ctx context.Context, videoId int64, userId int64) (int64, error)
	SetUserVideoStatus(ctx context.Context, videoId int64, userId int64, status int64) error
	UpdateUserVideoHistory(ctx context.Context, videoId int64, userId int64, history *model.VideoSummary) error
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
// 游客和登录用户均沿用增加播放量和热度的行为，只有登录用户写入观看历史。
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
			ret.UserVideoStatus = &videoinfov1.GetVideoInfoResp_UserVideoStatus{
				IsFavorited:    checkIsFavorited(status),
				IsShared:       checkIsShared(status),
				IsUpvoted:      checkIsUpvoted(status),
				IsThrownShells: checkIsThrownShell(status),
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
		err = uc.repo.UpdateUserVideoHistory(ctx, videoId, userId, &model.VideoSummary{
			VideoId:       videoDetail.VideoID,
			CntBarrages:   videoDetail.CntBarrages,
			CntViewed:     videoDetail.CntViewed,
			Title:         videoDetail.Title,
			Duration:      videoDetail.Duration,
			CoverPath:     videoDetail.CoverPath,
			PublisherName: videoDetail.PublisherName,
		})
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

func (uc *VideoInfoUsecase) GetVideoFile(videoId int64, stream grpc.ServerStreamingServer[videoinfov1.GetVideoFileResp]) error {
	videoFilePath, err := uc.repo.GetVideoFilePathById(context.Background(), videoId)
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

func (uc *VideoInfoUsecase) GetVideoMpd(videoId int64, stream grpc.ServerStreamingServer[videoinfov1.GetVideoMpdResp]) error {
	// TODO: 设置 context 限制超时时间，目前先不设置
	publisherId, err := uc.repo.GetPublisherIdByVideoId(utilCtx.NewBaseContext(), videoId)
	if err != nil {
		return helper.HandleError(errdef.ErrGetVideoCoverFailed, err)
	}
	mpdPath := filepath.Join(
		resourcePath,
		strconv.FormatInt(publisherId, 10),
		videoPath,
		strconv.FormatInt(videoId, 10),
		dashPath,
		"dash.mpd")

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

func (uc *VideoInfoUsecase) GetVideoSegment(videoSegmentPath string, stream grpc.ServerStreamingServer[videoinfov1.GetVideoSegmentResp]) error {
	filenameMsgFn := func(filename string) *videoinfov1.GetVideoSegmentResp {
		return &videoinfov1.GetVideoSegmentResp{
			SegmentFile: &videoinfov1.FileResp{
				File: &videoinfov1.FileResp_Filename{
					Filename: filepath.Base(videoSegmentPath),
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
	err := sendFileStream(stream, videoSegmentPath, filenameMsgFn, fileDataMsgFn)
	if err != nil {
		return helper.HandleError(errdef.ErrGetVideoSegmentFailed, err)
	}
	return nil
}

func (uc *VideoInfoUsecase) GetVideoCover(videoId int64, stream grpc.ServerStreamingServer[videoinfov1.GetVideoCoverResp]) error {
	// TODO: 设置 context 限制超时时间，目前先不设置
	publisherId, err := uc.repo.GetPublisherIdByVideoId(utilCtx.NewBaseContext(), videoId)
	if err != nil {
		return helper.HandleError(errdef.ErrGetVideoCoverFailed, err)
	}
	coverDirPath := filepath.Join(
		resourcePath,
		strconv.FormatInt(publisherId, 10),
		videoPath,
		strconv.FormatInt(videoId, 10))
	coverFilePath, err := file.NewFileSearcher().Find(coverDirPath, coverName)

	filenameMsgFn := func(filename string) *videoinfov1.GetVideoCoverResp {
		return &videoinfov1.GetVideoCoverResp{
			CoverFile: &videoinfov1.FileResp{
				File: &videoinfov1.FileResp_Filename{
					Filename: filepath.Base(coverFilePath),
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

func sendFileStream[resT any, streamT grpc.ServerStreamingServer[resT]](
	stream streamT,
	filePath string,
	fileMetaMsgFn func(filename string) *resT,
	fileDataMsgFn func(data []byte) *resT,
) error {
	filename := filepath.Base(filePath)
	// 1. Send the file name to the client.
	if err := stream.Send(fileMetaMsgFn(filename)); err != nil {
		return nil
	}

	// 2. Send the file data to the client.
	f, err := os.Open(filePath)
	if err != nil {
		return err
	}
	defer f.Close()

	buf := make([]byte, 1*mb)
	for {
		n, err := f.Read(buf)
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		err = stream.Send(fileDataMsgFn(buf[:n]))
		if err != nil {
			return err
		}
	}
	return nil
}
