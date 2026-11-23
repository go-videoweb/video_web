package scanner

import "regexp"

var (
	// groupRE 匹配 router/*.go 中常见的 Gin 分组定义。
	// 示例输入：ModelsV2 := g.Group("/api/v2/model")
	// 匹配结果：name=ModelsV2, base=g, path=/api/v2/model
	// 示例输入：var Models = g.Group("/user")
	// 匹配结果：name=Models, base=g, path=/user
	groupRE = regexp.MustCompile(`(\w+)\s*(?::=|=)\s*(\w+)\.Group\(\s*"([^"]*)"`)

	// routeRE 匹配 router/*.go 中路径为字符串字面量的 Gin 路由定义。
	// 示例输入：Models.GET("/userspace/get", user.GetUserSpaceView)
	// 匹配结果：group=Models, method=GET, path=/userspace/get
	// 示例输入：ModelsV2.POST("/experiment/create", simulate.ExperimentCreateV2View)
	// 匹配结果：group=ModelsV2, method=POST, path=/experiment/create
	routeRE = regexp.MustCompile(`(\w+)\.(GET|POST|PUT|PATCH|DELETE|OPTIONS|HEAD|Any)\(\s*"([^"]*)"`)

	// apiMarkRE 匹配 go test -json 的 Output 中由测试 client 打印的主测接口标记。
	// 示例输入：API_MARK role=primary method=GET path=/api/v2/user/userspace/get
	// 匹配结果：role=primary, method=GET, path=/api/v2/user/userspace/get
	apiMarkRE = regexp.MustCompile(`API_MARK\s+role=(\w+)\s+method=(\w+)\s+path=(/\S+)`)
)
