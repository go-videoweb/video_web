package service_test

import (
	"context"
	"io"
	"testing"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	videov1 "vw_video/api/v1/videoinfo"
	"vw_video/internal/biz"
	"vw_video/internal/data/dal/model"
	"vw_video/internal/service"
)

type favoriteVideoRepo struct {
	biz.VideoInfoRepo
	rows []*biz.FavoriteVideo
}

func (r *favoriteVideoRepo) GetFavoriteVideos(context.Context, int64, int64, int32, int32) ([]*biz.FavoriteVideo, int64, error) {
	return r.rows, int64(len(r.rows)), nil
}

// TestFavoriteVideoSummaryMapping 验证复用摘要完整字段，并将收藏时间与视频发布时间分开，空时间保持 nil。
func TestFavoriteVideoSummaryMapping(t *testing.T) {
	created, collected := time.Unix(100, 0), time.Unix(200, 0)
	row := &model.Video{VideoID: 9007199254740993, Title: "摘要", Description: "说明", VideoPath: "video", CoverPath: "cover", PublisherName: "作者", Duration: "00:01:00", CntBarrages: 2, CntViewed: 3, CreatedAt: created}
	repo := &favoriteVideoRepo{rows: []*biz.FavoriteVideo{{Video: row, CollectedAt: &collected}, {Video: row}}}
	logger := log.NewStdLogger(io.Discard)
	svc := service.NewVideoInfo(biz.NewVideoInfoUsecase(repo, nil, logger), logger)
	resp, err := svc.GetFavoriteVideos(context.Background(), &videov1.GetFavoriteVideosReq{UserId: 7, FavoritesId: 9})
	if err != nil || resp.Total != 2 || resp.PageNum != 1 || resp.PageSize != 20 || len(resp.Videos) != 2 {
		t.Fatalf("列表响应错误：%v %v", resp, err)
	}
	v := resp.Videos[0].Video
	if v.VideoId != row.VideoID || v.Title != row.Title || v.Description != row.Description || v.VideoPath != row.VideoPath || v.CoverPath != row.CoverPath || v.PublisherName != row.PublisherName || v.Duration != row.Duration || v.CntBarrages != 2 || v.CntViewed != 3 || !v.CreatedTime.AsTime().Equal(created) || !resp.Videos[0].CollectedAt.AsTime().Equal(collected) || resp.Videos[1].CollectedAt != nil {
		t.Fatal("摘要或时间被错误转换")
	}
}
