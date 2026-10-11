package server

import (
	"context"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/go-kratos/kratos/v2/middleware/logging"
	"github.com/go-kratos/kratos/v2/middleware/recovery"
	"github.com/go-kratos/kratos/v2/middleware/selector"
	"github.com/go-kratos/kratos/v2/middleware/tracing"
	"github.com/go-kratos/kratos/v2/transport/http"
	"github.com/gorilla/handlers"
	"github.com/redis/go-redis/v9"
	"time"
	"vw_gateway/api/v1/user/captcha"
	"vw_gateway/api/v1/user/favorites"
	"vw_gateway/api/v1/user/follow"
	"vw_gateway/api/v1/user/identity"
	"vw_gateway/api/v1/user/userfile"
	"vw_gateway/api/v1/user/userinfo"
	videocommentv1 "vw_gateway/api/v1/video/video_comment"
	videointeractv1 "vw_gateway/api/v1/video/video_interact"
	videoinfov1 "vw_gateway/api/v1/video/videoinfo"
	"vw_gateway/internal/conf"
	"vw_gateway/internal/pkg/codecs"
	"vw_gateway/internal/pkg/middlewares/auth"
	"vw_gateway/internal/service/user_service"
	"vw_gateway/internal/service/video_service"
)

// NewWhitelistMatcher 创建决定是否执行 JwtAuth 的匹配器。
// 输入：返回的匹配函数接收请求上下文和 Kratos operation（/包名.服务名/方法名），不按 HTTP URL 匹配。
// 输出：白名单接口返回 false，完全跳过鉴权；其他接口返回 true，执行 JwtAuth。
// 示例：登录接口返回 false，视频详情接口返回 true，后者是否允许游客由匿名访问匹配器决定。
func NewWhitelistMatcher() selector.MatchFunc {
	whiteList := make(map[string]struct{})

	whiteList["/gateway.api.v1.user.id.Identity/Register"] = struct{}{}
	whiteList["/gateway.api.v1.user.id.Identity/Login"] = struct{}{}
	whiteList["/gateway.api.v1.user.id.Identity/Logout"] = struct{}{}
	whiteList["/gateway.api.v1.user.captcha.Captcha/GetImageCaptcha"] = struct{}{}
	whiteList["/gateway.api.v1.user.captcha.Captcha/GetCodeCaptcha"] = struct{}{}
	whiteList["/gateway.api.v1.user.file.FileService/UploadAvatar"] = struct{}{}
	return func(ctx context.Context, operation string) bool {
		if _, ok := whiteList[operation]; ok {
			return false
		}
		return true
	}
}

// NewAnonymousAllowedMatcher 创建允许凭证缺失或失效时按游客访问的匹配器。
// 输入：返回的匹配函数接收请求上下文和 Kratos operation，不按 HTTP URL 匹配。
// 输出：目前仅视频详情接口返回 true，其他接口返回 false。
// 命中后仍执行 JwtAuth：有效凭证保留用户身份，凭证缺失或失效按游客放行，基础设施故障仍返回错误。
func NewAnonymousAllowedMatcher() selector.MatchFunc {
	return func(_ context.Context, operation string) bool {
		return operation == videoinfov1.OperationVideoInfoGetVideoInfo
	}
}

// NewHTTPServer 创建网关 HTTP 服务，注册业务路由、分级鉴权和统一编解码。
// 输入：c、jwt 为服务及鉴权配置，redis 提供登录态缓存，各业务服务提供路由实现，logger 用于请求日志。
// 输出：已完成配置和路由注册的 HTTP 服务，不在本函数中启动监听。
// 鉴权先由跳过白名单决定是否执行 JwtAuth，再由匿名访问匹配器决定凭证失效时能否按游客访问。
func NewHTTPServer(
	c *conf.Server,
	jwt *conf.JWT,
	captcha *user.CaptchaService,
	file *user.FileService,
	identity *user.IdentityService,
	follow *user.FollowService,
	redis *redis.ClusterClient,
	info *user.InfoService,
	favorites *user.FavoritesService,
	videoInfo *video.InfoService,
	videoInteract *video.InteractService,
	videoComment *video.CommentService,
	logger log.Logger) *http.Server {
	var opts = []http.ServerOption{
		http.Middleware(
			recovery.Recovery(),
			tracing.Server(),
			logging.Server(logger),
			selector.Server(
				auth.JwtAuth(jwt.Secret, time.Duration(jwt.AccessTokenExpiration)*time.Hour, redis, NewAnonymousAllowedMatcher()),
			).Match(NewWhitelistMatcher()).Build(),
		),
		// 跨域
		http.Filter(handlers.CORS(
			//据AllowedHeaders方法介绍，如果accepting Content-Type除了
			//application/x-www-form-urlencoded, multipart/form-data, 或 text/plain之外还有其他类型，
			//则需要在AllowedHeaders中显式声明“Content-Type”。
			//由于Content-Type可能还会有application/octet-stream等类型，所以这里需要显式声明。
			handlers.AllowedHeaders([]string{"Authorization", "Refresh-Token", "Content-Type"}), // 增加刷新token以及Content-Type的跨域支持.
			handlers.AllowedMethods([]string{"GET", "POST", "PUT", "HEAD", "PATCH", "OPTIONS", "DELETE"}),
			handlers.AllowedOrigins([]string{"*"}),
		)),
		http.RequestDecoder(codecs.RequestDecoder),
		http.ErrorEncoder(codecs.ErrorEncoder),
		http.ResponseEncoder(codecs.ResponseEncoder),
	}
	if c.Http.Network != "" {
		opts = append(opts, http.Network(c.Http.Network))
	}
	if c.Http.Addr != "" {
		opts = append(opts, http.Address(c.Http.Addr))
	}
	if c.Http.Timeout != nil {
		opts = append(opts, http.Timeout(c.Http.Timeout.AsDuration()))
	}
	srv := http.NewServer(opts...)
	idv1.RegisterIdentityHTTPServer(srv, identity)
	captv1.RegisterCaptchaHTTPServer(srv, captcha)
	filev1.RegisterFileServiceHTTPServer(srv, file)
	infov1.RegisterUserinfoHTTPServer(srv, info)
	favorv1.RegisterFavoriteHTTPServer(srv, favorites)
	followv1.RegisterFollowHTTPServer(srv, follow)
	videoinfov1.RegisterVideoInfoHTTPServer(srv, videoInfo)
	videointeractv1.RegisterVideoInteractHTTPServer(srv, videoInteract)
	videocommentv1.RegisterVideoCommentHTTPServer(srv, videoComment)
	return srv
}
