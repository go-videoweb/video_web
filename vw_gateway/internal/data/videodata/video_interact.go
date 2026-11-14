package videodata

import (
	"context"
	stderr "errors"
	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"util/resolver"
	interactv1 "vw_gateway/api/v1/video/video_interact"
	"vw_gateway/internal/biz/videobiz"
	userinfoGRPC "vw_user/api/v1/userinfo"
	interactGRPC "vw_video/api/v1/interact"
	videoinfoGRPC "vw_video/api/v1/videoinfo"
)

type interactRepo struct {
	data   *Data
	logger *log.Helper
}

func NewInteractRepo(data *Data, logger log.Logger) videobiz.InteractRepo {
	return &interactRepo{
		data:   data,
		logger: log.NewHelper(logger),
	}
}

// FavoriteVideo 通过一次视频 RPC 转发收藏操作，不调用用户服务、不读取状态、不提交 Saga。
// 输入：鉴权身份及 HTTP 请求；输出：视频服务提交后的结果，RPC 失败原样返回。
func (i *interactRepo) FavoriteVideo(ctx context.Context, userId int64, req *interactv1.VideoFavoriteReq) (*interactv1.VideoFavoriteResp, error) {
	resp, err := i.data.videoInteractClient.SetVideoFavorite(ctx, &interactGRPC.SetVideoFavoriteReq{
		UserId: userId, VideoId: req.GetVideoId(), FavoriteId: req.GetFavoriteId(), Favorite: req.GetFavorite(), FavoriteRecordId: req.GetFavoriteRecordId(),
	})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, kerrors.InternalServer("INVALID_VIDEO_RESPONSE", "视频服务未返回收藏结果")
	}
	return &interactv1.VideoFavoriteResp{StatusCode: 200, Message: resp.Message, IsFavorited: resp.IsFavorited, FavoriteId: resp.FavoriteId, FavoriteRecordId: resp.FavoriteRecordId}, nil
}

func (i *interactRepo) UpvoteVideo(ctx context.Context, userId int64, videoId int64) (bool, error) {
	origin, err := i.data.videoInteractClient.GetUserVideoStatus(ctx, &interactGRPC.GetUserVideoStatusReq{
		VideoId: videoId,
		UserId:  userId,
	})
	if err != nil {
		return false, err
	}

	// Get the video publisher's id.
	resp, err := i.data.videoInfoClient.GetPublisherIdByVideoId(ctx, &videoinfoGRPC.GetPublisherIdByVideoIdReq{
		VideoId: videoId,
	})

	videoAddr, err := resolver.GetRandomAddr(ctx, videoService)
	if err != nil {
		return false, err
	}

	userAddr, err := resolver.GetRandomAddr(ctx, userService)
	if err != nil {
		return false, err
	}
	gid, saga := newSaga(ctx, i.data.dtmServerAddr)
	i.logger.Infof("[UpvoteVideo] gid: %s", gid)

	isUpvoted := checkIsUpvoted(origin.Status)
	wrapSagaAdd(
		saga,
		// 1. Add the first step of the saga, which is to upvote the video
		&sagaMember{
			method:           videoAddr + interactGRPC.VideoInteract_VideoUpvote_FullMethodName,
			compensateMethod: videoAddr + interactGRPC.VideoInteract_VideoUpvoteRevert_FullMethodName,
			payload: &interactGRPC.VideoUpvoteReq{
				VideoId:    videoId,
				UpvoteFlag: -isUpvoted, // reverse the status of Upvoted
			},
		},

		// 2. Add the second step of the saga, which is to update the publisher's like count
		&sagaMember{
			method:           userAddr + userinfoGRPC.Userinfo_UpdateUserCntLikes_FullMethodName,
			compensateMethod: userAddr + userinfoGRPC.Userinfo_UpdateUserCntLikesRevert_FullMethodName,
			payload: &userinfoGRPC.UpdateUserCntLikesReq{
				UserId:     resp.PublisherId,
				UpvoteFlag: -isUpvoted, // reverse the status of Upvoted
			},
		},

		// 3. Add the third step of the saga, which is to update the user-video status
		&sagaMember{
			method:           videoAddr + interactGRPC.VideoInteract_SetUserVideoStatus_FullMethodName,
			compensateMethod: videoAddr + interactGRPC.VideoInteract_SetUserVideoStatusRevert_FullMethodName,
			payload: &interactGRPC.SetUserVideoStatusReq{
				VideoId:      videoId,
				UserId:       userId,
				OriginStatus: origin.Status,
				Statuses: &interactGRPC.SetUserVideoStatusReq_Statuses{
					IsUpvoted: -isUpvoted,
				},
			},
		},
	)
	return isUpvoted == 1, saga.Submit()
}

