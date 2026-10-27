package data

import (
	"context"
	"database/sql"
	"fmt"
	"github.com/go-kratos/kratos/contrib/registry/consul/v2"
	"github.com/go-kratos/kratos/v2/middleware/recovery"
	"github.com/go-kratos/kratos/v2/middleware/tracing"
	"github.com/go-kratos/kratos/v2/registry"
	"github.com/go-kratos/kratos/v2/transport/grpc"
	consulAPI "github.com/hashicorp/consul/api"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	utilCtx "util/context"
	"util/dbutil"
	mysql2 "util/dbutil/mysqlutil"
	favorv1 "vw_user/api/v1/favorites"
	infov1 "vw_user/api/v1/userinfo"
	"vw_video/internal/conf"
	"vw_video/internal/data/dal/model"
	"vw_video/internal/data/dal/query"

	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/wire"
)

// ProviderSet is data providers.
var ProviderSet = wire.NewSet(
	// Data
	NewData,
	NewTransaction,
	NewMySQL,
	NewRedisClusterClient,

	// Repo
	NewVideoInfoRepo,
	NewInteractRepo,
	NewVideoCommentRepo,

	// Client
	NewDiscovery,
	NewRegistrar,
	NewUserinfoClient,
	NewFavoriteClient,
)

type keyType struct{}

// transactionKey is a context key for gorm transactionKey
type transactionKey keyType

// Data .
type Data struct {
	mysql          *gorm.DB
	redis          *redis.ClusterClient
	userInfoClient infov1.UserinfoClient
	favoriteClient favorv1.FavoriteClient
}

// NewTransaction return a util.Transaction interface.
func NewTransaction(d *Data) mysql2.Transaction {
	return d
}

// NewData 组装视频仓储共享的 MySQL、Redis 和用户服务客户端，不再创建 MongoDB 客户端。
// 输入：依赖由 Wire 构造并完成各自初始化；logger 用于记录资源清理。
// 输出：共享数据容器、清理回调及错误；此函数不执行表结构迁移或额外数据库连接。
func NewData(
	mysql *gorm.DB,
	redisCluster *redis.ClusterClient,
	userInfoClient infov1.UserinfoClient,
	favoriteClient favorv1.FavoriteClient,
	logger log.Logger) (*Data, func(), error) {
	cleanup := func() {
		log.NewHelper(logger).Info("closing the data resources")
	}
	return &Data{
		mysql:          mysql,
		redis:          redisCluster,
		userInfoClient: userInfoClient,
		favoriteClient: favoriteClient,
	}, cleanup, nil
}

func NewMySQL(c *conf.Data) *gorm.DB {
	host, port, username, password, dbname := c.Mysql.Host, c.Mysql.Port, c.Mysql.User, c.Mysql.Password, c.Mysql.Database
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local", username, password, host, port, dbname)
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		panic(err)
	}
	sqlDB, _ := db.DB()
	sqlDB.SetMaxIdleConns(int(c.Mysql.MaxIdle))
	sqlDB.SetMaxOpenConns(int(c.Mysql.MaxOpen))
	if err := sqlDB.Ping(); err != nil {
		panic(err)
	}
	query.SetDefault(db)
	initOrderMap()
	return db
}

func NewDiscovery(conf *conf.Registry) registry.Discovery {
	c := consulAPI.DefaultConfig()
	c.Address = conf.Consul.Address
	c.Scheme = conf.Consul.Scheme
	cli, err := consulAPI.NewClient(c)
	if err != nil {
		panic(err)
	}
	r := consul.New(cli, consul.WithHealthCheck(false))
	return r
}

func NewRegistrar(conf *conf.Registry) registry.Registrar {
	c := consulAPI.DefaultConfig()
	c.Address = conf.Consul.Address
	c.Scheme = conf.Consul.Scheme
	cli, err := consulAPI.NewClient(c)
	if err != nil {
		panic(err)
	}
	r := consul.New(cli, consul.WithHealthCheck(false))
	return r
}

func NewRedisClusterClient(c *conf.Data) *redis.ClusterClient {
	var address []string
	ipAddress := c.RedisCluster.Host
	for _, port := range c.RedisCluster.Port {
		address = append(address, fmt.Sprintf("%s:%s", ipAddress, port))
	}
	redisCluster := redis.NewClusterClient(&redis.ClusterOptions{
		Addrs:        address,
		Password:     c.RedisCluster.Password,
		PoolSize:     int(c.RedisCluster.PoolSize),
		MinIdleConns: int(c.RedisCluster.MinIdleConns),
		MaxRetries:   int(c.RedisCluster.MaxRetries),
		DialTimeout:  c.RedisCluster.DialTimeout.AsDuration(),
		ReadTimeout:  c.RedisCluster.ReadTimeout.AsDuration(),
		WriteTimeout: c.RedisCluster.WriteTimeout.AsDuration(),
		PoolTimeout:  c.RedisCluster.PoolTimeout.AsDuration(),
	})
	err := redisCluster.ForEachShard(context.Background(), func(ctx context.Context, shard *redis.Client) error {
		return shard.Ping(ctx).Err()
	})
	if err != nil {
		panic(err)
	}
	return redisCluster
}

