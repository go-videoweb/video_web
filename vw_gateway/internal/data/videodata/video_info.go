package videodata

import (
	"context"
	"fmt"
	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
	"io"
	"mime/multipart"
	"os"
	"path/filepath"
	"time"
	utilCtx "util/context"
	"util/getid"
	"util/helper"
	"vw_gateway"
	videoinfov2 "vw_gateway/api/v1/video/videoinfo"
	"vw_gateway/internal/biz/videobiz"
	"vw_gateway/internal/domain"
	errdef "vw_gateway/internal/pkg/ecode/errdef/verr"
	videoinfov1 "vw_video/api/v1/videoinfo"
)

type videoInfoRepo struct {
	data *Data
	log  *log.Helper
}

func NewVideoInfoRepo(data *Data, logger log.Logger) videobiz.VideoInfoRepo {
	return &videoInfoRepo{
		data: data,
		log:  log.NewHelper(log.With(logger, "module", "videodata/video_info")),
	}
}

func (v *videoInfoRepo) GetVideoInfo(ctx context.Context, videoId, userId int64) (*domain.VideoDetail, error) {
	resp, err := v.data.videoInfoClient.GetVideoInfo(ctx, &videoinfov1.GetVideoInfoReq{VideoId: videoId, UserId: userId})
	if err != nil {
		return nil, err
	}
	return domain.NewVideoDetail(resp), nil
}

func (v *videoInfoRepo) GetVideoList(ctx context.Context, class []string, num int32, size int32) ([]*videoinfov1.VideoSummary, error) {
	resp, err := v.data.videoInfoClient.GetVideoList(ctx, &videoinfov1.GetVideoListReq{
		Class:    class,
		PageNum:  num,
		PageSize: size,
	})
	if err != nil {
		return nil, err
	}

	return resp.VideoSummary, nil
}

func (v *videoInfoRepo) UploadVideoInfo(ctx context.Context, info *domain.VideoDetail) (int64, error) {
	resp, err := v.data.videoInfoClient.UploadVideoInfo(ctx, &videoinfov1.UploadVideoInfoReq{
		VideoInfo: &videoinfov1.VideoMetaInfo{
			PublisherId:   info.PublisherId,
			PublisherName: info.PublisherName,
			Title:         info.Title,
			Description:   info.Description,
			Classes:       info.Classes,
			Tags:          info.Tags,
			Hot:           info.Hot,
			Duration:      info.Duration,
		},
	})
	if err != nil {
		return 0, err
	}
	if resp.GetVideoId() <= 0 {
		return 0, kerrors.InternalServer("VIDEO_ID_MISSING", "video service returned an invalid video ID; verify deployment before retrying")
	}
	return resp.VideoId, nil
}

func (v *videoInfoRepo) UploadVideoCover(ctx context.Context, publisherId, videoId int64, fh *multipart.FileHeader) error {
	stream, err := v.data.videoInfoClient.UploadVideoCover(ctx)
	if err != nil {
		return err
	}

	// 1. Open the cover file
	f, err := fh.Open()
	if err != nil {
		return err
	}
	defer f.Close()

	// 2. Send the cover file name, and grpc service will create a file by the name's extension
	err = stream.Send(&videoinfov1.UploadVideoCoverReq{
		CoverFile: &videoinfov1.UploadVideoCoverReq_CoverMetadata{
			CoverMetadata: &videoinfov1.UploadVideoCoverReq_CoverInfo{
				VideoId:       videoId,
				PublisherId:   publisherId,
				CoverFileName: fh.Filename,
			},
		},
	})
	if err != nil {
		return err
	}

	// 3. Send the cover file data
	buf := make([]byte, 32*kb)
	for {
		n, err := f.Read(buf)
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		err = stream.Send(&videoinfov1.UploadVideoCoverReq{
			CoverFile: &videoinfov1.UploadVideoCoverReq_CoverFileData{
				CoverFileData: buf[:n],
			},
		})
		if err != nil {
			return err
		}
	}
	_, err = stream.CloseAndRecv()
	if err != nil {
		return err
	}
	return nil
}

