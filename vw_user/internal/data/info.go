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

func (u *userInfoRepo) GetUserInfoByUsername(ctx context.Context, username string) (*domain.UserInfo, error) {
	user := getQuery(ctx).User
	userdo := user.WithContext(ctx)
	userInfo, err := userdo.Where(user.Username.Eq(username)).
		Select(user.UserID, user.Username, user.IsAdmin, user.Email, user.Password,
			user.Gender, user.AvatarPath, user.Birthday, user.Signature, user.Shells,
			user.CntFollows, user.CntFans).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errdef.ErrUserNotFound
		}
		return nil, err
	}

	return domain.NewUserInfo(userInfo), nil
}

func (u *userInfoRepo) GetUserInfoByUserId(ctx context.Context, userid int64) (*domain.UserInfo, error) {
	queryUser := getQuery(ctx).User
	userDo := queryUser.WithContext(ctx)
	userInfo, err := userDo.Where(queryUser.UserID.Eq(userid)).
		Select(queryUser.UserID, queryUser.Username, queryUser.IsAdmin, queryUser.Email, queryUser.CntLikes, queryUser.Password,
			queryUser.Gender, queryUser.AvatarPath, queryUser.Birthday, queryUser.Signature).First()
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, errdef.ErrUserNotFound
		}
		return nil, err
	}
	return domain.NewUserInfo(userInfo), nil
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
	return domain.NewUserInfos(users...), nil
}

func (u *userInfoRepo) GetUserFolloweesByUserIDFollowListID(ctx context.Context, userId, followListId int64, pageNum, pageSize int32) ([]*domain.UserSummary, error) {
	userFollow := getQuery(ctx).UserFollow
	userFollowDo := userFollow.WithContext(ctx)

	// Get userbiz followees' ids
	tmpIds, _, err := userFollowDo.Select(userFollow.FollowUserID).
		Where(userFollow.UserID.Eq(userId), userFollow.FollowlistID.Eq(followListId)).
		FindByPage(int((pageNum-1)*pageSize), int(pageSize))

	// Get userbiz followees' info by ids
	userIds := make([]int64, len(tmpIds))
	for i, v := range tmpIds {
		userIds[i] = v.FollowUserID
	}

	// Transform userbiz ids to userbiz summaries
	users, err := getUsersInfoByIDs(ctx, userIds)
	if err != nil {
		return nil, err
	}

	return domain.NewUserSummaries(users...), nil
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

// UpdateCntFans 在上下文选定的连接上调整粉丝数，拒绝更新为负数。
func (u *userInfoRepo) UpdateCntFans(ctx context.Context, userId int64, change int64) error {
	// 1. Check if the change is valid
	user := getQuery(ctx).User
	userDo, tmpUser, err := addUserModel(ctx, userId)
	if err != nil {
		return err
	}
	if tmpUser.CntFans+change < 0 {
		return stderr.New("cnt_fans can not be less than 0")
	}

	// 2. Update cnt_fans
	_, err = userDo.
		Where(user.UserID.Eq(userId)).
		Updates(&model.User{CntFans: tmpUser.CntFans + change})
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
