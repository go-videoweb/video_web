package biz

import (
	"context"
	kerrors "github.com/go-kratos/kratos/v2/errors"
	"regexp"
	"strings"
	"unicode/utf8"
	api "vw_video/api/v1/interact"
	"vw_video/internal/data/dal/model"
)

// BarragePageSize 限制单个时间区间内一次响应的弹幕条数。
const BarragePageSize = 500

const (
	maxBarrageRangeSeconds = 3600
	maxBarrageContentRunes = 150
)

// BarrageQuery 限定视频播放区间，时间单位为非负整数秒，区间左闭右开。
type BarrageQuery struct {
	VideoID int64          // 目标视频 ID。
	UserID  int64          // 鉴权用户 ID，仅用于读取本批个人点赞状态。
	Start   int32          // 播放区间起点，单位为非负整数秒，包含该秒。
	End     int32          // 播放区间终点，单位为整数秒，不包含该秒。
	After   *BarrageCursor // 上一批末条弹幕的位置；nil 表示首批。
}

// BarrageCursor 绑定播放区间，防止切换视频或拖动进度后复用旧游标。
type BarrageCursor struct {
	VideoID  int64 `json:"video_id"`        // 原查询的视频 ID。
	Start    int32 `json:"start_second"`    // 原区间起点，单位为秒，包含该秒。
	End      int32 `json:"end_second"`      // 原区间终点，单位为秒，不包含该秒。
	Position int32 `json:"position_second"` // 末条弹幕的播放位置，单位为秒。
	ID       int64 `json:"barrage_id"`      // 末条弹幕 ID，用于同秒内继续翻页。
}

// NewBarrageQuery 校验播放区间和游标，单次时间范围最多一小时，批内最多五百条。
func NewBarrageQuery(req *api.GetBarragesReq) (*BarrageQuery, error) {
	invalid := kerrors.BadRequest("INVALID_BARRAGE_QUERY", "弹幕时间范围或游标无效")
	if req.VideoId <= 0 || req.UserId <= 0 {
		return nil, invalid
	}
	if req.StartSecond < 0 || req.EndSecond <= req.StartSecond {
		return nil, invalid
	}
	// 先转换为 int64 再求差，范围上限与整数秒参数的类型分开处理。
	rangeSeconds := int64(req.EndSecond) - int64(req.StartSecond)
	if rangeSeconds > maxBarrageRangeSeconds {
		return nil, invalid
	}
	q := &BarrageQuery{
		VideoID: req.VideoId,
		UserID:  req.UserId,
		Start:   req.StartSecond,
		End:     req.EndSecond,
	}
	if req.Cursor == nil {
		return q, nil
	}
	c := &BarrageCursor{
		VideoID:  req.Cursor.VideoId,
		Start:    req.Cursor.StartSecond,
		End:      req.Cursor.EndSecond,
		Position: req.Cursor.PositionSecond,
		ID:       req.Cursor.BarrageId,
	}
	// 拖动进度或切换视频后必须重新读取区间，不能沿用原来的翻页位置。
	if c.VideoID != q.VideoID {
		return nil, invalid
	}
	if c.Start != q.Start || c.End != q.End {
		return nil, invalid
	}
	if c.ID <= 0 {
		return nil, invalid
	}
	// 游标位置必须仍属于左闭右开的原播放区间。
	if c.Position < q.Start || c.Position >= q.End {
		return nil, invalid
	}
	q.After = c
	return q, nil
}

// NextCursor 返回本批最后一条已展示弹幕的位置和 ID，同秒弹幕按 ID 继续读取。
// 输入必须是截断后批次的末条，不能使用第 501 条预读数据，否则会漏掉该条。
func (q *BarrageQuery) NextCursor(row *model.Barrage) *api.BarrageCursor {
	return &api.BarrageCursor{
		VideoId:        q.VideoID,
		StartSecond:    q.Start,
		EndSecond:      q.End,
		PositionSecond: int32(row.PublishTime),
		BarrageId:      row.BarrageID,
	}
}

// GetBarrages 读取播放区间内的弹幕并批量附加当前用户点赞状态，不增加播放量。
func (u *InteractUsecase) GetBarrages(ctx context.Context, req *api.GetBarragesReq) (*api.GetBarragesResp, error) {
	q, err := NewBarrageQuery(req)
	if err != nil {
		return nil, err
	}
	rows, err := u.repo.GetBarragePage(ctx, q)
	if err != nil {
		return nil, err
	}
	more := len(rows) > BarragePageSize
	if more {
		rows = rows[:BarragePageSize]
	}
	ids := make([]int64, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.BarrageID)
	}
	likes, err := u.repo.GetBarrageLikes(ctx, q.UserID, q.VideoID, ids)
	if err != nil {
		return nil, err
	}
	result := &api.GetBarragesResp{Barrages: make([]*api.BarrageInfo, 0, len(rows)), HasMore: more}
	for _, row := range rows {
		result.Barrages = append(result.Barrages, &api.BarrageInfo{
			BarrageId:      row.BarrageID,
			PublisherId:    row.PublisherID,
			VideoId:        row.VideoID,
			Content:        row.Content,
			Color:          row.Color,
			PositionSecond: int32(row.PublishTime),
			UpvoteCount:    row.Likes,
			IsUpvoted:      likes[row.BarrageID],
		})
	}
	if more {
		// 多取的一条仅用于判断 has_more；下一批从本批真正返回的末条之后开始。
		result.NextCursor = q.NextCursor(rows[len(rows)-1])
	}
	return result, nil
}

// SetBarrageLike 在本地事务内设置明确点赞目标，关系和计数一起提交。
func (u *InteractUsecase) SetBarrageLike(ctx context.Context, req *api.SetBarrageLikeReq) (*api.SetBarrageLikeResp, error) {
	invalid := kerrors.BadRequest("INVALID_BARRAGE_LIKE", "必须提供身份、视频、弹幕及点赞目标状态")
	if req.UserId <= 0 || req.VideoId <= 0 {
		return nil, invalid
	}
	if req.BarrageId <= 0 {
		return nil, invalid
	}
	if req.Liked == nil {
		return nil, invalid
	}
	var result *api.SetBarrageLikeResp
	err := u.tx.WithTx(ctx, func(txctx context.Context) error {
		var err error
		result, err = u.repo.SetBarrageLike(txctx, req.UserId, req.VideoId, req.BarrageId, req.GetLiked())
		return err
	})
	if err != nil {
		return nil, err
	}
	return result, err
}

var barrageColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

// validateBarrageSend 校验秒数、正文和颜色，不接受客户端指定初始点赞数。
func validateBarrageSend(req *api.VideoSendBarrageReq) error {
	invalid := kerrors.BadRequest("INVALID_BARRAGE", "弹幕内容、颜色或播放位置无效")
	if req.PublisherId <= 0 || req.VideoId <= 0 {
		return invalid
	}
	if req.PositionSecond < 0 {
		return invalid
	}
	if strings.TrimSpace(req.Content) == "" || utf8.RuneCountInString(req.Content) > maxBarrageContentRunes {
		return invalid
	}
	if !barrageColor.MatchString(req.Color) {
		return invalid
	}
	return nil
}