func (v *videoInfoRepo) UploadVideoFile(publisherId int64, videoId int64, fh *multipart.FileHeader) error {
	ctx, cancel := utilCtx.WithTimeout(nil, 100*time.Second)
	defer cancel()
	stream, err := v.data.videoInfoClient.UploadVideoFile(ctx)
	if err != nil {
		return err
	}

	// 1. Open the video file
	f, err := fh.Open()
	if err != nil {
		return err
	}
	defer f.Close()

	// 2. Send the video file name, and grpc service will create a video file by this name
	err = stream.Send(&videoinfov1.UploadVideoFileReq{
		VideoFile: &videoinfov1.UploadVideoFileReq_VideoMetadata{
			VideoMetadata: &videoinfov1.UploadVideoFileReq_VideoFileInfo{
				VideoId:       videoId,
				PublisherId:   publisherId,
				VideoFileName: fh.Filename,
			},
		},
	})
	if err != nil {
		return err
	}

	// 3. Send the cover file data
	buf := make([]byte, 1*mb)
	for {
		n, err := f.Read(buf)
		if err != nil {
			if err == io.EOF {
				break
			}
			return err
		}

		err = stream.Send(&videoinfov1.UploadVideoFileReq{
			VideoFile: &videoinfov1.UploadVideoFileReq_VideoFileData{
				VideoFileData: buf[:n],
			},
		})
		if err != nil {
			return err
		}
	}
	_, err = stream.CloseAndRecv()
	if err != nil {
		return err
	}
	return nil
}

func (v *videoInfoRepo) GetVideoFile(ctx context.Context, videoId int64) (fileResp *videoinfov2.FileResp, retErr error) {
	var (
		tempFile *os.File
	)

	defer func() {
		if tempFile != nil {
			_ = tempFile.Close()
		}
		if retErr != nil && tempFile != nil {
			_ = os.Remove(tempFile.Name())
		}
	}()

	stream, err := v.data.videoInfoClient.GetVideoFile(ctx, &videoinfov1.GetVideoFileReq{VideoId: videoId})
	if err != nil {
		retErr = err
		return nil, retErr
	}

	getFilenameFn := func(resp *videoinfov1.GetVideoFileResp) (string, error) {
		if name, ok := resp.VideoFile.File.(*videoinfov1.FileResp_Filename); ok {
			return name.Filename, nil
		}
		return "", fmt.Errorf("invalid response: %v", resp)
	}
	getFileDataFn := func(resp *videoinfov1.GetVideoFileResp) ([]byte, error) {
		if data, ok := resp.VideoFile.File.(*videoinfov1.FileResp_FileData); ok {
			return data.FileData, nil
		}
		return nil, fmt.Errorf("invalid response: %v", resp)
	}
	fileResp, err = receiveStreamFile(stream, getFilenameFn, getFileDataFn)
	if err != nil {
		retErr = err
		return nil, retErr
	}

	// Handle HTTP response headers
	fileResp.Headers = make(map[string]*videoinfov2.FileResp_HeaderValues)
	h := fileResp.Headers
	h["Content-Type"] = new(videoinfov2.FileResp_HeaderValues)
	h["Content-Type"] = &videoinfov2.FileResp_HeaderValues{
		Value: []string{"application/octet-stream"},
	}

	h["Content-Disposition"] = &videoinfov2.FileResp_HeaderValues{
		Value: []string{fmt.Sprintf("%s; filename*=UTF-8''%s", "attachment", fileResp.Filename)},
	}
	return fileResp, nil
}

func (v *videoInfoRepo) GetVideoMpd(ctx context.Context, videoId int64) (*videoinfov2.FileResp, error) {
	md := metadata.New(nil)
	otel.GetTextMapPropagator().Inject(ctx, propagation.HeaderCarrier(md))
	ctx = metadata.NewOutgoingContext(ctx, md)
	stream, err := v.data.videoInfoClient.GetVideoMpd(ctx, &videoinfov1.GetVideoMpdReq{
		VideoId: videoId,
	})
	if err != nil {
		return nil, helper.HandleError(errdef.ErrGetVideoMpdFailed, err)
	}

	getFilenameFn := func(resp *videoinfov1.GetVideoMpdResp) (string, error) {
		if name, ok := resp.MpdFile.File.(*videoinfov1.FileResp_Filename); ok {
			return name.Filename, nil
		}
		return "", fmt.Errorf("invalid response: %v", resp)
	}
	getFileDataFn := func(resp *videoinfov1.GetVideoMpdResp) ([]byte, error) {
		if data, ok := resp.MpdFile.File.(*videoinfov1.FileResp_FileData); ok {
			return data.FileData, nil
		}
		return nil, fmt.Errorf("invalid response: %v", resp)
	}

	fileResp, err := receiveStreamFile(stream, getFilenameFn, getFileDataFn)
	if err != nil {
		return nil, helper.HandleError(errdef.ErrGetVideoMpdFailed, err)
	}

	return fileResp, nil
}

