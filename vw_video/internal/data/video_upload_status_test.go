package data

import (
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	videoinfov1 "vw_video/api/v1/videoinfo"
	"vw_video/internal/biz"
)

// TestUploadVideoInfoWithUploadStatus 验证真实用例的两次单表插入共用一个事务，任一写入失败都会回滚。
func TestUploadVideoInfoWithUploadStatus(t *testing.T) {
	for _, scenario := range []string{"成功", "视频插入失败", "状态插入失败", "提交失败"} {
		t.Run(scenario, func(t *testing.T) {
			fault := errors.New("模拟数据库失败")
			video := &socialSQLStep{contains: []string{"INSERT INTO `video`", "`video_id`"}, affected: 1}
			status := &socialSQLStep{
				contains: []string{"INSERT INTO `video_upload_status` (`created_at`,`updated_time`,`status`,`upload_error_msg`,`video_id`) VALUES (?,?,?,?,?)"},
				affected: 1,
			}
			steps := []*socialSQLStep{video, status}
			if scenario == "视频插入失败" {
				video.err = fault
				steps = steps[:1]
			}
			if scenario == "状态插入失败" {
				status.err = fault
			}
			d, state := newSocialSQL(t, steps...)
			if scenario == "提交失败" {
				state.commitErr = fault
			}
			uc := biz.NewVideoInfoUsecase(&videoInfoRepo{}, d, log.NewStdLogger(io.Discard))
			videoID, err := uc.UploadVideoInfo(context.Background(), &videoinfov1.VideoMetaInfo{PublisherId: 7, Title: "投稿"})
			if state.begins != 1 {
				t.Fatalf("两个 INSERT 必须共用一个事务: begins=%d", state.begins)
			}

			if scenario != "视频插入失败" {
				if len(status.args) != 5 {
					t.Fatalf("初始状态绑定参数不完整: %v", status.args)
				}
				created, createdOK := status.args[0].Value.(time.Time)
				updated, updatedOK := status.args[1].Value.(time.Time)
				if !createdOK || !updatedOK || created.IsZero() || !created.Equal(updated) {
					t.Fatalf("初始时间没有同时写入: %v", status.args[:2])
				}
				statusID, ok := status.args[4].Value.(int64)
				if !ok || statusID <= 0 || status.args[2].Value != "waiting_cover" || status.args[3].Value != "" {
					t.Fatalf("初始状态参数错误: %v", status.args)
				}
				if scenario == "成功" && videoID != statusID {
					t.Fatalf("返回的视频 ID %d 与状态行 ID %d 不同", videoID, statusID)
				}
			}
			if scenario != "成功" && (videoID != 0 || err == nil || !strings.Contains(err.Error(), fault.Error())) {
				t.Fatalf("失败不应返回视频 ID，且需保留失败原因: id=%d err=%v", videoID, err)
			}

			switch scenario {
			case "成功":
				if err != nil || state.commits != 1 || state.rollbacks != 0 {
					t.Fatalf("写入成功应只提交一次: err=%v commits=%d rollbacks=%d", err, state.commits, state.rollbacks)
				}
			case "提交失败":
				if state.commits != 1 {
					t.Fatalf("提交错误未透传: err=%v commits=%d", err, state.commits)
				}
			default:
				if state.commits != 0 || state.rollbacks != 1 {
					t.Fatalf("插入错误未回滚: err=%v commits=%d rollbacks=%d", err, state.commits, state.rollbacks)
				}
			}
		})
	}
}

// TestUpdateVideoUploadStatus 验证状态更新限定视频 ID 和旧状态，未命中时由业务层决定如何处理。
func TestUpdateVideoUploadStatus(t *testing.T) {
	for _, affected := range []int64{0, 1} {
		step := &socialSQLStep{
			contains: []string{"UPDATE `video_upload_status`", "SET `status`=?,`updated_time`=?", "WHERE `video_upload_status`.`video_id` = ? AND `video_upload_status`.`status` = ?"},
			affected: affected,
		}
		d, state := newSocialSQL(t, step)
		now := time.Now().Truncate(time.Millisecond)
		err := d.WithTx(context.Background(), func(ctx context.Context) error {
			ok, err := (&videoInfoRepo{}).UpdateVideoUploadStatus(ctx, 42, "waiting_cover", "waiting_video", now)
			if ok != (affected == 1) {
				t.Errorf("影响 %d 行时返回 %t", affected, ok)
			}
			return err
		})
		if err != nil || state.begins != 1 || state.commits != 1 {
			t.Fatalf("状态更新未沿用业务事务：%v", err)
		}
		if len(step.args) != 4 || step.args[0].Value != "waiting_video" || step.args[1].Value != now ||
			step.args[2].Value != int64(42) || step.args[3].Value != "waiting_cover" {
			t.Fatalf("状态更新参数错误：%v", step.args)
		}
	}
}
