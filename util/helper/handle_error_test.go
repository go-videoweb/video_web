package helper

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	kerr "github.com/go-kratos/kratos/v2/errors"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	health "google.golang.org/grpc/health/grpc_health_v1"
	"google.golang.org/grpc/test/bufconn"
)

type businessErrorHealthServer struct {
	health.UnimplementedHealthServer
	err error
}

// Check 模拟普通 RPC 返回独立业务码。
func (s *businessErrorHealthServer) Check(context.Context, *health.HealthCheckRequest) (*health.HealthCheckResponse, error) {
	return nil, s.err
}

// Watch 模拟流式 RPC 在发送数据前返回业务错误。
func (s *businessErrorHealthServer) Watch(*health.HealthCheckRequest, health.Health_WatchServer) error {
	return s.err
}

// TestBusinessErrorMiddlewareRPC 验证普通和流式 RPC 均保留业务码、reason 和原 metadata。
func TestBusinessErrorMiddlewareRPC(t *testing.T) {
	original := kerr.New(6101, "BUSINESS_FAILURE", "模拟业务失败").WithMetadata(map[string]string{"saved": "true"})
	server := grpc.NewServer(
		grpc.UnaryInterceptor(func(ctx context.Context, req interface{}, _ *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
			return BusinessErrorMiddleware()(func(ctx context.Context, req interface{}) (interface{}, error) {
				return handler(ctx, req)
			})(ctx, req)
		}),
		grpc.StreamInterceptor(func(srv interface{}, stream grpc.ServerStream, _ *grpc.StreamServerInfo, handler grpc.StreamHandler) error {
			_, err := BusinessErrorMiddleware()(func(context.Context, interface{}) (interface{}, error) {
				return nil, handler(srv, stream)
			})(stream.Context(), nil)
			return err
		}),
	)
	health.RegisterHealthServer(server, &businessErrorHealthServer{err: original})
	listener := bufconn.Listen(1 << 20)
	t.Cleanup(server.Stop)
	t.Cleanup(func() { _ = listener.Close() })
	go func() { _ = server.Serve(listener) }()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(ctx, "passthrough:///business-error",
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return listener.Dial()
		}),
	)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	client := health.NewHealthClient(conn)
	_, unaryErr := client.Check(ctx, &health.HealthCheckRequest{})
	stream, err := client.Watch(ctx, &health.HealthCheckRequest{})
	if err != nil {
		t.Fatal(err)
	}
	_, streamErr := stream.Recv()
	for _, rpcErr := range []error{unaryErr, streamErr} {
		decoded := kerr.FromError(rpcErr)
		if decoded.Reason != original.Reason || decoded.Metadata[RPCBusinessCodeKey] != strconv.Itoa(int(original.Code)) || decoded.Metadata["saved"] != "true" {
			t.Fatalf("RPC 丢失业务错误信息：%v", decoded)
		}
	}
	if _, changed := original.Metadata[RPCBusinessCodeKey]; changed {
		t.Fatal("RPC 适配改写了公共错误的 metadata")
	}
}
