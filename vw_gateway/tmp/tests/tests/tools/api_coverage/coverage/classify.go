package coverage

import "strings"

// classifyRoutes 按“当前接口 / 旧接口 / 被高版本替代接口”给每条路由分类。
// 同一个 method + tail 代表同一个接口族，带 /api/vN 前缀的最高版本是当前新版接口。
func classifyRoutes(routes []Route, deprecatedRoutes map[RouteKey]string) {
	// versionedByTail 的 key 是接口族：method + 去版本后的 tail，例如 GET + /user/service/restart。
	// value 是 routes 切片中的下标列表，表示同一接口族下所有带 /api/vN 前缀的候选路由。
	versionedByTail := make(map[string][]int)
	for index := range routes {
		if strings.HasPrefix(routes[index].Path, "/Test") {
			routes[index].Classification = ClassInternalTest
			continue
		}
		if routes[index].Version != nil {
			tailKey := routeFamilyKey(routes[index].Method, routes[index].Tail)
			versionedByTail[tailKey] = append(versionedByTail[tailKey], index)
		}
	}

	// latestByTail 的 key 与 versionedByTail 相同；value 是该接口族中版本号最高的 Route。
	// 它既用于标记低版本接口为 versioned_superseded，也用于后面判断无版本接口是否被新版接口覆盖。
	latestByTail := make(map[string]Route)
	for tailKey, indexes := range versionedByTail {
		latestIndex := indexes[0]
		// 获取一个接口族的最高版本，latestIndex 为该最高版本的接口在 routes 中的下标。
		for _, index := range indexes[1:] {
			if routes[index].Version != nil && routes[latestIndex].Version != nil && *routes[index].Version > *routes[latestIndex].Version {
				latestIndex = index
			}
		}

		// 将接口族中的接口进行分类。最高版本是当前接口；其余带版本接口是已被更高版本替代的旧接口。
		latest := routes[latestIndex]
		latestByTail[tailKey] = latest
		for _, index := range indexes {
			if index == latestIndex {
				routes[index].Classification = ClassVersionedCurrent
			} else {
				routes[index].Classification = ClassVersionedSuperseded
				routes[index].LatestPath = latest.Path
			}
		}
	}

	// 最后处理无版本路由：如果同 method + tail 存在新版接口，则它是已有新版替代的旧接口；
	// 否则说明它目前没有新版替代，仍作为无版本当前接口计入当前接口覆盖率。
	for index := range routes {
		if routes[index].Classification != ClassUnclassified {
			continue
		}
		tailKey := routeFamilyKey(routes[index].Method, routes[index].Tail)
		if latest, exists := latestByTail[tailKey]; exists {
			routes[index].Classification = ClassLegacyShadowed
			routes[index].LatestPath = latest.Path
		} else {
			routes[index].Classification = ClassVersionlessCurrent
		}
	}

	// 废弃接口黑名单需要在“新版本 / 旧版本”关系判断完成后再应用。
	// 例如同时存在 /simulation/state/get 和 /api/v3/simulation/state/get 时，
	// 即使 v3 接口已废弃，也要先根据 v3 接口判断无版本接口是“已有新版替代的旧接口”，
	// 然后再把 v3 接口从覆盖统计中排除，避免无版本旧接口被重新计入“当前需要测试的接口”。
	for index := range routes {
		if _, ok := deprecatedRouteReason(routes[index].Key, deprecatedRoutes); ok {
			routes[index].Classification = ClassDeprecated
		}
	}
}
