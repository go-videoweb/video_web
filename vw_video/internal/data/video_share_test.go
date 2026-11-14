package data

import (
	"context"
	"database/sql/driver"
	"errors"
	"io"
	"testing"

	"vw_video/internal/biz"
	"vw_video/internal/pkg/ecode/errdef"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
)

// TestRecordVideoShareLocalTransaction 验证首次计数、重复分享及事务失败的行为。
func TestRecordVideoShareLocalTransaction(t *testing.T) {
	for _, scenario := range []string{"首次", "重复", "视频不存在", "计数零行", "提交失败"} {
		t.Run(scenario, func(t *testing.T) {
			video := &socialSQLStep{
				contains: []string{"FROM `video`", "video_id", "deleted_at"},
				columns:  []string{"video_id", "publisher_id"},
				rows:     [][]driver.Value{{int64(9), int64(11)}},
			}
			ensure := &socialSQLStep{
				contains: []string{"INSERT INTO `user_video_status`", "ON DUPLICATE KEY UPDATE `deleted_at`=VALUES(`deleted_at`)"},
				affected: 1,
			}
			mark := &socialSQLStep{
				contains: []string{"UPDATE `user_video_status`", "SET `is_shared`=?", "`user_video_status`.`user_id` = ? AND `user_video_status`.`video_id` = ? AND `user_video_status`.`is_shared` <> ?", "deleted_at"},
				affected: 1,
			}
			count := &socialSQLStep{
				contains: []string{"UPDATE `video`", "COALESCE(cnt_shares, 0) + ?", "COALESCE(hot, 0) + ?", "`version`=`version`+1", "video_id", "deleted_at"},
				affected: 1,
			}
			steps := []*socialSQLStep{video, ensure, mark, count}
			var wantErr *kerrors.Error
			switch scenario {
			case "重复":
				mark.affected = 0
				steps = steps[:3]
			case "视频不存在":
				video.rows = nil
				steps = steps[:1]
				wantErr = errdef.ErrVideoNotFound
			case "计数零行":
				count.affected = 0
				wantErr = errdef.ErrVideoNotFound
			case "提交失败":
				wantErr = errdef.ErrShareVideoFailed
			}
			d, state := newSocialSQL(t, steps...)
			if scenario == "提交失败" {
				state.commitErr = errors.New("commit failed")
			}
			uc := biz.NewInteractUseCase(&interactRepo{}, nil, d, log.NewStdLogger(io.Discard))
			err := uc.RecordVideoShare(context.Background(), 7, 9)
			if state.begins != 1 || (wantErr == nil && err != nil) || (wantErr != nil && !errors.Is(err, wantErr)) {
				t.Fatalf("分享结果或事务次数不符：err=%v begin=%d", err, state.begins)
			}
			if scenario == "视频不存在" || scenario == "计数零行" {
				if state.rollbacks != 1 || state.commits != 0 {
					t.Fatalf("失败操作未回滚：%+v", state)
				}
			} else if state.commits != 1 {
				t.Fatalf("事务提交次数不符：%d", state.commits)
			}
		})
	}
}
