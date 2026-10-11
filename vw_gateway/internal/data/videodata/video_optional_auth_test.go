package videodata

import (
	"context"
	"io"
	"testing"

	"github.com/go-kratos/kratos/v2/log"
	"google.golang.org/grpc"
	videov1 "vw_gateway/api/v1/video/videoinfo"
	"vw_gateway/internal/biz/videobiz"
	video "vw_gateway/internal/service/video_service"
	videov2 "vw_video/api/v1/videoinfo"
)

type optionalViewerClient struct {
	videov2.VideoInfoClient
	request *videov2.GetVideoInfoReq
}

func (c *optionalViewerClient) GetVideoInfo(_ context.Context, req *videov2.GetVideoInfoReq, _ ...grpc.CallOption) (*videov2.GetVideoInfoResp, error) {
	c.request = req
	return &videov2.GetVideoInfoResp{VideoDetail: &videov2.VideoMetaInfo{VideoId: req.VideoId, Records: &videov2.VideoMetaInfo_Records{}}}, nil
}

// TestOptionalViewerPropagation 验证网关调用链向视频 RPC 传递 context 中的正用户 ID。
// 身份缺失、类型不符或非正数时应传入游客 ID 0，目标视频 ID 保持不变。
func TestOptionalViewerPropagation(t *testing.T) {
	for _, tc := range []struct {
		name   string
		value  interface{}
		wantID int64
	}{
		{name: "anonymous"}, {name: "signed in", value: int64(7), wantID: 7},
		{name: "wrong type", value: "7"}, {name: "zero", value: int64(0)}, {name: "negative", value: int64(-1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			client := &optionalViewerClient{}
			logger := log.NewStdLogger(io.Discard)
			svc := video.NewVideoInfoService(videobiz.NewVideoInfoUsecase(NewVideoInfoRepo(&Data{videoInfoClient: client}, logger), logger), logger)
			ctx := context.Background()
			if tc.value != nil {
				ctx = context.WithValue(ctx, "user_id", tc.value)
			}
			resp, err := svc.GetVideoInfo(ctx, &videov1.GetVideoInfoReq{VideoId: 42})
			if err != nil || resp == nil || client.request.UserId != tc.wantID || client.request.VideoId != 42 {
				t.Fatalf("viewer propagation failed: %v, %+v", err, client.request)
			}
		})
	}
}
