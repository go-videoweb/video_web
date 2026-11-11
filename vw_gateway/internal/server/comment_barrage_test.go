package server

import (
	"context"
	"encoding/json"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/redis/go-redis/v9"
	"google.golang.org/grpc"
	"google.golang.org/protobuf/types/known/emptypb"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	"vw_gateway/internal/biz/videobiz"
	"vw_gateway/internal/conf"
	"vw_gateway/internal/data/videodata"
	"vw_gateway/internal/pkg/middlewares/auth"
	video "vw_gateway/internal/service/video_service"
	comments "vw_video/api/v1/comment"
	interact "vw_video/api/v1/interact"
)

type commentHTTPClient struct {
	comments.VideoCommentClient
	t     *testing.T
	calls int
}

// GetCommentList 验证当前用户、视频、热度排序及游标的真实 HTTP 绑定。
func (c *commentHTTPClient) GetCommentList(_ context.Context, req *comments.GetCommentListReq, _ ...grpc.CallOption) (*comments.GetCommentListResp, error) {
	c.calls++
	if req.UserId != 7 || req.VideoId != 9 || req.GetCursor().GetCommentId() != 9007199254740993 || req.SortBy != "cnt_upvote" {
		c.t.Fatalf("评论绑定错误：%v", req)
	}
	if req.Cursor.VideoId != 9 || req.Cursor.RootId != 0 || req.Cursor.SortBy != "cnt_upvote" || req.Cursor.Order != "desc" || req.Cursor.UpvoteCount != 3 {
		c.t.Fatalf("评论游标字段未完整绑定：%v", req.Cursor)
	}
	return &comments.GetCommentListResp{Comments: []*comments.CommentInfo{{CommentId: 9007199254740993, IsUpvoted: true}}, HasMore: true, NextCursor: req.Cursor}, nil
}

// GetCommentReplies 验证平铺回复端点不丢弃视频范围、引用及后续页信息。
func (c *commentHTTPClient) GetCommentReplies(_ context.Context, req *comments.GetCommentRepliesReq, _ ...grpc.CallOption) (*comments.GetCommentRepliesResp, error) {
	c.calls++
	if req.UserId != 7 || req.VideoId != 9 || req.CommentId != 11 {
		c.t.Fatalf("回复绑定错误：%v", req)
	}
	if req.Cursor == nil || req.Cursor.RootId != 11 || req.Cursor.CreatedAt == nil || req.Cursor.CreatedAt.AsTime().Nanosecond() != 123000000 {
		c.t.Fatalf("时间排序游标绑定错误：%v", req.Cursor)
	}
	return &comments.GetCommentRepliesResp{Replies: []*comments.CommentInfo{{CommentId: 12, ParentId: 13, Reference: &comments.CommentReference{CommentId: 13, Content: "完整引用"}}}, HasMore: true, NextCursor: req.Cursor}, nil
}

// UpvoteComment 验证 false 是显式取消，而非缺少操作或反向切换。
func (c *commentHTTPClient) UpvoteComment(_ context.Context, req *comments.UpvoteCommentReq, _ ...grpc.CallOption) (*comments.UpvoteCommentResp, error) {
	c.calls++
	if req.UserId != 7 || req.VideoId != 9 || req.CommentId != 11 || req.Liked == nil || req.GetLiked() {
		c.t.Fatalf("取消绑定错误：%v", req)
	}
	return &comments.UpvoteCommentResp{IsUpvoted: false, UpvoteCount: 2}, nil
}

type barrageHTTPClient struct {
	interact.VideoInteractClient
	t     *testing.T
	calls int
}

// VideoSendBarrage 验证零秒位置以整数传递，发布者来自鉴权而不是请求字段。
func (c *barrageHTTPClient) VideoSendBarrage(_ context.Context, req *interact.VideoSendBarrageReq, _ ...grpc.CallOption) (*emptypb.Empty, error) {
	c.calls++
	if req.PublisherId != 7 || req.VideoId != 9 || req.PositionSecond != 0 || req.Content != "弹幕" || req.Color != "#ffffff" {
		c.t.Fatalf("发送参数错误：%v", req)
	}
	return &emptypb.Empty{}, nil
}

