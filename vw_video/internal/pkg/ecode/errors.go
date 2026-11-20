package ecode

const (
	// Video info errors
	InternalError = 5001 + iota
	VIDEOINFO_GetVideoInfoFailed
	VIDEOINFO_VideoNotFound
	VIDEOINFO_GetVideoListFailed
	VIDEOINFO_UploadVideoInfoFailed
	VIDEOINFO_UploadVideoFileFailed
	VIDEOINFO_UploadVideoCoverFailed
	VIDEOINFO_GetVideoFileFailed
	VIDEOINFO_GetVideoMpdFailed
	VIDEOINFO_GetVideoCoverFailed
	VIDEOINFO_GetVideoSegmentFailed

	// Video interact errors
	VIDEOINTERACT_UpvoteVideoFailed
	VIDEOINTERACT_UpvoteVideoRevertFailed

	VIDEOINTERACT_CancelUpvoteVideoFailed
	VIDEOINTERACT_CancelUpvoteVideoRevertFailed

	VIDEOINTERACT_FavoriteVideoFailed
	VIDEOINTERACT_FavoriteVideoRevertFailed

	VIDEOINTERACT_CancelFavoriteVideoFailed
	VIDEOINTERACT_CancelFavoriteVideoRevertFailed

	VIDEOINTERACT_ThrowShellsFailed
	VIDEOINTERACT_ThrowShellsRevertFailed

	VIDEOINTERACT_ShareVideoFailed
	VIDEOINTERACT_SendBarrageFailed
	VIDEOINTERACT_UpvoteBarrageFailed
	VIDEOINTERACT_GetUserVideoStatusFailed
	VIDEOINTERACT_SetUserVideoStatusFailed
	VIDEOINTERACT_SetUserVideoStatusRevertFailed
)

const (
	// 视频上传校验错误使用独立区间，保留既有业务码。
	VIDEOINFO_InvalidUploadIDs = 5101 + iota
	VIDEOINFO_InvalidUploadStream
	VIDEOINFO_InvalidFileName
	VIDEOINFO_InvalidCoverFormat
	VIDEOINFO_EmptyUploadFile
	VIDEOINFO_UploadForbidden
	VIDEOINFO_CoverUploadStage
	VIDEOINFO_FileUploadStage
	VIDEOINFO_CoverRequired
	VIDEOINFO_UploadStatusNotFound
	VIDEOINFO_GetUploadStatusFailed
)

const RetryStr = "请稍后再试"

var ()

// 以下业务错误按独立区间分配，数值随接口传递给客户端。
const (
	InvalidBarrageQuery        = 5201 + iota // INVALID_BARRAGE_QUERY
	InvalidBarrageLike                       // INVALID_BARRAGE_LIKE
	InvalidBarrage                           // INVALID_BARRAGE
	BarrageNotFound                          // BARRAGE_NOT_FOUND
	BarrageLikeConflict                      // BARRAGE_LIKE_CONFLICT
	InvalidComment                           // INVALID_COMMENT
	CommentCountOverflow                     // COMMENT_COUNT_OVERFLOW
	InvalidRootComment                       // INVALID_ROOT_COMMENT
	CommentNotFound                          // COMMENT_NOT_FOUND
	InvalidCommentLike                       // INVALID_COMMENT_LIKE
	InvalidCommentQuery                      // INVALID_COMMENT_QUERY
	CommentRootChanged                       // COMMENT_ROOT_CHANGED
	InvalidCommentRoot                       // INVALID_COMMENT_ROOT
	CommentLikeConflict                      // COMMENT_LIKE_CONFLICT
	CommentSagaRemoved                       // COMMENT_SAGA_REMOVED
	FavoriteVersionConflict                  // FAVORITE_VERSION_CONFLICT
	InvalidFavorite                          // INVALID_FAVORITE
	FavoriteNotFound                         // FAVORITE_NOT_FOUND
	FavoriteRelationConflict                 // FAVORITE_RELATION_CONFLICT
	VideoAlreadyFavorited                    // VIDEO_ALREADY_FAVORITED
	FavoriteRecordChanged                    // FAVORITE_RECORD_CHANGED
	InvalidFavoritePage                      // INVALID_FAVORITE_PAGE
	FavoriteServiceUnavailable               // FAVORITE_SERVICE_UNAVAILABLE
	InvalidFavoriteMetadata                  // INVALID_FAVORITE_METADATA
	FavoriteCountConflict                    // FAVORITE_COUNT_CONFLICT
	InvalidInteractionIdentity               // INVALID_INTERACTION_IDENTITY
	InteractionNotSupported                  // INTERACTION_NOT_SUPPORTED
	InvalidInteractionStatus                 // INVALID_INTERACTION_STATUS
	InvalidShellsCount                       // INVALID_SHELLS_COUNT
	VideoChanged                             // VIDEO_CHANGED
	InvalidVideoVersion                      // INVALID_VIDEO_VERSION
	InteractionStateConflict                 // INTERACTION_STATE_CONFLICT
	PublishedVideoNotFound                   // VIDEO_NOT_FOUND
	VideoTitleRequired                       // VIDEO_TITLE_REQUIRED
)
