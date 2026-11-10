package errdef

import (
	kerr "github.com/go-kratos/kratos/v2/errors"
	"vw_video/internal/pkg/ecode"
)

func init() {
	ErrGetVideoInfoFailed = kerr.New(ecode.VIDEOINFO_GetVideoInfoFailed, "获取视频失败", "获取视频信息失败，请稍后再试")
	ErrVideoNotFound = kerr.New(ecode.VIDEOINFO_VideoNotFound, "视频不存在", "获取视频信息失败，请稍后再试")
	ErrGetVideoListFailed = kerr.New(ecode.VIDEOINFO_GetVideoListFailed, "获取视频列表失败", "获取视频列表失败，请稍后再试")

	ErrUploadVideoInfoFailed = kerr.New(ecode.VIDEOINFO_UploadVideoInfoFailed, "上传视频信息失败", "上传视频信息失败，请稍后再试")
	ErrUploadVideoFileFailed = kerr.New(ecode.VIDEOINFO_UploadVideoFileFailed, "上传视频文件失败", "上传视频文件失败，请稍后再试")
	ErrUploadVideoCoverFailed = kerr.New(ecode.VIDEOINFO_UploadVideoCoverFailed, "上传视频封面失败", "上传视频封面失败，请稍后再试")
	ErrGetVideoFileFailed = kerr.New(ecode.VIDEOINFO_GetVideoFileFailed, "获取视频文件失败", "获取视频文件失败，请稍后再试")
	ErrGetVideoMpdFailed = kerr.New(ecode.VIDEOINFO_GetVideoMpdFailed, "获取视频mpd失败", "获取视频mpd失败，请稍后再试")
	ErrGetVideoCoverFailed = kerr.New(ecode.VIDEOINFO_GetVideoCoverFailed, "获取视频封面失败", "获取视频封面失败，请稍后再试")
	ErrGetVideoSegmentFailed = kerr.New(ecode.VIDEOINFO_GetVideoSegmentFailed, "获取视频服务器失败", "获取视频 segment 失败，请稍后再试")
}

var (
	ErrGetVideoListFailed     *kerr.Error
	ErrGetVideoInfoFailed     *kerr.Error
	ErrVideoNotFound          *kerr.Error
	ErrUploadVideoInfoFailed  *kerr.Error
	ErrUploadVideoFileFailed  *kerr.Error
	ErrUploadVideoCoverFailed *kerr.Error
	ErrGetVideoFileFailed     *kerr.Error
	ErrGetVideoMpdFailed      *kerr.Error
	ErrGetVideoCoverFailed    *kerr.Error
	ErrGetVideoSegmentFailed  *kerr.Error
)

var (
	ErrInvalidVideoUploadIDs      = kerr.New(ecode.VIDEOINFO_InvalidUploadIDs, "INVALID_VIDEO_UPLOAD_IDS", "视频和发布者 ID 必须为正数")
	ErrInvalidVideoUploadStream   = kerr.New(ecode.VIDEOINFO_InvalidUploadStream, "INVALID_VIDEO_UPLOAD_STREAM", "上传时须先发送一次文件元信息，再发送文件数据")
	ErrInvalidVideoFileName       = kerr.New(ecode.VIDEOINFO_InvalidFileName, "INVALID_VIDEO_FILE_NAME", "原片文件名无效")
	ErrInvalidVideoCoverFormat    = kerr.New(ecode.VIDEOINFO_InvalidCoverFormat, "INVALID_VIDEO_COVER_FORMAT", "不支持的封面文件格式")
	ErrEmptyVideoUploadFile       = kerr.New(ecode.VIDEOINFO_EmptyUploadFile, "EMPTY_VIDEO_UPLOAD_FILE", "上传的文件不能为空")
	ErrVideoUploadForbidden       = kerr.New(ecode.VIDEOINFO_UploadForbidden, "VIDEO_UPLOAD_FORBIDDEN", "只有视频发布者可以上传文件")
	ErrVideoCoverUploadStage      = kerr.New(ecode.VIDEOINFO_CoverUploadStage, "VIDEO_COVER_UPLOAD_STAGE_INVALID", "视频当前阶段不能上传封面")
	ErrVideoFileUploadStage       = kerr.New(ecode.VIDEOINFO_FileUploadStage, "VIDEO_FILE_UPLOAD_STAGE_INVALID", "视频当前阶段不能上传原片")
	ErrVideoCoverRequired         = kerr.New(ecode.VIDEOINFO_CoverRequired, "VIDEO_COVER_REQUIRED", "请先上传封面")
	ErrVideoUploadStatusNotFound  = kerr.New(ecode.VIDEOINFO_UploadStatusNotFound, "VIDEO_UPLOAD_STATUS_NOT_FOUND", "找不到视频上传状态")
	ErrGetVideoUploadStatusFailed = kerr.New(ecode.VIDEOINFO_GetUploadStatusFailed, "GET_VIDEO_UPLOAD_STATUS_FAILED", "查询视频上传状态失败，请稍后再试")
)
