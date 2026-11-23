package coverage

import "regexp"

const (
	ClassVersionedCurrent = "versioned_current" // 带 /api/vN 前缀、且在同一接口族中版本号最高的当前新版接口，会计入当前接口覆盖率。

	ClassVersionlessCurrent = "versionless_current" // 没有 /api/vN 前缀、且不存在同族新版接口的当前接口，会计入当前接口覆盖率。

	ClassVersionedSuperseded = "versioned_superseded" // 带 /api/vN 前缀、但同一接口族存在更高版本的旧版接口，不计入当前接口覆盖率。

	ClassLegacyShadowed = "legacy_shadowed" // 没有 /api/vN 前缀、且同一接口族已有新版接口的旧接口，不计入当前接口覆盖率。

	ClassInternalTest = "internal_test" // router 中以 /Test 开头的内部测试路由，不属于对外业务接口，不计入当前接口覆盖率。

	ClassDeprecated = "deprecated_blacklisted" // 已写入废弃接口黑名单的路由，即使仍在 router 中注册，也会从覆盖统计中排除。

	ClassUnclassified = "unclassified" // 路由刚被解析出来时的临时分类，后续会被 classifyRoutes 归入具体类别。

	RolePrimary = "primary" // API_MARK 中表示“当前 Test 函数主测接口”的角色，只有该角色会计入接口覆盖率。
)

var (
	// versionRE 匹配新版接口的 /api/vN 前缀。
	// 示例输入：/api/v3/model/instance
	// 匹配结果：version=3, tail=/model/instance
	versionRE = regexp.MustCompile(`^/api/v(\d+)(/.*)$`)
)
