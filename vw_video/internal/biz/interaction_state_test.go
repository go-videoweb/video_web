package biz

import (
	"context"
	"reflect"
	"testing"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	interactv1 "vw_video/api/v1/interact"
)

// TestStatusFlagChange 验证普通函数只处理目标状态位，零标记不生成变更，且不共享结果对象。
func TestStatusFlagChange(t *testing.T) {
	type flagCase struct {
		name   string
		origin int64
		flag   int32
		mask   int64
		want   *StatusChange
	}
	for _, tc := range []flagCase{
		{"忽略零标记", UpvoteStatus | ShareStatus, 0, UpvoteStatus, nil},
		{"首次点赞", ShareStatus, 1, UpvoteStatus, &StatusChange{Before: 0, After: 1}},
		{"保留已有点赞", UpvoteStatus, 1, UpvoteStatus, &StatusChange{Before: 1, After: 1}},
		{"取消分享", UpvoteStatus | ShareStatus, -1, ShareStatus, &StatusChange{Before: 1, After: 0}},
		{"取消未设置位", ThrowShellStatus, -1, ShareStatus, &StatusChange{Before: 0, After: 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := statusFlagChange(tc.origin, tc.flag, tc.mask)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("状态位转换错误：got=%+v want=%+v", got, tc.want)
			}
		})
	}
	first := statusFlagChange(0, 1, UpvoteStatus)
	second := statusFlagChange(0, 1, UpvoteStatus)
	first.After = 0
	if second.After != 1 {
		t.Fatal("不同请求共享了可变的变更对象")
	}
}

// TestVideoStatusPatch 验证局部更新、收藏事实来源以及贝壳补偿，不连接数据库。
func TestVideoStatusPatch(t *testing.T) {
	req := &interactv1.SetUserVideoStatusReq{
		UserId: 7, VideoId: 9, OriginStatus: UpvoteStatus | ThrowShellStatus,
		OriginShellsCount: 1, ShellsCount: 2,
		Statuses: &interactv1.SetUserVideoStatusReq_Statuses{IsShared: 1},
	}
	patch, err := videoStatusPatch(req, false)
	if err != nil || patch.IsLike != nil || patch.ShellsCount != nil || *patch.IsShared != (StatusChange{Before: 0, After: 1}) {
		t.Fatalf("分享操作覆盖其他字段：%+v %v", patch, err)
	}
	req.Statuses = &interactv1.SetUserVideoStatusReq_Statuses{IsThrownShells: 1}
	patch, err = videoStatusPatch(req, true)
	if err != nil || *patch.ShellsCount != (StatusChange{Before: 2, After: 1}) || patch.IsLike != nil || patch.IsShared != nil {
		t.Fatalf("贝壳补偿未恢复原数量：%+v %v", patch, err)
	}
	req.Statuses = &interactv1.SetUserVideoStatusReq_Statuses{IsFavorited: 1}
	patch, err = videoStatusPatch(req, false)
	if err != nil || patch.IsLike != nil || patch.IsShared != nil || patch.ShellsCount != nil {
		t.Fatalf("收藏位产生了独立状态副本：%+v %v", patch, err)
	}
	req.Statuses = &interactv1.SetUserVideoStatusReq_Statuses{IsUpvoted: -1}
	patch, err = videoStatusPatch(req, true)
	if err != nil || *patch.IsLike != (StatusChange{Before: 0, After: 1}) {
		t.Fatalf("取消点赞补偿错误：%+v %v", patch, err)
	}
	for _, bad := range []*interactv1.SetUserVideoStatusReq{
		nil, {}, {UserId: 7, VideoId: 9},
		{UserId: 7, VideoId: 9, Statuses: &interactv1.SetUserVideoStatusReq_Statuses{IsUpvoted: 2}},
		{UserId: 7, VideoId: 9, ShellsCount: 3, Statuses: &interactv1.SetUserVideoStatusReq_Statuses{IsThrownShells: 1}},
		{UserId: 7, VideoId: 9, OriginShellsCount: -1, Statuses: &interactv1.SetUserVideoStatusReq_Statuses{IsThrownShells: 1}},
	} {
		if _, err := videoStatusPatch(bad, false); err == nil {
			t.Fatalf("非法请求未拒绝：%+v", bad)
		}
	}
}

// TestDisabledPersonalLikes 验证没有仓储或基础设施时也明确拒绝，不允许先修改计数。
func TestDisabledPersonalLikes(t *testing.T) {
	ctx := context.Background()
	interact := &InteractUsecase{}
	comment := &VideoCommentUsecase{}
	for _, call := range []func() error{
		func() error { return interact.UpvoteBarrage(ctx, 9, 1) },
		func() error { _, err := interact.GetUserBarrageStatus(ctx, 7, 9); return err },
		func() error { return interact.SetUserBarrageStatus(ctx, 7, 9, nil) },
		func() error { return interact.SetUserBarrageStatusRevert(ctx, 7, 9, nil) },
		func() error { return comment.UpvoteComment(ctx, 9, 7, false) },
		func() error { _, err := comment.CheckIfUserUpvotedComment(ctx, 9, 7); return err },
	} {
		err := call()
		if err == nil || kerrors.FromError(err).Code != 501 || kerrors.FromError(err).Reason != "INTERACTION_NOT_SUPPORTED" {
			t.Fatalf("未启用功能返回值错误：%v", err)
		}
	}
}
