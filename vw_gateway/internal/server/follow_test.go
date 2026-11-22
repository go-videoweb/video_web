package server

import (
	"context"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/redis/go-redis/v9"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
	followv1 "vw_gateway/api/v1/user/follow"
	"vw_gateway/internal/biz/userbiz"
	"vw_gateway/internal/conf"
	"vw_gateway/internal/pkg/middlewares/auth"
	user "vw_gateway/internal/service/user_service"
)

type followHTTPRepo struct {
	userbiz.FollowRepo
	calls                    int
	userID, targetID, listID int64
	ids                      []int64
	name                     string
}

// record 记录网关转发的可信操作人和目标。
func (r *followHTTPRepo) record(userID, targetID, listID int64) {
	r.calls++
	r.userID = userID
	r.targetID = targetID
	r.listID = listID
}

// FollowOtherUser 模拟用户服务关注结果，不执行写入。
func (r *followHTTPRepo) FollowOtherUser(_ context.Context, userID, targetID, listID int64) (*followv1.FollowUserResp, error) {
	r.record(userID, targetID, listID)
	return &followv1.FollowUserResp{IsFollowing: true, FollowListId: 13}, nil
}

// UnfollowOtherUser 记录取消关注参数。
func (r *followHTTPRepo) UnfollowOtherUser(_ context.Context, userID, targetID int64) error {
	r.record(userID, targetID, 0)
	return nil
}

// GetFolloweeInfo 返回空分页用于绑定和数组编码验证。
func (r *followHTTPRepo) GetFolloweeInfo(_ context.Context, userID, listID int64, _, _ int32) (*followv1.GetFolloweeInfoResp, error) {
	r.record(userID, 0, listID)
	return &followv1.GetFolloweeInfoResp{FolloweeInfo: []*followv1.GetFolloweeInfoResp_UserSummary{}, PageNum: 1, PageSize: 20}, nil
}

// CreateFollowList 记录名称并返回超出 JS 安全整数范围的 ID。
func (r *followHTTPRepo) CreateFollowList(_ context.Context, userID int64, name string) (*followv1.CreateFollowListResp, error) {
	r.record(userID, 0, 0)
	r.name = name
	return &followv1.CreateFollowListResp{FollowListId: 9007199254740993}, nil
}

// DeleteFollowLists 保留整批输入，不由网关拆分。
func (r *followHTTPRepo) DeleteFollowLists(_ context.Context, userID int64, ids []int64) (*followv1.FollowListMutationResp, error) {
	r.record(userID, 0, 0)
	r.ids = ids
	return &followv1.FollowListMutationResp{Success: true}, nil
}

// GetFollowLists 返回本人唯一默认列表。
func (r *followHTTPRepo) GetFollowLists(_ context.Context, userID int64) (*followv1.GetFollowListsResp, error) {
	r.record(userID, 0, 0)
	return &followv1.GetFollowListsResp{Lists: []*followv1.FollowListInfo{{UserId: userID, FollowListId: 13, IsDefault: true}}}, nil
}

// ModifyFollowList 记录列表及名称。
func (r *followHTTPRepo) ModifyFollowList(_ context.Context, userID, listID int64, name string) (*followv1.FollowListMutationResp, error) {
	r.record(userID, 0, listID)
	r.name = name
	return &followv1.FollowListMutationResp{Success: true}, nil
}

// SetDefaultFollowList 记录原子切换的目标。
func (r *followHTTPRepo) SetDefaultFollowList(_ context.Context, userID, listID int64) (*followv1.FollowListMutationResp, error) {
	r.record(userID, 0, listID)
	return &followv1.FollowListMutationResp{Success: true}, nil
}

// GetFollowRelation 模拟确定的关系状态。
func (r *followHTTPRepo) GetFollowRelation(_ context.Context, userID, targetID int64) (*followv1.GetFollowRelationResp, error) {
	r.record(userID, targetID, 0)
	return &followv1.GetFollowRelationResp{IsFollowing: true, FollowListId: 13}, nil
}

