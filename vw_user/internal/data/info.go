package data

import (
	"context"
	"database/sql"
	stderr "errors"
	"github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"gorm.io/gorm"
	"util/dbutil"
	"vw_user/internal/biz"
	"vw_user/internal/data/dal/model"
	"vw_user/internal/data/dal/query"
	"vw_user/internal/domain"
	"vw_user/internal/pkg/ecode/errdef"
)

type userInfoRepo struct {
	data   *Data
	logger *log.Helper
}

func NewUserInfoRepo(data *Data, logger log.Logger) biz.UserInfoRepo {
	return &userInfoRepo{
		data:   data,
		logger: log.NewHelper(logger),
	}
}

// CountFans 从有效粉丝关系统计数量，使用统一查询入口，不依赖已移除的用户表字段。
func (u *userInfoRepo) CountFans(ctx context.Context, userID int64) (int64, error) {
	q := getQuery(ctx).UserFan
	count, err := q.WithContext(ctx).Where(q.UserID.Eq(userID)).Count()
	if err == nil && count > 2147483647 {
		return 0, errors.InternalServer("FANS_COUNT_OVERFLOW", "粉丝数量超过当前 RPC int32 范围")
	}
	return count, err
}

// GetUserInfoByUsername 读取用户信息，粉丝数从有效 user_fans 关系统计，不依赖用户表计数字段。
func (u *userInfoRepo) GetUserInfoByUsername(ctx context.Context, username string) (*domain.UserInfo, error) {
	user := getQuery(ctx).User
	userdo := user.WithContext(ctx)
	userInfo, err := userdo.Where(user.Username.Eq(username)).
		Select(user.UserID, user.Username, user.IsAdmin, user.Email, user.Password,
			user.Gender, user.AvatarPath, user.Birthday, user.Signature, user.Shells,
			user.CntFollows).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errdef.ErrUserNotFound
		}
		return nil, err
	}

	info := domain.NewUserInfo(userInfo)
	count, err := u.CountFans(ctx, userInfo.UserID)
	if err != nil {
		return nil, err
	}
	info.CntFans = int32(count)
	return info, nil
}

// GetUserInfoByUserId 读取用户信息及当前有效粉丝数，任何统计失败原样返回。
func (u *userInfoRepo) GetUserInfoByUserId(ctx context.Context, userid int64) (*domain.UserInfo, error) {
	queryUser := getQuery(ctx).User
	userDo := queryUser.WithContext(ctx)
	userInfo, err := userDo.Where(queryUser.UserID.Eq(userid)).
		Select(queryUser.UserID, queryUser.Username, queryUser.IsAdmin, queryUser.Email, queryUser.CntLikes, queryUser.Password,
			queryUser.Gender, queryUser.AvatarPath, queryUser.Birthday, queryUser.Signature, queryUser.CntFollows).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errdef.ErrUserNotFound
		}
		return nil, err
	}
	info := domain.NewUserInfo(userInfo)
	count, err := u.CountFans(ctx, userInfo.UserID)
	if err != nil {
		return nil, err
	}
	info.CntFans = int32(count)
	return info, nil
}

func (u *userInfoRepo) GetUserLevelById(ctx context.Context, userId int64) (*model.UserLevel, error) {
	userLevel := getQuery(ctx).UserLevel
	userLevelDo := userLevel.WithContext(ctx)
	userLevelInfo, err := userLevelDo.Where(userLevel.UserID.Eq(userId)).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errdef.ErrUserNotFound
		}
		return nil, err
	}
	return userLevelInfo, nil
}

