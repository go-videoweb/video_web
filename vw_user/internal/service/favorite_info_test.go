package service

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"
	"time"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/test/bufconn"
	"gorm.io/gorm"
	favorv1 "vw_user/api/v1/favorites"
	"vw_user/internal/biz"
	"vw_user/internal/data/dal/model"
)

type favoriteInfoRepo struct {
	biz.FavoritesRepo
	row   *model.Favorite
	err   error
	calls int
	id    int64
}

// TestGetFavoriteInfoGRPC 验证生成的客户端、服务注册和大 ID 的 protobuf 编解码，不监听真实端口。
func TestGetFavoriteInfoGRPC(t *testing.T) {
	const id = int64(9007199254740993)
	repo := &favoriteInfoRepo{row: &model.Favorite{FavoritesID: id, UserID: id + 1, IsPrivate: -1}}
	logger := log.NewStdLogger(io.Discard)
	svc := NewFavoritesService(biz.NewFavoritesUsecase(repo, nil, logger), logger)
	listener := bufconn.Listen(1024 * 1024)
	server := grpc.NewServer()
	favorv1.RegisterFavoriteServer(server, svc)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop(); _ = listener.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := grpc.DialContext(ctx, "passthrough:///favorite-info", grpc.WithBlock(),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
		grpc.WithContextDialer(func(ctx context.Context, _ string) (net.Conn, error) { return listener.DialContext(ctx) }))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	resp, err := favorv1.NewFavoriteClient(conn).GetFavoriteInfo(ctx, &favorv1.GetFavoriteInfoReq{FavoriteId: id})
	if err != nil || resp.FavoriteId != id || resp.UserId != id+1 || !resp.IsPrivate || repo.id != id {
		t.Fatalf("gRPC metadata mismatch: %v %v", resp, err)
	}
}

// GetFavoritesMetadata 记录指定 ID，模拟元信息存在、缺失及存储故障。
func (r *favoriteInfoRepo) GetFavoritesMetadata(_ context.Context, id int64) (*model.Favorite, error) {
	r.calls++
	r.id = id
	return r.row, r.err
}

// TestGetFavoriteInfo 验证 RPC 隐私转换、正 ID、缺失和异常数据不被转换为公开元信息。
func TestGetFavoriteInfo(t *testing.T) {
	fault := errors.New("metadata failure")
	for _, tc := range []struct {
		name string
		id   int64
		row  *model.Favorite
		err  error
		code int32
	}{
		{name: "公开", id: 9, row: &model.Favorite{FavoritesID: 9, UserID: 7, IsPrivate: 1}},
		{name: "私密", id: 9, row: &model.Favorite{FavoritesID: 9, UserID: 7, IsPrivate: -1}},
		{name: "零ID", code: 400},
		{name: "负ID", id: -1, code: 400},
		{name: "不存在", id: 9, err: gorm.ErrRecordNotFound, code: 404},
		{name: "空记录", id: 9, code: 404},
		{name: "存储故障", id: 9, err: fault, code: 500},
		{name: "错误ID", id: 9, row: &model.Favorite{FavoritesID: 8, UserID: 7, IsPrivate: 1}, code: 500},
		{name: "缺少归属", id: 9, row: &model.Favorite{FavoritesID: 9, IsPrivate: 1}, code: 500},
		{name: "异常隐私", id: 9, row: &model.Favorite{FavoritesID: 9, UserID: 7}, code: 500},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &favoriteInfoRepo{row: tc.row, err: tc.err}
			logger := log.NewStdLogger(io.Discard)
			svc := NewFavoritesService(biz.NewFavoritesUsecase(repo, nil, logger), logger)
			resp, err := svc.GetFavoriteInfo(context.Background(), &favorv1.GetFavoriteInfoReq{FavoriteId: tc.id})
			if tc.code != 0 {
				if err == nil || resp != nil || kerrors.FromError(err).Code != tc.code {
					t.Fatalf("unexpected response: %v %v", resp, err)
				}
				if tc.err == fault && !errors.Is(err, fault) {
					t.Fatal("storage error lost")
				}
			} else if err != nil || resp.FavoriteId != 9 || resp.UserId != 7 || resp.IsPrivate != (tc.row.IsPrivate == -1) {
				t.Fatalf("metadata mismatch: %v %v", resp, err)
			}
			if tc.id <= 0 && repo.calls != 0 {
				t.Fatal("invalid ID queried storage")
			}
			if tc.id > 0 && (repo.calls != 1 || repo.id != tc.id) {
				t.Fatal("metadata query mismatch")
			}
		})
	}
}
