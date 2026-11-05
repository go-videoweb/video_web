package biz

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"testing"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
	api "vw_user/api/v1/userfile"
)

// avatarMemoryRepo 模拟一个已存在用户，不初始化真实数据库。
type avatarMemoryRepo struct {
	path      string // 当前头像路径，空值表示未设置。
	updateErr error  // 模拟数据库切换失败。
	getErr    error  // 模拟用户不存在或读取失败。
	updates   int    // 实际执行路径更新的次数。
}

// GetAvatarPath 返回内存路径或模拟错误。
func (r *avatarMemoryRepo) GetAvatarPath(context.Context, int64) (string, error) {
	return r.path, r.getErr
}

// UpdateAvatarPath 验证旧路径后更新，模拟与真实仓储一致的比较并切换行为。
func (r *avatarMemoryRepo) UpdateAvatarPath(_ context.Context, _ int64, previous, next string) error {
	r.updates++
	if r.updateErr != nil {
		return r.updateErr
	}
	if r.path != previous {
		return errors.New("stale avatar")
	}
	r.path = next
	return nil
}

// avatarUploadStream 模拟分块上传，不创建网络连接。
type avatarUploadStream struct {
	grpc.ServerStream                        // 本测试不使用的标准流方法。
	ctx               context.Context        // 请求上下文，可模拟取消。
	requests          []*api.UpdateAvatarReq // 按顺序收到的帧。
	at                int                    // 当前帧位置。
	receiveErr        error                  // 正常帧结束后注入的错误。
	closed            bool                   // 是否已发送成功响应。
}

// Context 返回模拟请求上下文。
func (s *avatarUploadStream) Context() context.Context {
	if s.ctx == nil {
		return context.Background()
	}
	return s.ctx
}

// Recv 按帧返回数据，结束时返回 EOF 或指定故障。
func (s *avatarUploadStream) Recv() (*api.UpdateAvatarReq, error) {
	if s.at == len(s.requests) {
		if s.receiveErr != nil {
			return nil, s.receiveErr
		}
		return nil, io.EOF
	}
	req := s.requests[s.at]
	s.at++
	return req, nil
}

// SendAndClose 记录成功关闭，断言不会返回 nil protobuf。
func (s *avatarUploadStream) SendAndClose(response *emptypb.Empty) error {
	if response == nil {
		return errors.New("nil success response")
	}
	s.closed = true
	return nil
}

// avatarPNG 创建有实际像素的完整 PNG，格式识别不依赖扩展名。
func avatarPNG(t *testing.T, value uint8) []byte {
	t.Helper()
	img := image.NewNRGBA(image.Rect(0, 0, 2, 2))
	img.Set(0, 0, color.NRGBA{R: value, A: 255})
	var data bytes.Buffer
	if err := png.Encode(&data, img); err != nil {
		t.Fatal(err)
	}
	return data.Bytes()
}

// avatarFixture 创建隔离的默认头像和文件用例，所有写入均在测试临时目录内。
func avatarFixture(t *testing.T) (*FileUsecase, *avatarMemoryRepo, []byte) {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "default"), 0755); err != nil {
		t.Fatal(err)
	}
	original := avatarPNG(t, 1)
	path := filepath.Join(root, "default", defaultAvatarName)
	if err := os.WriteFile(path, original, 0600); err != nil {
		t.Fatal(err)
	}
	repo := &avatarMemoryRepo{path: path}
	usecase := NewFileUsecase(log.NewStdLogger(io.Discard), repo)
	usecase.resourceDir = root
	return usecase, repo, original
}

// avatarFrames 将完整图片拆成多个块，首帧只放可信用户 ID。
func avatarFrames(data []byte) []*api.UpdateAvatarReq {
	middle := len(data) / 2
	return []*api.UpdateAvatarReq{
		{Data: &api.UpdateAvatarReq_MetaData{MetaData: &api.UpdateAvatarReq_FileMetadata{UserId: 7}}},
		{Data: &api.UpdateAvatarReq_FileContent{FileContent: data[:middle]}},
		{Data: &api.UpdateAvatarReq_FileContent{FileContent: data[middle:]}},
	}
}

