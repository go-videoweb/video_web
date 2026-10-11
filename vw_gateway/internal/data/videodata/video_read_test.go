package videodata

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"reflect"
	"testing"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/timestamppb"
	videov1 "vw_gateway/api/v1/video/videoinfo"
	"vw_gateway/internal/biz/videobiz"
	"vw_gateway/internal/pkg/codecs"
	video "vw_gateway/internal/service/video_service"
	videov2 "vw_video/api/v1/videoinfo"
)

type readClient struct {
	videov2.VideoInfoClient
	summary       *videov2.VideoSummary
	detail        *videov2.VideoMetaInfo
	err           error
	listRequest   *videov2.GetVideoListReq
	detailRequest *videov2.GetVideoInfoReq
}

func (c *readClient) GetVideoList(_ context.Context, req *videov2.GetVideoListReq, _ ...grpc.CallOption) (*videov2.GetVideoListResp, error) {
	c.listRequest = req
	if c.err != nil {
		return nil, c.err
	}
	resp := &videov2.GetVideoListResp{}
	if c.summary != nil {
		resp.VideoSummary = []*videov2.VideoSummary{c.summary}
	}
	return resp, nil
}
func (c *readClient) GetVideoInfo(_ context.Context, req *videov2.GetVideoInfoReq, _ ...grpc.CallOption) (*videov2.GetVideoInfoResp, error) {
	c.detailRequest = req
	if c.err != nil {
		return nil, c.err
	}
	return &videov2.GetVideoInfoResp{VideoDetail: c.detail}, nil
}

// TestGatewayVideoReadFields 验证元信息与时间字段的完整映射、旧服务兼容、空列表及 RPC 错误传播。
func TestGatewayVideoReadFields(t *testing.T) {
	created := timestamppb.New(time.Date(2024, 6, 9, 10, 11, 12, 345000000, time.FixedZone("test", 8*60*60)))
	for _, tc := range []struct {
		name             string
		empty, fail, old bool
	}{
		{name: "success"}, {name: "empty list", empty: true}, {name: "RPC error", fail: true}, {name: "old service without timestamp", old: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stamp := created
			if tc.old {
				stamp = nil
			}
			client := &readClient{
				summary: &videov2.VideoSummary{VideoId: 42, Title: "title", Description: "description", VideoPath: "/video.mp4", CoverPath: "/cover.png", CreatedTime: stamp},
				detail:  &videov2.VideoMetaInfo{VideoId: 42, Title: "title", Description: "description", VideoPath: "/video.mp4", CreatedTime: stamp, Records: &videov2.VideoMetaInfo_Records{}},
			}
			if tc.empty {
				client.summary = nil
			}
			if tc.fail {
				client.err = errors.New("RPC failed")
			}
			logger := log.NewStdLogger(io.Discard)
			svc := video.NewVideoInfoService(videobiz.NewVideoInfoUsecase(NewVideoInfoRepo(&Data{videoInfoClient: client}, logger), logger), logger)
			list, err := svc.GetVideoList(context.Background(), &videov1.GetVideoListReq{Class: []string{"music"}, PageNum: 2, PageSize: 3})
			if !reflect.DeepEqual(client.listRequest.Class, []string{"music"}) || client.listRequest.PageNum != 2 || client.listRequest.PageSize != 3 {
				t.Fatal("list request changed")
			}
			if tc.fail {
				if err == nil || list != nil {
					t.Fatal("RPC error was swallowed")
				}
				return
			}
			if err != nil || list == nil {
				t.Fatalf("unexpected list error: %v", err)
			}
			if tc.empty {
				if len(list.VideoSummary) != 0 {
					t.Fatal("expected empty list")
				}
				return
			}
			summary := list.VideoSummary[0]
			if summary.Description != "description" || summary.VideoPath != "/video.mp4" || !reflect.DeepEqual(summary.CreatedTime, stamp) {
				t.Fatal("list DTO mapping lost fields")
			}
			ctx := context.WithValue(context.Background(), "user_id", int64(7))
			detail, err := svc.GetVideoInfo(ctx, &videov1.GetVideoInfoReq{VideoId: 42})
			if err != nil || detail == nil || !reflect.DeepEqual(detail.VideoDetail.CreatedTime, stamp) {
				t.Fatal("detail DTO mapping lost time")
			}
			if client.detailRequest.VideoId != 42 || client.detailRequest.UserId != 7 {
				t.Fatal("detail request changed")
			}
			if tc.old {
				return
			}
			for _, response := range []interface{}{list, detail} {
				writer := httptest.NewRecorder()
				request := httptest.NewRequest("GET", "/api/v1/video/list", nil)
				request.Header.Set("Accept", "application/json")
				if err := codecs.ResponseEncoder(writer, request, response); err != nil {
					t.Fatal(err)
				}
				var body struct {
					VideoSummary []struct {
						CreatedTime string `json:"createdTime"`
					} `json:"VideoSummary"`
					VideoDetail struct {
						CreatedTime string `json:"createdTime"`
					} `json:"videoDetail"`
				}
				if err := json.Unmarshal(writer.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				got := body.VideoDetail.CreatedTime
				if len(body.VideoSummary) > 0 {
					got = body.VideoSummary[0].CreatedTime
				}
				if got != "2024-06-09T02:11:12.345Z" {
					t.Fatalf("timestamp JSON contract or precision changed: %s", got)
				}
			}
		})
	}
}