func (v *videoInfoRepo) GetVideoSegment(ctx context.Context, segmentPath string) (*videoinfov2.FileResp, error) {
	stream, err := v.data.videoInfoClient.GetVideoSegments(ctx, &videoinfov1.GetVideoSegmentReq{
		VideoSegmentPath: segmentPath,
	})
	if err != nil {
		return nil, helper.HandleError(errdef.ErrGetVideoSegmentFailed, err)
	}

	getFilenameFn := func(resp *videoinfov1.GetVideoSegmentResp) (string, error) {
		if name, ok := resp.SegmentFile.File.(*videoinfov1.FileResp_Filename); ok {
			return name.Filename, nil
		}
		return "", fmt.Errorf("invalid response: %v", resp)
	}
	getFileDataFn := func(resp *videoinfov1.GetVideoSegmentResp) ([]byte, error) {
		if data, ok := resp.SegmentFile.File.(*videoinfov1.FileResp_FileData); ok {
			return data.FileData, nil
		}
		return nil, fmt.Errorf("invalid response: %v", resp)
	}

	fileResp, err := receiveStreamFile(stream, getFilenameFn, getFileDataFn)
	if err != nil {
		return nil, helper.HandleError(errdef.ErrGetVideoSegmentFailed, err)
	}

	return fileResp, nil
}

func (v *videoInfoRepo) GetVideoCover(ctx context.Context, videoId int64) (*videoinfov2.FileResp, error) {
	stream, err := v.data.videoInfoClient.GetVideoCover(ctx, &videoinfov1.GetVideoCoverReq{
		VideoId: videoId,
	})
	if err != nil {
		return nil, helper.HandleError(errdef.ErrGetVideoCoverFailed, err)
	}

	getFilenameFn := func(resp *videoinfov1.GetVideoCoverResp) (string, error) {
		if name, ok := resp.CoverFile.File.(*videoinfov1.FileResp_Filename); ok {
			return name.Filename, nil
		}
		return "", fmt.Errorf("invalid response: %v", resp)
	}
	getFileDataFn := func(resp *videoinfov1.GetVideoCoverResp) ([]byte, error) {
		if data, ok := resp.CoverFile.File.(*videoinfov1.FileResp_FileData); ok {
			return data.FileData, nil
		}
		return nil, fmt.Errorf("invalid response: %v", resp)
	}

	fileResp, err := receiveStreamFile(stream, getFilenameFn, getFileDataFn)
	if err != nil {
		return nil, helper.HandleError(errdef.ErrGetVideoCoverFailed, err)
	}

	return fileResp, nil

}

// receiveStreamFile receives a stream of file data from the grpc server, and save the file content to a temporary file.
// The grpc server always return file name first, and then file data.
func receiveStreamFile[resT any, streamT grpc.ServerStreamingClient[resT]](
	stream streamT,
	getFilenameFn func(*resT) (filename string, err error),
	getFileDataFn func(*resT) (fileData []byte, err error),
) (*videoinfov2.FileResp, error) {
	fileResp := &videoinfov2.FileResp{}

	// 1. Receive the file name
	resp, err := stream.Recv()
	if err != nil {
		return nil, err
	}
	fileResp.Filename, err = getFilenameFn(resp)
	if err != nil {
		return nil, err
	}

	// 2. Create a temporary file to store the file data.
	// If occurs error, return the error and remove the temporary file.
	// Else, Remove the temporary file by codecs.ResponseEncoder.
	tmpFileName := "video_" + getid.GetUUID() + filepath.Ext(fileResp.Filename)
	tempFile, err := os.Create(filepath.Join(vw_gateway.ResourcePath, "/tmp", tmpFileName))
	if err != nil {
		return nil, err
	}

	defer func() {
		tempFile.Close()
		// if error occurs, remove the temporary file.
		//TODO: if remove failed, use message queue to retry later.
		if err != nil {
			_ = os.Remove(tempFile.Name())
		}
	}()
	fileResp.FilePath, err = filepath.Abs(tempFile.Name())
	if err != nil {
		return nil, err
	}

	// 3. Receive the file data
	for {
		resp, err := stream.Recv()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, err
		}

		fileContent, err := getFileDataFn(resp)
		if err != nil {
			return nil, err
		}

		_, err = tempFile.Write(fileContent)
		if err != nil {
			return nil, err
		}
	}
	return fileResp, nil
}
