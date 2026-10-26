package videodata

import (
	"context"
	"errors"
	"io"
	"testing"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"google.golang.org/grpc"
	interactv1 "vw_gateway/api/v1/video/video_interact"
	"vw_gateway/internal/biz/videobiz"
	video "vw_gateway/internal/service/video_service"
	interactv2 "vw_video/api/v1/interact"
)

type localFavoriteClient struct {
	interactv2.VideoInteractClient
	t     *testing.T
	calls int
	req   *interactv2.SetVideoFavoriteReq
	resp  *interactv2.SetVideoFavoriteResp
	err   error
}

// SetVideoFavorite 记录唯一允许的下游调用；其他客户端方法为空，误读状态或编排时测试会失败。
func (c *localFavoriteClient) SetVideoFavorite(ctx context.Context, req *interactv2.SetVideoFavoriteReq, _ ...grpc.CallOption) (*interactv2.SetVideoFavoriteResp, error) {
	if ctx.Value("user_id") != int64(7) {
		c.t.Fatal("request identity context lost")
	}
	c.calls++
	c.req = req
	return c.resp, c.err
}

// TestFavoriteSingleVideoRPC 验证网关不读取当前状态、不调用用户服务或 DTM，只转发一次视频请求。
func TestFavoriteSingleVideoRPC(t *testing.T) {
	for _, mode := range []string{"collect", "cancel", "conflict", "missing-response"} {
		t.Run(mode, func(t *testing.T) {
			c := &localFavoriteClient{t: t, resp: &interactv2.SetVideoFavoriteResp{IsFavorited: true, FavoriteId: 13, FavoriteRecordId: 9007199254740993, Message: "收藏成功"}}
			req := &interactv1.VideoFavoriteReq{VideoId: 9, FavoriteId: 13, Favorite: 1}
			if mode == "cancel" {
				req.Favorite, req.FavoriteRecordId = -1, 9007199254740993
				c.resp = &interactv2.SetVideoFavoriteResp{Message: "取消收藏成功"}
			}
			if mode == "conflict" {
				c.err = kerrors.Conflict("FAVORITE_RECORD_CHANGED", "changed")
			}
			if mode == "missing-response" {
				c.resp = nil
			}
			logger := log.NewStdLogger(io.Discard)
			svc := video.NewInteractService(videobiz.NewInteractUsecase(NewInteractRepo(&Data{videoInteractClient: c}, logger), logger), logger)
			ctx := context.WithValue(context.Background(), "user_id", int64(7))
			resp, err := svc.VideoFavorite(ctx, req)
			if c.calls != 1 || c.req.UserId != 7 || c.req.VideoId != 9 || c.req.FavoriteId != 13 || c.req.Favorite != req.Favorite || c.req.FavoriteRecordId != req.FavoriteRecordId {
				t.Fatal("gateway combined or reordered favorite business")
			}
			if c.err != nil || c.resp == nil {
				if err == nil || resp != nil || (c.err != nil && !errors.Is(err, c.err)) {
					t.Fatal("RPC failure hidden")
				}
			} else if err != nil || resp.StatusCode != 200 || resp.Message != c.resp.Message || resp.IsFavorited != c.resp.IsFavorited || resp.FavoriteRecordId != c.resp.FavoriteRecordId || resp.FavoriteId != c.resp.FavoriteId {
				t.Fatalf("response mismatch: %v %v", resp, err)
			}
			for _, actor := range []interface{}{nil, "7", int64(0), int64(-1)} {
				ctx := context.Background()
				if actor != nil {
					ctx = context.WithValue(ctx, "user_id", actor)
				}
				if resp, err := svc.VideoFavorite(ctx, req); err == nil || resp != nil || c.calls != 1 {
					t.Fatal("invalid identity reached video")
				}
			}
		})
	}
}
