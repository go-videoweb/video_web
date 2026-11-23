package main

import (
	"fmt"
	"os"

	"yssim-go/tests/tools/zentao_bug_export/config"
	"yssim-go/tests/tools/zentao_bug_export/output"
	"yssim-go/tests/tools/zentao_bug_export/zentao"
)

// main 是命令行工具入口。
//
// 作用：读取进程参数，调用 run 完成禅道导出解析，并把错误写入 stderr。
// 输入：os.Args[1:]，例如 -xml bugs.xml -html bugs.html -out bugs.json。
// 输出：通过 os.Exit 返回进程退出码；0 表示成功，2 表示参数、解析或写文件失败。
// 示例：go run ./tests/tools/zentao_bug_export -xml bugs.xml -html bugs.html -out bugs.json。
func main() {
	exitCode, err := run(os.Args[1:])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
	}
	os.Exit(exitCode)
}

// run 串联参数读取、HTML 附件读取、模型资源附件下载、XML Bug 解析和 JSON 输出。
//
// 作用：把一次禅道导出转换成整理后的 JSON。
// 输入：命令行参数切片，例如 []string{"-xml", "bugs.xml", "-bug-id", "1108", "-resource-dir", "tmp/zentao"}。
// 输出：exitCode 和 error；exitCode=0 表示成功，exitCode=2 表示工具执行失败，error 包含失败原因。
// 示例：run([]string{"-xml", "bugs.xml", "-html", "bugs.html", "-out", "-"}) 会把解析结果打印到命令行窗口。
func run(args []string) (int, error) {
	cfg, err := config.Parse(args)
	if err != nil {
		return 2, err
	}

	attachments, warnings, err := zentao.ParseHTMLAttachmentIndex(cfg.HTMLPath)
	if err != nil {
		return 2, err
	}
	attachments = zentao.FilterAttachmentIndex(attachments, cfg.BugID)
	attachments, collectWarnings, err := zentao.CollectModelResourceAttachments(attachments, cfg.ResourceDir, cfg.Cookie)
	if err != nil {
		return 2, err
	}
	warnings = append(warnings, collectWarnings...)

	export, err := zentao.ParseXMLExport(cfg.XMLPath, cfg.HTMLPath, attachments, cfg.BugID, warnings)
	if err != nil {
		return 2, err
	}

	if err := output.WriteJSON(cfg.Output, export, cfg.Pretty); err != nil {
		return 2, err
	}
	return 0, nil
}
