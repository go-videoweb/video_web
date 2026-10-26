package data

import (
	"context"
	"database/sql"
	stderr "errors"
	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	drivermysql "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
	"strings"
	interactv1 "vw_video/api/v1/interact"
	"vw_video/internal/biz"
	"vw_video/internal/data/dal/model"
)

type interactRepo struct {
	data *Data
	log  *log.Helper
}

func NewInteractRepo(data *Data, logger log.Logger) biz.InteractRepo {
	return &interactRepo{
		data: data,
		log:  log.NewHelper(logger),
	}
}

// AddVideoUpvote 在传入的屏障事务内增加点赞数和热度，查询及更新均经过统一入口。
func (i *interactRepo) AddVideoUpvote(ctx context.Context, tx *sql.Tx, videoID int64) error {
	ctx, err := bindSQLTx(ctx, tx)
	if err != nil {
		return err
	}
	video := getQuery(ctx).Video
	videoDo, videoModel, err := addVideoModel(ctx, videoID)
	if err != nil {
		return err
	}
	// 一次更新两个字段，避免拆成多条 SQL 后重复使用同一旧版本号。
	_, err = videoDo.Where(video.VideoID.Eq(videoID)).
		Updates(map[string]interface{}{
			"likes": videoModel.Likes + 1,
			"hot":   videoModel.Hot + HotEachUpvote,
		})
	if err != nil {
		return err
	}
	return nil
}

// DecrementVideoUpvote 在传入事务内减少点赞数和热度，数据库错误透传。
func (i *interactRepo) DecrementVideoUpvote(ctx context.Context, tx *sql.Tx, videoID int64) error {
	ctx, err := bindSQLTx(ctx, tx)
	if err != nil {
		return err
	}
	video := getQuery(ctx).Video
	videoDo, videoModel, err := addVideoModel(ctx, videoID)
	if err != nil {
		return err
	}

	_, err = videoDo.Where(video.VideoID.Eq(videoID)).
		Updates(map[string]interface{}{
			"likes": videoModel.Likes - 1,
			"hot":   videoModel.Hot - HotEachUpvote,
		})
	if err != nil {
		return err
	}
	return nil
}

// AddVideoCntFavorite 在传入事务内增加收藏数和热度，不创建或提交其他事务。
func (i *interactRepo) AddVideoCntFavorite(ctx context.Context, tx *sql.Tx, videoID int64) error {
	ctx, err := bindSQLTx(ctx, tx)
	if err != nil {
		return err
	}
	video := getQuery(ctx).Video
	videoDo, videoModel, err := addVideoModel(ctx, videoID)
	if err != nil {
		return err
	}

	_, err = videoDo.Debug().Where(video.VideoID.Eq(videoID)).
		Updates(map[string]interface{}{
			"cnt_favorited": videoModel.CntFavorited + 1,
			"hot":           videoModel.Hot + HotEachFavorite,
		})
	if err != nil {
		return err
	}
	return nil
}

// DecrementVideoCntFavorite 在传入事务内减少收藏数和热度，数据库错误透传。
func (i *interactRepo) DecrementVideoCntFavorite(ctx context.Context, tx *sql.Tx, videoID int64) error {
	ctx, err := bindSQLTx(ctx, tx)
	if err != nil {
		return err
	}
	video := getQuery(ctx).Video
	videoDo, videoModel, err := addVideoModel(ctx, videoID)
	if err != nil {
		return err
	}

	_, err = videoDo.Where(video.VideoID.Eq(videoID)).
		Updates(map[string]interface{}{
			"cnt_favorited": videoModel.CntFavorited - 1,
			"hot":           videoModel.Hot - HotEachFavorite,
		})

	if err != nil {
		return err
	}
	return nil
}

