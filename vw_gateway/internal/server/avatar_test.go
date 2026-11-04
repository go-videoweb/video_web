package server

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	kerrors "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/redis/go-redis/v9"
	filev1 "vw_gateway/api/v1/user/userfile"
	"vw_gateway/internal/biz/userbiz"
	"vw_gateway/internal/conf"
	"vw_gateway/internal/pkg/middlewares/auth"
	user "vw_gateway/internal/service/user_service"
)

// avatarHTTPRepo 模拟用户服务返回图片，测试关注 HTTP、鉴权和缓存而不访问磁盘。
type avatarHTTPRepo struct {
	data        []byte // 当前图片内容。
	updatedUser int64  // 更新时使用的鉴权用户 ID。
	updates     int    // 成功转发更新次数。
}

// GetAvatar 只暴露指定测试用户头像，不存在的用户返回明确 404。
func (r *avatarHTTPRepo) GetAvatar(_ context.Context, id int64) (*filev1.AvatarResp, error) {
	if id != 9007199254740993 {
		return nil, kerrors.NotFound("USER_NOT_FOUND", "用户不存在")
	}
	return &filev1.AvatarResp{Content: r.data, ContentType: "image/png"}, nil
}

// UpdateAvatar 保存上传内容及实际操作人，调用者不可通过表单 user_id 冒充。
func (r *avatarHTTPRepo) UpdateAvatar(_ context.Context, id int64, header *multipart.FileHeader) error {
	input, err := header.Open()
	if err != nil {
		return err
	}
	defer input.Close()
	r.data, err = io.ReadAll(input)
	r.updatedUser, r.updates = id, r.updates+1
	return err
}

// avatarRegisterRepo 模拟注册和登录态缓存，缓存结果供真实 JwtAuth 中间件读取。
type avatarRegisterRepo struct {
	userbiz.UserIdentityRepo                   // 本测试不调用其他身份操作。
	store                    *sessionStoreHook // 离线 Redis 登录态。
	failCache                bool              // 模拟账号已创建但缓存服务不可用。
}

// Register 返回稳定的大用户 ID，不依赖头像上传分配身份。
func (r *avatarRegisterRepo) Register(context.Context, *userbiz.RegisterInfo) (int64, bool, error) {
	return 9007199254740993, false, nil
}

// CacheAccessToken 使注册结果立即成为有效会话，失败时不伪造缓存成功。
func (r *avatarRegisterRepo) CacheAccessToken(_ context.Context, _, token string, _ time.Duration) error {
	if r.failCache {
		return errors.New("session cache unavailable")
	}
	r.store.value = token
	return nil
}

