package zentao

// Source 记录这个 JSON 是从哪些禅道导出文件生成的。
type Source struct {
	// System 是来源系统名称；当前固定为 "zentao"。
	System string `json:"system"`
	// XMLPath 是主要读取的 XML 文件路径；示例："C:/Downloads/YSSIM-Bug.xml"。
	XMLPath string `json:"xml_path"`
	// HTMLPath 是用来补全附件的 HTML 文件路径；未提供 HTML 时为空并从 JSON 省略。
	HTMLPath string `json:"html_path,omitempty"`
}

// Summary 记录这次一共解析到多少 Bug，以及其中有多少带附件。
type Summary struct {
	// Total 是最终输出的 Bug 数量；指定 -bug-id 时通常为 0 或 1。
	Total int `json:"total"`
	// WithImages 是存在图片证据的 Bug 数量，包括 steps 内嵌图片和附件图片。
	WithImages int `json:"with_images"`
	// WithVideos 是存在视频附件的 Bug 数量；视频只记录存在性，不做 AI 自动理解。
	WithVideos int `json:"with_videos"`
	// WithModelResources 是存在模型类资源附件的 Bug 数量；包括 .mo/.mos/.fmu/.mat 和 .zip/.rar/.7z 等模型库压缩包。
	WithModelResources int `json:"with_model_resources"`
	// Warnings 是不会让工具失败、但需要使用者留意的问题；例如 HTML 中找不到 Bug编号/附件 表格。
	Warnings []string `json:"warnings,omitempty"`
}

// Export 是工具最终输出的完整 JSON。
type Export struct {
	// Source 记录输入文件和来源系统。
	Source Source `json:"source"`
	// Summary 记录解析数量、图片/视频统计和警告。
	Summary Summary `json:"summary"`
	// Bugs 是整理后的 Bug 列表。
	Bugs []ParsedBug `json:"bugs"`
}

// Attachment 表示一个禅道文件引用。steps 内嵌图片通常只有 FilenameHint，未必有真实附件文件名。
type Attachment struct {
	// Source 表示附件来源字段；"steps" 表示重现步骤内嵌图片，"files" 表示附件列。
	Source string `json:"source"`
	// Kind 是根据文件后缀判断出的附件类型；可为 image、video、model、archive 或 other。
	Kind string `json:"kind"`
	// Filename 是附件真实显示文件名；示例："参数受控.mp4"。
	Filename string `json:"filename,omitempty"`
	// FilenameHint 是内嵌图片 src 中的提示名；示例："1291.png"。
	FilenameHint string `json:"filename_hint,omitempty"`
	// FileID 是禅道文件 ID；示例："1862"。
	FileID string `json:"file_id,omitempty"`
	// DownloadURL 是 HTML 附件链接中的下载地址；未提供 HTML 时可能为空。
	DownloadURL string `json:"download_url,omitempty"`
	// LocalPath 是工具把附件下载到本机后的路径；示例："tmp/zentao_bug_attachments/BUG_1108/1862_Model.mo"。
	LocalPath string `json:"local_path,omitempty"`
	// CollectionError 是附件下载或收集失败原因；为空表示未尝试收集或收集成功。
	CollectionError string `json:"collection_error,omitempty"`
}

