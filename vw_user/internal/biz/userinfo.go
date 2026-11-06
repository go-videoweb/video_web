package biz

import (
	"context"
	"database/sql"
	stderr "errors"
	"github.com/dtm-labs/dtm/client/dtmcli"
	"github.com/dtm-labs/dtm/client/dtmgrpc"
	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"golang.org/x/crypto/bcrypt"
	"google.golang.org/grpc/codes"
	"unicode/utf8"
	"util/helper"
	"vw_user/internal/data/dal/model"
	"vw_user/internal/domain"
	"vw_user/internal/pkg/ecode/errdef"
)

type UserInfoRepo interface {
	// CheckEmailConflict 同时检查有效和软删除账号，exceptUserID 为换绑时排除的本人 ID。
	CheckEmailConflict(ctx context.Context, email string, exceptUserID int64) error
	// GetUserByEmail 仅接受唯一有效邮箱归属，历史多匹配不得静默选择。
	GetUserByEmail(ctx context.Context, email string) (*model.User, error)
	// ResetPasswordByEmail 同时匹配用户 ID、已验证邮箱及有效记录，检查影响行数。
	ResetPasswordByEmail(ctx context.Context, userID int64, email, encryptedPassword string) error
	// InvalidateUserSession 删除当前用户登录态，不签发新 token。
	InvalidateUserSession(ctx context.Context, userID int64) error
	GetSqlTx() *sql.Tx

	// GetUserInfoByUserId get user summary info by user id
	GetUserInfoByUserId(ctx context.Context, userId int64) (*domain.UserInfo, error)

	// GetUserInfoByUsername get user summary info by username
	GetUserInfoByUsername(ctx context.Context, username string) (*domain.UserInfo, error)

	// GetUserLevelById get user level by user id
	GetUserLevelById(ctx context.Context, userId int64) (*model.UserLevel, error)

	// GetUserFansByUserID get user fans by user id
	GetUserFansByUserID(ctx context.Context, userId int64) ([]*domain.UserInfo, error)

	// GetUserFolloweesByUserIDFollowListID GetUserFollowersByUserID get user followers by user id and followList id
	GetUserFolloweesByUserIDFollowListID(ctx context.Context, userId, followListID int64, pageNum, pageSize int32) ([]*domain.UserSummary, error)

	// UpdateEmail update user email
	UpdateEmail(ctx context.Context, userId int64, newEmail string) error

	// UpdatePassword update user password. Front-end should check if the password is valid
	UpdatePassword(ctx context.Context, userId int64, newPassword string) error

	// UpdateUserSignature update user signature
	UpdateUserSignature(ctx context.Context, userId int64, signature string) error

	// CheckUsernameConflict check if the new username is conflict with existing username
	CheckUsernameConflict(ctx context.Context, newUsername string) (ok bool, err error)

	// UpdateUsername update user username
	UpdateUsername(ctx context.Context, userId int64, newUsername string) error

	// UpdateCntFollows update user fans count
	UpdateCntFollows(ctx context.Context, userId int64, change int64) error

	// AddUserCntLike add user like count
	AddUserCntLike(ctx context.Context, tx *sql.Tx, userId int64) error

	// DecrementUserCntLike decrement user like count
	DecrementUserCntLike(ctx context.Context, tx *sql.Tx, userId int64) error

	// UpdateUserShells update user shells
	UpdateUserShells(ctx context.Context, tx *sql.Tx, userId int64, shells int64) error

	// GetUserShells get user shells
	GetUserShells(ctx context.Context, tx *sql.Tx, id int64) (int64, error)
}

type UserInfoUsecase struct {
	infoRepo UserInfoRepo
	captcha  CaptchaRepo
	logger   *log.Helper
}

func NewUserInfoUsecase(repo UserInfoRepo, captcha CaptchaRepo, logger log.Logger) *UserInfoUsecase {
	return &UserInfoUsecase{
		infoRepo: repo,
		captcha:  captcha,
		logger:   log.NewHelper(logger),
	}
}