// AddUserFavoriteVideo 校验收藏夹所有者，保存操作人并拒绝同用户同视频的重复或跨夹收藏。
// 输入：屏障传入的事务和正 ID；输出：成功或关系冲突，不自行提交或修复历史删除记录。
func (i *interactRepo) AddUserFavoriteVideo(ctx context.Context, tx *sql.Tx, userID, favoritesID, videoID int64) error {
	if userID <= 0 || favoritesID <= 0 || videoID <= 0 {
		return kerrors.BadRequest("INVALID_FAVORITE", "用户、视频和收藏夹 ID 必须为正数")
	}
	ctx, err := bindSQLTx(ctx, tx)
	if err != nil {
		return err
	}
	if err := i.data.requireFavoriteAccess(ctx, userID, favoritesID, true); err != nil {
		return err
	}
	q := getQuery(ctx)
	if _, err := q.Video.WithContext(ctx).Where(q.Video.VideoID.Eq(videoID)).First(); err != nil {
		return err
	}
	relation, err := q.FavoriteVideo.WithContext(ctx).Unscoped().Where(q.FavoriteVideo.UserID.Eq(userID), q.FavoriteVideo.VideoID.Eq(videoID)).First()
	if err != nil && !stderr.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if err == nil {
		if relation.DeletedAt.Valid {
			return kerrors.Conflict("FAVORITE_RELATION_DELETED", "该视频存在历史删除收藏记录，需要先核对数据")
		}
		if relation.FavoritesID != favoritesID {
			return kerrors.Conflict("VIDEO_ALREADY_FAVORITED", "视频已在其他收藏夹中，请先取消原收藏")
		}
		return kerrors.Conflict("FAVORITE_RELATION_EXISTS", "视频已在该收藏夹中")
	}
	err = q.FavoriteVideo.WithContext(ctx).Create(&model.FavoriteVideo{
		UserID:      userID,
		FavoritesID: favoritesID,
		VideoID:     videoID,
	})
	var duplicate *drivermysql.MySQLError
	if stderr.Is(err, gorm.ErrDuplicatedKey) || (stderr.As(err, &duplicate) && duplicate.Number == 1062) {
		return kerrors.Conflict("FAVORITE_RELATION_CONFLICT", "收藏关系已发生变化，请刷新后重试")
	}
	return err
}

// DeleteUserFavoriteVideo 校验所有者，只硬删除用户、收藏夹和视频均匹配的有效关系。
// 无匹配返回错误，使同屏障事务内已执行的计数变更回滚。
func (i *interactRepo) DeleteUserFavoriteVideo(ctx context.Context, tx *sql.Tx, userID, favoritesID, videoID int64) error {
	if userID <= 0 || favoritesID <= 0 || videoID <= 0 {
		return kerrors.BadRequest("INVALID_FAVORITE", "用户、视频和收藏夹 ID 必须为正数")
	}
	ctx, err := bindSQLTx(ctx, tx)
	if err != nil {
		return err
	}
	if err := i.data.requireFavoriteAccess(ctx, userID, favoritesID, true); err != nil {
		return err
	}
	q := getQuery(ctx)
	if _, err := q.Video.WithContext(ctx).Where(q.Video.VideoID.Eq(videoID)).First(); err != nil {
		return err
	}
	result, err := q.FavoriteVideo.WithContext(ctx).Where(q.FavoriteVideo.UserID.Eq(userID), q.FavoriteVideo.FavoritesID.Eq(favoritesID), q.FavoriteVideo.VideoID.Eq(videoID), q.FavoriteVideo.DeletedAt.IsNull()).
		Unscoped().
		Delete(&model.FavoriteVideo{})
	if err != nil {
		return err
	}
	if result.RowsAffected == 0 {
		return kerrors.NotFound("FAVORITE_RELATION_NOT_FOUND", "该收藏夹中不存在此视频收藏")
	}
	return nil
}

// UpdateVideoShells 沿统一入口在传入事务内调整视频贝壳数和热度。
func (i *interactRepo) UpdateVideoShells(ctx context.Context, tx *sql.Tx, videoID int64, countShells int32) error {
	ctx, err := bindSQLTx(ctx, tx)
	if err != nil {
		return err
	}
	video := getQuery(ctx).Video
	videoDo, videoModel, err := addVideoModel(ctx, videoID)
	if err != nil {
		return err
	}

	_, err = videoDo.Where(video.VideoID.Eq(videoID)).
		Updates(map[string]interface{}{
			"shells": videoModel.Shells + int64(countShells),
			"hot":    videoModel.Hot + int64(HotEachShell*countShells),
		})
	if err != nil {
		return err
	}
	return nil
}

func (i *interactRepo) ShareVideo(ctx context.Context, videoID int64) string {
	return ""
}

// AddVideoCntBarrage 在上下文选定的连接上增加弹幕数和热度，数据库错误透传。
func (i *interactRepo) AddVideoCntBarrage(ctx context.Context /*tx *sql.Tx,*/, videoID int64) error {
	video := getQuery(ctx).Video
	videoDo, videoModel, err := addVideoModel(ctx, videoID)
	if err != nil {
		return err
	}
	_, err = videoDo.Where(video.VideoID.Eq(videoID)).Updates(&model.Video{
		CntBarrages: videoModel.CntBarrages + 1,
		Hot:         videoModel.Hot + HotEachBarrage,
	})
	if err != nil {
		return err
	}
	return nil
}

