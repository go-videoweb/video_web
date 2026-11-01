package service

import (
	"context"
	api "vw_video/api/v1/interact"
)

// GetBarrages 返回指定播放区间的弹幕及个人状态，时间单位为整数秒。
func (s *InteractService) GetBarrages(ctx context.Context, req *api.GetBarragesReq) (*api.GetBarragesResp, error) {
	return s.interactUsecase.GetBarrages(ctx, req)
}

// SetBarrageLike 返回本地事务提交后的目标状态及点赞数。
func (s *InteractService) SetBarrageLike(ctx context.Context, req *api.SetBarrageLikeReq) (*api.SetBarrageLikeResp, error) {
	return s.interactUsecase.SetBarrageLike(ctx, req)
}
