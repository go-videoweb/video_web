package data

import (
	"context"
	"errors"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/redis/go-redis/v9"
	"golang.org/x/crypto/bcrypt"
	"strconv"
	"time"
	"util/getid"
	"util/helper"
	"vw_user/internal/biz"
	"vw_user/internal/data/dal/model"
	"vw_user/internal/data/dal/query"
	"vw_user/internal/pkg/ecode/errdef"
)

const (
	login      = "login"
	expOfLogin = 5
)

type userIdentityRepo struct {
	data   *Data
	logger *log.Helper
}

func NewUserRepo(data *Data, logger log.Logger) biz.UserIdentityRepo {
	return &userIdentityRepo{
		data:   data,
		logger: log.NewHelper(logger),
	}
}

func (u *userIdentityRepo) CheckPassword(ctx context.Context, password, hashedPassword string) error {
	err := bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(password))
	if err != nil {
		return errdef.ErrUserPasswordError
	}
	return nil
}

// AddExpForLogin 在当天尚未领取时增加登录经验，MySQL 访问沿用上下文事务。
// 输入：ctx 可携带本地事务；userId 为登录用户 ID。
// 输出：读取或更新失败时返回错误；Redis 登录标记不受 MySQL 回滚保护。
func (u *userIdentityRepo) AddExpForLogin(ctx context.Context, userId int64) error {
	userLevel := getQuery(ctx).UserLevel
	userLevelDo := userLevel.WithContext(ctx)
	level, err := userLevelDo.Where(userLevel.UserID.Eq(userId)).First()
	if err != nil {
		return err
	}
	loginKey := strconv.FormatInt(userId, 10) + login

	// check if userbiz has logged in today
	_, err = u.data.redis.Get(ctx, loginKey).Result()

	if err != nil {
		// no record in redis,add the exp and set the logging record in redis
		if errors.Is(err, redis.Nil) {
			//add the exp and update it in db
			level.AddExp(expOfLogin)
			_, err = userLevelDo.Where(userLevel.UserID.Eq(userId)).Updates(level)
			if err != nil {
				return err
			}

			//get tomorrow time at 00:00:00 for setting the expiration of login record
			now := time.Now()
			t := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, now.Location())
			u.data.redis.SetEx(ctx, loginKey, "1", t.Sub(now))
		} else { // other redis error
			return err
		}
	}
	return nil
}

func (u *userIdentityRepo) CacheAccessToken(ctx context.Context, userId, accessToken string, expiration time.Duration) error {
	_, err := u.data.redis.SetEx(ctx, userId, accessToken, expiration).Result()
	if err != nil {
		return helper.HandleError(errdef.ErrCacheAccessToken, err)
	}
	return nil
}

// CreatRecordsForRegister 原子创建用户及默认关注列表、收藏夹和等级记录。
// 输入：newUser 为待创建用户；ctx 可携带已有事务，使用该事务上的保存点，不独立提交外层事务。
// 输出：创建或事务错误；无外层事务时创建本地事务，出错或 panic 时由 GORM 处理回滚。
func (u *userIdentityRepo) CreatRecordsForRegister(ctx context.Context, newUser *model.User) error {
	defaultFavorites := &model.Favorite{
		FavoritesID:   getid.GetID(),
		UserID:        newUser.UserID,
		FavoritesName: "默认收藏夹",
		Description:   "",
		IsPrivate:     1,
	}
	privateFavorites := &model.Favorite{
		FavoritesID:   getid.GetID(),
		UserID:        newUser.UserID,
		FavoritesName: "私密收藏夹",
		Description:   "",
		IsPrivate:     -1,
	}
	userLevel := &model.UserLevel{
		UserID: newUser.UserID,
	}
	defaultFollowList := &model.FollowList{
		IsDefault: true,
		ListID:    getid.GetID(),
		UserID:    newUser.UserID,
		ListName:  "默认关注列表",
	}

	// 复用生成器的事务克隆，保留各模型独立的表信息；已有事务使用保存点。
	return getQuery(ctx).Transaction(func(tx *query.Query) error {
		txQuery := tx.WithContext(ctx)
		if err := txQuery.User.Create(newUser); err != nil {
			return err
		}
		if err := txQuery.FollowList.Create(defaultFollowList); err != nil {
			return err
		}
		if err := txQuery.Favorite.Create(defaultFavorites, privateFavorites); err != nil {
			return err
		}
		return txQuery.UserLevel.Create(userLevel)
	})
}

func (u *userIdentityRepo) DeleteCachedAccessToken(ctx context.Context, userId string) error {
	_, err := u.data.redis.Del(ctx, userId).Result()
	return err
}
