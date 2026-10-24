package main

import (
	"fmt"
	"vw_video/internal/conf"

	"github.com/go-kratos/kratos/v2/config"
	"github.com/go-kratos/kratos/v2/config/file"
	"gorm.io/driver/mysql"
	"gorm.io/gen"
	"gorm.io/gorm"
)

// main 根据当前 MySQL 表结构生成视频服务的 DAL 模型和查询代码。
// 输入：从视频 module 根目录读取 configs/config.yaml；需要数据库结构读取权限。
// 输出：重写 internal/data/dal 下选定表的生成文件；配置或连接失败时终止，不执行表结构迁移。
//
//go:generate go run gen_generator.go
func main() {
	db := getGormDB()
	g := gen.NewGenerator(gen.Config{
		OutPath: "./internal/data/dal/query",

		// WithDefaultQuery 生成默认查询结构体(作为全局变量使用), 即`Q`结构体和其字段(各表模型)
		// WithoutContext 生成没有context调用限制的代码供查询
		// WithQueryInterface 生成interface形式的查询代码(可导出), 如`Where()`方法返回的就是一个可导出的接口类型
		Mode: gen.WithDefaultQuery | gen.WithQueryInterface | gen.WithoutContext,

		// 表字段可为 null 值时, 对应结体字段使用指针类型
		//FieldNullable: true,

		// 表字段默认值与模型结构体字段零值不一致的字段, 在插入数据时需要赋值该字段值为零值的, 结构体字段须是指针类型才能成功, 即`FieldCoverable:true`配置下生成的结构体字段.
		// 因为在插入时遇到字段为零值的会被GORM赋予默认值. 如字段`age`表默认值为10, 即使你显式设置为0最后也会被GORM设为10提交.
		// 如果该字段没有上面提到的插入时赋零值的特殊需要, 则字段为非指针类型使用起来会比较方便.
		FieldCoverable: false,

		// 模型结构体字段的数字类型的符号表示是否与表字段的一致, `false`指示都用有符号类型
		FieldSignable: false,

		// 生成 gorm 标签的字段索引属性
		FieldWithIndexTag: true,

		// 生成 gorm 标签的字段类型属性
		FieldWithTypeTag: true,

		WithUnitTest: false,
	})
	g.UseDB(db)
	// 自定义字段的数据类型
	// 统一数字类型为int64,兼容protobuf
	dataMap := map[string]func(columnType gorm.ColumnType) (dataType string){
		"tinyint":   func(columnType gorm.ColumnType) (dataType string) { return "int64" },
		"smallint":  func(columnType gorm.ColumnType) (dataType string) { return "int64" },
		"mediumint": func(columnType gorm.ColumnType) (dataType string) { return "int64" },
		"int":       func(columnType gorm.ColumnType) (dataType string) { return "int64" },
		"bigint":    func(columnType gorm.ColumnType) (dataType string) { return "int64" },
	}

	// 自定义字段的数据类型
	// 统一数字类型为int64,兼容protobuf
	g.WithDataTypeMap(dataMap)

	// 生成model
	video := g.GenerateModel("video",
		gen.FieldType("version", "optimisticlock.Version"),
		gen.FieldType("is_admin", "bool"),
		gen.FieldType("gender", "int"))
	//gen.WithMethod(methods.Video{}))
	favoriteVideo := g.GenerateModel("favorite_video")
	barrages := g.GenerateModel("barrages")
	userVideoStatus := g.GenerateModel("user_video_status", gen.FieldType("last_watch_time", "*time.Time"))
	comment := g.GenerateModel("comment",
		gen.FieldType("version", "optimisticlock.Version"),
		gen.FieldType("cnt_upvote", "uint64"),
	)

	g.ApplyBasic(video, barrages, favoriteVideo, comment, userVideoStatus)

	g.Execute()

}

// getGormDB 为一次性 DAL 生成建立独立的 MySQL 连接。
// 输入：相对于进程工作目录的 configs/config.yaml，仅使用其中的 MySQL 配置。
// 输出：可读取表结构的 GORM 连接；加载、解析或连接失败时 panic，不输出连接凭据。
// 不依赖业务 data 包或现有 DAL，避免模型缺失时无法编译生成器；不初始化业务查询对象和排序映射。
func getGormDB() *gorm.DB {
	c := config.New(
		config.WithSource(
			file.NewSource("./configs/config.yaml"),
		),
	)
	defer c.Close()

	if err := c.Load(); err != nil {
		panic(err)
	}

	var bc conf.Bootstrap
	if err := c.Scan(&bc); err != nil {
		panic(err)
	}
	cfg := bc.Data.Mysql
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?charset=utf8mb4&parseTime=True&loc=Local", cfg.User, cfg.Password, cfg.Host, cfg.Port, cfg.Database)
	db, err := gorm.Open(mysql.Open(dsn), &gorm.Config{})
	if err != nil {
		panic(err)
	}
	return db
}