// TestAvatarDefaultReplaceAndRead 验证默认读取、首次更新、后续替换及共享默认文件不被删除。
func TestAvatarDefaultReplaceAndRead(t *testing.T) {
	usecase, repo, original := avatarFixture(t)
	response, err := usecase.GetAvatar(context.Background(), 7)
	if err != nil || response.ContentType != "image/png" || !bytes.Equal(response.Content, original) {
		t.Fatalf("默认头像错误: %v", err)
	}
	var previous string
	for _, value := range []uint8{2, 3} {
		data := avatarPNG(t, value)
		stream := &avatarUploadStream{requests: avatarFrames(data)}
		if err := usecase.UpdateAvatar(stream); err != nil {
			t.Fatal(err)
		}
		if !stream.closed || repo.path == previous || filepath.Ext(repo.path) != ".png" {
			t.Fatal("新头像未保存")
		}
		if previous != "" {
			if _, err := os.Stat(previous); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("旧的自定义头像未清理")
			}
		}
		previous = repo.path
		response, err = usecase.GetAvatar(context.Background(), 7)
		if err != nil || !bytes.Equal(response.Content, data) {
			t.Fatalf("替换后读取错误: %v", err)
		}
	}
	if data, err := os.ReadFile(filepath.Join(usecase.resourceDir, "default", defaultAvatarName)); err != nil || !bytes.Equal(data, original) {
		t.Fatal("共享默认头像遭到修改")
	}
}

// TestAvatarFailuresPreserveOldFile 验证空流、错误顺序、损坏图片、超限、中断及数据库失败不会破坏旧头像。
func TestAvatarFailuresPreserveOldFile(t *testing.T) {
	for _, name := range []string{"empty", "content first", "duplicate metadata", "no content", "invalid image", "oversize", "interrupted", "database", "cancelled"} {
		t.Run(name, func(t *testing.T) {
			usecase, repo, original := avatarFixture(t)
			old := repo.path
			stream := &avatarUploadStream{requests: avatarFrames(avatarPNG(t, 9))}
			switch name {
			case "empty":
				stream.requests = nil
			case "content first":
				stream.requests = stream.requests[1:]
			case "duplicate metadata":
				stream.requests[1] = stream.requests[0]
			case "no content":
				stream.requests = stream.requests[:1]
			case "invalid image":
				stream.requests = avatarFrames([]byte("<svg>not an avatar</svg>"))
			case "oversize":
				stream.requests = avatarFrames(make([]byte, maxAvatarBytes+1))
			case "interrupted":
				stream.receiveErr = io.ErrUnexpectedEOF
			case "database":
				repo.updateErr = errors.New("write failed")
			case "cancelled":
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				stream.ctx = ctx
			}
			if err := usecase.UpdateAvatar(stream); err == nil || stream.closed {
				t.Fatal("无效上传被报告成功")
			}
			if repo.path != old {
				t.Fatal("失败后数据库指向变化")
			}
			if data, err := os.ReadFile(old); err != nil || !bytes.Equal(data, original) {
				t.Fatal("失败破坏旧头像")
			}
			files, _ := filepath.Glob(filepath.Join(usecase.resourceDir, "7", "avatar-*"))
			if len(files) != 0 {
				t.Fatal("失败留下新文件")
			}
		})
	}
}

