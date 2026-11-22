package biz

import (
	"github.com/google/wire"
	"path/filepath"
	"runtime"
)

const (
	kb = 1024
	mb = 1024 * 1024

	coverName = "cover"
	videoPath = "video"
	dashPath  = "dash"
	dashName  = "dash.mpd"
)

// ProviderSet is biz providers.
var ProviderSet = wire.NewSet(
	NewVideoInfoUsecase,
	NewInteractUseCase,
	NewVideoCommentUsecase,
)

var (
	resourcePath string
)

func init() {
	initPath()
}
func initPath() {
	_, filename, _, _ := runtime.Caller(1)

	// tmp = internal/
	tmp := filepath.Dir(filepath.Dir(filename))

	// rootPath = video_web/
	rootPath := filepath.Dir(filepath.Dir(tmp))

	// resourcePath = video_web/resources
	resourcePath = filepath.Join(rootPath, "resources")
}
