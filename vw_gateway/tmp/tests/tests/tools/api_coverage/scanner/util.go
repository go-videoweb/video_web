package scanner

import "sort"

// sortStrings 对字符串切片原地排序。
// 这里保留一个很薄的包装，避免 router 扫描逻辑直接暴露排序细节。
func sortStrings(items []string) {
	sort.Strings(items)
}
