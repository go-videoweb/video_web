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

// 以下业务错误按独立区间分配，数值随接口传递给客户端。
const (
	PasswordSessionInvalidationFailed = 4101 + iota // PASSWORD_SESSION_INVALIDATION_FAILED
	EmailInUse                                      // EMAIL_IN_USE
	PasswordResetUnavailable                        // PASSWORD_RESET_UNAVAILABLE
	EmailRelationConflict                           // EMAIL_RELATION_CONFLICT
	PasswordResetConflict                           // PASSWORD_RESET_CONFLICT
	AccountConflict                                 // ACCOUNT_CONFLICT
	EmailUpdateConflict                             // EMAIL_UPDATE_CONFLICT
	PasswordUpdateConflict                          // PASSWORD_UPDATE_CONFLICT
	InvalidEmail                                    // INVALID_EMAIL
	InvalidPassword                                 // INVALID_PASSWORD
	InvalidCodePurpose                              // INVALID_CODE_PURPOSE
	InvalidClientIP                                 // INVALID_CLIENT_IP
	InvalidCodeExpiration                           // INVALID_CODE_EXPIRATION
	InvalidEmailCode                                // INVALID_EMAIL_CODE
	EmailCodeRateLimited                            // EMAIL_CODE_RATE_LIMITED
	InvalidFavorite                                 // INVALID_FAVORITE
	InvalidFavoritePrivacy                          // INVALID_FAVORITE_PRIVACY
	FavoriteNotFound                                // FAVORITE_NOT_FOUND
	InvalidFavoriteMetadata                         // INVALID_FAVORITE_METADATA
	AvatarNotFound                                  // AVATAR_NOT_FOUND
	InvalidStoredAvatar                             // INVALID_STORED_AVATAR
	InvalidAvatarStream                             // INVALID_AVATAR_STREAM
	AvatarTooLarge                                  // AVATAR_TOO_LARGE
	InvalidAvatar                                   // INVALID_AVATAR
	InvalidAvatarDirectory                          // INVALID_AVATAR_DIRECTORY
	InvalidAvatarPath                               // INVALID_AVATAR_PATH
	InvalidAvatarFile                               // INVALID_AVATAR_FILE
	AvatarChanged                                   // AVATAR_CHANGED
	InvalidFollowListName                           // INVALID_FOLLOW_LIST_NAME
	InvalidFollowList                               // INVALID_FOLLOW_LIST
	DefaultFollowListConflict                       // DEFAULT_FOLLOW_LIST_CONFLICT
	FollowListNotFound                              // FOLLOW_LIST_NOT_FOUND
	FollowListNameExists                            // FOLLOW_LIST_NAME_EXISTS
	InvalidFollowListID                             // INVALID_FOLLOW_LIST
	InvalidFollowListIDs                            // INVALID_FOLLOW_LIST_IDS
	DefaultFollowListDelete                         // DEFAULT_FOLLOW_LIST_DELETE
	InvalidFollowTarget                             // INVALID_FOLLOW_TARGET
	FollowGroupConflict                             // FOLLOW_GROUP_CONFLICT
	FollowNotFound                                  // FOLLOW_NOT_FOUND
	InvalidFollowPage                               // INVALID_FOLLOW_PAGE
	FollowConflict                                  // FOLLOW_CONFLICT
	FollowWriteConflict                             // FOLLOW_WRITE_CONFLICT
	FansCountOverflow                               // FANS_COUNT_OVERFLOW
	ParseMultipartForm                              // ParseMultipartForm ERROR
	BindRequest                                     // BIND
	RequestCodec                                    // CODEC
	InvalidUserID                                   // INVALID_USER_ID
	UserNotFoundByID                                // USER_NOT_FOUND
)
