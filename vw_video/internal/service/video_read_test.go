package service_test

import (
	"context"
	"errors"
	"io"
	"reflect"
	"testing"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	videov1 "vw_video/api/v1/videoinfo"
	"vw_video/internal/biz"
	"vw_video/internal/data/dal/model"
	"vw_video/internal/service"
)

type readRepo struct {
	biz.VideoInfoRepo
	row        *model.Video
	err        error
	classes    []string
	page, size int32
}

func (r *readRepo) GetVideoListByClass(_ context.Context, classes []string, page, size int32) ([]*model.Video, error) {
	r.classes, r.page, r.size = classes, page, size
	if r.row == nil {
		return nil, r.err
	}
	return []*model.Video{r.row}, r.err
}
func (r *readRepo) GetVideoInfoById(context.Context, int64) (*model.Video, error) {
	return r.row, r.err
}
func (*readRepo) GetBarragesByVideoId(context.Context, int64) ([]*model.Barrage, error) {
	return nil, nil
}
func (*readRepo) GetUserVideoStatus(context.Context, int64, int64) (int64, error) { return 0, nil }
func (*readRepo) GetUserFavoriteID(context.Context, int64, int64) (int64, error)  { return 0, nil }
func (*readRepo) AddVideoCntView(context.Context, int64) error                    { return nil }
func (*readRepo) UpdateUserVideoHistory(context.Context, int64, int64, *model.VideoSummary) error {
	return nil
}

type readTransaction struct{}

func (readTransaction) WithTx(ctx context.Context, fn func(context.Context) error) error {
	return fn(ctx)
}

// TestVideoReadFieldsAndErrors 验证视频列表与详情的元信息、创建时间一致，空列表和仓储错误正确返回。
func TestVideoReadFieldsAndErrors(t *testing.T) {
	created := time.Date(2024, 6, 9, 10, 11, 12, 345000000, time.FixedZone("test", 8*60*60))
	for _, tc := range []struct {
		name        string
		empty, fail bool
	}{
		{name: "success"}, {name: "empty list", empty: true}, {name: "query error", fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &readRepo{row: &model.Video{
				VideoID: 42, Title: "title", Description: "description", VideoPath: "/video.mp4", CoverPath: "/cover.png",
				Duration: "00:01:00", PublisherID: 7, PublisherName: "publisher", CntBarrages: 4, CntViewed: 9, CreatedAt: created,
			}}
			if tc.empty {
				repo.row = nil
			}
			if tc.fail {
				repo.err = errors.New("query failed")
			}
			logger := log.NewStdLogger(io.Discard)
			svc := service.NewVideoInfo(biz.NewVideoInfoUsecase(repo, readTransaction{}, logger), logger)
			resp, err := svc.GetVideoList(context.Background(), &videov1.GetVideoListReq{Class: []string{"music"}, PageNum: 2, PageSize: 3})
			if !reflect.DeepEqual(repo.classes, []string{"music"}) || repo.page != 2 || repo.size != 3 {
				t.Fatal("list request changed")
			}
			if tc.fail {
				if err == nil || resp != nil {
					t.Fatal("failed query returned a success response")
				}
				detail, detailErr := svc.GetVideoInfo(context.Background(), &videov1.GetVideoInfoReq{VideoId: 42, UserId: 7})
				if detailErr == nil || detail != nil {
					t.Fatal("failed detail query returned a success response")
				}
				return
			}
			if err != nil || resp == nil {
				t.Fatalf("unexpected response: %v", err)
			}
			if tc.empty {
				if len(resp.VideoSummary) != 0 {
					t.Fatal("expected an empty list")
				}
				return
			}
			summary := resp.VideoSummary[0]
			if summary.VideoId != 42 || summary.Title != "title" || summary.CntBarrages != 4 || summary.CntViewed != 9 || summary.Description != "description" || summary.VideoPath != "/video.mp4" || summary.CreatedTime == nil || !summary.CreatedTime.AsTime().Equal(created) {
				t.Fatal("list fields were lost")
			}
			detail, err := svc.GetVideoInfo(context.Background(), &videov1.GetVideoInfoReq{VideoId: 42, UserId: 7})
			if err != nil || detail == nil || detail.VideoDetail.CreatedTime == nil || !detail.VideoDetail.CreatedTime.AsTime().Equal(created) {
				t.Fatal("detail creation time was lost")
			}
			if detail.VideoDetail.Description != summary.Description || detail.VideoDetail.VideoPath != summary.VideoPath {
				t.Fatal("list and detail fields disagree")
			}
		})
	}
}