// avatarMultipart 构造头像表单，并注入伪造用户 ID 验证后端不会信任它。
func avatarMultipart(t *testing.T, data []byte, withFile bool) (*bytes.Buffer, string) {
	t.Helper()
	body := new(bytes.Buffer)
	writer := multipart.NewWriter(body)
	if err := writer.WriteField("user_id", "99"); err != nil {
		t.Fatal(err)
	}
	if withFile {
		part, err := writer.CreateFormFile("avatar", "../../untrusted.png")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := part.Write(data); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return body, writer.FormDataContentType()
}

// TestRegisterThenUpdateAvatarHTTP 验证注册双 token 可直接更新、公开读取二进制、缓存重验和旧入口移除。
func TestRegisterThenUpdateAvatarHTTP(t *testing.T) {
	logger := log.NewStdLogger(io.Discard)
	store := &sessionStoreHook{}
	cache := redis.NewClusterClient(&redis.ClusterOptions{Addrs: []string{"127.0.0.1:1"}, MaxRetries: -1})
	cache.AddHook(store)
	t.Cleanup(func() { _ = cache.Close() })
	identityRepo := &avatarRegisterRepo{store: store}
	const secret = "avatar-http-only-secret"
	identity := user.NewUserIdentityService(userbiz.NewUserIdentityUsecase(identityRepo, &auth.JWTAuth{
		Secret: secret, AccessExpireTime: time.Hour, RefreshExpireTime: 24 * time.Hour,
	}, logger), logger)
	repo := &avatarHTTPRepo{data: []byte("default-image")}
	avatar := user.NewUserFileService(userbiz.NewUserFileUsecase(repo, logger), logger)
	server := NewHTTPServer(&conf.Server{Http: &conf.Server_HTTP{}}, &conf.JWT{Secret: secret, AccessTokenExpiration: 1},
		nil, avatar, identity, nil, cache, nil, nil, nil, nil, nil, logger)

	request := httptest.NewRequest(http.MethodPost, "/api/v1/user/register", strings.NewReader(`{"username":"new-user","birthday":"2001-01-01"}`))
	request.Header.Set("Content-Type", "application/json")
	registration := httptest.NewRecorder()
	server.ServeHTTP(registration, request)
	if registration.Code != 200 {
		t.Fatalf("注册失败: %s", registration.Body.String())
	}
	var response struct {
		Data struct {
			UserID  string `json:"userId"`       // 注册服务生成的用户 ID。
			Access  string `json:"accessToken"`  // 已缓存的 access token。
			Refresh string `json:"refreshToken"` // 配套 refresh token。
		} `json:"data"` // 注册响应数据。
	}
	if err := json.Unmarshal(registration.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Data.UserID != "9007199254740993" || response.Data.Access == "" || response.Data.Access != store.value || response.Data.Refresh == "" {
		t.Fatal("注册身份或登录态不完整")
	}
	url := "/api/v1/users/9007199254740993/avatar"
	first := httptest.NewRecorder()
	server.ServeHTTP(first, httptest.NewRequest(http.MethodGet, url, nil))
	if first.Code != 200 || first.Body.String() != "default-image" || first.Header().Get("Content-Type") != "image/png" {
		t.Fatalf("公开二进制读取失败: %d %s", first.Code, first.Body.String())
	}
	etag := first.Header().Get("ETag")
	if etag == "" || first.Header().Get("Cache-Control") != "no-cache" || first.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("头像缓存或安全头缺失")
	}
	conditional := httptest.NewRequest(http.MethodGet, url, nil)
	conditional.Header.Set("If-None-Match", etag)
	unchanged := httptest.NewRecorder()
	server.ServeHTTP(unchanged, conditional)
	if unchanged.Code != http.StatusNotModified || unchanged.Body.Len() != 0 {
		t.Fatal("未变化头像没有返回 304")
	}

	for _, scenario := range []string{"unauthenticated", "no file", "empty", "oversize", "body limit", "success"} {
		t.Run(scenario, func(t *testing.T) {
			data := []byte("replacement-image")
			switch scenario {
			case "empty":
				data = nil
			case "oversize":
				data = make([]byte, (5<<20)+1)
			case "body limit":
				data = make([]byte, 6<<20)
			case "success":
				// HTTP 层使用仓储替身，验证满 5 MiB 的 multipart 内容能完整透传。
				data = bytes.Repeat([]byte{1}, 5<<20)
			}
			body, contentType := avatarMultipart(t, data, scenario != "no file")
			request := httptest.NewRequest(http.MethodPut, "/api/v1/user/profile/avatar", body)
			request.Header.Set("Content-Type", contentType)
			if scenario != "unauthenticated" {
				request.Header.Set("Authorization", "Bearer "+response.Data.Access)
				request.Header.Set("Refresh-Token", "Bearer "+response.Data.Refresh)
			}
			writer := httptest.NewRecorder()
			server.ServeHTTP(writer, request)
			if scenario == "unauthenticated" {
				// 项目既有鉴权错误使用业务码，HTTP 状态可能为 200；按统一协议断言拒绝。
				assertMissingToken(t, writer)
				if repo.updates != 0 {
					t.Fatal("未登录更新调用了下游")
				}
				return
			}
			if scenario == "success" {
				if writer.Code != 200 || repo.updatedUser != 9007199254740993 || repo.updates != 1 {
					t.Fatalf("可信更新失败: %d %s", writer.Code, writer.Body.String())
				}
				if !strings.Contains(writer.Body.String(), url) {
					t.Fatal("更新响应缺少可访问地址")
				}
			} else {
				if writer.Code < 400 || repo.updates != 0 {
					t.Fatalf("无效上传调用了下游: %d %s", writer.Code, writer.Body.String())
				}
				if (scenario == "oversize" || scenario == "body limit") && writer.Code != 413 {
					t.Fatalf("大小限制错误: %d %s", writer.Code, writer.Body.String())
				}
			}
		})
	}
	changedReq := httptest.NewRequest(http.MethodGet, url, nil)
	changedReq.Header.Set("If-None-Match", etag)
	changed := httptest.NewRecorder()
	server.ServeHTTP(changed, changedReq)
	if changed.Code != 200 || changed.Body.Len() != 5<<20 || !bytes.Equal(changed.Body.Bytes(), repo.data) || changed.Header().Get("ETag") == etag {
		t.Fatal("替换后仍返回旧缓存")
	}
	for _, path := range []string{"/api/v1/users/0/avatar", "/api/v1/users/99/avatar"} {
		writer := httptest.NewRecorder()
		server.ServeHTTP(writer, httptest.NewRequest(http.MethodGet, path, nil))
		if writer.Code < 400 {
			t.Fatal("无效或不存在的用户头像返回成功")
		}
	}
	removed := httptest.NewRecorder()
	server.ServeHTTP(removed, httptest.NewRequest(http.MethodPost, "/api/v1/user/profile/avatar", nil))
	if removed.Code != 405 && removed.Code != 404 {
		t.Fatal("匿名 UploadAvatar 入口仍存在")
	}
	identityRepo.failCache = true
	failed := httptest.NewRecorder()
	retry := httptest.NewRequest(http.MethodPost, "/api/v1/user/register", strings.NewReader(`{"username":"another","birthday":"2001-01-01"}`))
	retry.Header.Set("Content-Type", "application/json")
	server.ServeHTTP(failed, retry)
	if failed.Code != 500 || !strings.Contains(failed.Body.String(), "REGISTRATION_SESSION_FAILED") || !strings.Contains(failed.Body.String(), "user_created") {
		t.Fatalf("部分成功未明确告知: %s", failed.Body.String())
	}
}
