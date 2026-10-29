package data

import (
	"context"
	"fmt"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/wire"
	"github.com/redis/go-redis/v9"
	"gorm.io/driver/mysql"
	"gorm.io/gorm"
	utilCtx "util/context"
	mysql2 "util/dbutil/mysqlutil"
	"vw_user/internal/conf"
	"vw_user/internal/data/dal/model"
	"vw_user/internal/data/dal/query"
)

// ProviderSet is data providers.
var ProviderSet = wire.NewSet(
	NewData,
	NewMySQL,
	NewRedisClusterClient,
	NewUserRepo,
	NewUserInfoRepo,
	NewCaptRepo,
	NewFileRepo,
	NewFavoritesRepo,
	NewFollowRepo,
	NewTransaction,
)

const (
	kb = 1024
	mb = 1024 * kb
)

// transactionKey is a context key for gorm transactionKey
type transactionKey struct{}

// Data .
type Data struct {
	mysql *gorm.DB
	redis *redis.ClusterClient
}

// NewTransaction return a util.Transaction interface.
func NewTransaction(d *Data) mysql2.Transaction {
	return d
}

// NewData .
func NewData(
	mysql *gorm.DB,
	redisCluster *redis.ClusterClient,
	logger log.Logger) (*Data, func(), error) {
	cleanup := func() {
		log.NewHelper(logger).Info("closing the data resources")
	}
	return &Data{
		mysql: mysql,
		redis: redisCluster,
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
	return db
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

// WithTx 使用现有查询连接管理本地事务，回调必须使用收到的 ctx；嵌套调用沿用保存点。
// 开启、业务和提交错误均返回；失败或 panic 回滚，不吞掉提交失败。
func (d *Data) WithTx(ctx context.Context, fn func(context.Context) error) error {
	db := getQuery(ctx).User.WithContext(ctx).UnderlyingDB().Session(&gorm.Session{NewDB: true, Context: ctx})
	return db.Transaction(func(tx *gorm.DB) error {
		return fn(context.WithValue(ctx, transactionKey{}, &query.QueryTx{Query: query.Use(tx)}))
	})
}

// WithSqlTx is not implemented.
func (d *Data) WithSqlTx(context.Context, func(context.Context) error) error {
	panic("not implemented")
}

// startTx sets transactionKey to context and starts a transaction.
func startTx(ctx context.Context) (context.Context, func(err error)) {
	tx := query.Q.Begin()
	ctx = utilCtx.WithValue(ctx, transactionKey{}, tx) // set transactionKey to context
	return ctx, func(err error) { commitTx(ctx, err) }
}

// commitTx commits the transactionKey in context.
// err is the error that out of the transaction.
func commitTx(ctx context.Context, err error) {
	value := utilCtx.MustGetValue(ctx, transactionKey{})

	tx := value.(*query.QueryTx)

	// Handle panic which could occur in tilCtx.MustGetValue.
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r) // 确保 err 被赋值，事务正确回滚
			tx.Rollback()
			panic(r) // 继续抛出 panic，防止业务逻辑被吞掉
		}
	}()

	if err != nil || tx.Error != nil {
		tx.Rollback()
		return
	}
	tx.Commit()
}

// BeginTx  starts a transaction manually.
// ! DON'T FORGET TO CALL THE COMMIT function to COMMIT or ROLLBACK TRANSACTION MANUALLY.
func BeginTx(ctx context.Context) (context.Context, func(err error)) {
	tx := query.Q.Begin()
	ctx = utilCtx.WithValue(ctx, transactionKey{}, tx) // set transactionKey to context
	return ctx, func(err error) { commitTx(ctx, err) }
}

// getQuery is a helper function.
// It returns common query *query.Query or transactional query *(query.QueryTx).Query.
// With this function, methods of data layer don't need to care about if it's in transactionKey or not.
func getQuery(ctx context.Context) *query.Query {
	// if ctx has transactionKey, return transactional query
	tx, ok := utilCtx.GetValue(ctx, transactionKey{})
	if ok {
		return tx.(*query.QueryTx).Query
	}
	return query.Q
}

// addUserModel is a helper function.
// It adds a user model, so that gorm-gen can use optimistic lock.
// Use the function when need to UPDATE a user model.
// See https://github.com/go-gorm/optimisticlock/issues/36 for more details.
func addUserModel(ctx context.Context, userId int64) (query.IUserDo, *model.User, error) {
	user := getQuery(ctx).User
	userDo := user.WithContext(ctx)
	userModel, err := userDo.Where(user.UserID.Eq(userId)).First()
	if err != nil {
		return userDo, nil, err
	}
	userDo.ReplaceDB(userDo.UnderlyingDB().Model(userModel))
	return userDo, userModel, nil
}
