package biz

import (
	"context"
	"database/sql"
	"github.com/dtm-labs/dtm/client/dtmcli"
	"github.com/dtm-labs/dtm/client/dtmgrpc"
	_ "github.com/dtm-labs/dtmdriver-kratos"
	"github.com/go-kratos/kratos/v2/log"
	"google.golang.org/grpc/codes"
	"strconv"
	"util/dbutil/mysqlutil"
	"util/helper"
	interactv1 "vw_video/api/v1/interact"
	"vw_video/internal/data/dal/model"
	"vw_video/internal/pkg/ecode/errdef"
)

// InteractRepo 定义视频互动、个人状态读取及 DTM SQL 分支使用的仓储边界。
type InteractRepo interface {
	/* Used for dtm */
	GetSqlTx() *sql.Tx

	/* MySQL */
	GetPublisherIdByVideoId(ctx context.Context, videoID int64) (int64, error)
	AddVideoUpvote(ctx context.Context, tx *sql.Tx, videoID int64) error
	DecrementVideoUpvote(ctx context.Context, tx *sql.Tx, videoID int64) error

	// GetFavoriteMetadata 查询用户域元信息，由用例校验归属。
	GetFavoriteMetadata(ctx context.Context, favoriteID int64) (*FavoriteMetadata, error)
	// GetFavoriteRelation 普通读取关系，无记录返回 nil，不添加显式行锁。
	GetFavoriteRelation(ctx context.Context, userID, videoID int64) (*model.FavoriteVideo, error)
	// CreateFavoriteRelation 保存用例生成的 ID，不忽略唯一键冲突。
	CreateFavoriteRelation(ctx context.Context, relation *model.FavoriteVideo) error
	// DeleteFavoriteRelation 精确删除指定代关系，实际删除必须为一行。
	DeleteFavoriteRelation(ctx context.Context, relation *model.FavoriteVideo) error
	// UpdateFavoriteCount 调整收藏数和热度，乐观更新失败返回可识别的版本冲突。
	UpdateFavoriteCount(ctx context.Context, videoID, delta int64) error
	// SetFavoriteStatus 局部 upsert 收藏状态，不覆盖其他个人数据。
	SetFavoriteStatus(ctx context.Context, userID, videoID int64, favorite bool) error

	UpdateVideoShells(ctx context.Context, tx *sql.Tx, videoID int64, countShells int32) error
	ShareVideo(ctx context.Context, videoID int64) string
	AddVideoCntBarrage(ctx context.Context, videoID int64) error
	AddBarrage(ctx context.Context, into *interactv1.VideoSendBarrageReq) error
	AddVideoBarrageUpvote(ctx context.Context, tx *sql.Tx, barrageID int64) error
	DecrementVideoBarrageUpvote(ctx context.Context, tx *sql.Tx, barrageID int64) error

	/* 个人互动状态 */
	// GetUserVideoStatus 返回既有 RPC 位掩码及已投贝壳数量，收藏位来源于有效关系。
	GetUserVideoStatus(ctx context.Context, userID, videoID int64) (status int64, shellsCount int32, err error)
	// GetUserBarrageStatus 在个人弹幕状态设计确定前返回未支持错误。
	GetUserBarrageStatus(ctx context.Context, userID int64, barrageID int64) (int64, error)
	// ApplyUserVideoStatus 在传入的 SQL 屏障事务内校验旧值并更新目标字段，不自行提交。
	ApplyUserVideoStatus(ctx context.Context, tx *sql.Tx, userID, videoID int64, patch *VideoStatusPatch) error
}

type InteractUsecase struct {
	repo          InteractRepo
	videoinfoRepo VideoInfoRepo
	tx            mysqlutil.Transaction
	log           *log.Helper
}

func NewInteractUseCase(repo InteractRepo, infoRepo VideoInfoRepo, tx mysqlutil.Transaction, logger log.Logger) *InteractUsecase {
	return &InteractUsecase{
		repo:          repo,
		videoinfoRepo: infoRepo,
		tx:            tx,
		log:           log.NewHelper(logger),
	}
}

