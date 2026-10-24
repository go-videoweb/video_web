package data

import (
	"context"
	"database/sql"
	"github.com/go-kratos/kratos/v2/log"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"time"
	"vw_video/internal/biz"
	"vw_video/internal/data/dal/model"
	"vw_video/internal/data/dal/query"
)

type videoInfoRepo struct {
	data   *Data
	logger *log.Helper
}

func NewVideoInfoRepo(data *Data, logger log.Logger) biz.VideoInfoRepo {
	return &videoInfoRepo{
		data:   data,
		logger: log.NewHelper(logger),
	}
}

func (v *videoInfoRepo) AddVideoCntView(ctx context.Context, videoId int64) error {
	video := query.Video
	videoDo, videoModel, err := addVideoModel(ctx, videoId)
	if err != nil {
		return err
	}

	_, err = videoDo.Where(video.VideoID.Eq(videoId)).Updates(&model.Video{
		Hot:       videoModel.Hot + HotEachView,
		CntViewed: videoModel.CntViewed + 1,
	})
	if err != nil {
		return err
	}
	return nil
}

func (v *videoInfoRepo) SetCoverPath(ctx context.Context, videoId int64, coverPath string) error {
	video := getQuery(ctx).Video
	videoDo, _, err := addVideoModel(ctx, videoId)
	if err != nil {
		return err
	}

	_, err = videoDo.Where(video.VideoID.Eq(videoId)).Updates(&model.Video{
		CoverPath: coverPath,
	})

	return err
}

func (v *videoInfoRepo) UpdateVideoDurationSizeInfo(ctx context.Context, videoId int64, duration string, size int64) error {
	video := getQuery(ctx).Video
	videoDo, _, err := addVideoModel(ctx, videoId)
	if err != nil {
		return err
	}

	_, err = videoDo.Where(video.VideoID.Eq(videoId)).Updates(&model.Video{
		Duration: duration,
		Size:     size,
	})
	return err
}

func (v *videoInfoRepo) UpdateVideoFilePath(ctx context.Context, videoId int64, videoFilePath string) error {
	video := getQuery(ctx).Video
	videoDo, _, err := addVideoModel(ctx, videoId)
	if err != nil {
		return err
	}

	_, err = videoDo.Where(video.VideoID.Eq(videoId)).Updates(&model.Video{
		VideoPath: videoFilePath,
	})
	return err
}

func (v *videoInfoRepo) CreateBasicVideoInfo(ctx context.Context, videoInfo *model.Video) error {
	video := getQuery(ctx).Video
	videoDo := video.WithContext(ctx)
	return videoDo.Create(videoInfo)
}

func (v *videoInfoRepo) GetVideoInfoById(ctx context.Context, videoId int64) (*model.Video, error) {
	video := getQuery(ctx).Video
	videoDo := video.WithContext(ctx)
	return videoDo.Where(video.VideoID.Eq(videoId)).First()
}

func (v *videoInfoRepo) GetBarragesByVideoId(ctx context.Context, videoId int64) ([]*model.Barrage, error) {
	barrage := getQuery(ctx).Barrage
	barrageDo := barrage.WithContext(ctx)
	return barrageDo.Where(barrage.VideoID.Eq(videoId)).Find()
}

func (v *videoInfoRepo) UpdateVideoCntShare(ctx context.Context, tx *sql.Tx, videoID int64) error {
	video := query.Video
	db, videoModel, err := getGormDBAndVideoModel(tx, videoID)
	if err != nil {
		return err
	}
	return db.Model(videoModel).Where(video.VideoID.Eq(videoID)).Updates(map[string]any{
		"cnt_shares": videoModel.CntShares + 1,
		"hot":        videoModel.Hot + HotEachShare,
	}).Error
}
func (v *videoInfoRepo) UpdateVideoCntShareCompensation(ctx context.Context, tx *sql.Tx, videoID int64) error {
	video := query.Video
	db, videoModel, err := getGormDBAndVideoModel(tx, videoID)
	if err != nil {
		return err
	}
	return db.Model(videoModel).Where(video.VideoID.Eq(videoID)).Updates(map[string]any{
		"cnt_shares": videoModel.CntShares - 1,
		"hot":        videoModel.Hot - HotEachShare,
	}).Error
}