// TestAvatarReadScopeAndFallback 验证空路径与丢失文件回退，不存在的用户或越界路径不会泄露数据。
func TestAvatarReadScopeAndFallback(t *testing.T) {
	usecase, repo, original := avatarFixture(t)
	for _, path := range []string{"", filepath.Join(usecase.resourceDir, "7", "avatar-missing.png")} {
		repo.path = path
		response, err := usecase.GetAvatar(context.Background(), 7)
		if err != nil || !bytes.Equal(response.Content, original) {
			t.Fatalf("默认回退失败: %v", err)
		}
	}
	outside := filepath.Join(t.TempDir(), "avatar.png")
	if err := os.WriteFile(outside, original, 0600); err != nil {
		t.Fatal(err)
	}
	repo.path = outside
	if _, err := usecase.GetAvatar(context.Background(), 7); err == nil {
		t.Fatal("读取了资源目录以外的文件")
	}
	repo.getErr = kerrors.NotFound("USER_NOT_FOUND", "用户不存在")
	if _, err := usecase.GetAvatar(context.Background(), 8); kerrors.Code(err) != 404 {
		t.Fatal("不存在用户被伪装成默认头像")
	}
}

// TestAvatarSymlinkEscape 验证目录和文件链接不能跳出本用户头像目录。
func TestAvatarSymlinkEscape(t *testing.T) {
	usecase, repo, data := avatarFixture(t)
	target := t.TempDir()
	if err := os.Symlink(target, filepath.Join(usecase.resourceDir, "7")); err != nil {
		t.Skipf("当前系统不允许创建符号链接: %v", err)
	}
	if err := usecase.UpdateAvatar(&avatarUploadStream{requests: avatarFrames(data)}); err == nil {
		t.Fatal("上传逃逸到符号链接目标")
	}
	if err := os.WriteFile(filepath.Join(target, "avatar.png"), data, 0600); err != nil {
		t.Fatal(err)
	}
	repo.path = filepath.Join(usecase.resourceDir, "7", "avatar.png")
	if _, err := usecase.GetAvatar(context.Background(), 7); err == nil {
		t.Fatal("读取逃逸到符号链接目标")
	}
}

// TestAvatarActualContentAndDimensions 验证 MIME 来自内容、损坏图片及超大解码尺寸会被拒绝。
func TestAvatarActualContentAndDimensions(t *testing.T) {
	data := avatarPNG(t, 5)
	contentType, extension, err := validateAvatar(data)
	if err != nil || contentType != "image/png" || extension != ".png" {
		t.Fatal("实际 PNG 内容未识别")
	}
	if _, _, err := validateAvatar(data[:len(data)/2]); err == nil {
		t.Fatal("截断 PNG 被接受")
	}
	var large bytes.Buffer
	if err := png.Encode(&large, image.NewNRGBA(image.Rect(0, 0, maxAvatarDimension+1, 1))); err != nil {
		t.Fatal(err)
	}
	if _, _, err := validateAvatar(large.Bytes()); err == nil {
		t.Fatal("过大解码尺寸被接受")
	}
}

// TestAvatarFiveMiBBoundary 验证恰好 5 MiB 的有效图片可保存及读回，多一个字节被拒绝。
func TestAvatarFiveMiBBoundary(t *testing.T) {
	usecase, repo, _ := avatarFixture(t)
	image := avatarPNG(t, 9)
	// 在完整 PNG 后补齐字节，仅测试文件长度边界，图片内容仍由生产解码器验证。
	content := make([]byte, 5<<20)
	copy(content, image)
	stream := &avatarUploadStream{requests: avatarFrames(content)}
	if err := usecase.UpdateAvatar(stream); err != nil {
		t.Fatal(err)
	}
	response, err := usecase.GetAvatar(context.Background(), 7)
	if err != nil || !bytes.Equal(response.Content, content) {
		t.Fatalf("满 5 MiB 头像读取失败: %v", err)
	}
	previous := repo.path
	stream = &avatarUploadStream{requests: avatarFrames(append(content, 0))}
	if err := usecase.UpdateAvatar(stream); kerrors.Code(err) != 413 {
		t.Fatalf("超限未返回 413: %v", err)
	}
	if repo.path != previous {
		t.Fatal("超限上传修改了当前头像")
	}
}