func (uic *UserInfoUsecase) GetUserinfo(ctx context.Context, userId int64) (*domain.UserInfo, error) {
	user, err := uic.infoRepo.GetUserInfoByUserId(ctx, userId)
	if err != nil {
		return nil, helper.HandleError(errdef.ErrGetUserInfoFailed, err)
	}

	// TODO：向 vw_video 系统请求视频数量，并填充到 UserInfo 结构体中
	return user, nil

}

// ModifyEmail 验证新邮箱的唯一性和专用验证码后换绑；并发冲突由唯一索引兜底。
func (uic *UserInfoUsecase) ModifyEmail(ctx context.Context, userID int64, email string, inputCode string) (newEmail string, err error) {
	if userID <= 0 {
		return "", kerrors.BadRequest("INVALID_USER_ID", "用户ID不正确")
	}
	email, err = helper.NormalizeEmail(email)
	if err != nil {
		return "", err
	}
	if err := uic.infoRepo.CheckEmailConflict(ctx, email, userID); err != nil {
		return "", err
	}
	if err := uic.captcha.ConsumeCode(ctx, email, helper.EmailCodeChange, inputCode); err != nil {
		return "", err
	}

	// Update the email in the database
	err = uic.infoRepo.UpdateEmail(ctx, userID, email)
	if err != nil {
		return "", err
	}

	return email, nil
}

// ModifyPassword 保留旧密码验证流程，保存合规的新密码后使已有登录态失效。
func (uic *UserInfoUsecase) ModifyPassword(ctx context.Context, userId int64, oldPassword, newPassword string) error {
	if userId <= 0 {
		return kerrors.BadRequest("INVALID_USER_ID", "用户ID不正确")
	}
	if err := helper.ValidatePassword(newPassword); err != nil {
		return err
	}
	info, err := uic.infoRepo.GetUserInfoByUserId(ctx, userId)
	if err != nil {
		return helper.HandleError(errdef.ErrModifyPasswordFailed, err)
	}

	err = bcrypt.CompareHashAndPassword([]byte(info.Password), []byte(oldPassword))
	if err != nil {
		return helper.HandleError(errdef.ErrUserPasswordError)
	}

	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return errdef.ErrEncryptPasswordFailed
	}

	err = uic.infoRepo.UpdatePassword(ctx, userId, string(hashedPassword))
	if err != nil {
		return helper.HandleError(errdef.ErrModifyPasswordFailed, err)
	}
	return uic.invalidatePasswordSession(ctx, userId)
}

func (uic *UserInfoUsecase) ModifyUserSignature(ctx context.Context, userId int64, signature string) (newSignature string, err error) {
	// Check if the signature length is valid
	if utf8.RuneCountInString(signature) > 25 {
		return "", helper.HandleError(errdef.ErrModifySignatureFailed, stderr.New("签名不能超过25个字符"))
	}

	err = uic.infoRepo.UpdateUserSignature(ctx, userId, signature)
	if err != nil {
		return "", helper.HandleError(errdef.ErrModifySignatureFailed, err)
	}
	return signature, nil
}

// ForgetPassword 只按已验证邮箱定位账号，不接受客户端 user_id，也不自动登录。
// 验证码消费、密码保存与会话清理跨 Redis/MySQL；保存失败须重新申请验证码，不恢复旧验证码。
func (uic *UserInfoUsecase) ForgetPassword(ctx context.Context, email string, inputCode string, newPassword string) error {
	email, err := helper.NormalizeEmail(email)
	if err != nil {
		return err
	}
	if err := helper.ValidatePassword(newPassword); err != nil {
		return err
	}
	if err := uic.captcha.ConsumeCode(ctx, email, helper.EmailCodeReset, inputCode); err != nil {
		return err
	}
	user, err := uic.infoRepo.GetUserByEmail(ctx, email)
	if err != nil {
		return err
	}
	encryptedPassword, err := bcrypt.GenerateFromPassword([]byte(newPassword), bcrypt.DefaultCost)
	if err != nil {
		return errdef.ErrEncryptPasswordFailed
	}
	if err := uic.infoRepo.ResetPasswordByEmail(ctx, user.UserID, email, string(encryptedPassword)); err != nil {
		return err
	}
	return uic.invalidatePasswordSession(ctx, user.UserID)
}

