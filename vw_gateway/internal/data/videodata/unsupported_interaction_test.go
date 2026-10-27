package videodata

import (
	"context"
	"testing"

	kerrors "github.com/go-kratos/kratos/v2/errors"
)

// TestDisabledLikesBeforeRPC 验证停用入口不发现服务、不调用下游、不提交 Saga。
func TestDisabledLikesBeforeRPC(t *testing.T) {
	ctx := context.Background()
	for _, call := range []func() error{
		func() error { return (&interactRepo{}).UpvoteBarrage(ctx, 7, 9) },
		func() error { return (&videoCommentRepo{}).UpvoteComment(ctx, 9, 7, 8, false) },
		func() error { _, err := (&videoCommentRepo{}).CheckIfUserUpvotedComment(ctx, 9, 7); return err },
	} {
		err := call()
		if err == nil || kerrors.FromError(err).Code != 501 || kerrors.FromError(err).Reason != "INTERACTION_NOT_SUPPORTED" {
			t.Fatalf("停用入口没有返回明确错误：%v", err)
		}
	}
}
