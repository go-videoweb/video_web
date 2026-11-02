package videodata

import (
	"context"
	kerrors "github.com/go-kratos/kratos/v2/errors"
	api "vw_gateway/api/v1/video/video_interact"
	rpc "vw_video/api/v1/interact"
)

// GetBarrages 转发可信用户和播放区间，保留弹幕 ID、个人状态和下一批游标。
func (i *interactRepo) GetBarrages(ctx context.Context, userID int64, req *api.GetBarragesReq) (*api.GetBarragesResp, error) {
	resp, err := i.data.videoInteractClient.GetBarrages(ctx, &rpc.GetBarragesReq{
		UserId:      userID,
		VideoId:     req.VideoId,
		StartSecond: req.StartSecond,
		EndSecond:   req.EndSecond,
		Cursor:      barrageCursorToRPC(req.Cursor),
	})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, kerrors.InternalServer("INVALID_VIDEO_RESPONSE", "视频服务未返回弹幕列表")
	}
	result := &api.GetBarragesResp{
		Barrages:   make([]*api.BarrageInfo, 0, len(resp.Barrages)),
		NextCursor: barrageCursorFromRPC(resp.NextCursor),
		HasMore:    resp.HasMore,
	}
	for _, row := range resp.Barrages {
		result.Barrages = append(result.Barrages, &api.BarrageInfo{
			BarrageId:      row.BarrageId,
			PublisherId:    row.PublisherId,
			VideoId:        row.VideoId,
			Content:        row.Content,
			Color:          row.Color,
			PositionSecond: row.PositionSecond,
			UpvoteCount:    row.UpvoteCount,
			IsUpvoted:      row.IsUpvoted,
		})
	}
	return result, nil
}

// barrageCursorToRPC 保留区间、播放秒数及 ID；nil 表示从区间起点开始读取。
func barrageCursorToRPC(cursor *api.BarrageCursor) *rpc.BarrageCursor {
	if cursor == nil {
		return nil
	}
	return &rpc.BarrageCursor{
		VideoId:        cursor.VideoId,
		StartSecond:    cursor.StartSecond,
		EndSecond:      cursor.EndSecond,
		PositionSecond: cursor.PositionSecond,
		BarrageId:      cursor.BarrageId,
	}
}

// barrageCursorFromRPC 保留下一批的结构化位置，末页不创建空游标对象。
func barrageCursorFromRPC(cursor *rpc.BarrageCursor) *api.BarrageCursor {
	if cursor == nil {
		return nil
	}
	return &api.BarrageCursor{
		VideoId:        cursor.VideoId,
		StartSecond:    cursor.StartSecond,
		EndSecond:      cursor.EndSecond,
		PositionSecond: cursor.PositionSecond,
		BarrageId:      cursor.BarrageId,
	}
}
