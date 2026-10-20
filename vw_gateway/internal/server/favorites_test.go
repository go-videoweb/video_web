package server

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/redis/go-redis/v9"
	favorv1 "vw_gateway/api/v1/user/favorites"
	"vw_gateway/internal/biz/userbiz"
	"vw_gateway/internal/conf"
	"vw_gateway/internal/domain"
	"vw_gateway/internal/pkg/middlewares/auth"
	user "vw_gateway/internal/service/user_service"
)

type favoritesHTTPRepo struct {
	userbiz.FavoritesRepo
	userID      int64
	createCalls int
	listCalls   int
}

func (r *favoritesHTTPRepo) CreateFavorites(_ context.Context, userId int64, name string, privacy int32, description string) (int64, error) {
	r.userID = userId
	r.createCalls++
	return 9007199254740993, nil
}

func (r *favoritesHTTPRepo) GetFavoritesList(_ context.Context, userId int64) ([]*domain.FavoriteDetail, error) {
	r.userID = userId
	r.listCalls++
	return []*domain.FavoriteDetail{{FavoriteId: 9007199254740993, FavoriteName: "私密收藏夹", Description: "说明", IsPrivate: true}}, nil
}

// TestFavoritesHTTPRouting 验证真实 GET 列表和 POST 创建路由均要求双 token，忽略伪造身份，并保持大 ID JSON 精度。
func TestFavoritesHTTPRouting(t *testing.T) {
	const secret = "favorites-http-test-secret"
	access, refresh, err := (&auth.JWTAuth{Secret: secret, AccessExpireTime: time.Hour, RefreshExpireTime: 24 * time.Hour}).CreateToken(7, "owner", false)
	if err != nil {
		t.Fatal(err)
	}
	hook := &sessionStoreHook{value: access}
	cache := redis.NewClusterClient(&redis.ClusterOptions{Addrs: []string{"127.0.0.1:1"}, MaxRetries: -1})
	cache.AddHook(hook)
	t.Cleanup(func() { _ = cache.Close() })
	logger := log.NewStdLogger(io.Discard)
	repo := &favoritesHTTPRepo{}
	svc := user.NewFavoritesService(userbiz.NewFavoritesUsecase(repo, logger), logger)
	srv := NewHTTPServer(&conf.Server{Http: &conf.Server_HTTP{}}, &conf.JWT{Secret: secret, AccessTokenExpiration: 1}, nil, nil, nil, nil, cache, nil, svc, nil, nil, nil, logger)

	for _, op := range []string{favorv1.OperationFavoriteCreateFavorites, favorv1.OperationFavoriteGetFavoritesList} {
		if !NewWhitelistMatcher()(context.Background(), op) || NewAnonymousAllowedMatcher()(context.Background(), op) {
			t.Fatalf("收藏夹路由不应允许匿名访问：%s", op)
		}
	}
	for _, authorized := range []bool{false, true} {
		for _, tc := range []struct{ method, path, body string }{
			{http.MethodGet, "/api/v1/favorites/list?user_id=999", ""},
			{http.MethodPost, "/api/v1/favorites", `{"userId":"999","favoritesName":"收藏夹","isPrivate":-1}`},
		} {
			req := httptest.NewRequest(tc.method, tc.path, strings.NewReader(tc.body))
			req.Header.Set("Content-Type", "application/json")
			if authorized {
				req.Header.Set("Authorization", "Bearer "+access)
				req.Header.Set("Refresh-Token", "Bearer "+refresh)
			}
			writer := httptest.NewRecorder()
			srv.ServeHTTP(writer, req)
			if !authorized {
				assertMissingToken(t, writer)
				if repo.createCalls != 0 || repo.listCalls != 0 {
					t.Fatal("匿名请求触发业务调用")
				}
				continue
			}
			if writer.Code != http.StatusOK || repo.userID != 7 {
				t.Fatalf("路由或身份隔离失败：%d %s", writer.Code, writer.Body.String())
			}
			var body struct {
				FavoriteID   string `json:"favoriteId"`
				UserID       string `json:"userId"`
				FavoriteList []struct {
					FavoriteID   string `json:"favoriteId"`
					FavoriteName string `json:"favoriteName"`
					Description  string `json:"description"`
					IsPrivate    bool   `json:"isPrivate"`
				} `json:"favoriteList"`
			}
			if err := json.Unmarshal(writer.Body.Bytes(), &body); err != nil {
				t.Fatal(err)
			}
			if tc.method == http.MethodPost {
				if body.FavoriteID != "9007199254740993" {
					t.Fatal("创建响应缺少准确的收藏夹 ID")
				}
			} else if body.UserID != "7" || len(body.FavoriteList) != 1 || body.FavoriteList[0].FavoriteID != "9007199254740993" || body.FavoriteList[0].FavoriteName != "私密收藏夹" || body.FavoriteList[0].Description != "说明" || !body.FavoriteList[0].IsPrivate {
				t.Fatalf("列表 JSON 字段错误：%s", writer.Body.String())
			}
		}
	}
	if repo.createCalls != 1 || repo.listCalls != 1 {
		t.Fatal("HTTP 请求未按对应接口调用一次")
	}
}