// GetUserFansByUserID 读取粉丝关系及对应用户，两次查询均复用上下文事务。
// 输入：userId 为被关注用户 ID；输出：有效粉丝信息列表，查询错误透传。
func (u *userInfoRepo) GetUserFansByUserID(ctx context.Context, userId int64) ([]*domain.UserInfo, error) {
	userFan := getQuery(ctx).UserFan
	var fansIds = make([]int64, 0)
	err := userFan.WithContext(ctx).Where(userFan.UserID.Eq(userId)).Pluck(userFan.FollowerID, &fansIds)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errdef.ErrUserNotFound
		}
		return nil, err
	}

	// get userFan's fans' info by fansIds
	users, err := getUsersInfoByIDs(ctx, fansIds)
	if err != nil {
		return nil, err
	}
	infos := domain.NewUserInfos(users...)
	if len(users) == 0 {
		return infos, nil
	}
	counts := make([]fanCountRow, 0)
	q := getQuery(ctx).UserFan
	db := q.WithContext(ctx).UnderlyingDB().Session(&gorm.Session{NewDB: true, Context: ctx})
	if err := db.Model(&model.UserFan{}).Select("user_id, COUNT(*) AS count").Where("user_id IN ?", fansIds).Group("user_id").Find(&counts).Error; err != nil {
		return nil, err
	}
	byID := make(map[int64]int32, len(counts))
	for _, row := range counts {
		if row.Count > 2147483647 {
			return nil, stderr.New("粉丝数量超过当前 RPC int32 范围")
		}
		byID[row.UserID] = int32(row.Count)
	}
	for _, info := range infos {
		info.CntFans = byID[info.UserId]
	}
	return infos, nil
}

// fanCountRow 接收批量粉丝聚合，避免在粉丝用户摘要列表中逐条查询数量。
type fanCountRow struct{ UserID, Count int64 }

// GetUserFolloweesByUserIDFollowListID 沿用关注分页仓储，保留旧领域接口并透传全部读取错误。
func (u *userInfoRepo) GetUserFolloweesByUserIDFollowListID(ctx context.Context, userId, followListId int64, pageNum, pageSize int32) ([]*domain.UserSummary, error) {
	page, err := (&followRepo{}).GetFolloweesPage(ctx, userId, followListId, pageNum, pageSize)
	if err != nil {
		return nil, err
	}
	return page.Users, nil
}

// UpdateEmail 在上下文选定的连接上读取并更新用户邮箱，数据库错误透传。
func (u *userInfoRepo) UpdateEmail(ctx context.Context, userId int64, newEmail string) error {
	user := getQuery(ctx).User
	userDo, _, err := addUserModel(ctx, userId)
	if err != nil {
		return err
	}

	_, err = userDo.
		Where(user.UserID.Eq(userId)).
		Debug().
		Updates(&model.User{Email: newEmail})
	if err != nil {
		return err
	}
	return nil
}

// UpdatePassword 在上下文选定的连接上保存已加密密码，数据库错误透传。
func (u *userInfoRepo) UpdatePassword(ctx context.Context, userId int64, encryptedPassword string) error {
	user := getQuery(ctx).User
	//userDo, _, err := addUserModel(ctx, userId)
	//if err != nil {
	//	return err
	//}
	userDo := user.WithContext(ctx)

	_, err := userDo.
		Where(user.UserID.Eq(userId)).
		Debug().
		Updates(&model.User{Password: encryptedPassword})
	if err != nil {
		return err
	}
	return nil
}

// UpdateUserSignature 在上下文选定的连接上读取并更新签名，保留零值更新语义。
func (u *userInfoRepo) UpdateUserSignature(ctx context.Context, userId int64, signature string) error {
	user := getQuery(ctx).User
	userDo, _, err := addUserModel(ctx, userId)
	if err != nil {
		return err
	}

	_, err = userDo.Where(user.UserID.Eq(userId)).Update(user.Signature, signature)
	if err != nil {
		return err
	}
	return nil
}

// CheckUsernameConflict check if the new username is conflict with existing username
func (u *userInfoRepo) CheckUsernameConflict(ctx context.Context, newUsername string) (ok bool, err error) {
	user := getQuery(ctx).User
	userDo := user.WithContext(ctx)
	count, err := userDo.Where(user.Username.Eq(newUsername)).Count()
	if err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return false, err
		}
	}
	if count > 0 {
		return false, nil
	}
	return true, nil
}

