package data

import (
	"context"
	"database/sql"
	"errors"

	"util/dbutil"
	"vw_video/internal/biz"
	"vw_video/internal/data/dal/model"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// videoStatusFieldChange 将数据库列、锁定行中的当前值和本次变更绑定，供逐字段校验。
type videoStatusFieldChange struct {
	column  string
	current int64
	change  *biz.StatusChange
}

// interactionDB 保留上下文中的本地事务，并清除其他查询的语句状态。
// 输入：ctx 可以携带视频本地事务；无事务时使用默认查询连接。
// 输出：带请求上下文的干净 GORM 会话，不新开、提交或回滚事务。
func interactionDB(ctx context.Context) *gorm.DB {
	return getQuery(ctx).UserVideoStatus.WithContext(ctx).UnderlyingDB().Session(&gorm.Session{NewDB: true, Context: ctx})
}

// readUserVideoStatus 无记录时返回全零状态，不在读取路径初始化个人数据。
// 输入：db 为读取连接或事务；userID、videoID 必须为正数。
// 输出：排除软删除行；无记录返回默认状态，其他数据库错误原样返回，始终不写入。
func readUserVideoStatus(ctx context.Context, db *gorm.DB, userID, videoID int64) (*model.UserVideoStatus, error) {
	if userID <= 0 || videoID <= 0 {
		return nil, biz.ErrInvalidInteractionIdentity
	}
	row := &model.UserVideoStatus{}
	err := db.WithContext(ctx).Where("user_id = ? AND video_id = ?", userID, videoID).Take(row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &model.UserVideoStatus{UserID: userID, VideoID: videoID}, nil
	}
	return row, err
}

// videoStatusMask 保持既有 RPC 位掩码；收藏位由关系表另行补充。
// 输入：row 为 MySQL 状态记录，可以为 nil。
// 输出：点赞、分享和已投贝壳状态位；nil 返回 0，不读取或修改收藏副本。
func videoStatusMask(row *model.UserVideoStatus) int64 {
	if row == nil {
		return 0
	}
	var status int64
	if row.IsLike == 1 {
		status |= biz.UpvoteStatus
	}
	if row.IsShared == 1 {
		status |= biz.ShareStatus
	}
	if row.ShellsCount > 0 {
		status |= biz.ThrowShellStatus
	}
	return status
}

// ApplyUserVideoStatus 在屏障传入的同一事务中锁定状态行，仅更新操作指定字段。
// 输入：tx 须为有效 SQL 屏障事务，用户/视频 ID 须为正数，patch 不能为空。
// 输出：无目标字段直接成功；旧值不符返回冲突，其他数据库错误透传，由屏障负责提交或回滚。
func (i *interactRepo) ApplyUserVideoStatus(ctx context.Context, tx *sql.Tx, userID, videoID int64, patch *biz.VideoStatusPatch) error {
	if userID <= 0 || videoID <= 0 || tx == nil || patch == nil {
		return biz.ErrInvalidInteractionIdentity
	}
	if patch.IsLike == nil && patch.IsShared == nil && patch.ShellsCount == nil {
		return nil
	}
	db, err := dbutil.SqlTxToGormDB(tx)
	if err != nil {
		return err
	}
	return applyVideoStatusPatch(db.WithContext(ctx), userID, videoID, patch)
}

// applyVideoStatusPatch 仅供已有事务调用；首次互动不会生成观看时间。
// 输入：db 必须绑定调用方事务；ID 和 patch 已由上层验证。
// 输出：先确保状态行存在并加排他锁，再校验旧值并更新目标字段；失败由调用方回滚。
// 不覆盖收藏或观看时间，也不在此函数提交事务。
func applyVideoStatusPatch(db *gorm.DB, userID, videoID int64, patch *biz.VideoStatusPatch) error {
	row := &model.UserVideoStatus{UserID: userID, VideoID: videoID}
	if err := db.Clauses(clause.OnConflict{DoNothing: true}).Create(row).Error; err != nil {
		return err
	}
	if err := db.Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ? AND video_id = ?", userID, videoID).Take(row).Error; err != nil {
		return err
	}
	updates := make(map[string]interface{})
	videoStatusSet := []videoStatusFieldChange{
		{"is_like", row.IsLike, patch.IsLike},
		{"is_shared", row.IsShared, patch.IsShared},
		{"shells_count", row.ShellsCount, patch.ShellsCount},
	}
	for _, entry := range videoStatusSet {
		if entry.change == nil {
			continue
		}
		if entry.current != entry.change.Before {
			return kerrors.Conflict("INTERACTION_STATE_CONFLICT", "互动状态已变化，请重新查询后操作")
		}
		updates[entry.column] = entry.change.After
	}
	if len(updates) == 0 {
		return nil
	}
	return db.Model(&model.UserVideoStatus{}).Where("user_id = ? AND video_id = ?", userID, videoID).Updates(updates).Error
}
