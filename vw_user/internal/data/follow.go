package data

import (
	"context"
	"errors"
	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	drivermysql "github.com/go-sql-driver/mysql"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"vw_user/internal/biz"
	"vw_user/internal/data/dal/model"
	"vw_user/internal/domain"
)

type followRepo struct {
	data *Data
	log  *log.Helper
}

// NewFollowRepo 构造用户域关注仓储，不建立额外连接。
func NewFollowRepo(data *Data, logger log.Logger) biz.FollowRepo {
	return &followRepo{data: data, log: log.NewHelper(logger)}
}

// followDB 从统一查询入口获取当前事务，清理模型状态以便在关注域不同表之间使用。
func followDB(ctx context.Context) *gorm.DB {
	return getQuery(ctx).User.WithContext(ctx).UnderlyingDB().Session(&gorm.Session{NewDB: true, Context: ctx})
}

// followWriteError 将同名或重复关系的唯一键错误转换为业务冲突，其他存储故障原样返回。
func followWriteError(err error) error {
	var duplicate *drivermysql.MySQLError
	if errors.Is(err, gorm.ErrDuplicatedKey) || (errors.As(err, &duplicate) && duplicate.Number == 1062) {
		return kerrors.Conflict("FOLLOW_CONFLICT", "关注列表名称或关系已存在")
	}
	return err
}

// followAffected 要求业务写入确实命中预期行数，失败由外层事务统一回滚。
func followAffected(result *gorm.DB, want int64) error {
	if result.Error != nil {
		return followWriteError(result.Error)
	}
	if result.RowsAffected != want {
		return kerrors.Conflict("FOLLOW_WRITE_CONFLICT", "关注数据已变化，请刷新后重试")
	}
	return nil
}

// LockFollowOwner 在现有用户行上串行化该用户的关注域写入，避免默认切换、删除和新增关注交错。
// 必须在本地事务内调用；不加目标用户锁，粉丝数量不维护用户表副本。
func (r *followRepo) LockFollowOwner(ctx context.Context, userID int64) error {
	var row model.User
	err := followDB(ctx).Select("user_id").Clauses(clause.Locking{Strength: "UPDATE"}).Where("user_id = ?", userID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return kerrors.NotFound("USER_NOT_FOUND", "用户不存在")
	}
	return err
}

// CheckFollowTarget 检查目标用户有效，不为不存在或已软删除用户创建新关注。
func (r *followRepo) CheckFollowTarget(ctx context.Context, targetID int64) error {
	var row model.User
	err := followDB(ctx).Select("user_id").Where("user_id = ?", targetID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return kerrors.NotFound("USER_NOT_FOUND", "目标用户不存在")
	}
	return err
}

// GetFollowLists 返回本人所有列表，默认列表优先，其他按 ID 升序。
func (r *followRepo) GetFollowLists(ctx context.Context, userID int64) ([]*model.FollowList, error) {
	rows := make([]*model.FollowList, 0)
	err := followDB(ctx).Where("user_id = ?", userID).Order("is_default DESC").Order("list_id ASC").Find(&rows).Error
	return rows, err
}

// FollowListNameExists 使用数据库排序规则检查本人同名列表，可排除本次修改的列表。
func (r *followRepo) FollowListNameExists(ctx context.Context, userID int64, name string, exclude int64) (bool, error) {
	var count int64
	err := followDB(ctx).Model(&model.FollowList{}).Where("user_id = ? AND list_name = ? AND list_id <> ?", userID, name, exclude).Count(&count).Error
	return count > 0, err
}

// CreateFollowList 保存普通列表，返回数据库冲突或实际写入失败。
func (r *followRepo) CreateFollowList(ctx context.Context, row *model.FollowList) error {
	return followAffected(followDB(ctx).Create(row), 1)
}

// RenameFollowList 仅修改本人目标列表名称。
func (r *followRepo) RenameFollowList(ctx context.Context, userID, listID int64, name string) error {
	return followAffected(followDB(ctx).Model(&model.FollowList{}).Where("user_id = ? AND list_id = ?", userID, listID).Update("list_name", name), 1)
}

// SetDefaultFollowList 在持有用户行锁的同一事务内先清除原默认标记，再设置目标。
func (r *followRepo) SetDefaultFollowList(ctx context.Context, userID, listID int64) error {
	if err := followAffected(followDB(ctx).Model(&model.FollowList{}).Where("user_id = ? AND is_default = ?", userID, true).Update("is_default", false), 1); err != nil {
		return err
	}
	return followAffected(followDB(ctx).Model(&model.FollowList{}).Where("user_id = ? AND list_id = ?", userID, listID).Update("is_default", true), 1)
}

// DeleteFollowLists 精确硬删除本人普通列表，默认列表条件在 SQL 再次限制。
func (r *followRepo) DeleteFollowLists(ctx context.Context, userID int64, ids []int64) error {
	return followAffected(followDB(ctx).Where("user_id = ? AND list_id IN ? AND is_default = ?", userID, ids, false).Delete(&model.FollowList{}), int64(len(ids)))
}

// GetFollowRelation 查询有效的用户对目标关注，无有效记录时返回 nil。
func (r *followRepo) GetFollowRelation(ctx context.Context, userID, targetID int64) (*model.UserFollow, error) {
	var row model.UserFollow
	err := followDB(ctx).Where("user_id = ? AND follow_user_id = ?", userID, targetID).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	return &row, err
}

