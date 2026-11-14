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

// AddVideoCntView 在上下文选定的连接上增加播放量和热度，数据库错误透传。
func (v *videoInfoRepo) AddVideoCntView(ctx context.Context, videoId int64) error {
	video := getQuery(ctx).Video
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

// SetCoverPath 更新视频封面路径，软删除或版本冲突导致未写入时返回错误。
func (v *videoInfoRepo) SetCoverPath(ctx context.Context, videoId int64, coverPath string) error {
	video := getQuery(ctx).Video
	videoDo, _, err := addVideoModel(ctx, videoId)
	if err != nil {
		return err
	}

	result, err := videoDo.Where(video.VideoID.Eq(videoId)).Updates(&model.Video{
		CoverPath: coverPath,
	})
	if err != nil {
		return err
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// UpdateVideoMediaInfo 在上下文选定的连接上保存原片信息，并检查版本条件是否命中。
func (v *videoInfoRepo) UpdateVideoMediaInfo(ctx context.Context, videoId int64, videoFilePath, duration string, size int64) error {
	video := getQuery(ctx).Video
	videoDo, _, err := addVideoModel(ctx, videoId)
	if err != nil {
		return err
	}

	result, err := videoDo.Where(video.VideoID.Eq(videoId)).Updates(&model.Video{
		VideoPath: videoFilePath,
		Duration:  duration,
		Size:      size,
	})
	if err != nil {
		return err
	}
	if result.RowsAffected != 1 {
		return gorm.ErrRecordNotFound
	}
	return nil
}

// CreateBasicVideoInfo 使用 ctx 选定的连接插入视频元信息，不管理事务。
func (v *videoInfoRepo) CreateBasicVideoInfo(ctx context.Context, videoInfo *model.Video) error {
	return getQuery(ctx).Video.WithContext(ctx).Create(videoInfo)
}

// CreateVideoUploadStatus 使用 ctx 选定的连接插入上传状态，不决定状态值或管理事务。
func (v *videoInfoRepo) CreateVideoUploadStatus(ctx context.Context, uploadStatus *model.VideoUploadStatus) error {
	return getQuery(ctx).VideoUploadStatus.WithContext(ctx).Create(uploadStatus)
}

// GetVideoUploadStatus 在 ctx 选定的连接上查询单个视频的上传状态。
func (v *videoInfoRepo) GetVideoUploadStatus(ctx context.Context, videoId int64) (*model.VideoUploadStatus, error) {
	upload := getQuery(ctx).VideoUploadStatus
	return upload.WithContext(ctx).Where(upload.VideoID.Eq(videoId)).First()
}

// UpdateVideoUploadStatus 按视频 ID 和旧状态更新阶段、错误描述和更新时间。
// errorMessage 为空时清除旧错误，调用方负责处理未命中。
func (v *videoInfoRepo) UpdateVideoUploadStatus(ctx context.Context, videoId int64, from, to, errorMessage string, updatedTime time.Time) (bool, error) {
	upload := getQuery(ctx).VideoUploadStatus
	result, err := upload.WithContext(ctx).
		Where(upload.VideoID.Eq(videoId), upload.Status.Eq(from)).
		UpdateSimple(upload.Status.Value(to), upload.UpdatedTime.Value(updatedTime), upload.UploadErrorMsg.Value(errorMessage))
	return result.RowsAffected == 1, err
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

// SetVideoHot 通过统一查询入口在传入事务内调整热度，不改变事务生命周期。
func (v *videoInfoRepo) SetVideoHot(ctx context.Context, tx *sql.Tx, videoId int64, hotCount int64) error {
	ctx, err := bindSQLTx(ctx, tx)
	if err != nil {
		return err
	}
	video := getQuery(ctx).Video
	videoDo, videoModel, err := addVideoModel(ctx, videoId)
	if err != nil {
		return err
	}

	_, err = videoDo.Where(video.VideoID.Eq(videoId)).Update(video.Hot, videoModel.Hot+hotCount)
	if err != nil {
		return err
	}
	return nil
}
