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
	"mime"
	"mime/multipart"
	"os"
	"path/filepath"
	"strings"
	"time"
	utilCtx "util/context"
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

// GetFavoriteVideos 仅传递网关鉴权确认的身份，不以客户端参数作为访问者。
func (v *videoInfoRepo) GetFavoriteVideos(ctx context.Context, userId, favoritesId int64, pageNum, pageSize int32) (*videoinfov1.GetFavoriteVideosResp, error) {
	resp, err := v.data.videoInfoClient.GetFavoriteVideos(ctx, &videoinfov1.GetFavoriteVideosReq{
		UserId: userId, FavoritesId: favoritesId, PageNum: pageNum, PageSize: pageSize,
	})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, kerrors.InternalServer("FAVORITE_VIDEOS_MISSING", "视频服务未返回收藏夹内容响应")
	}
	for _, item := range resp.Videos {
		if item.GetVideo() == nil {
			return nil, kerrors.InternalServer("FAVORITE_VIDEO_INVALID", "视频服务返回了缺少视频摘要的收藏记录")
		}
	}
	return resp, nil
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
		if name, ok := resp.GetVideoFile().GetFile().(*videoinfov1.FileResp_Filename); ok {
			return name.Filename, nil
		}
		return "", fmt.Errorf("invalid response: %v", resp)
	}
	getFileDataFn := func(resp *videoinfov1.GetVideoFileResp) ([]byte, error) {
		if data, ok := resp.GetVideoFile().GetFile().(*videoinfov1.FileResp_FileData); ok {
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

// GetVideoMpd 获取清单 RPC 流并转换为临时文件响应。
// 输入：ctx 保留追踪、取消和超时；videoId 指定视频，不使用路由 user_id 定位文件。
// 输出：带 application/dash+xml 类型的文件响应，成功后由 HTTP 编码器清理；失败时返回包装错误。
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
		if name, ok := resp.GetMpdFile().GetFile().(*videoinfov1.FileResp_Filename); ok {
			return name.Filename, nil
		}
		return "", fmt.Errorf("invalid response: %v", resp)
	}
	getFileDataFn := func(resp *videoinfov1.GetVideoMpdResp) ([]byte, error) {
		if data, ok := resp.GetMpdFile().GetFile().(*videoinfov1.FileResp_FileData); ok {
			return data.FileData, nil
		}
		return nil, fmt.Errorf("invalid response: %v", resp)
	}

	fileResp, err := receiveStreamFile(stream, getFilenameFn, getFileDataFn)
	if err != nil {
		return nil, helper.HandleError(errdef.ErrGetVideoMpdFailed, err)
	}

	fileResp.Headers = map[string]*videoinfov2.FileResp_HeaderValues{
		"Content-Type": {Value: []string{"application/dash+xml"}},
	}
	return fileResp, nil
}

