package data

import (
	"context"
	"errors"
	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"gorm.io/gen/field"
	"gorm.io/gorm"
	"vw_user/internal/biz"
)

// fileRepo 通过当前事务查询用户头像，不处理磁盘文件。
type fileRepo struct {
	data *Data       // Wire 装配的现有数据依赖。
	log  *log.Helper // 仓储日志。
}

// NewFileRepo 装配头像仓储，查询统一使用 getQuery。
func NewFileRepo(data *Data, logger log.Logger) biz.FileRepo {
	return &fileRepo{data: data, log: log.NewHelper(logger)}
}

// GetAvatarPath 返回有效用户当前路径，用户不存在或已删除时返回 404。
func (r *fileRepo) GetAvatarPath(ctx context.Context, userID int64) (string, error) {
	u := getQuery(ctx).User
	user, err := u.WithContext(ctx).Select(u.AvatarPath).Where(u.UserID.Eq(userID)).First()
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", kerrors.NotFound("USER_NOT_FOUND", "用户不存在")
	}
	if err != nil {
		return "", err
	}
	return user.AvatarPath, nil
}

// UpdateAvatarPath 以旧路径为条件切换头像，只更新目标字段和版本；零行不能报告成功。
func (r *fileRepo) UpdateAvatarPath(ctx context.Context, userID int64, previous, next string) error {
	u := getQuery(ctx).User
	result, err := u.WithContext(ctx).Where(u.UserID.Eq(userID), u.AvatarPath.Eq(previous)).
		UpdateSimple(u.AvatarPath.Value(next), field.NewInt64(u.TableName(), "version").Add(1))
	if err != nil {
		return err
	}
	if result.RowsAffected != 1 {
		return kerrors.Conflict("AVATAR_CHANGED", "用户或头像已变化，请重新查询后重试")
	}
	return nil
}