// CreateFollowPair 同事务创建关注和粉丝关系，允许恢复联合主键下的旧软删除记录。
// 已有效的关注由 biz 幂等判断；这里不更新关注数或提交事务。
func (r *followRepo) CreateFollowPair(ctx context.Context, row *model.UserFollow) error {
	result := followDB(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}, {Name: "follow_user_id"}}, DoUpdates: clause.AssignmentColumns([]string{"followlist_id", "deleted_at", "created_at", "updated_at"})}).Create(row)
	if result.Error != nil {
		return followWriteError(result.Error)
	}
	if result.RowsAffected < 1 || result.RowsAffected > 2 {
		return kerrors.Conflict("FOLLOW_WRITE_CONFLICT", "关注关系未成功创建")
	}
	fan := &model.UserFan{UserID: row.FollowUserID, FollowerID: row.UserID}
	return followDB(ctx).Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "user_id"}, {Name: "follower_id"}}, DoUpdates: clause.AssignmentColumns([]string{"deleted_at", "created_at", "updated_at"})}).Create(fan).Error
}

// DeleteFollowPairs 精确硬删除已核对的有效关注和反向粉丝关系，允许旧数据缺失反向记录。
// 关注删除数量必须与输入一致，防止重复取消造成计数下溢；粉丝表不维护额外数量副本。
func (r *followRepo) DeleteFollowPairs(ctx context.Context, userID int64, targets []int64) error {
	if len(targets) == 0 {
		return nil
	}
	if err := followAffected(followDB(ctx).Unscoped().Where("user_id = ? AND follow_user_id IN ? AND deleted_at IS NULL", userID, targets).Delete(&model.UserFollow{}), int64(len(targets))); err != nil {
		return err
	}
	return followDB(ctx).Unscoped().Where("follower_id = ? AND user_id IN ?", userID, targets).Delete(&model.UserFan{}).Error
}

// GetFollowsInLists 在用户写事务中读取指定列表下全部有效关系，用于实际解除数量和目标集合。
func (r *followRepo) GetFollowsInLists(ctx context.Context, userID int64, ids []int64) ([]*model.UserFollow, error) {
	rows := make([]*model.UserFollow, 0)
	err := followDB(ctx).Where("user_id = ? AND followlist_id IN ?", userID, ids).Order("follow_user_id ASC").Find(&rows).Error
	return rows, err
}

// MoveFollowee 仅更新有效关注的分组，用户 ID 和目标 ID 同时限定。
func (r *followRepo) MoveFollowee(ctx context.Context, userID, targetID, listID int64) error {
	return followAffected(followDB(ctx).Model(&model.UserFollow{}).Where("user_id = ? AND follow_user_id = ?", userID, targetID).Update("followlist_id", listID), 1)
}

// ChangeFollowCount 在用户行已锁定的事务中原子调整关注数，拒绝下溢或零行更新。
func (r *followRepo) ChangeFollowCount(ctx context.Context, userID, delta int64) error {
	if delta == 0 {
		return nil
	}
	db := followDB(ctx).Model(&model.User{}).Where("user_id = ?", userID)
	if delta < 0 {
		db = db.Where("cnt_follows >= ?", -delta)
	}
	return followAffected(db.Updates(map[string]interface{}{"cnt_follows": gorm.Expr("cnt_follows + ?", delta)}), 1)
}

// GetFolloweesPage 分步计数、分页关系和批量用户查询，返回前按关系顺序重组摘要，不使用 JOIN。
// 已删除或缺失用户在分页前排除；同时间按目标 ID 降序稳定排序，不承诺跨语句快照。
func (r *followRepo) GetFolloweesPage(ctx context.Context, userID, listID int64, page, size int32) (*biz.FollowPage, error) {
	base := func() *gorm.DB {
		liveUsers := followDB(ctx).Model(&model.User{}).Select("user_id")
		db := followDB(ctx).Model(&model.UserFollow{}).Where("user_id = ? AND follow_user_id IN (?)", userID, liveUsers)
		if listID > 0 {
			db = db.Where("followlist_id = ?", listID)
		}
		return db
	}
	result := &biz.FollowPage{Users: make([]*domain.UserSummary, 0), PageNum: page, PageSize: size}
	if err := base().Count(&result.Total).Error; err != nil {
		return nil, err
	}
	rows := make([]*model.UserFollow, 0)
	if err := base().Order("created_at DESC").Order("follow_user_id DESC").Offset(int(int64(page-1) * int64(size))).Limit(int(size)).Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return result, nil
	}
	ids := make([]int64, len(rows))
	for i, row := range rows {
		ids[i] = row.FollowUserID
	}
	users := make([]*model.User, 0)
	if err := followDB(ctx).Where("user_id IN ?", ids).Find(&users).Error; err != nil {
		return nil, err
	}
	byID := make(map[int64]*model.User, len(users))
	for _, user := range users {
		byID[user.UserID] = user
	}
	for _, row := range rows {
		if user := byID[row.FollowUserID]; user != nil {
			summary := domain.NewUserSummary(user)
			summary.FollowListID = row.FollowlistID
			result.Users = append(result.Users, summary)
		}
	}
	return result, nil
}
