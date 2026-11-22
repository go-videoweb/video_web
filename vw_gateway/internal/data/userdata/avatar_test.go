package userdata

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net"
	"net/http/httptest"
	"testing"
	"time"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
	"google.golang.org/grpc/test/bufconn"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/emptypb"
	"google.golang.org/protobuf/types/known/timestamppb"
	rpc "vw_user/api/v1/userfile"
	info "vw_user/api/v1/userinfo"
)

// avatarRPCStream 记录真实发送帧，不提供数据库或网络。
type avatarRPCStream struct {
	grpc.ClientStream                        // 不使用的标准流方法。
	frames            []*rpc.UpdateAvatarReq // 已发送帧的独立副本。
	sendErr           error                  // 发送时模拟的错误。
	closeErr          error                  // 结束时模拟的服务端业务错误。
	closed            bool                   // 是否读取最终结果。
}

// Send 复制帧，避免测试依赖生产代码复用的读缓冲区。
func (s *avatarRPCStream) Send(req *rpc.UpdateAvatarReq) error {
	if s.sendErr != nil {
		return s.sendErr
	}
	s.frames = append(s.frames, proto.Clone(req).(*rpc.UpdateAvatarReq))
	return nil
}

// CloseAndRecv 返回成功结果或模拟错误。
func (s *avatarRPCStream) CloseAndRecv() (*emptypb.Empty, error) {
	s.closed = true
	return &emptypb.Empty{}, s.closeErr
}

// avatarRPCClient 模拟用户服务的单次头像读取和更新。
type avatarRPCClient struct {
	rpc.FileServiceClient                  // 其他方法不使用。
	response              *rpc.AvatarResp  // 模拟读取结果。
	err                   error            // 读取故障。
	stream                *avatarRPCStream // 模拟客户端上传流。
	userID                int64            // 读取时的目标用户。
	ctx                   context.Context  // 上传时收到的原始派生上下文。
}

// GetAvatar 记录目标并返回二进制，不返回文件路径。
func (c *avatarRPCClient) GetAvatar(_ context.Context, req *rpc.GetAvatarReq, _ ...grpc.CallOption) (*rpc.AvatarResp, error) {
	c.userID = req.UserId
	return c.response, c.err
}

// UpdateAvatar 返回可控上传流，保留可取消的请求上下文。
func (c *avatarRPCClient) UpdateAvatar(ctx context.Context, _ ...grpc.CallOption) (grpc.ClientStreamingClient[rpc.UpdateAvatarReq, emptypb.Empty], error) {
	c.ctx = ctx
	return c.stream, nil
}

// avatarHeader 将测试图片放入真实 multipart 文件字段，不访问真实资源目录。
func avatarHeader(t *testing.T, data []byte) *multipart.FileHeader {
	t.Helper()
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)
	part, err := writer.CreateFormFile("avatar", "untrusted.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest("PUT", "/", &body)
	request.Header.Set("Content-Type", writer.FormDataContentType())
	if err := request.ParseMultipartForm(maxAvatarBytes); err != nil {
		t.Fatal(err)
	}
	input, header, err := request.FormFile("avatar")
	if err != nil {
		t.Fatal(err)
	}
	_ = input.Close()
	t.Cleanup(func() { _ = request.MultipartForm.RemoveAll() })
	return header
}

// TestAvatarRPCForwarding 验证分块内容不丢失，身份只来自用例参数，错误可传播且上下文会释放。
func TestAvatarRPCForwarding(t *testing.T) {
	data := bytes.Repeat([]byte{1, 2, 3}, 22001)
	stream := &avatarRPCStream{}
	client := &avatarRPCClient{stream: stream, response: &rpc.AvatarResp{Content: []byte("png-data"), ContentType: "image/png"}}
	repo := &userFileRepo{data: &Data{fileClient: client}}
	if err := repo.UpdateAvatar(context.Background(), 7, avatarHeader(t, data)); err != nil {
		t.Fatal(err)
	}
	if !stream.closed || len(stream.frames) < 3 || stream.frames[0].GetMetaData().GetUserId() != 7 {
		t.Fatal("首帧身份或关闭行为错误")
	}
	var uploaded []byte
	for _, frame := range stream.frames[1:] {
		uploaded = append(uploaded, frame.GetFileContent()...)
	}
	if !bytes.Equal(uploaded, data) {
		t.Fatal("上传内容发生截断或缓冲区污染")
	}
	if client.ctx.Err() != context.Canceled {
		t.Fatal("上传派生上下文未释放")
	}
	response, err := repo.GetAvatar(context.Background(), 7)
	if err != nil || client.userID != 7 || !bytes.Equal(response.Content, client.response.Content) {
		t.Fatal("头像二进制透传失败")
	}
	for _, invalid := range []*rpc.AvatarResp{nil, {}, {Content: []byte("x"), ContentType: "text/html"}} {
		client.response = invalid
		if _, err := repo.GetAvatar(context.Background(), 7); err == nil {
			t.Fatal("异常下游响应未拒绝")
		}
	}
	failure := kerrors.NotFound("USER_NOT_FOUND", "用户不存在")
	client.err = failure
	if _, err := repo.GetAvatar(context.Background(), 7); !errors.Is(err, failure) {
		t.Fatal("读取错误丢失")
	}
	stream.sendErr, stream.closeErr = io.EOF, failure
	if err := repo.UpdateAvatar(context.Background(), 7, avatarHeader(t, data)); !errors.Is(err, failure) {
		t.Fatal("服务端提前关闭丢失真实错误")
	}
}

