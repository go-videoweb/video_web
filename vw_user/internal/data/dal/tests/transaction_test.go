package tests

import (
	"context"
	"fmt"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/mysql"
	"gorm.io/gen"
	"gorm.io/gorm"
	"sync"
	"testing"
	"time"
	"util/getid"
	"vw_user/internal/data/dal/model"
	"vw_user/internal/data/dal/query"
)

func TestTransactionManually(t *testing.T) {
	var err error

	tx := query.Q.Begin()
	defer func() {
		if err != nil {
			fmt.Println("rollback")
			tx.Rollback()
		}
		if r := recover(); r != nil {
			tx.Rollback()
		}
	}()
	require.NoError(t, tx.Error)
	// create userbiz's default follow_list and favorites
	userID := int64(987654321)
	defaultFavorites := &model.Favorite{
		FavoritesID:   getid.GetID(),
		UserID:        userID,
		FavoritesName: "默认收藏夹",
		Description:   "",
		IsPrivate:     1,
	}
	privateFavorites := &model.Favorite{
		FavoritesID:   getid.GetID(),
		UserID:        userID,
		FavoritesName: "私密收藏夹",
		Description:   "",
		IsPrivate:     -1,
	}
	userLevel := &model.UserLevel{
		UserID: userID,
	}
	defaultFollowList := &model.FollowList{
		ListID:   getid.GetID(),
		UserID:   userID,
		ListName: "默认关注列表",
	}
	err = tx.UserLevel.Create(userLevel)
	if err != nil {
		return
	}
	err = tx.Favorite.Create(defaultFavorites, privateFavorites)
	if err != nil {
		return
	}
	err = tx.FollowList.Create(defaultFollowList)
	if err != nil {
		return
	}
	tx.Commit()
}

func TestTransactionAutomatically(t *testing.T) {
	userID := int64(123456789)
	defaultFavorites := &model.Favorite{
		FavoritesID:   getid.GetID(),
		UserID:        userID,
		FavoritesName: "默认收藏夹",
		Description:   "",
		IsPrivate:     1,
	}
	privateFavorites := &model.Favorite{
		FavoritesID:   getid.GetID(),
		UserID:        userID,
		FavoritesName: "私密收藏夹",
		Description:   "",
		IsPrivate:     -1,
	}
	userLevel := &model.UserLevel{
		UserID: userID,
	}
	defaultFollowList := &model.FollowList{
		ListID:   getid.GetID(),
		UserID:   userID,
		ListName: "默认关注列表",
	}
	err := query.Q.Transaction(func(tx *query.Query) error {
		err := tx.UserLevel.Create(userLevel)
		if err != nil {
			return err
		}
		err = tx.Favorite.Create(defaultFavorites, privateFavorites)
		if err != nil {
			return err
		}
		err = tx.FollowList.Create(defaultFollowList)
		if err != nil {
			return err
		}
		return nil
	})
	require.NoError(t, err)
}

func TestOptimisticLockWithGen(t *testing.T) {
	userId := int64(1)
	user := query.User
	userDo := user.WithContext(context.Background())
	queryUser, err := userDo.Where(user.UserID.Eq(userId)).First()
	require.NoError(t, err)
	userDo.ReplaceDB(userDo.UnderlyingDB().Model(queryUser))

	//Test case 1: 使用 Update 更新 CntFollows 字段
	rowsInfect, err := userDo.
		Debug().
		Where(user.UserID.Eq(userId)).
		Update(user.CntFollows, user.CntFollows.Add(1))
	require.NoError(t, err)
	require.Equal(t, int64(1), rowsInfect.RowsAffected)

	// Test case 2: 使用 Updates + struct 更新 CntFollows 字段
	_, err = userDo.
		Debug().
		Where(user.UserID.Eq(userId)).
		Updates(&model.User{
			CntFollows: queryUser.CntFollows + 1,
		})
	require.NoError(t, err)

	// Test case 3: 使用 Updates + map[string]interface{} 更新 CntFollows 字段
	_, err = userDo.
		Debug().
		Where(user.UserID.Eq(userId)).
		Updates(map[string]interface{}{
			"cnt_follows": queryUser.CntFollows + 1,
		},
		)
	require.NoError(t, err)
}

func TestOptimisticLockWithGorm(t *testing.T) {
	userId := int64(1)
	host, port, name, password, dbname :=
		"127.0.0.1", "3306", "root", "YTGyhPntlpcYxs17Up3d", "vw_user"
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local", name, password, host, port, dbname)
	db, _ := gorm.Open(mysql.Open(dsn), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})

	var user1 *model.User
	db.Where("user_id = ?", userId).First(&user1)

	err := db.Model(user1).
		Where("user_id = ?", userId).
		Debug().
		Update("cnt_follows", user1.CntFollows+1).Error
	require.NoError(t, err)
}

func testParallelUpdateShells(t *testing.T) {
	t.Parallel()
	query.SetDefault(db)
	require.NotNil(t, query.User)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, err := updateShellsUseGen(1, 100, 0)
		if err != nil {
			t.Error(err)
		}
	}()

	go func() {
		defer wg.Done()
		// 延时 1s，这时另一个goroutine已经更新了shells，导致乐观锁失败
		info, err := updateShellsUseGen(1, 200, 1000)
		if info.RowsAffected == 0 {
			fmt.Println("optimistic lock is working!")
		}
		if err != nil {
			t.Error(err)
		}
	}()
	wg.Wait()
}

// 测试gorm乐观锁:使用gen生成的查询代码
func updateShellsUseGen(id int64, value int64, sleep int) (info gen.ResultInfo, err error) {
	tx := query.Q.Begin()
	defer func() {
		if recover() != nil || err != nil {
			//回滚事务
			_ = tx.Rollback()
		}
	}()

	do := tx.User.WithContext(context.Background())
	user, err := do.Where(query.User.UserID.Eq(id)).First()
	if err != nil {
		return info, err
	}
	fmt.Println("====================>", user.Shells)

	//模拟并发更新，事务存在延迟的情况
	time.Sleep(time.Duration(sleep) * time.Millisecond)
	do.ReplaceDB(do.UnderlyingDB().Model(user)) //指定底层gorm.DB的model为user，这样才能使用乐观锁插件
	//info, err = do.Debug().Where(query.User.UserID.Eq(id)).Update(tx.User.Shells, tx.User.Shells.Add(value))
	info, err = do.Debug().Where(query.User.UserID.Eq(id)).Updates(&model.User{
		Shells: user.Shells + value,
	})
	if err != nil {
		fmt.Println(err)
		return info, err
	}

	err = tx.Commit()
	return info, err
}
