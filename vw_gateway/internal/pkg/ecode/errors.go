package ecode

const (
	// Intertal errors
	INTERNAL_ErrInternal = 3001 + iota

	// identity errors
	IDENTITY_ErrUserLoggedOut

	// auth errors
	AUTH_TokenMissing
	AUTH_TokenInvalid
	AUTH_AccessTokenExpired
	AUTH_RefreshTokenExpired
	AUTH_ParseTokenFailed
	AUTH_CreateTokenFailed

	// parse HTTP request's file errors
	HTTP_FormFileFailed
	HTTP_UploadAvatarFailed

	// file errors
	FILE_UploadAvatarFailed
	FILE_UpdateAvatarFailed
	FILE_GetMpdFileFailed
	FILE_CoverFileNotFound
	FILE_SegmentFileNotFound

	// video errors
	VIDEO_GetVideoListFailed
	VIDEO_GetVideoInfoFailed
	VIDEOINFO_GetVideoFileFailed
	VIDEOINFO_GetVideoMpdFailed
	VIDEOINFO_GetVideoCoverFailed
	VIDEOINFO_GetVideoSegmentFailed
)

var ()

// 以下业务错误按独立区间分配，数值随接口传递给客户端。
const (
	RegistrationSessionFailed = 3101 + iota // REGISTRATION_SESSION_FAILED
	InvalidEmail                            // INVALID_EMAIL
	InvalidPassword                         // INVALID_PASSWORD
	InvalidCodePurpose                      // INVALID_CODE_PURPOSE
	InvalidBarrageLike                      // INVALID_BARRAGE_LIKE
	InvalidClientIP                         // INVALID_CLIENT_IP
	EmailCodeFailed                         // EMAIL_CODE_FAILED
	EmailSendFailed                         // EMAIL_SEND_FAILED
	InvalidCommentLike                      // INVALID_COMMENT_LIKE
	InvalidPublicSiteOrigin                 // INVALID_PUBLIC_SITE_ORIGIN
	FavoriteIDMissing                       // FAVORITE_ID_MISSING
	FavoritesListMissing                    // FAVORITES_LIST_MISSING
	FavoriteVideosMissing                   // FAVORITE_VIDEOS_MISSING
	FavoriteVideoInvalid                    // FAVORITE_VIDEO_INVALID
	AvatarRequired                          // AVATAR_REQUIRED
	InvalidAvatar                           // INVALID_AVATAR
	AvatarTooLarge                          // AVATAR_TOO_LARGE
	InvalidAvatarResponse                   // INVALID_AVATAR_RESPONSE
	ParseMultipartForm                      // ParseMultipartForm ERROR
	BindRequest                             // BIND
	RequestCodec                            // CODEC
	RequestContextMissing                   // REQUEST_CONTEXT_MISSING
	InvalidUserID                           // INVALID_USER_ID
	InvalidIdentity                         // INVALID_IDENTITY
	InvalidContext                          // INVALID_CONTEXT
	InvalidRequestContext                   // invalid context
	Unauthorized                            // UNAUTHORIZED
	UserIDMissing                           // USER_ID_MISSING
	InvalidUserIDUnauthorized               // INVALID_USER_ID
	InvalidVideoResponse                    // INVALID_VIDEO_RESPONSE
	VideoIDMissing                          // VIDEO_ID_MISSING
	VideoInfoRequired                       // VIDEO_INFO_REQUIRED
	SegmentPathUnsupported                  // SEGMENT_PATH_UNSUPPORTED
	ErrorEncodingFailed                     // ERROR_ENCODING_FAILED
	EmailCodeCacheFailed                    // EMAIL_CODE_CACHE_FAILED
)