// UpvoteBarrage 单次调用视频本地事务，网关不维护点赞关系或计数。
func (i *interactRepo) UpvoteBarrage(ctx context.Context, userId int64, req *interactv1.VideoUpvoteBarrageReq) (*interactv1.VideoUpvoteBarrageResp, error) {
	resp, err := i.data.videoInteractClient.SetBarrageLike(ctx, &interactGRPC.SetBarrageLikeReq{UserId: userId, VideoId: req.VideoId, BarrageId: req.BarrageId, Liked: req.Liked})
	if err != nil {
		return nil, err
	}
	if resp == nil {
		return nil, kerrors.InternalServer("INVALID_VIDEO_RESPONSE", "视频服务未返回点赞结果")
	}
	return &interactv1.VideoUpvoteBarrageResp{StatusCode: 200, Message: "操作成功", IsUpvoted: resp.IsUpvoted, UpvoteCount: resp.UpvoteCount}, nil
}

func (i *interactRepo) SendBarrage(ctx context.Context, userId int64, req *interactv1.VideoSendBarrageReq) error {
	_, err := i.data.videoInteractClient.VideoSendBarrage(ctx, &interactGRPC.VideoSendBarrageReq{
		PublisherId:    userId,
		VideoId:        req.VideoId,
		Content:        req.Content,
		PositionSecond: req.PositionSecond,
		Color:          req.Color,
	})
	return err
}

// RecordVideoShare 将可信身份和视频 ID 交给视频服务，等待分享事务提交。
func (i *interactRepo) RecordVideoShare(ctx context.Context, userId, videoId int64) error {
	_, err := i.data.videoInteractClient.RecordVideoShare(ctx, &interactGRPC.RecordVideoShareReq{
		UserId: userId, VideoId: videoId,
	})
	return err
}

// ThrowShells 读取原个人状态后编排用户余额、视频计数和个人状态的 Saga。
// 输入：用户/视频 ID 和本次投递数量；输出：查询、发现或 Saga 执行错误，累计数量超过 2 时拒绝。
// 状态分支携带变更前数量供补偿使用；此方法不会直接写数据库，也不保证跨 GID 的并发串行化。
func (i *interactRepo) ThrowShells(ctx context.Context, userId int64, videoId int64, count int32) error {
	origin, err := i.data.videoInteractClient.GetUserVideoStatus(ctx, &interactGRPC.GetUserVideoStatusReq{
		VideoId: videoId,
		UserId:  userId,
	})
	if err != nil {
		return err
	}
	if origin.ShellsCount+count > 2 {
		return stderr.New("贝壳投递数量已达上限，最多可投递两个☺️")
	}

	resp, err := i.data.videoInfoClient.GetPublisherIdByVideoId(ctx, &videoinfoGRPC.GetPublisherIdByVideoIdReq{
		VideoId: videoId,
	})
	if err != nil {
		return err
	}

	videoAddr, err := resolver.GetRandomAddr(ctx, videoService)
	if err != nil {
		return err
	}

	userAddr, err := resolver.GetRandomAddr(ctx, userService)
	if err != nil {
		return err
	}

	gid, saga := newSaga(ctx, i.data.dtmServerAddr)
	i.logger.Infof("[ThrowShells] gid: %s", gid)

	wrapSagaAdd(
		saga,

		// 1. Update shells of the thrower and the publisher
		&sagaMember{
			method:           userAddr + userinfoGRPC.Userinfo_UpdateUserShells_FullMethodName,
			compensateMethod: userAddr + userinfoGRPC.Userinfo_UpdateUserShellsRevert_FullMethodName,
			payload: &userinfoGRPC.UpdateUserShellsReq{
				UserId:      userId,
				PublisherId: resp.PublisherId,
				Shells:      int64(count),
			},
		},

		// 2. Update the shells the video received
		&sagaMember{
			method:           videoAddr + interactGRPC.VideoInteract_VideoThrowShells_FullMethodName,
			compensateMethod: videoAddr + interactGRPC.VideoInteract_VideoThrowShellsRevert_FullMethodName,
			payload: &interactGRPC.VideoThrowShellsReq{
				VideoId: videoId,
				UserId:  userId,
				Shells:  count,
			},
		},

		// 3. Update the user-video status
		&sagaMember{
			method:           videoAddr + interactGRPC.VideoInteract_SetUserVideoStatus_FullMethodName,
			compensateMethod: videoAddr + interactGRPC.VideoInteract_SetUserVideoStatusRevert_FullMethodName,
			payload: &interactGRPC.SetUserVideoStatusReq{
				VideoId:           videoId,
				UserId:            userId,
				OriginStatus:      origin.Status,
				ShellsCount:       origin.ShellsCount + count,
				OriginShellsCount: origin.ShellsCount,
				Statuses: &interactGRPC.SetUserVideoStatusReq_Statuses{
					IsThrownShells: 1,
				},
			}},
	)
	return saga.Submit()
}
