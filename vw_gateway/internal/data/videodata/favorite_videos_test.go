package videodata

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
	videov1 "vw_gateway/api/v1/video/videoinfo"
	"vw_gateway/internal/biz/videobiz"
	video "vw_gateway/internal/service/video_service"
	videov2 "vw_video/api/v1/videoinfo"
)

type favoriteVideosClient struct {
	videov2.VideoInfoClient
	request  *videov2.GetFavoriteVideosReq
	response *videov2.GetFavoriteVideosResp
	err      error
	calls    int
}

func (c *favoriteVideosClient) GetFavoriteVideos(_ context.Context, req *videov2.GetFavoriteVideosReq, _ ...grpc.CallOption) (*videov2.GetFavoriteVideosResp, error) {
	c.request = req
	c.calls++
	return c.response, c.err
}

// TestFavoriteVideosGatewayChain 验证完整网关调用链传递鉴权身份、分页并映射摘要，空页和下游错误不被掩盖。
func TestFavoriteVideosGatewayChain(t *testing.T) {
	created, collected := timestamppb.New(time.Unix(100, 0)), timestamppb.New(time.Unix(200, 0))
	for _, tc := range []struct {
		name    string
		resp    *videov2.GetFavoriteVideosResp
		err     error
		invalid bool
	}{
		{name: "正常", resp: &videov2.GetFavoriteVideosResp{Total: 3, PageNum: 2, PageSize: 2, Videos: []*videov2.FavoriteVideoItem{{Video: &videov2.VideoSummary{VideoId: 9007199254740993, Title: "摘要", Description: "说明", Duration: "00:01:00", PublisherName: "作者", CoverPath: "cover", VideoPath: "video", CntBarrages: 2, CntViewed: 3, CreatedTime: created}, CollectedAt: collected}}}},
		{name: "空页", resp: &videov2.GetFavoriteVideosResp{Total: 3, PageNum: 3, PageSize: 2}},
		{name: "无权", err: kerrors.NotFound("FAVORITE_NOT_FOUND", "收藏夹不存在")},
		{name: "缺少响应", invalid: true},
		{name: "缺少摘要", resp: &videov2.GetFavoriteVideosResp{Videos: []*videov2.FavoriteVideoItem{{}}}, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &favoriteVideosClient{response: tc.resp, err: tc.err}
			logger := log.NewStdLogger(io.Discard)
			svc := video.NewVideoInfoService(videobiz.NewVideoInfoUsecase(NewVideoInfoRepo(&Data{videoInfoClient: client}, logger), logger), logger)
			ctx := context.WithValue(context.Background(), "user_id", int64(7))
			resp, err := svc.GetFavoriteVideos(ctx, &videov1.GetFavoriteVideosReq{FavoritesId: 9, PageNum: 2, PageSize: 2})
			if client.calls != 1 || client.request.UserId != 7 || client.request.FavoritesId != 9 || client.request.PageNum != 2 || client.request.PageSize != 2 {
				t.Fatal("身份或分页传递错误")
			}
			if tc.err != nil || tc.invalid {
				if err == nil || resp != nil || (tc.err != nil && !errors.Is(err, tc.err)) {
					t.Fatal("下游错误被吞掉")
				}
				return
			}
			if err != nil || resp.Total != tc.resp.Total || resp.PageNum != tc.resp.PageNum || resp.PageSize != tc.resp.PageSize || resp.Videos == nil || len(resp.Videos) != len(tc.resp.Videos) {
				t.Fatalf("响应映射错误：%v %v", resp, err)
			}
			if len(resp.Videos) > 0 {
				v := resp.Videos[0].Video
				if v.VideoId != 9007199254740993 || v.Title != "摘要" || v.Description != "说明" || v.Duration != "00:01:00" || v.PublisherName != "作者" || v.CoverPath != "cover" || v.VideoPath != "video" || v.CntBarrages != 2 || v.CntViewed != 3 || !v.CreatedTime.AsTime().Equal(created.AsTime()) || !resp.Videos[0].CollectedAt.AsTime().Equal(collected.AsTime()) {
					t.Fatal("摘要或收藏时间丢失")
				}
			}
			for _, actor := range []interface{}{nil, "7", int64(0), int64(-1)} {
				anonymous := context.Background()
				if actor != nil {
					anonymous = context.WithValue(anonymous, "user_id", actor)
				}
				if response, err := svc.GetFavoriteVideos(anonymous, &videov1.GetFavoriteVideosReq{FavoritesId: 9}); err == nil || response != nil || client.calls != 1 {
					t.Fatal("无效身份访问下游")
				}
			}
		})
	}
}

type favoriteDetailClient struct{ videov2.VideoInfoClient }

func (*favoriteDetailClient) GetVideoInfo(context.Context, *videov2.GetVideoInfoReq, ...grpc.CallOption) (*videov2.GetVideoInfoResp, error) {
	return &videov2.GetVideoInfoResp{VideoDetail: &videov2.VideoMetaInfo{VideoId: 42, Records: &videov2.VideoMetaInfo_Records{}}, UserVideoStatus: &videov2.GetVideoInfoResp_UserVideoStatus{IsUpvoted: -1, IsFavorited: 1, IsShared: -1, IsThrownShells: -1, FavoriteId: 9007199254740993, FavoriteRecordId: 9007199254740994}}, nil
}

// TestFavoriteDetailGatewayState 验证当前用户状态透传到 HTTP 响应模型，游客即使收到下游状态也不能获得它。
func TestFavoriteDetailGatewayState(t *testing.T) {
	logger := log.NewStdLogger(io.Discard)
	svc := video.NewVideoInfoService(videobiz.NewVideoInfoUsecase(NewVideoInfoRepo(&Data{videoInfoClient: &favoriteDetailClient{}}, logger), logger), logger)
	for _, signed := range []bool{false, true} {
		ctx := context.Background()
		if signed {
			ctx = context.WithValue(ctx, "user_id", int64(7))
		}
		resp, err := svc.GetVideoInfo(ctx, &videov1.GetVideoInfoReq{VideoId: 42})
		if err != nil {
			t.Fatal(err)
		}
		if !signed {
			if resp.UserVideoStatus != nil {
				t.Fatal("游客获得个人状态")
			}
		} else if resp.UserVideoStatus == nil || resp.UserVideoStatus.FavoriteId != 9007199254740993 || resp.UserVideoStatus.FavoriteRecordId != 9007199254740994 || resp.UserVideoStatus.IsFavorited != 1 || resp.UserVideoStatus.IsUpvoted != -1 {
			t.Fatal("个人状态映射错误")
		}
	}
}
