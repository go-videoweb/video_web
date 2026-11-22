package main

import (
	"fmt"
	"vw_user/cmd/gorm_gen/methods"
	"vw_user/internal/conf"

	"github.com/go-kratos/kratos/v2/config"
	"github.com/go-kratos/kratos/v2/config/file"
	"gorm.io/driver/mysql"
	"gorm.io/gen"
	"gorm.io/gorm"
)

// main 从现有 MySQL schema 生成用户 DAL，不执行 DDL。
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
	user := g.GenerateModel("user",
		gen.FieldType("version", "optimisticlock.Version"),
		gen.FieldType("is_admin", "bool"),
		gen.FieldType("gender", "int"))
	level := g.GenerateModel("user_level",
		gen.FieldType("level", "uint32"),
		gen.FieldType("exp", "uint32"),
		gen.FieldType("next_exp", "uint32"),
		gen.WithMethod(methods.UserLevel{}))
	followList := g.GenerateModel("follow_list", gen.FieldType("is_default", "bool"))
	userFollows := g.GenerateModel("user_follows")
	userFans := g.GenerateModel("user_fans")
	userSearchHistory := g.GenerateModel("user_search_history")
	favorites := g.GenerateModel("favorites")
	favoriteVideo := g.GenerateModel("favorite_video")

	g.ApplyBasic(
		user, level,
		followList, userFollows, userFans,
		favorites, favoriteVideo, userSearchHistory)

	g.Execute()

}

// getGormDB 独立初始化生成器连接，不导入业务 data 或旧 DAL，避免 schema 变化后无法生成。
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
