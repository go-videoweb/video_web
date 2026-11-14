package videodata

import (
	"context"
	"errors"
	"github.com/go-kratos/kratos/v2/log"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/proto"
	"google.golang.org/protobuf/types/known/timestamppb"
	"io"
	"testing"
	"time"
	apiComment "vw_gateway/api/v1/video/video_comment"
	apiInteract "vw_gateway/api/v1/video/video_interact"
	"vw_gateway/internal/biz/videobiz"
	video "vw_gateway/internal/service/video_service"
	rpcComment "vw_video/api/v1/comment"
	rpcInteract "vw_video/api/v1/interact"
)

type socialCommentClient struct {
	rpcComment.VideoCommentClient
	lastLike    *rpcComment.UpvoteCommentReq
	lastList    *rpcComment.GetCommentListReq
	lastReplies *rpcComment.GetCommentRepliesReq
	calls       int
	err         error
}

// TestStructuredCursorMapping 验证所有游标字段双向映射，nil 和显式空对象不会混淆。
func TestStructuredCursorMapping(t *testing.T) {
	comment := &rpcComment.CommentCursor{
		VideoId:     9,
		RootId:      11,
		SortBy:      "created_at",
		Order:       "asc",
		CommentId:   9007199254740993,
		CreatedAt:   timestamppb.New(time.Date(2026, 11, 1, 12, 0, 0, 123000000, time.UTC)),
		UpvoteCount: 37,
	}
	if got := commentCursorToRPC(commentCursorFromRPC(comment)); !proto.Equal(got, comment) {
		t.Fatalf("评论游标丢失字段：%v", got)
	}
	barrage := &rpcInteract.BarrageCursor{
		VideoId: 9, StartSecond: 0, EndSecond: 60, PositionSecond: 0, BarrageId: 9007199254740993,
	}
	if got := barrageCursorToRPC(barrageCursorFromRPC(barrage)); !proto.Equal(got, barrage) {
		t.Fatalf("弹幕游标丢失零秒或大 ID：%v", got)
	}
	if commentCursorToRPC(nil) != nil || commentCursorFromRPC(nil) != nil {
		t.Fatal("评论 nil 游标被转为空对象")
	}
	if barrageCursorToRPC(nil) != nil || barrageCursorFromRPC(nil) != nil {
		t.Fatal("弹幕 nil 游标被转为空对象")
	}
	if commentCursorToRPC(&apiComment.CommentCursor{}) == nil || barrageCursorToRPC(&apiInteract.BarrageCursor{}) == nil {
		t.Fatal("显式空游标被错误地当成首批")
	}
}

// UpvoteComment 仅接受单次明确操作，其他未实现客户端调用会导致测试失败。
func (c *socialCommentClient) UpvoteComment(_ context.Context, req *rpcComment.UpvoteCommentReq, _ ...grpc.CallOption) (*rpcComment.UpvoteCommentResp, error) {
	c.lastLike = req
	c.calls++
	return &rpcComment.UpvoteCommentResp{IsUpvoted: req.GetLiked(), UpvoteCount: 3}, c.err
}

// GetCommentList 返回带一层完整引用的样例，验证网关不截断正文。
func (c *socialCommentClient) GetCommentList(_ context.Context, req *rpcComment.GetCommentListReq, _ ...grpc.CallOption) (*rpcComment.GetCommentListResp, error) {
	c.lastList = req
	return &rpcComment.GetCommentListResp{Comments: []*rpcComment.CommentInfo{{CommentId: 9007199254740993, IsUpvoted: true, ParentId: 8, Reference: &rpcComment.CommentReference{CommentId: 8, Content: "完整引用正文"}}}, NextCursor: req.Cursor, HasMore: true, TotalCount: 99}, c.err
}

// GetCommentReplies 记录视频、用户及楼层，验证游标响应不被 service 丢弃。
func (c *socialCommentClient) GetCommentReplies(_ context.Context, req *rpcComment.GetCommentRepliesReq, _ ...grpc.CallOption) (*rpcComment.GetCommentRepliesResp, error) {
	c.lastReplies = req
	return &rpcComment.GetCommentRepliesResp{Replies: []*rpcComment.CommentInfo{}, NextCursor: req.Cursor, HasMore: true}, c.err
}

type socialBarrageClient struct {
	rpcInteract.VideoInteractClient
	lastLike *rpcInteract.SetBarrageLikeReq
	lastRead *rpcInteract.GetBarragesReq
	lastSend *rpcInteract.VideoSendBarrageReq
	calls    int
	err      error
}

// SetBarrageLike 模拟本地事务结果，不提供状态预读或 DTM 客户端。
func (c *socialBarrageClient) SetBarrageLike(_ context.Context, req *rpcInteract.SetBarrageLikeReq, _ ...grpc.CallOption) (*rpcInteract.SetBarrageLikeResp, error) {
	c.lastLike = req
	c.calls++
	return &rpcInteract.SetBarrageLikeResp{IsUpvoted: req.GetLiked(), UpvoteCount: 2}, c.err
}

