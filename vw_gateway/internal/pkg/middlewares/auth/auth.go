package auth

import (
	"context"
	"errors"
	kerr "github.com/go-kratos/kratos/v2/errors"
	"github.com/go-kratos/kratos/v2/middleware"
	"github.com/go-kratos/kratos/v2/transport"
	"github.com/golang-jwt/jwt/v5"
	"github.com/redis/go-redis/v9"
	"strconv"
	"strings"
	"time"
	"util/helper"
	"vw_gateway/internal/conf"
	"vw_gateway/internal/pkg/ecode/errdef"
	"vw_gateway/internal/pkg/ecode/errdef/uerr"
)

// JWTAuth 保存 JWT 签发密钥及两种 token 的有效期。
type JWTAuth struct {
	Secret string

	AccessExpireTime  time.Duration
	RefreshExpireTime time.Duration
}

// NewJWTAuth 根据配置创建 JWT 签发器。
// 输入：cfg 为非 nil 的 JWT 配置，access token 有效期以小时配置，refresh token 以天配置。
// 输出：保存签发密钥及转换后有效期的签发器，不签发 token 或写入登录态缓存。
func NewJWTAuth(cfg *conf.JWT) *JWTAuth {
	return &JWTAuth{
		Secret:            cfg.Secret,
		AccessExpireTime:  time.Duration(cfg.AccessTokenExpiration) * time.Hour,
		RefreshExpireTime: time.Duration(cfg.RefreshTokenExpiration) * 24 * time.Hour,
	}
}

const (
	BearerTokenPrefix  = "Bearer"
	AccessTokenHeader  = "Authorization"
	RefreshTokenHeader = "Refresh-Token"

	// TokenIssuer 是签发和校验 JWT 时使用的签发者标识。
	TokenIssuer = "ZEY_HUNTER_ETL"
)

// AccessTokenClaims 保存 access token 中的用户身份及标准 JWT 声明。
type AccessTokenClaims struct {
	UserID   int64  `json:"user_id"`
	Username string `json:"username"`
	IsAdmin  bool   `json:"is_admin"`
	jwt.RegisteredClaims
}

// NewAccessTokenClaims 创建尚未填充身份和有效期的 access token 声明。
// 输出：独立的空声明，签发前需填充用户身份、签发者和过期时间。
func NewAccessTokenClaims() *AccessTokenClaims {
	return &AccessTokenClaims{}
}

// padding 填充用户身份和签发者，并从当前时间起计算有效期。
// 输入：userID、username、isAdmin 为用户身份；duration 为从当前时间起算的有效时长。
// 直接修改接收者的身份、签发者和过期时间，不生成 token 字符串。
func (t *AccessTokenClaims) padding(userID int64, username string, isAdmin bool, duration time.Duration) {
	t.UserID = userID
	t.Username = username
	t.IsAdmin = isAdmin
	t.Issuer = TokenIssuer
	t.ExpiresAt = jwt.NewNumericDate(time.Now().Add(duration))

}

// getTokenString 使用 HS256 签发 access token。
// 输入：secret 为签名密钥，接收者应已填充身份和有效期。
// 输出：成功时返回 token 字符串；签名失败时返回空字符串和 ErrCreateTokenFailed。
func (t *AccessTokenClaims) getTokenString(secret string) (tokenString string, err error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, t)
	tokenString, err = token.SignedString([]byte(secret))
	if err != nil {
		return "", uerr.ErrCreateTokenFailed
	}
	return
}

// RefreshTokenClaims 保存刷新凭证的标准 JWT 声明，不携带用户身份。
type RefreshTokenClaims struct {
	jwt.RegisteredClaims
}

// NewRefreshTokenClaims 创建尚未填充签发者和有效期的 refresh token 声明。
// 输出：独立的空声明，签发前需填充签发者和过期时间。
func NewRefreshTokenClaims() *RefreshTokenClaims {
	return &RefreshTokenClaims{}
}

// padding 填充签发者，并从当前时间起计算有效期。
// 输入：duration 为从当前时间起算的有效时长。
// 直接修改接收者的签发者和过期时间，不生成 token 字符串。
func (t *RefreshTokenClaims) padding(duration time.Duration) {
	t.Issuer = TokenIssuer
	t.ExpiresAt = jwt.NewNumericDate(time.Now().Add(duration))
}

// getTokenString 使用 HS256 签发 refresh token。
// 输入：secret 为签名密钥，接收者应已填充签发者和有效期。
// 输出：成功时返回 token 字符串；签名失败时返回空字符串和包装后的 ErrCreateTokenFailed。
func (t *RefreshTokenClaims) getTokenString(secret string) (tokenString string, err error) {
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, t)
	tokenString, err = token.SignedString([]byte(secret))
	if err != nil {
		return "", helper.HandleError(uerr.ErrCreateTokenFailed, err)
	}
	return
}