// AddVideoBarrageUpvote 在传入事务内增加弹幕点赞数，沿用既有未启用入口的仓储能力。
func (i *interactRepo) AddVideoBarrageUpvote(ctx context.Context, tx *sql.Tx, barrageID int64) error {
	ctx, err := bindSQLTx(ctx, tx)
	if err != nil {
		return err
	}
	barrage := getQuery(ctx).Barrage
	barrageDo := barrage.WithContext(ctx)
	barrageModel, err := barrageDo.Where(barrage.BarrageID.Eq(barrageID)).First()
	if err != nil {
		return err
	}

	_, err = barrageDo.Where(barrage.BarrageID.Eq(barrageID)).Update(barrage.Likes, barrageModel.Likes+1)
	if err != nil {
		return err
	}
	return nil
}

// DecrementVideoBarrageUpvote 在传入事务内减少弹幕点赞数，数据库错误透传。
func (i *interactRepo) DecrementVideoBarrageUpvote(ctx context.Context, tx *sql.Tx, barrageID int64) error {
	ctx, err := bindSQLTx(ctx, tx)
	if err != nil {
		return err
	}
	barrage := getQuery(ctx).Barrage
	barrageDo := barrage.WithContext(ctx)
	barrageModel, err := barrageDo.Where(barrage.BarrageID.Eq(barrageID)).First()
	if err != nil {
		return err
	}

	_, err = barrageDo.Where(barrage.BarrageID.Eq(barrageID)).Update(barrage.Likes, barrageModel.Likes-1)
	if err != nil {
		return err
	}
	return nil
}

func (i *interactRepo) AddBarrage(ctx context.Context /*tx *sql.Tx,*/, info *interactv1.VideoSendBarrageReq) error {
	//db, err := dbutil.SqlTxToGormDB(tx)
	//if err != nil {
	//	return err
	//}
	//
	//return db.Model(&model.Barrage{}).Create(&model.Barrage{
	//	UserID:  info.PublisherId,
	//	VideoID: info.VideoId,
	//	Content: info.Content,
	//	Hour:    info.Hour,
	//	Minute:  info.Minute,
	//	Second:  info.Second,
	//	Color:   info.Color,
	//}).Error
	hms := strings.Split(info.Time, ":")
	if len(hms) != 3 {
		return stderr.New("invalid time format")
	}
	barrage := getQuery(ctx).Barrage
	barrageDo := barrage.WithContext(ctx)
	return barrageDo.Create(&model.Barrage{
		PublisherID: info.PublisherId,
		VideoID:     info.VideoId,
		Content:     info.Content,
		Hour:        hms[0],
		Minute:      hms[1],
		Second:      hms[2],
		Color:       info.Color,
	})
}

func (i *interactRepo) GetPublisherIdByVideoId(ctx context.Context, videoID int64) (int64, error) {
	_, video, err := addVideoModel(ctx, videoID)
	if err != nil {
		return 0, err
	}
	return video.PublisherID, nil
}

// GetUserVideoStatus 从 MySQL 读取状态；收藏位由实际收藏关系计算。
// 输入：用户/视频 ID 必须为正数；输出：RPC 状态位和已投贝壳数量，任何读取错误均透传。
// 不初始化记录，也不把状态表中的兼容 is_favor 列作为收藏事实来源。
func (i *interactRepo) GetUserVideoStatus(ctx context.Context, userID, videoID int64) (int64, int32, error) {
	row, err := readUserVideoStatus(ctx, interactionDB(ctx), userID, videoID)
	if err != nil {
		return 0, 0, err
	}
	status := videoStatusMask(row)
	favoriteID, err := (&videoInfoRepo{data: i.data}).GetUserFavoriteID(ctx, userID, videoID)
	if err != nil {
		return 0, 0, err
	}
	if favoriteID > 0 {
		status |= biz.FavoriteStatus
	}
	return status, int32(row.ShellsCount), nil
}

// GetUserBarrageStatus 保留仓储接口但暂不接入个人弹幕状态。
// 输入：保留原用户/弹幕 ID；输出：0 和未支持错误，不访问数据库。
func (i *interactRepo) GetUserBarrageStatus(ctx context.Context, userID, barrageID int64) (int64, error) {
	return 0, biz.ErrInteractionNotSupported
}

// GetSqlTx is a helper function to get *sql.Tx from *gorm.DB.
// This is used to be the DTM barrier's Call method's argument.
func (i *interactRepo) GetSqlTx() *sql.Tx {
	tx := i.data.mysql.Begin()
	return tx.Statement.ConnPool.(*sql.Tx)
}
