package helpers

// Response 统一响应结构。
type Response struct {
	Data   any    `json:"data"`
	Msg    string `json:"msg"`
	Status int    `json:"status"` // 正常是0, 系统级错误是1, 用户错误是2, omc未启动则是3, 资源已被删除是4
	Err    string `json:"err"`
}