// CreateToken 为指定用户签发 access/refresh token。
// 输入：userID、username、isAdmin 为写入 access token 的用户身份；密钥和有效期取自签发器。
// 输出：成功时返回两个 token 和 nil；任一签发失败时返回两个空字符串和错误。
// 本函数不验证用户身份，也不写入登录态缓存，由调用方完成身份确认和缓存操作。
func (jwtAuthorizer *JWTAuth) CreateToken(userID int64, username string, isAdmin bool) (accessToken, refreshToken string, err error) {
	atokenClaims := NewAccessTokenClaims()
	atokenClaims.padding(userID, username, isAdmin, jwtAuthorizer.AccessExpireTime)
	accessToken, err = atokenClaims.getTokenString(jwtAuthorizer.Secret)
	if err != nil {
		return "", "", helper.HandleError(uerr.ErrCreateTokenFailed, err)
	}

	rtokenClaims := NewRefreshTokenClaims()
	rtokenClaims.padding(jwtAuthorizer.RefreshExpireTime)
	refreshToken, err = rtokenClaims.getTokenString(jwtAuthorizer.Secret)
	if err != nil {
		return "", "", helper.HandleError(uerr.ErrCreateTokenFailed, err)
	}
	return
}

// JwtAuth 创建统一处理强制鉴权和游客访问的 JWT 中间件。
// 输入：secret 为签名校验密钥；accessTokenExpireTime 为刷新后 access token 的有效时长；
// cache 为登录态缓存；allowAnonymous 按 Kratos operation 匹配接口，nil 表示所有经过本中间件的请求须登录。
// 输出：校验双 token 和 Redis 登录态的中间件，通过后将正用户 ID 写入 context 的 user_id。
// 允许匿名的接口在凭证缺失或失效时以 user_id=0 执行业务，不刷新 token；Redis 故障仍返回错误。
// 强制鉴权请求刷新成功后仍返回携带新 token 的过期错误，不继续执行业务；业务错误直接返回，不重试。
func JwtAuth(secret string, accessTokenExpireTime time.Duration, cache redis.Cmdable, allowAnonymous func(context.Context, string) bool) middleware.Middleware {
	return func(handler middleware.Handler) middleware.Handler {
		return func(ctx context.Context, req interface{}) (interface{}, error) {
			// 先清除可能继承的身份，避免游客请求携带未经本次鉴权确认的用户 ID。
			ctx = context.WithValue(ctx, "user_id", int64(0))
			tr, ok := transport.FromServerContext(ctx)
			if !ok {
				return nil, uerr.ErrTokenMissing
			}
			anonymousAllowed := allowAnonymous != nil && allowAnonymous(ctx, tr.Operation())
			session, err := authenticateSession(ctx, tr, secret, cache, !anonymousAllowed)
			if err != nil {
				if anonymousAllowed && isCredentialError(err) {
					return handler(ctx, req)
				}
				return nil, err
			}
			if session.accessExpired {
				return nil, refreshAccessToken(ctx, session, secret, accessTokenExpireTime, cache)
			}
			ctx = context.WithValue(ctx, "user_id", session.access.UserID)
			return handler(ctx, req)
		}
	}
}

// authenticatedSession 保存通过凭证和缓存校验的会话；accessExpired 为 true 时只能用于刷新。
type authenticatedSession struct {
	access        *AccessTokenClaims
	refresh       *RefreshTokenClaims
	accessExpired bool
}

// authenticateSession 校验双 token，并确认 access token 与 Redis 当前登录态完全一致。
// 输入：ctx 为请求上下文；tr 提供请求头；secret 为校验密钥；cache 为登录态缓存。
// allowExpiredAccess 为 true 时仅暂缓处理 access token 的过期错误，refresh token 仍须有效。
// 输出：通过校验的会话及 access token 是否过期；凭证或缓存校验失败时返回 nil 和错误。
// 本函数只读取缓存，不刷新 token；过期会话只能供调用方刷新，不能作为有效身份执行业务。
func authenticateSession(ctx context.Context, tr transport.Transporter, secret string, cache redis.Cmdable, allowExpiredAccess bool) (*authenticatedSession, error) {
	access, err := getToken(tr, secret, AccessTokenHeader)
	accessExpired := errors.Is(err, jwt.ErrTokenExpired)
	if err != nil && !accessExpired {
		return nil, err
	}
	if accessExpired && !allowExpiredAccess {
		return nil, uerr.ErrAccessTokenExpired
	}
	refresh, err := getToken(tr, secret, RefreshTokenHeader)
	if errors.Is(err, jwt.ErrTokenExpired) {
		return nil, uerr.ErrRefreshTokenExpired
	}
	if err != nil {
		return nil, err
	}
	aclaims, atok := access.Claims.(*AccessTokenClaims)
	rclaims, rtok := refresh.Claims.(*RefreshTokenClaims)
	if !atok || !rtok || aclaims.UserID <= 0 {
		return nil, uerr.ErrTokenInvalid
	}
	if cache == nil {
		return nil, helper.HandleError(errdef.INTERNAL_ERROR, errors.New("session store unavailable"))
	}
	cached, err := cache.Get(ctx, strconv.FormatInt(aclaims.UserID, 10)).Result()
	if errors.Is(err, redis.Nil) {
		return nil, uerr.ErrUserLoggedOut
	}
	if err != nil {
		return nil, helper.HandleError(errdef.INTERNAL_ERROR, err)
	}
	// 仅存在用户缓存键不足以证明登录态有效，还需拒绝已被替换的旧 token。
	if cached != access.Raw {
		return nil, uerr.ErrUserLoggedOut
	}
	return &authenticatedSession{access: aclaims, refresh: rclaims, accessExpired: accessExpired}, nil
}