// GetBarrages 验证非负零秒、区间终点和可信身份，返回大 ID。
func (c *barrageHTTPClient) GetBarrages(_ context.Context, req *interact.GetBarragesReq, _ ...grpc.CallOption) (*interact.GetBarragesResp, error) {
	c.calls++
	if req.UserId != 7 || req.VideoId != 9 || req.StartSecond != 0 || req.EndSecond != 60 {
		c.t.Fatalf("弹幕绑定错误：%v", req)
	}
	if req.Cursor != nil && (req.Cursor.VideoId != 9 || req.Cursor.StartSecond != 0 || req.Cursor.EndSecond != 60 || req.Cursor.PositionSecond != 0 || req.Cursor.BarrageId != 9007199254740993) {
		c.t.Fatalf("零秒弹幕游标未完整绑定：%v", req.Cursor)
	}
	return &interact.GetBarragesResp{Barrages: []*interact.BarrageInfo{{BarrageId: 9007199254740993, PositionSecond: 0, IsUpvoted: true}}, HasMore: req.Cursor != nil, NextCursor: req.Cursor}, nil
}

// SetBarrageLike 验证取消目标及弹幕归属参数均由网关传递。
func (c *barrageHTTPClient) SetBarrageLike(_ context.Context, req *interact.SetBarrageLikeReq, _ ...grpc.CallOption) (*interact.SetBarrageLikeResp, error) {
	c.calls++
	if req.UserId != 7 || req.VideoId != 9 || req.BarrageId != 11 || req.Liked == nil || req.GetLiked() {
		c.t.Fatalf("弹幕取消绑定错误：%v", req)
	}
	return &interact.SetBarrageLikeResp{UpvoteCount: 2}, nil
}