// invalidatePasswordSession 明确报告密码已改变但会话清理失败，不能误报密码未修改或整体成功。
func (uic *UserInfoUsecase) invalidatePasswordSession(ctx context.Context, userID int64) error {
	if err := uic.infoRepo.InvalidateUserSession(ctx, userID); err != nil {
		return kerrors.InternalServer("PASSWORD_SESSION_INVALIDATION_FAILED", "密码已更新，但登录态清理失败，请使用新密码重新登录并联系管理员处理旧会话").
			WithMetadata(map[string]string{"password_changed": "true", "session_revoked": "false"})
	}
	return nil
}

func (uic *UserInfoUsecase) ModifyUsername(ctx context.Context, userId int64, username string) (newUsername string, err error) {
	// Check if the new username is conflict with existing username
	ok, err := uic.infoRepo.CheckUsernameConflict(ctx, username)
	if !ok {
		return "", helper.HandleError(errdef.ErrModifyUsernameFailed, stderr.New("该用户名已存在"))
	}
	if err != nil {
		return "", helper.HandleError(errdef.ErrModifyUsernameFailed, err)
	}

	// The new username is valid, update the username in the database
	err = uic.infoRepo.UpdateUsername(ctx, userId, username)
	if err != nil {
		return "", helper.HandleError(errdef.ErrModifyUsernameFailed, err)
	}

	return username, nil
}

func (uic *UserInfoUsecase) UpdateUserCntLikes(ctx context.Context, userID int64, isUpvoted int32) error {
	tx := uic.infoRepo.GetSqlTx()
	barrier, err := dtmgrpc.BarrierFromGrpc(ctx)
	if err != nil {
		return helper.HandleGrpcError(codes.Aborted, dtmcli.ResultFailure, err)
	}

	err = barrier.Call(tx, func(tx *sql.Tx) error {
		// Case 1: isUpvoted == 1, add user like count
		if isUpvoted == 1 {
			return uic.infoRepo.AddUserCntLike(ctx, tx, userID)

			// Case 2: isUpvoted == -1, decrement user like count
		} else {
			return uic.infoRepo.DecrementUserCntLike(ctx, tx, userID)
		}
	})
	if err != nil {
		return helper.HandleGrpcError(codes.Aborted, dtmcli.ResultFailure, err)
	}
	return nil
}
func (uic *UserInfoUsecase) DecrementUserCntLikes(ctx context.Context, userID int64) error {
	tx := uic.infoRepo.GetSqlTx()
	barrier, err := dtmgrpc.BarrierFromGrpc(ctx)
	if err != nil {
		return helper.HandleGrpcError(codes.Aborted, dtmcli.ResultFailure, err)
	}

	err = barrier.Call(tx, func(tx *sql.Tx) error {
		return uic.infoRepo.DecrementUserCntLike(ctx, tx, userID)
	})
	if err != nil {
		return helper.HandleGrpcError(codes.Aborted, dtmcli.ResultFailure, err)
	}
	return nil
}

func (uic *UserInfoUsecase) UpdateUserShells(ctx context.Context, userId int64, publisherId int64, shells int64) error {
	tx := uic.infoRepo.GetSqlTx()
	barrier, err := dtmgrpc.BarrierFromGrpc(ctx)
	if err != nil {
		return helper.HandleGrpcError(codes.Aborted, dtmcli.ResultFailure, err)
	}

	err = barrier.Call(tx, func(tx *sql.Tx) error {
		//  1. Deduct the shells from the user who throws the shells
		//	1.1 Check if the user has enough shells
		userShells, err := uic.infoRepo.GetUserShells(ctx, tx, userId)
		if err != nil {
			return err
		}
		if userShells < shells {
			return stderr.New("用户剩余贝壳不足")
		}

		// 1.2 Deduct the shells from the user who throws the shells
		err = uic.infoRepo.UpdateUserShells(ctx, tx, userId, -shells)
		if err != nil {
			return err
		}

		if userId != publisherId {
			// 2. Add the shells to the publisher
			err = uic.infoRepo.UpdateUserShells(ctx, tx, publisherId, shells)
			if err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return helper.HandleGrpcError(codes.Aborted, dtmcli.ResultFailure, err)
	}
	return nil
}