// UpdateUsername 在上下文选定的连接上读取并更新用户名，数据库错误透传。
func (u *userInfoRepo) UpdateUsername(ctx context.Context, userId int64, newUsername string) error {
	user := getQuery(ctx).User
	userDo, _, err := addUserModel(ctx, userId)
	if err != nil {
		return err
	}
	_, err = userDo.
		Where(user.UserID.Eq(userId)).
		Debug().
		Updates(&model.User{Username: newUsername})
	if err != nil {
		return err
	}
	return nil
}

// UpdateCntFollows 在上下文选定的连接上调整关注数，拒绝更新为负数。
func (u *userInfoRepo) UpdateCntFollows(ctx context.Context, userId int64, change int64) error {
	// 1. Check if the change is valid
	user := getQuery(ctx).User
	userDo, tmpUser, err := addUserModel(ctx, userId)
	if err != nil {
		return err
	}
	if tmpUser.CntFollows+change < 0 {
		return stderr.New("cnt_follows can not be less than 0")
	}

	// 2. Update cnt_follows
	_, err = userDo.
		Where(user.UserID.Eq(userId)).
		Updates(&model.User{CntFollows: tmpUser.CntFollows + change})
	if err != nil {
		return err
	}
	return nil
}

func (u *userInfoRepo) AddUserCntLike(ctx context.Context, tx *sql.Tx, userId int64) error {
	user := query.User
	userDo, userModel, err := getGormDBAndUserModel(tx, userId)
	if err != nil {
		return err
	}

	return userDo.Model(userModel).
		Where(user.UserID.Eq(userId)).
		Updates(map[string]any{
			"cnt_likes": userModel.CntLikes + 1,
		}).Error
}

func (u *userInfoRepo) DecrementUserCntLike(ctx context.Context, tx *sql.Tx, userId int64) error {
	user := query.User
	userDo, userModel, err := getGormDBAndUserModel(tx, userId)
	if err != nil {
		return err
	}

	return userDo.Model(userModel).
		Where(user.UserID.Eq(userId)).
		Updates(map[string]any{
			"cnt_likes": userModel.CntLikes - 1,
		}).Error
}

func (u *userInfoRepo) UpdateUserShells(ctx context.Context, tx *sql.Tx, userId int64, shells int64) error {
	user := query.User
	userDo, userModel, err := getGormDBAndUserModel(tx, userId)
	if err != nil {
		return err
	}
	return userDo.Model(userModel).
		Where(user.UserID.Eq(userId)).
		Updates(map[string]any{
			"shells": userModel.Shells + shells,
		}).Error
}

func (u *userInfoRepo) GetUserShells(ctx context.Context, tx *sql.Tx, id int64) (int64, error) {
	_, userModel, err := getGormDBAndUserModel(tx, id)
	if err != nil {
		return 0, err
	}
	return userModel.Shells, nil
}

func (u *userInfoRepo) GetSqlTx() *sql.Tx {
	tx := u.data.mysql.Begin()
	return tx.Statement.ConnPool.(*sql.Tx)
}

func getUsersInfoByIDs(ctx context.Context, userIds []int64) ([]*model.User, error) {
	user := getQuery(ctx).User
	userdo := user.WithContext(ctx)
	userInfo, err := userdo.Where(user.UserID.In(userIds...)).Find()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errdef.ErrUserNotFound
		}
		return nil, err
	}
	return userInfo, nil
}

func getGormDBAndUserModel(tx *sql.Tx, userID int64) (*gorm.DB, *model.User, error) {
	db, err := dbutil.SqlTxToGormDB(tx)
	if err != nil {
		return nil, nil, err
	}

	var userModel model.User
	err = db.Where("user_id = ?", userID).First(&userModel).Error
	if err != nil {
		return nil, nil, err
	}
	return db, &userModel, nil
}
