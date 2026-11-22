package domain

import "strconv"

// AvatarURL 返回 API 相对地址；前端应相对于 API origin 解析，不使用服务器磁盘路径。
func AvatarURL(userID int64) string {
	if userID <= 0 {
		return ""
	}
	return "/api/v1/users/" + strconv.FormatInt(userID, 10) + "/avatar"
}
