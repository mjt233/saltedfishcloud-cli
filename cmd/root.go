// Package cmd 定义 sfc-cli 的全部 CLI 命令。
package cmd

import (
	"github.com/spf13/cobra"
)

// apiTicket 存储通过命令行标志传入的 API Ticket，供认证使用。
var apiTicket string

// serviceURL 存储通过命令行标志传入的服务基础 URL。
var serviceURL string

// NewRootCommand 构造并返回根 cobra 命令，并注册全局持久标志。
func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "sfc-cli",
		Short:         "CLI client for Salted Fish Cloud",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	// 注册全局持久标志，将标志值绑定到包级变量，供子命令复用。
	root.PersistentFlags().StringVar(&apiTicket, "api-ticket", "", "用于鉴权的 API Ticket")
	root.PersistentFlags().StringVar(&serviceURL, "service-url", "", "咸鱼云服务的基础 URL")

	return root
}

// Execute 运行根命令并返回遇到的错误。
func Execute() error {
	return NewRootCommand().Execute()
}