// avatarInfoClient 返回旧版服务器路径，验证网关不会对外泄露它。
type avatarInfoClient struct {
	info.UserinfoClient // 其他资料方法不使用。
}

// GetUserinfo 模拟用户服务资料，不连接数据库。
func (avatarInfoClient) GetUserinfo(context.Context, *info.UserinfoReq, ...grpc.CallOption) (*info.UserinfoResp, error) {
	return &info.UserinfoResp{UserInfo: &info.UserInfo{AvatarPath: "D:/private/resources/7/avatar.jpg", Birthday: timestamppb.Now()}}, nil
}

// TestUserinfoAvatarURL 验证旧字段名 avatarPath 现在包含浏览器可用地址，而不是内部路径。
func TestUserinfoAvatarURL(t *testing.T) {
	response, err := (&userinfoRepo{data: &Data{userInfoClient: avatarInfoClient{}}}).Userinfo(context.Background(), 7)
	if err != nil || response.AvatarPath != "/api/v1/users/7/avatar" {
		t.Fatalf("头像 URL 映射失败: %v %v", response, err)
	}
}

// largeAvatarServer 提供超过默认 4 MiB 的实际 protobuf 响应，只使用进程内连接。
type largeAvatarServer struct {
	rpc.UnimplementedFileServiceServer        // 未使用的其他 RPC。
	content                            []byte // 大头像内容，用于验证传输大小而非图片解码。
}

// GetAvatar 返回完整 5 MiB 或超限一字节的内容，以分别验证传输上限和业务上限。
func (s *largeAvatarServer) GetAvatar(_ context.Context, req *rpc.GetAvatarReq) (*rpc.AvatarResp, error) {
	content := s.content
	if req.UserId == 8 {
		content = append(append([]byte(nil), content...), 0)
	}
	return &rpc.AvatarResp{Content: content, ContentType: "image/png"}, nil
}

// TestAvatarFiveMiBGRPCResponse 验证头像单次调用放宽默认接收上限，但仍拒绝实际内容超限。
func TestAvatarFiveMiBGRPCResponse(t *testing.T) {
	listener := bufconn.Listen(1 << 20)
	server := grpc.NewServer()
	content := bytes.Repeat([]byte{1}, 5<<20)
	rpc.RegisterFileServiceServer(server, &largeAvatarServer{content: content})
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	connection, err := grpc.NewClient("passthrough:///avatar-test",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) { return listener.Dial() }))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	client := rpc.NewFileServiceClient(connection)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := client.GetAvatar(ctx, &rpc.GetAvatarReq{UserId: 7}); status.Code(err) != codes.ResourceExhausted {
		t.Fatalf("未复现默认 4 MiB 接收限制: %v", err)
	}
	repo := &userFileRepo{data: &Data{fileClient: client}}
	response, err := repo.GetAvatar(ctx, 7)
	if err != nil || !bytes.Equal(response.Content, content) {
		t.Fatalf("5 MiB 头像 RPC 失败: %v", err)
	}
	if _, err := repo.GetAvatar(ctx, 8); kerrors.Reason(err) != "INVALID_AVATAR_RESPONSE" {
		t.Fatalf("协议预留空间错误地放行超限头像: %v", err)
	}
}

// TestAvatarFiveMiBUploadBoundary 验证网关完整传递 5 MiB 文件，多一个字节则中止，不返回成功。
func TestAvatarFiveMiBUploadBoundary(t *testing.T) {
	for _, size := range []int{5 << 20, (5 << 20) + 1} {
		stream := &avatarRPCStream{}
		client := &avatarRPCClient{stream: stream}
		repo := &userFileRepo{data: &Data{fileClient: client}}
		err := repo.UpdateAvatar(context.Background(), 7, avatarHeader(t, make([]byte, size)))
		if size > 5<<20 {
			if kerrors.Code(err) != 413 || stream.closed {
				t.Fatalf("超限上传未中止: %v", err)
			}
			continue
		}
		if err != nil || !stream.closed {
			t.Fatalf("满 5 MiB 上传失败: %v", err)
		}
		total := 0
		for _, frame := range stream.frames {
			total += len(frame.GetFileContent())
		}
		if total != size {
			t.Fatalf("上传内容不完整: %d != %d", total, size)
		}
	}
}
