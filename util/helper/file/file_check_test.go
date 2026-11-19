package file

import (
	"errors"
	"mime/multipart"
	"testing"
)

// TestFileValidationErrors 验证图片和视频文件校验返回可识别的普通错误。
func TestFileValidationErrors(t *testing.T) {
	if err := CheckIfPictureValid(&multipart.FileHeader{Filename: "cover.png", Size: picMaxWidth + 1}); !errors.Is(err, ErrUploadFileTooLarge) {
		t.Fatalf("图片超限错误不符：%v", err)
	}
	if err := CheckIfPictureValid(&multipart.FileHeader{Filename: "cover.txt"}); !errors.Is(err, ErrUploadFileType) {
		t.Fatalf("图片格式错误不符：%v", err)
	}
	if err := CheckIfVideoValid(&multipart.FileHeader{Filename: "video.txt"}); !errors.Is(err, ErrUploadFileType) {
		t.Fatalf("视频格式错误不符：%v", err)
	}
	if err := CheckIfPictureValid(&multipart.FileHeader{Filename: "cover.png", Size: picMaxWidth}); err != nil {
		t.Fatalf("拒绝有效图片：%v", err)
	}
}
