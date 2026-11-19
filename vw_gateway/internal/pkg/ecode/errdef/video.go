package errdef

import (
	kerr "github.com/go-kratos/kratos/v2/errors"
	"vw_gateway/internal/pkg/ecode"
)

// ErrInvalidVideoResponse 表示视频服务未返回弹幕列表。
var ErrInvalidVideoResponse = kerr.New(ecode.InvalidVideoResponse, "INVALID_VIDEO_RESPONSE", "视频服务未返回弹幕列表")

// ErrVideoIDMissing 表示video service returned an invalid video ID; verify deployment before retrying。
var ErrVideoIDMissing = kerr.New(ecode.VideoIDMissing, "VIDEO_ID_MISSING", "video service returned an invalid video ID; verify deployment before retrying")

// ErrVideoInfoRequired 表示请填写视频元信息。
var ErrVideoInfoRequired = kerr.New(ecode.VideoInfoRequired, "VIDEO_INFO_REQUIRED", "请填写视频元信息")

// ErrSegmentPathUnsupported 表示use video_id and segment_name instead of segment_path。
var ErrSegmentPathUnsupported = kerr.New(ecode.SegmentPathUnsupported, "SEGMENT_PATH_UNSUPPORTED", "use video_id and segment_name instead of segment_path")
