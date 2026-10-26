package videodata

import (
	"context"
	stderr "errors"
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

// FavoriteVideo 将可信身份传入现有收藏分支，不调整 Saga 编排、补偿或提交方式。
func (i *interactRepo) FavoriteVideo(ctx context.Context, userId, favoriteId, videoId int64) (bool, error) {
	// 1. Get the current status of the user-video
	origin, err := i.data.videoInteractClient.GetUserVideoStatus(ctx, &interactGRPC.GetUserVideoStatusReq{
		VideoId: videoId,
		UserId:  userId,
	})
	if err != nil {
		return false, err
	}

	gid, saga := newSaga(ctx, i.data.dtmServerAddr)
	i.logger.Infof("[FavoriteVideo] gid: %s", gid)

	// 2. Get the address of the video service. format --> ip:port
	addr, err := resolver.GetRandomAddr(ctx, videoService)
	if err != nil {
		return false, err
	}

	// 3.1 Add the first step of the saga, which is to favorite the video
	isFavorited := checkIsFavorited(origin.Status)
	wrapSagaAdd(
		saga,
		&sagaMember{
			method:           addr + interactGRPC.VideoInteract_VideoFavorite_FullMethodName,
			compensateMethod: addr + interactGRPC.VideoInteract_VideoFavoriteRevert_FullMethodName,
			payload: &interactGRPC.VideoFavoriteReq{
				UserId:     userId,
				VideoId:    videoId,
				FavoriteId: favoriteId,
				Favorite:   -isFavorited,
			},
		},
		// 3.2 Update User-Video status
		&sagaMember{
			addr + interactGRPC.VideoInteract_SetUserVideoStatus_FullMethodName,
			addr + interactGRPC.VideoInteract_SetUserVideoStatusRevert_FullMethodName,
			&interactGRPC.SetUserVideoStatusReq{
				VideoId:      videoId,
				UserId:       userId,
				OriginStatus: origin.Status,
				Statuses: &interactGRPC.SetUserVideoStatusReq_Statuses{
					IsFavorited: -isFavorited,
				},
			},
		},
	)
	return isFavorited == 1, saga.Submit()
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

// UpvoteBarrage 在创建 Saga 前拒绝尚未启用的弹幕点赞。
func (i *interactRepo) UpvoteBarrage(ctx context.Context, userId, barrageId int64) error {
	return interactionNotSupported()
}

func (i *interactRepo) SendBarrage(ctx context.Context, userId int64, req *interactv1.VideoSendBarrageReq) error {
	_, err := i.data.videoInteractClient.VideoSendBarrage(ctx, &interactGRPC.VideoSendBarrageReq{
		PublisherId: userId,
		VideoId:     req.VideoId,
		Content:     req.Content,
		Time:        req.Time,
		Color:       req.Color,
	})
	return err
}

func (i *interactRepo) ShareVideo(ctx context.Context, userId int64, videoId int64) (string, error) {
	// 1. Get the current status of the user-video
	origin, err := i.data.videoInteractClient.GetUserVideoStatus(ctx, &interactGRPC.GetUserVideoStatusReq{
		VideoId: videoId,
		UserId:  userId,
	})
	if err != nil {
		return "", err
	}

	videoAddr, err := resolver.GetRandomAddr(ctx, videoService)
	if err != nil {
		return "", err
	}

	resp, err := i.data.videoInteractClient.VideoShare(ctx, &interactGRPC.VideoShareReq{
		VideoId:  videoId,
		IsShared: checkIsShared(origin.Status),
	})
	if err != nil {
		return "", err
	}
	uri := resp.Uri

	// TODO: 目前先写死返回的url，后续需要改成根据实际情况返回
	url := "http://127.0.0.1:8080" + uri

	isShared := checkIsShared(origin.Status)

	// If isShared == -1, it means the video is not shared by this user before,
	// so we need to add the saga to update the hot count and set the user-video status
	if isShared == -1 {
		gid, saga := newSaga(ctx, i.data.dtmServerAddr)
		i.logger.Infof("[ShareVideo] gid: %s", gid)
		wrapSagaAdd(
			saga,
			&sagaMember{
				videoAddr + videoinfoGRPC.VideoInfo_AddVideoCntShared_FullMethodName,
				videoAddr + videoinfoGRPC.VideoInfo_AddVideoCntSharedRevert_FullMethodName,
				&videoinfoGRPC.AddVideoCntSharedReq{
					VideoId:        videoId,
					IsCompensation: false,
				},
			},
			&sagaMember{
				videoAddr + interactGRPC.VideoInteract_SetUserVideoStatus_FullMethodName,
				videoAddr + interactGRPC.VideoInteract_SetUserVideoStatusRevert_FullMethodName,
				&interactGRPC.SetUserVideoStatusReq{
					UserId:       userId,
					VideoId:      videoId,
					OriginStatus: origin.Status,
					Statuses: &interactGRPC.SetUserVideoStatusReq_Statuses{
						IsShared: -isShared, // reverse the status of Shared
					},
				},
			},
		)
		err = saga.Submit()
		if err != nil {
			return "", err
		}
	}

	return url, nil
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