// isCredentialError 判断鉴权阶段的错误是否属于凭证缺失或失效，不包含基础设施故障。
// 输入：err 为鉴权阶段返回的错误，不用于分类业务处理结果。
// 输出：凭证缺失、解析失败、过期或登录态失效时返回 true；其余错误及 nil 返回 false。
func isCredentialError(err error) bool {
	// HandleError 会扩展 Reason，不能依赖原错误的匹配结果，因此使用稳定的业务码分类。
	switch int32(kerr.Code(err)) {
	case uerr.ErrTokenMissing.Code, uerr.ErrTokenInvalid.Code, uerr.ErrAccessTokenExpired.Code,
		uerr.ErrParseTokenFailed.Code, uerr.ErrRefreshTokenExpired.Code, uerr.ErrUserLoggedOut.Code:
		return true
	default:
		return false
	}
}

// refreshAccessToken 为强制鉴权请求刷新 access token，并更新 Redis 登录态。
// 输入：session 须已通过凭证和缓存校验，且 refresh token 未过期；secret 为签发密钥，
// expiration 为新 access token 的有效时长；ctx 和 cache 用于更新登录态缓存。
// 输出：缓存成功后返回 ErrAccessTokenExpired，并通过 metadata.access_token 携带新 token；
// 签发失败、refresh token 到期或缓存写入失败时返回对应错误，不返回新 token。
// 缓存有效期取 refresh token 的剩余寿命；当前请求不继续执行业务，由客户端使用新 token 重试。
func refreshAccessToken(ctx context.Context, session *authenticatedSession, secret string, expiration time.Duration, cache redis.Cmdable) error {
	claims := NewAccessTokenClaims()
	claims.padding(session.access.UserID, session.access.Username, session.access.IsAdmin, expiration)
	access, err := claims.getTokenString(secret)
	if err != nil {
		return err
	}
	// 缓存与 refresh token 同时到期，既保留刷新窗口，也不延长原登录会话。
	ttl := time.Until(session.refresh.ExpiresAt.Time)
	if ttl <= 0 {
		return uerr.ErrRefreshTokenExpired
	}
	if err := cache.Set(ctx, strconv.FormatInt(claims.UserID, 10), access, ttl).Err(); err != nil {
		return helper.HandleError(errdef.INTERNAL_ERROR, err)
	}
	return helper.HandleError(uerr.ErrAccessTokenExpired.WithMetadata(map[string]string{"access_token": access}))
}

// getToken 读取 Bearer token，校验 HS256 签名、签发者和时间声明。
// 输入：tr 提供请求头；secret 为校验密钥；header 为 AccessTokenHeader 或 RefreshTokenHeader。
// 输出：校验通过时返回 token 和 nil；仅过期时返回 token 和 jwt.ErrTokenExpired；
// 凭证缺失、格式不符或其他校验失败时返回 nil 和鉴权错误。
// token 必须包含 exp；仅过期的 token 不能直接作为有效身份，由调用方决定刷新或按游客处理。
func getToken(tr transport.Transporter, secret, header string) (*jwt.Token, error) {
	parts := strings.SplitN(tr.RequestHeader().Get(header), " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], BearerTokenPrefix) || parts[1] == "" {
		return nil, uerr.ErrTokenMissing
	}
	var claims jwt.Claims = new(RefreshTokenClaims)
	if header == AccessTokenHeader {
		claims = new(AccessTokenClaims)
	}
	token, err := jwt.ParseWithClaims(parts[1], claims, func(*jwt.Token) (interface{}, error) {
		return []byte(secret), nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(TokenIssuer), jwt.WithExpirationRequired())
	if err != nil {
		// 过期若伴随 nbf、issuer 或必需声明错误，不能进入仅过期的刷新分支。
		if errors.Is(err, jwt.ErrTokenExpired) && !errors.Is(err, jwt.ErrTokenNotValidYet) &&
			!errors.Is(err, jwt.ErrTokenInvalidIssuer) && !errors.Is(err, jwt.ErrTokenRequiredClaimMissing) {
			return token, jwt.ErrTokenExpired
		}
		return nil, helper.HandleError(uerr.ErrParseTokenFailed, err)
	}
	return token, nil
}