func (v *videoInfoRepo) GetVideoListByClass(ctx context.Context, class []string, pageNum int32, pageSize int32) ([]*model.Video, error) {
	video := getQuery(ctx).Video
	videoDo := video.WithContext(ctx)

	return videoDo.
		Where(video.Class.In(class...)).
		Order(video.Hot.Desc()).
		Offset(int((pageNum - 1) * pageSize)).
		Limit(int(pageSize)).
		Find()
}

func (v *videoInfoRepo) GetVideoFilePathById(ctx context.Context, videoId int64) (string, error) {
	video := getQuery(ctx).Video
	videoDo := video.WithContext(ctx)
	res, err := videoDo.Where(video.VideoID.Eq(videoId)).Select(video.VideoPath).First()
	if err != nil {
		return "", err
	}
	return res.VideoPath, nil
}

func (v *videoInfoRepo) GetPublisherIdByVideoId(ctx context.Context, videoId int64) (publisherId int64, err error) {
	video := getQuery(ctx).Video
	videoDo := video.WithContext(ctx)
	res, err := videoDo.Where(video.VideoID.Eq(videoId)).Select(video.PublisherID).First()
	if err != nil {
		return 0, err
	}
	return res.PublisherID, nil
}

// GetUserVideoStatus 只读点赞、分享及贝壳状态，保留现有 RPC 位掩码。
// 输入：用户/视频 ID 必须为正数；输出：无记录返回 0，读取错误透传，收藏归属由另一方法查询。
func (v *videoInfoRepo) GetUserVideoStatus(ctx context.Context, videoID int64, userID int64) (int64, error) {
	row, err := readUserVideoStatus(ctx, interactionDB(ctx), userID, videoID)
	return videoStatusMask(row), err
}

// UpdateUserVideoHistory 只更新最近观看时间，不覆盖互动字段；沿用调用方的 SQL 事务。
// 输入：用户/视频 ID 必须为正数；观看时间取当前服务时间。
// 输出：upsert 成功或数据库错误；并发旧请求不会回退时间，不保存视频摘要或播放进度。
func (v *videoInfoRepo) UpdateUserVideoHistory(ctx context.Context, videoID int64, userID int64) error {
	if userID <= 0 || videoID <= 0 {
		return biz.ErrInvalidInteractionIdentity
	}
	now := time.Now()
	row := &model.UserVideoStatus{UserID: userID, VideoID: videoID, LastWatchTime: &now}
	return interactionDB(ctx).Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "user_id"}, {Name: "video_id"}},
		DoUpdates: clause.Assignments(map[string]interface{}{
			"last_watch_time": gorm.Expr("GREATEST(COALESCE(last_watch_time, VALUES(last_watch_time)), VALUES(last_watch_time))"),
			"deleted_at":      nil,
		}),
	}).Create(row).Error
}

func (v *videoInfoRepo) SetVideoHot(ctx context.Context, tx *sql.Tx, videoId int64, hotCount int64) error {
	db, videoModel, err := getGormDBAndVideoModel(tx, videoId)
	if err != nil {
		return err
	}

	err = db.Model(videoModel).Where("video_id = ?", videoId).Update("hot", videoModel.Hot+hotCount).Error
	if err != nil {
		return err
	}
	return nil
}

// GetSqlTx is a helper function to get *sql.Tx from *gorm.DB.
// This is used to be the DTM barrier's Call method's argument.
func (v *videoInfoRepo) GetSqlTx() *sql.Tx {
	tx := v.data.mysql.Begin()
	return tx.Statement.ConnPool.(*sql.Tx)
}