// ParsedBug 是工具整理后输出给 AI 阅读的单个 Bug。
type ParsedBug struct {
	// BugID 是禅道 Bug 编号；示例："1108"。
	BugID string `json:"bug_id"`
	// Title 是 Bug 标题；示例："组件参数受控异常"。
	Title string `json:"title"`
	// Product 是所属产品；示例："YSSIM(#19)"。
	Product string `json:"product,omitempty"`
	// Module 是所属模块；示例："/YSSIM-机理建模(#151)"。
	Module string `json:"module,omitempty"`
	// Project 是所属项目；示例："YSLab-机理建模(#66)"。
	Project string `json:"project,omitempty"`
	// Execution 是所属执行；禅道导出中可能为 "0"。
	Execution string `json:"execution,omitempty"`
	// Story 是相关需求；禅道导出中可能为 "0"。
	Story string `json:"story,omitempty"`
	// Task 是相关任务；示例："(#0)"。
	Task string `json:"task,omitempty"`
	// Keywords 是关键词字段；为空时从 JSON 省略。
	Keywords string `json:"keywords,omitempty"`
	// Severity 是严重程度；示例："一般(#1)"。
	Severity string `json:"severity,omitempty"`
	// Priority 是优先级；示例："中(#2)"。
	Priority string `json:"priority,omitempty"`
	// Type 是 Bug 类型；示例："功能问题(#functionality)"。
	Type string `json:"type,omitempty"`
	// OS 是操作系统字段；为空时从 JSON 省略。
	OS string `json:"os,omitempty"`
	// Browser 是浏览器字段；为空时从 JSON 省略。
	Browser string `json:"browser,omitempty"`
	// StepsRaw 是重现步骤原始内容，保留 <img> 等 HTML 片段用于证据追溯。
	StepsRaw string `json:"steps_raw,omitempty"`
	// StepsText 是去除 HTML 标签后的纯文本步骤，供 AI 优先阅读。
	StepsText string `json:"steps_text,omitempty"`
	// Status 是 Bug 状态；示例："已关闭(#closed)"。
	Status string `json:"status,omitempty"`
	// Deadline 是截止日期；为空时从 JSON 省略。
	Deadline string `json:"deadline,omitempty"`
	// ActivatedCount 是激活次数；示例："0"。
	ActivatedCount string `json:"activated_count,omitempty"`
	// Confirmed 是确认状态；示例："已确认(#1)"。
	Confirmed string `json:"confirmed,omitempty"`
	// Mailto 是抄送给字段；为空时从 JSON 省略。
	Mailto string `json:"mailto,omitempty"`
	// OpenedBy 是创建人；示例："曹闯"。
	OpenedBy string `json:"opened_by,omitempty"`
	// OpenedDate 是创建时间；示例："2025-08-06 17:56:39"。
	OpenedDate string `json:"opened_date,omitempty"`
	// OpenedBuild 是影响版本；示例："主干(#trunk)"。
	OpenedBuild string `json:"opened_build,omitempty"`
	// AssignedTo 是当前指派人；关闭后可能为 "closed"。
	AssignedTo string `json:"assigned_to,omitempty"`
	// AssignedDate 是指派时间；示例："2025-08-13 10:52:37"。
	AssignedDate string `json:"assigned_date,omitempty"`
	// ResolvedBy 是解决者；示例："张恩阳"。
	ResolvedBy string `json:"resolved_by,omitempty"`
	// Resolution 是解决方案；示例："修正(#fixed)"。
	Resolution string `json:"resolution,omitempty"`
	// ResolvedBuild 是解决版本；示例："主干(#trunk)"。
	ResolvedBuild string `json:"resolved_build,omitempty"`
	// ResolvedDate 是解决时间；示例："2025-08-13 09:09:00"。
	ResolvedDate string `json:"resolved_date,omitempty"`
	// ClosedBy 是关闭人；示例："曹闯"。
	ClosedBy string `json:"closed_by,omitempty"`
	// ClosedDate 是关闭时间；示例："2025-08-13 10:52:37"。
	ClosedDate string `json:"closed_date,omitempty"`
	// DuplicateBug 是重复 Bug 编号；"0" 表示非重复。
	DuplicateBug string `json:"duplicate_bug,omitempty"`
	// RelatedBug 是相关 Bug 列表；为空时从 JSON 省略。
	RelatedBug string `json:"related_bug,omitempty"`
	// Case 是相关用例；示例："(#0)"。
	Case string `json:"case,omitempty"`
	// LastEditedBy 是最后修改者；示例："曹闯"。
	LastEditedBy string `json:"last_edited_by,omitempty"`
	// LastEditedDate 是最后修改时间；示例："2025-08-13 10:52:37"。
	LastEditedDate string `json:"last_edited_date,omitempty"`
	// FilesRaw 是 XML <files> 原始附件文本；HTML 不可用时用于粗略判断图片/视频。
	FilesRaw string `json:"files_raw,omitempty"`
	// FeedbackBy 是反馈者字段；为空时从 JSON 省略。
	FeedbackBy string `json:"feedback_by,omitempty"`
	// NotifyEmail 是通知邮箱字段；为空时从 JSON 省略。
	NotifyEmail string `json:"notify_email,omitempty"`
	// HasImageAttachment 表示该 Bug 是否存在图片证据。
	HasImageAttachment bool `json:"has_image_attachment"`
	// HasVideoAttachment 表示该 Bug 是否存在视频附件；视频内容需要测试者说明。
	HasVideoAttachment bool `json:"has_video_attachment"`
	// HasModelResourceAttachment 表示该 Bug 是否存在模型类资源附件；存在时测试用例必须使用该附件，否则不能判为 A - 可直接编写。
	HasModelResourceAttachment bool `json:"has_model_resource_attachment"`
	// Images 是图片证据列表，包含 steps 内嵌图片和附件图片。
	Images []Attachment `json:"images,omitempty"`
	// Videos 是视频附件列表；工具只记录文件名、fileID、下载链接等信息，不解析视频内容。
	Videos []Attachment `json:"videos,omitempty"`
	// ModelResources 是模型类资源附件列表；包含 .mo/.mos/.fmu/.mat 文件和 .zip/.rar/.7z 等模型库压缩包。
	ModelResources []Attachment `json:"model_resources,omitempty"`
	// Attachments 是 HTML 附件列解析出的完整附件列表。
	Attachments []Attachment `json:"attachments,omitempty"`
}

// AttachmentIndex 按 Bug 编号保存 HTML 附件列里读到的附件列表。
type AttachmentIndex map[string][]Attachment