func (u *InteractUsecase) UpvoteVideo(ctx context.Context, videoID int64, upvote int32) error {
	u.log.Info("DTM-UpvoteVideo start...")
	defer u.log.Info("DTM-UpvoteVideo end...")

	tx := u.repo.GetSqlTx()
	barrier, err := dtmgrpc.BarrierFromGrpc(ctx)
	if err != nil {
		return helper.HandleGrpcError(codes.Aborted, dtmcli.ResultFailure, err)
	}

	err = barrier.Call(tx, func(tx *sql.Tx) error {
		if upvote == 1 {
			return u.repo.AddVideoUpvote(ctx, tx, videoID)
		} else {
			return u.repo.DecrementVideoUpvote(ctx, tx, videoID)
		}
	})
	if err != nil {
		return helper.HandleGrpcError(codes.Aborted, dtmcli.ResultFailure, err)
	}
	return nil
}

func (u *InteractUsecase) UpdateVideoShells(ctx context.Context, videoID int64, countShells int32) error {
	barrier, err := dtmgrpc.BarrierFromGrpc(ctx)
	if err != nil {
		return helper.HandleGrpcError(codes.Aborted, dtmcli.ResultFailure, err)
	}

	err = barrier.Call(u.repo.GetSqlTx(), func(tx *sql.Tx) error {
		// update video's shells(and video's hot)
		err := u.repo.UpdateVideoShells(ctx, tx, videoID, countShells)
		if err != nil {
			return err
		}
		return nil
	})

	if err != nil {
		return helper.HandleGrpcError(codes.Aborted, dtmcli.ResultFailure, err)
	}
	return nil
}

func (u *InteractUsecase) ShareVideo(ctx context.Context, videoID int64) (string, error) {
	return "/api/v1/video/" + strconv.FormatInt(videoID, 10) + "/info", nil
}

