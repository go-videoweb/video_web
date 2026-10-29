package tests

import (
	"context"
	"github.com/go-kratos/kratos/v2/config"
	"github.com/go-kratos/kratos/v2/config/file"
	"github.com/spewerspew/spew"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"testing"
	"vw_user/internal/conf"
	"vw_user/internal/data"
	"vw_user/internal/data/dal/model"
	"vw_user/internal/data/dal/query"
)

// 本文件是连接真实数据库的 user_follows/user_fans 练习测试，离线回归仅编译而不运行。
var db *gorm.DB

func init() {
	db = t_getGormDB()
	query.SetDefault(db)
}
func t_getGormDB() *gorm.DB {
	c := config.New(
		config.WithSource(
			//file.NewSource("D:\\Go\\WorkSpace\\src\\Go_Project\\demo\\kratos\\video_web\\vw_user\\configs\\config.yaml"),
			file.NewSource("../../../../configs/config.yaml"),
		),
	)
	defer c.Close()

	if err := c.Load(); err != nil {
		panic(err)
	}

	var bc conf.Bootstrap
	if err := c.Scan(&bc); err != nil {
		panic(err)
	}
	return data.NewMySQL(bc.Data)
}

func TestInsertUserFollow(t *testing.T) {
	uf := query.UserFollow.WithContext(context.Background())
	ufd := query.UserFan.WithContext(context.Background())
	ufValue := &model.UserFollow{
		FollowlistID: 1,
		FollowUserID: 2,
		UserID:       12,
	}
	ufdValue := &model.UserFan{
		UserID:     2,
		FollowerID: 12,
	}
	err := uf.Create(ufValue)
	require.NoError(t, err)

	err = ufd.Create(ufdValue)
	require.NoError(t, err)
}

func TestUpdateUserFollow(t *testing.T) {
	uf := query.UserFollow
	ufd := query.UserFan

	userFollow, err := uf.First()
	require.NoError(t, err)

	userFollowed, err := ufd.First()
	require.NoError(t, err)

	info, err := uf.Where(query.UserFollow.FollowlistID.Eq(userFollow.FollowlistID)).Update(uf.FollowlistID, 2)
	spew.Dump(info)
	require.NoError(t, err)

	info, err = ufd.Where(query.UserFan.UserID.Eq(userFollowed.UserID)).Update(ufd.UserID, 20)
	spew.Dump(info)
	require.NoError(t, err)
}

func TestDeleteUserFollow(t *testing.T) {
	uf := query.UserFollow.WithContext(context.Background())
	ufd := query.UserFan.WithContext(context.Background())

	userFollow, err := uf.First()
	require.NoError(t, err)

	userFollowed, err := ufd.First()
	require.NoError(t, err)

	info, err := uf.Where(query.UserFollow.UserID.Eq(userFollow.UserID)).Delete(&model.UserFollow{})
	spew.Dump(info)
	require.NoError(t, err)

	info, err = ufd.Where(query.UserFan.UserID.Eq(userFollowed.UserID)).Delete(&model.UserFan{})
	spew.Dump(info)
	require.NoError(t, err)
}