// TestCommentBarrageHTTP 验证路由、JSON false、大 ID、引用和新端点的登录要求。
func TestCommentBarrageHTTP(t *testing.T) {
	const secret = "social-http-offline-secret"
	access, refresh, err := (&auth.JWTAuth{Secret: secret, AccessExpireTime: time.Hour, RefreshExpireTime: 24 * time.Hour}).CreateToken(7, "viewer", false)
	if err != nil {
		t.Fatal(err)
	}
	cache := redis.NewClusterClient(&redis.ClusterOptions{Addrs: []string{"127.0.0.1:1"}, MaxRetries: -1})
	cache.AddHook(&sessionStoreHook{value: access})
	t.Cleanup(func() { _ = cache.Close() })
	logger := log.NewStdLogger(io.Discard)
	cc, bc := &commentHTTPClient{t: t}, &barrageHTTPClient{t: t}
	d, cleanup, err := videodata.NewData(logger, nil, nil, bc, cc, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(cleanup)
	cs := video.NewVideoCommentService(videobiz.NewVideoCommentUsecase(videodata.NewVideoCommentRepo(d, logger), logger), logger)
	bs := video.NewInteractService(videobiz.NewInteractUsecase(videodata.NewInteractRepo(d, logger), nil), logger)
	srv := NewHTTPServer(&conf.Server{Http: &conf.Server_HTTP{}}, &conf.JWT{Secret: secret, AccessTokenExpiration: 1}, nil, nil, nil, nil, cache, nil, nil, nil, bs, cs, logger)
	for _, tc := range []struct {
		name, method, path, body string
		errorCode, calls         int
		expect                   string
	}{
		{"主评论", http.MethodGet, "/api/v1/video/9/comment?cursor.video_id=9&cursor.root_id=0&cursor.sort_by=cnt_upvote&cursor.order=desc&cursor.comment_id=9007199254740993&cursor.upvote_count=3&sort_by=cnt_upvote&user_id=999", "", 0, 1, "9007199254740993"},
		{"主评论驼峰参数", http.MethodGet, "/api/v1/video/9/comment?cursor.videoId=9&cursor.rootId=0&cursor.sortBy=cnt_upvote&cursor.order=desc&cursor.commentId=9007199254740993&cursor.upvoteCount=3&sortBy=cnt_upvote", "", 0, 1, "9007199254740993"},
		{"平铺回复", http.MethodGet, "/api/v1/video/9/comments/11/replies?user_id=999&cursor.video_id=9&cursor.root_id=11&cursor.sort_by=created_at&cursor.order=asc&cursor.comment_id=12&cursor.created_at=2026-11-01T12:00:00.123Z", "", 0, 1, "完整引用"},
		{"取消评论点赞", http.MethodPut, "/api/v1/video/9/comment/11/upvote", `{"liked":false,"userId":"999"}`, 0, 1, "upvoteCount"},
		{"缺少评论操作", http.MethodPut, "/api/v1/video/9/comment/11/upvote", `{}`, 400, 0, ""},
		{"弹幕分段", http.MethodGet, "/api/v1/video/9/barrages?start_second=0&end_second=60&user_id=999", "", 0, 1, "9007199254740993"},
		{"弹幕结构化游标", http.MethodGet, "/api/v1/video/9/barrages?start_second=0&end_second=60&cursor.video_id=9&cursor.start_second=0&cursor.end_second=60&cursor.position_second=0&cursor.barrage_id=9007199254740993", "", 0, 1, "9007199254740993"},
		{"无效游标ID", http.MethodGet, "/api/v1/video/9/comment?cursor.comment_id=invalid", "", 400, 0, ""},
		{"无效游标时间", http.MethodGet, "/api/v1/video/9/comments/11/replies?cursor.created_at=invalid", "", 400, 0, ""},
		{"零秒弹幕", http.MethodPost, "/api/v1/video/interaction/9/barrage/send", `{"positionSecond":0,"content":"弹幕","color":"#ffffff","publisherId":"999"}`, 0, 1, "发送弹幕成功"},
		{"取消弹幕点赞", http.MethodPut, "/api/v1/video/interaction/9/barrage/upvote", `{"barrageId":"11","liked":false}`, 0, 1, "upvoteCount"},
		{"缺少弹幕操作", http.MethodPut, "/api/v1/video/interaction/9/barrage/upvote", `{"barrageId":"11"}`, 400, 0, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := cc.calls + bc.calls
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Authorization", "Bearer "+access)
			req.Header.Set("Refresh-Token", "Bearer "+refresh)
			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)
			if w.Code != http.StatusOK || cc.calls+bc.calls-before != tc.calls {
				t.Fatalf("HTTP 结果错误：%d %s", w.Code, w.Body.String())
			}
			if tc.expect != "" && !strings.Contains(w.Body.String(), tc.expect) {
				t.Fatalf("响应缺少 %q：%s", tc.expect, w.Body.String())
			}
			if tc.errorCode != 0 {
				assertHTTPErrorCode(t, w, tc.errorCode)
			} else {
				var body map[string]any
				if err = json.Unmarshal(w.Body.Bytes(), &body); err != nil {
					t.Fatal(err)
				}
				if tc.name == "取消评论点赞" || tc.name == "取消弹幕点赞" {
					if body["isUpvoted"] != false {
						t.Fatal("false 状态未明确返回")
					}
				}
				if strings.HasPrefix(tc.name, "主评论") || tc.name == "弹幕结构化游标" {
					cursor, ok := body["nextCursor"].(map[string]any)
					if !ok {
						t.Fatalf("nextCursor 不是对象：%s", w.Body.String())
					}
					idField := "commentId"
					if tc.name == "弹幕结构化游标" {
						idField = "barrageId"
					}
					if cursor[idField] != "9007199254740993" {
						t.Fatalf("游标大 ID 未以字符串返回：%v", cursor)
					}
				}
				if tc.name == "弹幕分段" && body["nextCursor"] != nil {
					t.Fatal("末页必须返回 null 游标")
				}
			}
			before = cc.calls + bc.calls
			unsigned := httptest.NewRecorder()
			// 绑定发生在鉴权中间件之前；游客检查使用合法路径，避免畸形游标先触发 400。
			unsignedReq := httptest.NewRequest(tc.method, strings.SplitN(tc.path, "?", 2)[0], strings.NewReader(tc.body))
			unsignedReq.Header.Set("Content-Type", "application/json")
			srv.ServeHTTP(unsigned, unsignedReq)
			if cc.calls+bc.calls != before {
				t.Fatal("游客调用了下游")
			}
			assertMissingToken(t, unsigned)
		})
	}
}
