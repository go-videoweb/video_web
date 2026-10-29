package service

import (
	"context"
	"github.com/go-kratos/kratos/v2/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"io"
	"net"
	"testing"
	"time"
	followv1 "vw_user/api/v1/follow"
	"vw_user/internal/biz"
)

// TestFollowListsGRPCOffline 验证新增 RPC 的生成客户端、服务注册和响应字段，不监听真实网络端口。
func TestFollowListsGRPCOffline(t *testing.T) {
	logger := log.NewStdLogger(io.Discard)
	svc := NewFollowService(biz.NewFollowUseCase(&followSummaryRepo{}, nil, nil, logger), logger)
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	followv1.RegisterFollowServer(server, svc)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(ctx, "passthrough:///follow-lists", grpc.WithBlock(), grpc.WithTransportCredentials(insecure.NewCredentials()), grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	resp, err := followv1.NewFollowClient(conn).GetFollowLists(ctx, &followv1.GetFollowListsReq{UserId: 7})
	if err != nil || len(resp.GetLists()) != 1 || resp.Lists[0].UserId != 7 || resp.Lists[0].FollowListId != 13 || !resp.Lists[0].IsDefault {
		t.Fatalf("关注列表 RPC 接线不完整：%v %v", resp, err)
	}
}
