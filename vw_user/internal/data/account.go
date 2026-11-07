package data

import (
	"context"
	stderr "errors"
	"strconv"

	"github.com/go-kratos/kratos/v2/errors"
	drivermysql "github.com/go-sql-driver/mysql"
	"gorm.io/gen/field"
	"vw_user/internal/data/dal/model"
)

// CheckEmailConflict 包含软删除账号，换绑时仅排除本人；并发冲突仍须数据库唯一索引兜底。
func (u *userInfoRepo) CheckEmailConflict(ctx context.Context, email string, exceptUserID int64) error {
	user := getQuery(ctx).User
	q := user.WithContext(ctx).Unscoped().Where(user.Email.Eq(email))
	if exceptUserID > 0 {
		q = q.Where(user.UserID.Neq(exceptUserID))
	}
	count, err := q.Count()
	if err != nil {
		return err
	}
	if count > 0 {
		return errors.Conflict("EMAIL_IN_USE", "该邮箱已被使用")
	}
	return nil
}

// GetUserByEmail 最多读取两条有效记录，拒绝历史多归属，不加载粉丝数或其他无关资料。
func (u *userInfoRepo) GetUserByEmail(ctx context.Context, email string) (*model.User, error) {
	user := getQuery(ctx).User
	rows, err := user.WithContext(ctx).Select(user.UserID, user.Email).Where(user.Email.Eq(email)).Limit(2).Find()
	if err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, errors.BadRequest("PASSWORD_RESET_UNAVAILABLE", "无法完成密码重置，请核对邮箱并重新获取验证码")
	}
	if len(rows) > 1 {
		return nil, errors.Conflict("EMAIL_RELATION_CONFLICT", "邮箱归属异常，请联系管理员")
	}
	return rows[0], nil
}

// ResetPasswordByEmail 在原邮箱仍属于同一有效账号时更新密码；换绑或删除导致零行则拒绝。
// 仅更新密码和版本，不使用 Debug 输出敏感 SQL 参数。
func (u *userInfoRepo) ResetPasswordByEmail(ctx context.Context, userID int64, email, encryptedPassword string) error {
	user := getQuery(ctx).User
	result, err := user.WithContext(ctx).Where(user.UserID.Eq(userID), user.Email.Eq(email)).UpdateSimple(
		user.Password.Value(encryptedPassword), field.NewInt64(user.TableName(), "version").Add(1),
	)
	if err != nil {
		return err
	}
	if result.RowsAffected != 1 {
		return errors.Conflict("PASSWORD_RESET_CONFLICT", "账号状态或邮箱已变更，请重新获取验证码")
	}
	return nil
}

// InvalidateUserSession 沿用现有按用户 ID 缓存 access token 的约定，使旧双 token 无法通过会话校验。
func (u *userInfoRepo) InvalidateUserSession(ctx context.Context, userID int64) error {
	return u.data.redis.Del(ctx, strconv.FormatInt(userID, 10)).Err()
}

// accountWriteError 将数据库并发唯一键冲突转为业务冲突，不暴露索引或账号原始数据。
func accountWriteError(err error) error {
	var duplicate *drivermysql.MySQLError
	if stderr.As(err, &duplicate) && duplicate.Number == 1062 {
		return errors.Conflict("ACCOUNT_CONFLICT", "邮箱或用户名已被使用")
	}
	return err
}
