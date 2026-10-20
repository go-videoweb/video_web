package ecode

const (
	// Intertal errors
	INTERNAL_ErrInternal = 4001 + iota

	// identity errors
	IDENTITY_LoginFailed
	IDENTITY_UserNameEmpty
	IDENTITY_UserPasswordEmpty
	IDENTITY_UserNotFound
	IDENTITY_UserPasswordError
	IDENTITY_UserAlreadyExist
	IDENTITY_PasswordTooShort
	IDENTITY_PasswordNotMatch
	IDENTITY_SignatureTooLong
	IDENTITY_VerifyCodeExpired
	IDENTITY_VerifyCodeNotMatch
	IDENTITY_EncryptPasswordFailed
	IDENTITY_CreateUserDirFailed
	IDENTITY_CreateUserAvatarFailed
	IDENTITY_ReadAvatarFailed
	IDENTITY_SaveAvatarFailed
	IDENTITY_ErrLogoutFailed
	IDENTITY_ErrCreateUserRecordsFailed

	// auth errors
	AUTH_TokenMissing
	AUTH_TokenInvalid
	AUTH_AccessTokenExpired
	AUTH_RefreshTokenExpired
	AUTH_ParseTokenFailed
	AUTH_CreateTokenFailed
	AUTH_CacheAccessTokenFailed

	// userinfo errors
	USERINFO_ForgetPasswordFailed
	USERINFO_ModifyEmailFailed
	USERINFO_ModifyEmailFailed_Get
	USERINFO_ModifyPasswordFailed
	USERINFO_ModifyUsernameFailed
	USERINFO_GetUserInfoFailed
	USERINFO_ModifySignatureFailed
	USERINFO_AddUserLikeFailed

	// favoritee errors
	FAVORITES_CreateFavoritesFailed
	FAVORITES_FavoritesNameConflict
	FAVORITES_DeleteFavoritesFailed
	FAVORITES_ModifyFavoritesFailed
	FAVORITES_FavoritesNotEmpty

	// follow errors
	FOLLOW_FollowOtherUserFailed
	FOLLOW_UnfollowOtherUserFailed

	// file errors
	FILE_UploadAvatarFailed
	FILE_UpdateAvatarFailed

	// 收藏夹列表错误追加在末尾，保持所有既有业务码的数值不变。
	FAVORITES_GetFavoritesListFailed
)

var ()