// GetVideoSegment 向视频服务请求指定视频的分片，并接收为网关临时文件。
// 输入：ctx 保留请求取消和超时；videoId、segmentName 标识目标分片，不发送旧路径字段。
// 输出：文件响应或包装后的下载错误；成功文件由 HTTP 编码器负责删除。
func (v *videoInfoRepo) GetVideoSegment(ctx context.Context, videoId int64, segmentName string) (*videoinfov2.FileResp, error) {
	stream, err := v.data.videoInfoClient.GetVideoSegments(ctx, &videoinfov1.GetVideoSegmentReq{
		VideoId:     videoId,
		SegmentName: segmentName,
	})
	if err != nil {
		return nil, helper.HandleError(errdef.ErrGetVideoSegmentFailed, err)
	}

	getFilenameFn := func(resp *videoinfov1.GetVideoSegmentResp) (string, error) {
		if name, ok := resp.GetSegmentFile().GetFile().(*videoinfov1.FileResp_Filename); ok {
			return name.Filename, nil
		}
		return "", fmt.Errorf("invalid response: %v", resp)
	}
	getFileDataFn := func(resp *videoinfov1.GetVideoSegmentResp) ([]byte, error) {
		if data, ok := resp.GetSegmentFile().GetFile().(*videoinfov1.FileResp_FileData); ok {
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

// GetVideoCover 获取封面 RPC 流并转换为临时文件响应。
// 输入：ctx 传递请求取消和超时；videoId 指定视频。
// 输出：保留真实文件名并按扩展名设置媒体类型的响应；失败时返回包装错误，成功文件由 HTTP 编码器清理。
func (v *videoInfoRepo) GetVideoCover(ctx context.Context, videoId int64) (*videoinfov2.FileResp, error) {
	stream, err := v.data.videoInfoClient.GetVideoCover(ctx, &videoinfov1.GetVideoCoverReq{
		VideoId: videoId,
	})
	if err != nil {
		return nil, helper.HandleError(errdef.ErrGetVideoCoverFailed, err)
	}

	getFilenameFn := func(resp *videoinfov1.GetVideoCoverResp) (string, error) {
		if name, ok := resp.GetCoverFile().GetFile().(*videoinfov1.FileResp_Filename); ok {
			return name.Filename, nil
		}
		return "", fmt.Errorf("invalid response: %v", resp)
	}
	getFileDataFn := func(resp *videoinfov1.GetVideoCoverResp) ([]byte, error) {
		if data, ok := resp.GetCoverFile().GetFile().(*videoinfov1.FileResp_FileData); ok {
			return data.FileData, nil
		}
		return nil, fmt.Errorf("invalid response: %v", resp)
	}

	fileResp, err := receiveStreamFile(stream, getFilenameFn, getFileDataFn)
	if err != nil {
		return nil, helper.HandleError(errdef.ErrGetVideoCoverFailed, err)
	}

	if contentType := mime.TypeByExtension(filepath.Ext(fileResp.Filename)); contentType != "" {
		fileResp.Headers = map[string]*videoinfov2.FileResp_HeaderValues{
			"Content-Type": {Value: []string{contentType}},
		}
	}
	return fileResp, nil

}

// receiveStreamFile 接收先文件名、后数据块的 RPC 流，并保存为独立临时文件。
// 输入：stream 传递请求上下文，两个回调分别提取文件名和数据块。
// 输出：成功时返回已关闭的临时文件响应，由 HTTP 编码器删除；接收、写入或关闭失败时返回错误并删除文件。
func receiveStreamFile[resT any, streamT grpc.ServerStreamingClient[resT]](
	stream streamT,
	getFilenameFn func(*resT) (filename string, err error),
	getFileDataFn func(*resT) (fileData []byte, err error),
) (_ *videoinfov2.FileResp, retErr error) {
	fileResp := &videoinfov2.FileResp{}

	if err := stream.Context().Err(); err != nil {
		return nil, err
	}
	resp, err := stream.Recv()
	if err != nil {
		return nil, err
	}
	fileResp.Filename, err = getFilenameFn(resp)
	if err != nil {
		return nil, err
	}
	if fileResp.Filename == "" || fileResp.Filename == "." || fileResp.Filename == ".." || strings.ContainsAny(fileResp.Filename, "/\\\r\n") {
		return nil, fmt.Errorf("invalid stream filename")
	}

	tmpDir := filepath.Join(vw_gateway.ResourcePath, "tmp")
	if err := os.MkdirAll(tmpDir, 0700); err != nil {
		return nil, err
	}
	tempFile, err := os.CreateTemp(tmpDir, "video_*"+filepath.Ext(fileResp.Filename))
	if err != nil {
		return nil, err
	}

	defer func() {
		_ = tempFile.Close()
		// 使用实际返回错误判断清理，避免循环内局部 err 遮蔽导致残留文件。
		if retErr != nil {
			_ = os.Remove(tempFile.Name())
		}
	}()
	fileResp.FilePath, err = filepath.Abs(tempFile.Name())
	if err != nil {
		return nil, err
	}

	for {
		if err := stream.Context().Err(); err != nil {
			return nil, err
		}
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
	if err := tempFile.Close(); err != nil {
		return nil, err
	}
	return fileResp, nil
}
