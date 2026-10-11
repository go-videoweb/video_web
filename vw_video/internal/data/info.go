package data

import (
	"context"
	"database/sql"
	stderr "errors"
	"github.com/go-kratos/kratos/v2/log"
	"go.mongodb.org/mongo-driver/mongo"
	"time"
	"util/dbutil/mgutil"
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

func (v *videoInfoRepo) GetUserVideoStatus(ctx context.Context, videoID int64, userID int64) (int64, error) {
	filter := mgutil.NewBsonM("video_id", videoID, "user_id", userID)
	res, err := v.data.mongo.FindOne(ctx, uv_status, filter)
	if err != nil {
		// Document not found, which means user has not watched this video before.
		// So we initialize the user-video status document and return 0(initial status).
		if stderr.Is(err, mongo.ErrNoDocuments) {
			err = v.data.mongo.InsertOne(ctx, uv_status, &model.UserVideoStatus{
				UserID:  userID,
				VideoID: videoID,
				Status:  0,
			})
			if err != nil {
				return -1, err
			}
			return 0, nil
		}
		return -1, err
	}

	return res.Status, nil
}

func (v *videoInfoRepo) SetUserVideoStatus(ctx context.Context, videoID int64, userID int64, status int64) error {
	filter := mgutil.NewBsonM("video_id", videoID, "user_id", userID)
	data := &model.UserVideoStatus{
		Status: status,
	}
	return v.data.mongo.UpsertOne(ctx, uv_status, filter, data)
}

func (v *videoInfoRepo) UpdateUserVideoHistory(ctx context.Context, videoID int64, userID int64, history *model.VideoSummary) error {
	filter := mgutil.NewBsonM("video_id", videoID, "user_id", userID)
	//filter, err := mgutil.NewBsonM("video_id", videoID, "user_id", userID)
	//if err != nil {
	//	return err
	//}
	data := &model.UserVideoHistory{
		UserID:       userID,
		VideoID:      videoID,
		VideoSummary: history,
		Timestamp:    time.Now().UnixNano(),
	}

	return v.data.mongo.UpsertOne(ctx, uv_history, filter, data)
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

// GetMongoClient is a helper function to get *mongo.Client from *mongo.Data.
// This is used to be the DTM barrier's MongoCall method's argument.
func (v *videoInfoRepo) GetMongoClient() *mongo.Client {
	return v.data.mongo.mongoClient
}

func (v *videoInfoRepo) GetDBAndCollection() (db, collection string) {
	return v.data.mongo.db, uv_status
}
