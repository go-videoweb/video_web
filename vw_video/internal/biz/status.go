package biz

// RPC 状态位掩码；MySQL 使用独立字段，收藏状态从实际关系读取。
const (
	UpvoteStatus     int64 = 1 << iota // 00..0001
	FavoriteStatus                     // 00..0010
	ShareStatus                        // 00..0100
	ThrowShellStatus                   // 00..1000 If user has thrown shell(s) to this video
)

// return 1 indicates the status is set, -1 is unset.
func checkIsUpvoted(status int64) int32 {
	if status&UpvoteStatus > 0 {
		return 1
	}
	return -1
}

func checkIsFavorited(status int64) int32 {
	if status&FavoriteStatus > 0 {
		return 1
	}
	return -1
}

func checkIsShared(status int64) int32 {
	if status&ShareStatus > 0 {
		return 1
	}
	return -1
}

func checkIsThrownShell(status int64) int32 {
	if status&ThrowShellStatus > 0 {
		return 1
	}
	return -1
}
