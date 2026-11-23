package helpers

import "fmt"

// UserSettings 表示 /user/settings/get 返回的用户设置快照。
type UserSettings struct {
	GridDisplay bool
	Interactive bool
}

// ParseUserSettings 从 /user/settings/get 响应中解析完整用户设置。
func ParseUserSettings(resp *Response) (UserSettings, error) {
	if resp == nil {
		return UserSettings{}, fmt.Errorf("用户设置响应为空")
	}
	if resp.Status != 0 {
		return UserSettings{}, fmt.Errorf("获取用户设置返回非成功状态: status=%d, msg=%s, err=%s", resp.Status, resp.Msg, resp.Err)
	}
	dataMap, err := ObjectData(resp, "获取当前用户设置")
	if err != nil {
		return UserSettings{}, err
	}
	gridDisplay, ok := dataMap["grid_display"].(bool)
	if !ok {
		return UserSettings{}, fmt.Errorf("grid_display 不是 bool: %T", dataMap["grid_display"])
	}
	interactive, ok := dataMap["interactive"].(bool)
	if !ok {
		return UserSettings{}, fmt.Errorf("interactive 不是 bool: %T", dataMap["interactive"])
	}
	return UserSettings{GridDisplay: gridDisplay, Interactive: interactive}, nil
}