// GetBarrages 返回带个人状态和整数秒位置的分段弹幕。
func (c *socialBarrageClient) GetBarrages(_ context.Context, req *rpcInteract.GetBarragesReq, _ ...grpc.CallOption) (*rpcInteract.GetBarragesResp, error) {
	c.lastRead = req
	return &rpcInteract.GetBarragesResp{Barrages: []*rpcInteract.BarrageInfo{{BarrageId: 9007199254740993, PositionSecond: 0, IsUpvoted: true}}, NextCursor: req.Cursor, HasMore: true}, c.err
}

// TestSocialGatewayIdentityAndForwarding 验证身份来源、明确取消、范围和 RPC 错误原样传播。
func TestSocialGatewayIdentityAndForwarding(t *testing.T) {
	logger := log.NewStdLogger(io.Discard)
	cc := &socialCommentClient{}
	bc := &socialBarrageClient{}
	d := &Data{videoCommentClient: cc, videoInteractClient: bc}
	cs := video.NewVideoCommentService(videobiz.NewVideoCommentUsecase(NewVideoCommentRepo(d, logger), logger), logger)
	bs := video.NewInteractService(videobiz.NewInteractUsecase(NewInteractRepo(d, logger), nil), logger)
	ctx := context.WithValue(context.Background(), "user_id", int64(7))
	liked := false
	reply, err := cs.UpvoteComment(ctx, &apiComment.UpvoteCommentReq{VideoId: 9, CommentId: 11, Liked: &liked})
	if err != nil || reply.IsUpvoted || reply.UpvoteCount != 3 || cc.calls != 1 || cc.lastLike.UserId != 7 || cc.lastLike.VideoId != 9 || cc.lastLike.Liked == nil {
		t.Fatalf("评论写入透传错误：%v", err)
	}
	br, err := bs.VideoUpvoteBarrage(ctx, &apiInteract.VideoUpvoteBarrageReq{VideoId: 9, BarrageId: 11, Liked: &liked})
	if err != nil || br.IsUpvoted || bc.calls != 1 || bc.lastLike.UserId != 7 || bc.lastLike.VideoId != 9 {
		t.Fatalf("弹幕写入透传错误：%v", err)
	}
	cursor := &apiComment.CommentCursor{VideoId: 9, SortBy: "cnt_upvote", Order: "desc", CommentId: 9007199254740993, UpvoteCount: 3}
	list, err := cs.GetCommentList(ctx, &apiComment.GetCommentListReq{VideoId: 9, Cursor: cursor, SortBy: "cnt_upvote"})
	if err != nil || cc.lastList.UserId != 7 || cc.lastList.GetCursor().GetCommentId() != cursor.CommentId || !list.HasMore || list.GetNextCursor().GetCommentId() != cursor.CommentId || !list.Comments[0].IsUpvoted || list.Comments[0].Reference.Content != "完整引用正文" {
		t.Fatal("评论读取丢失状态、引用或游标")
	}
	replies, err := cs.GetCommentReplies(ctx, &apiComment.GetCommentRepliesReq{VideoId: 9, CommentId: 11, Cursor: cursor})
	if err != nil || cc.lastReplies.VideoId != 9 || cc.lastReplies.UserId != 7 || !replies.HasMore || replies.GetNextCursor().GetCommentId() != cursor.CommentId || replies.Replies == nil {
		t.Fatal("回复范围或分页丢失")
	}
	barrages, err := bs.GetBarrages(ctx, &apiInteract.GetBarragesReq{VideoId: 9, StartSecond: 0, EndSecond: 60, Cursor: &apiInteract.BarrageCursor{VideoId: 9, EndSecond: 60, BarrageId: 9007199254740993}})
	if err != nil || bc.lastRead.UserId != 7 || bc.lastRead.EndSecond != 60 || !barrages.HasMore || !barrages.Barrages[0].IsUpvoted {
		t.Fatal("弹幕分段查询错误")
	}
	for _, actor := range []any{nil, int64(0), int64(-1), "7"} {
		bad := context.Background()
		if actor != nil {
			bad = context.WithValue(bad, "user_id", actor)
		}
		if _, err = cs.GetCommentList(bad, &apiComment.GetCommentListReq{}); err == nil {
			t.Fatal("无效身份到达下游")
		}
		if _, err = bs.GetBarrages(bad, &apiInteract.GetBarragesReq{}); err == nil {
			t.Fatal("无效身份到达下游")
		}
	}
	if _, err = cs.UpvoteComment(ctx, &apiComment.UpvoteCommentReq{}); err == nil || cc.calls != 1 {
		t.Fatal("缺少目标仍调用 RPC")
	}
	fault := errors.New("rpc failure")
	cc.err = fault
	bc.err = fault
	if _, err = cs.GetCommentList(ctx, &apiComment.GetCommentListReq{VideoId: 9}); !errors.Is(err, fault) {
		t.Fatal("RPC 错误丢失")
	}
	if _, err = bs.GetBarrages(ctx, &apiInteract.GetBarragesReq{VideoId: 9, EndSecond: 60}); !errors.Is(err, fault) {
		t.Fatal("RPC 错误丢失")
	}
}
