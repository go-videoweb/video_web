package file

import (
	"errors"
	"mime/multipart"
	"os"
	"path/filepath"
)

const (
	kb = 1024
	mb = 1024 * kb

	picMaxWidth = 10 * mb
)

var (
	// ErrUploadFileTooLarge 表示文件超过允许的大小。
	ErrUploadFileTooLarge = errors.New("上传文件不能超过10M")
	// ErrUploadFileType 表示文件扩展名不在支持范围内。
	ErrUploadFileType = errors.New("不支持该文件格式")
)

// picExtCheck 验证图片后缀名
var picExtCheck = make(map[string]struct{})

// videoExtCheck 验证视频后缀名
var videoExtCheck = make(map[string]struct{})

func init() {
	//支持的图片的格式
	picExtCheck[".jpg"] = struct{}{}
	picExtCheck[".jpeg"] = struct{}{}
	picExtCheck[".png"] = struct{}{}
	picExtCheck[".jfif"] = struct{}{}

	//支持的视频的格式
	videoExtCheck[".mp4"] = struct{}{}
	videoExtCheck[".mov"] = struct{}{}
	videoExtCheck[".avi"] = struct{}{}
	videoExtCheck[".mkv"] = struct{}{}
	videoExtCheck[".m4v"] = struct{}{}
	videoExtCheck[".3gp"] = struct{}{}
	videoExtCheck[".3g2"] = struct{}{}
}

// CheckIfPictureValid checks if the picture is valid
func CheckIfPictureValid(fh *multipart.FileHeader) error {
	ext := filepath.Ext(fh.Filename)
	if !checkPicExt(ext) {
		return ErrUploadFileType
	}
	if fh.Size > picMaxWidth {
		return ErrUploadFileTooLarge
	}
	return nil
}

// CheckIfVideoValid checks if the video is valid
func CheckIfVideoValid(fh *multipart.FileHeader) error {
	ext := filepath.Ext(fh.Filename)
	if !checkVideoExt(ext) {
		return ErrUploadFileType
	}
	return nil
}

func checkPicExt(ext string) bool {
	_, ok := picExtCheck[ext]
	return ok
}

func checkVideoExt(ext string) bool {
	_, ok := videoExtCheck[ext]
	return ok
}

// CheckIfFileExist checks if the file path exists
func CheckIfFileExist(filePath string) bool {
	_, err := os.Stat(filePath)
	return err == nil || os.IsExist(err)
}