func (u *InteractUsecase) SendBarrage(ctx context.Context, info *interactv1.VideoSendBarrageReq) error {
	//tx := u.repo.GetSqlTx()
	//barrier, err := dtmgrpc.BarrierFromGrpc(ctx)
	//if err != nil {
	//	return helper.HandleGrpcError(codes.Aborted, dtmcli.ResultFailure, err)
	//}
	//err = barrier.Call(tx, func(tx *sql.Tx) error {
	//	// 1. insert barrage into barrages table
	//	err := u.repo.AddBarrage(ctx, tx, info)
	//	if err != nil {
	//		return err
	//	}
	//
	//	// 2. add video's cnt_barrage
	//	err = u.repo.AddVideoCntBarrage(ctx, tx, info.VideoId)
	//	if err != nil {
	//		return err
	//	}
	//	return nil
	//})
	//if err != nil {
	//	return helper.HandleGrpcError(codes.Aborted, dtmcli.ResultFailure, err)
	//}
	//return nil
	err := u.tx.WithTx(ctx, func(ctx context.Context) error {
		// 1. insert barrage into barrages table
		err := u.repo.AddBarrage(ctx, info)
		if err != nil {
			return err
		}

		// 2. add video's cnt_barrage
		err = u.repo.AddVideoCntBarrage(ctx, info.VideoId)
		if err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return helper.HandleError(errdef.ErrSendBarrageFailed, err)
	}
	return nil
}

// UpvoteBarrage 在个人弹幕状态设计确定前拒绝点赞及取消操作。
// 输入：保留现有 RPC 参数；输出：固定返回未支持错误，不开启事务或修改计数。
func (u *InteractUsecase) UpvoteBarrage(ctx context.Context, barrageID int64, upvote int32) error {
	return ErrInteractionNotSupported
}

// GetUserVideoStatus 查询用户与视频的个人互动状态，不初始化状态记录。
// 输入：userID、videoID 须为正数；输出：RPC 位掩码和已投贝壳数量，查询失败返回 -1/-1 及原错误。
func (u *InteractUsecase) GetUserVideoStatus(ctx context.Context, userID, videoID int64) (int64, int32, error) {
	status, shellsCount, err := u.repo.GetUserVideoStatus(ctx, userID, videoID)
	if err != nil {
		return -1, -1, err
	}
	return status, shellsCount, nil
}

// GetUserBarrageStatus 在个人弹幕状态设计确定前拒绝读取。
// 输入：保留原用户/弹幕 ID；输出：0 和未支持错误，不访问仓储。
func (u *InteractUsecase) GetUserBarrageStatus(ctx context.Context, userID, barrageID int64) (int64, error) {
	return 0, ErrInteractionNotSupported
}

// SetUserVideoStatus 在 SQL 子事务屏障内更新本次涉及的字段。
// 输入：ctx 须包含 DTM 分支元数据，身份参数与 req 一致；req 提供原状态和目标操作。
// 输出：参数错误直接拒绝，事务失败返回 Saga 分支失败；成功不改变其他状态或观看时间。
func (u *InteractUsecase) SetUserVideoStatus(ctx context.Context, userID, videoID int64, req *interactv1.SetUserVideoStatusReq) error {
	return u.applyUserVideoStatus(ctx, userID, videoID, req, false)
}

// SetUserVideoStatusRevert 只补偿本次字段，不覆盖观看时间或其他互动。
// 输入：使用原正向请求和 DTM 补偿分支上下文；输出：成功或分支错误，空补偿由屏障过滤。
func (u *InteractUsecase) SetUserVideoStatusRevert(ctx context.Context, userID, videoID int64, req *interactv1.SetUserVideoStatusReq) error {
	return u.applyUserVideoStatus(ctx, userID, videoID, req, true)
}

// applyUserVideoStatus 将局部状态变更交给 SQL 屏障，业务写入复用屏障提供的事务。
// 输入：revert 控制是否交换本次字段的旧值和新值；请求及分支元数据须有效。
// 输出：验证或执行错误；验证失败不创建事务，重复/空补偿/悬挂请求由屏障处理。
func (u *InteractUsecase) applyUserVideoStatus(ctx context.Context, userID, videoID int64, req *interactv1.SetUserVideoStatusReq, revert bool) error {
	patch, err := videoStatusPatch(req, revert)
	if err != nil {
		return err
	}
	barrier, err := dtmgrpc.BarrierFromGrpc(ctx)
	if err != nil {
		return helper.HandleGrpcError(codes.Aborted, dtmcli.ResultFailure, err)
	}
	err = barrier.Call(u.repo.GetSqlTx(), func(tx *sql.Tx) error {
		return u.repo.ApplyUserVideoStatus(ctx, tx, userID, videoID, patch)
	})
	if err != nil {
		return helper.HandleGrpcError(codes.Aborted, dtmcli.ResultFailure, err)
	}
	return nil
}

// SetUserBarrageStatus 在弹幕个人状态设计确定前拒绝执行。
// 输入：保留原 RPC 身份和操作参数；输出：未支持错误，不写状态或修改计数。
func (u *InteractUsecase) SetUserBarrageStatus(ctx context.Context, userID, barrageID int64, req *interactv1.SetUserBarrageStatusReq) error {
	return ErrInteractionNotSupported
}

// SetUserBarrageStatusRevert 在弹幕个人状态设计确定前拒绝执行。
// 输入：保留原补偿请求；输出：未支持错误，不创建新的状态表或事务。
func (u *InteractUsecase) SetUserBarrageStatusRevert(ctx context.Context, userID, barrageID int64, req *interactv1.SetUserBarrageStatusReq) error {
	return ErrInteractionNotSupported
}

func setStatus(originStatus *int64, status int64, statusSwitch int32) {
	if statusSwitch == 1 {
		*originStatus |= status // set status
	} else if statusSwitch == -1 {
		*originStatus &= ^status // clear status
	}
}