// MoveFollowee 记录分组移动，不组合取消及新增请求。
func (r *followHTTPRepo) MoveFollowee(_ context.Context, userID, targetID, listID int64) (*followv1.FollowListMutationResp, error) {
	r.record(userID, targetID, listID)
	return &followv1.FollowListMutationResp{Success: true}, nil
}

// TestFollowHTTPOffline 验证真实路由强制鉴权、忽略伪造身份，且每个入口只转发一次请求。
func TestFollowHTTPOffline(t *testing.T) {
	const secret = "follow-http-offline-secret"
	access, refresh, err := (&auth.JWTAuth{Secret: secret, AccessExpireTime: time.Hour, RefreshExpireTime: 24 * time.Hour}).CreateToken(7, "owner", false)
	if err != nil {
		t.Fatal(err)
	}
	cache := redis.NewClusterClient(&redis.ClusterOptions{Addrs: []string{"127.0.0.1:1"}, MaxRetries: -1})
	cache.AddHook(&sessionStoreHook{value: access})
	t.Cleanup(func() { _ = cache.Close() })
	logger := log.NewStdLogger(io.Discard)
	repo := &followHTTPRepo{}
	svc := user.NewFollowService(logger, userbiz.NewFollowUsecase(repo, logger))
	srv := NewHTTPServer(&conf.Server{Http: &conf.Server_HTTP{}}, &conf.JWT{Secret: secret, AccessTokenExpiration: 1}, nil, nil, nil, svc, cache, nil, nil, nil, nil, nil, logger)
	cases := []struct {
		method, path, body, part string
		target, list             int64
	}{
		{"POST", "/api/v1/follow-lists", `{"user_id":"999","name":"技术"}`, `"9007199254740993"`, 0, 0},
		{"GET", "/api/v1/follow-lists?user_id=999", "", `"isDefault":true`, 0, 0},
		{"POST", "/api/v1/follow-lists/batch-delete", `{"id_list":["14","15"]}`, `"success":true`, 0, 0},
		{"PATCH", "/api/v1/follow-lists/14", `{"name":"新名称"}`, `"success":true`, 0, 14},
		{"PUT", "/api/v1/follow-lists/14/default", `{}`, `"success":true`, 0, 14},
		{"GET", "/api/v1/follow/relations/9", "", `"isFollowing":true`, 9, 0},
		{"PUT", "/api/v1/follow/relations/9/list", `{"follow_list_id":"14"}`, `"success":true`, 9, 14},
		{"POST", "/api/v1/999/follow", `{"follower_user_id":"9","follow_list_id":"13"}`, `"isFollowing":true`, 9, 13},
		{"PUT", "/api/v1/999/follow", `{"followed_user_id":"9"}`, `取消关注成功`, 9, 0},
		{"GET", "/api/v1/999/followList?follow_list_id=0", "", `"followeeInfo":[]`, 0, 0},
	}
	for _, tc := range cases {
		for _, signed := range []bool{false, true} {
			before := repo.calls
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			if signed {
				req.Header.Set("Authorization", "Bearer "+access)
				req.Header.Set("Refresh-Token", "Bearer "+refresh)
			}
			w := httptest.NewRecorder()
			srv.ServeHTTP(w, req)
			if !signed {
				assertMissingToken(t, w)
				if repo.calls != before {
					t.Fatal("匿名请求访问业务")
				}
				continue
			}
			if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), tc.part) {
				t.Fatalf("%s %s: %d %s", tc.method, tc.path, w.Code, w.Body.String())
			}
			if repo.calls != before+1 || repo.userID != 7 || repo.targetID != tc.target || repo.listID != tc.list {
				t.Fatalf("身份或目标被请求覆盖：%+v", repo)
			}
		}
	}
	if len(repo.ids) != 2 || repo.ids[0] != 14 || repo.ids[1] != 15 {
		t.Fatal("批量删除被拆分或丢失参数")
	}
}