func NewUserinfoClient(r registry.Discovery, s *conf.Service) infov1.UserinfoClient {
	conn, err := grpc.DialInsecure(
		context.Background(),
		grpc.WithEndpoint(s.User.Endpoint),
		grpc.WithDiscovery(r),
		grpc.WithMiddleware(
			recovery.Recovery(),
			tracing.Client(),
		),
	)
	if err != nil {
		panic(err)
	}
	return infov1.NewUserinfoClient(conn)
}

// NewFavoriteClient 通过已有用户服务发现配置创建收藏夹元信息客户端。
// 输入：服务发现与用户服务端点；输出：客户端，连接构造失败沿用现有初始化策略终止启动。
func NewFavoriteClient(r registry.Discovery, s *conf.Service) favorv1.FavoriteClient {
	conn, err := grpc.DialInsecure(context.Background(),
		grpc.WithEndpoint(s.User.Endpoint), grpc.WithDiscovery(r),
		grpc.WithMiddleware(recovery.Recovery(), tracing.Client()))
	if err != nil {
		panic(err)
	}
	return favorv1.NewFavoriteClient(conn)
}

// WithTx 使用 GORM 管理本地事务，向仓储传递同一连接，并向调用方返回提交错误。
// 输入：ctx 传递取消、超时及请求信息；fn 内的仓储操作必须使用回调收到的上下文。
// 输出：开启、业务执行或提交的错误；业务失败时回滚，panic 时尝试回滚并继续传播。
// 不复用 ctx 中已有事务，也不覆盖绕过 getQuery 的操作、Redis 或跨服务 RPC。
func (d *Data) WithTx(ctx context.Context, fn func(context.Context) error) error {
	return d.mysql.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		txContext := context.WithValue(ctx, transactionKey{}, &query.QueryTx{Query: query.Use(tx)})
		return fn(txContext)
	})
}

// getQuery 返回上下文中的事务查询器，无事务时使用默认查询器。
// 输入：ctx 可包含本地事务或经 bindSQLTx 适配的显式 SQL 事务。
// 输出：选定连接的查询器，不开启、提交或回滚事务；调用方仍需传递请求上下文。
func getQuery(ctx context.Context) *query.Query {
	// if ctx has transactionKey, return transactional query
	tx, ok := utilCtx.GetValue(ctx, transactionKey{})
	if ok {
		return tx.(*query.QueryTx).Query
	}
	return query.Q
}

// bindSQLTx 将显式 SQL 事务绑定到新上下文，供仓储统一通过 getQuery 取连接。
// 输入：tx 必须是调用方或 DTM 屏障提供的有效事务；ctx 保留取消、超时及请求信息。
// 输出：显式事务优先于原上下文中的事务；nil 事务或适配失败返回错误，不回退普通连接。
// 不执行业务查询、开启或结束事务，也不修改调用方持有的 ctx。
func bindSQLTx(ctx context.Context, tx *sql.Tx) (context.Context, error) {
	if tx == nil {
		return nil, gorm.ErrInvalidTransaction
	}
	db, err := dbutil.SqlTxToGormDB(tx)
	if err != nil {
		return nil, err
	}
	txContext := context.WithValue(ctx, transactionKey{}, &query.QueryTx{Query: query.Use(db)})
	return txContext, nil
}

// addVideoModel 从 getQuery 选择的连接读取视频，并附加读取到的版本条件供后续更新使用。
// 输入：ctx 可携带本地或显式 SQL 事务；videoId 为待更新的视频 ID。
// 输出：附带版本筛选的查询对象及模型；读取失败返回错误，不调整事务生命周期。
func addVideoModel(ctx context.Context, videoId int64) (query.IVideoDo, *model.Video, error) {
	video := getQuery(ctx).Video
	videoDo := video.WithContext(ctx)
	videoModel, err := videoDo.Where(video.VideoID.Eq(videoId)).First()
	if err != nil {
		return videoDo, nil, err
	}
	// Gen 更新会重建模型，不能只靠 ReplaceDB(Model(...)) 传递旧版本；版本递增仍由插件完成。
	if videoModel.Version.Valid {
		videoDo = videoDo.Where(video.Version.Eq(videoModel.Version))
	}
	return videoDo, videoModel, nil
}

func addCommentModel(ctx context.Context, commentId int64) (query.ICommentDo, *model.Comment, error) {
	comment := getQuery(ctx).Comment
	commentDo := comment.WithContext(ctx)
	commentModel, err := commentDo.Where(comment.CommentID.Eq(commentId)).First()
	if err != nil {
		return commentDo, nil, err
	}
	commentDo.ReplaceDB(commentDo.UnderlyingDB().Model(commentModel))
	return commentDo, commentModel, nil
}
