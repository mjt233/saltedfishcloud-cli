// Package cmd 定义 sfc-cli 的全部 CLI 命令。
// 当前文件实现 version 子命令，输出当前 CLI 版本号。
package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// version 由 ldflags 在构建时注入，开发环境默认为 dev。
var version = "dev"

// newVersionCommand 创建 version 子命令。
// 输出当前 CLI 版本号到 stdout。
func newVersionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "输出当前 CLI 版本号",
		Run: func(cmd *cobra.Command, args []string) {
			// 将版本号写入标准输出
			fmt.Fprintln(cmd.OutOrStdout(), version)
		},
	}
}
