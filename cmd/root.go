// Package cmd 定义 sfc-cli 的全部 CLI 命令。
package cmd

import (
	"github.com/mjt233/saltedfishcloud-cli/internal/client"
	"github.com/mjt233/saltedfishcloud-cli/internal/config"
	"github.com/mjt233/saltedfishcloud-cli/internal/oauth"
	"github.com/spf13/cobra"
)

// serviceURL 存储通过命令行标志传入的服务基础 URL。
var serviceURL string

// rootOptions 封装根命令的全局持久标志值，便于后续转换为 config.Options 供子命令使用。
type rootOptions struct {
	// ServiceURL 对应 --service-url 标志的值。
	ServiceURL string
}

// toConfigOptions 将当前已解析的根命令标志值转换为 config.Options，供子命令构造 config.Config 使用。
func (o rootOptions) toConfigOptions() config.Options {
	return config.Options{
		ServiceURL: o.ServiceURL,
	}
}

// currentRootOptions 从包级标志变量构造一个 rootOptions 实例。
func currentRootOptions() rootOptions {
	return rootOptions{
		ServiceURL: serviceURL,
	}
}

// toConfigOptions 从当前包级标志变量构造 config.Options，供子命令使用。
func toConfigOptions() config.Options {
	return currentRootOptions().toConfigOptions()
}

// NewRootCommand 构造并返回根 cobra 命令，并注册全局持久标志。
func NewRootCommand() *cobra.Command {
	root := &cobra.Command{
		Use:           "sfc-cli",
		Short:         "CLI client for Salted Fish Cloud",
		SilenceUsage:  true,
		SilenceErrors: true,
	}

	// 注册全局持久标志，将标志值绑定到包级变量，供子命令复用。
	root.PersistentFlags().StringVar(&serviceURL, "service-url", "", "Base URL of the Salted Fish Cloud service")

	// 注册子命令
	root.AddCommand(newLoginCommand())
	root.AddCommand(newLSCommand())
	root.AddCommand(newGetCommand())
	root.AddCommand(newUploadCommand())
	root.AddCommand(newRmCommand())
	root.AddCommand(newRenameCommand())
	root.AddCommand(newCpCommand())
	root.AddCommand(newMvCommand())
	root.AddCommand(newVersionCommand())
	root.AddCommand(rootAddClientCommand("remote-version", newRemoteVersionCommand))

	return root
}

// Execute 运行根命令并返回遇到的错误。
func Execute() error {
	return NewRootCommand().Execute()
}

// newAPIClientFromConfig 按配置构造 API 客户端。
// 使用 OAuth 登录态构造可自动刷新的令牌源（过期自动刷新并回写配置）。
func newAPIClientFromConfig(cfg config.Config) *client.APIClient {
	source := oauth.NewTokenSource(cfg.ServiceURL, cfg.ClientID, cfg.State(), config.SaveOAuthState)
	return client.NewAPIClientWithTokenSource(cfg.ServiceURL, source)
}

// rootAddClientCommand 注册需要 API 客户端的子命令。
// cmdName 为子命令名称，newCmd 为接收客户端工厂函数的命令工厂函数。
// 内部构造延迟创建客户端的工厂，在命令实际执行时才加载配置并创建客户端。
func rootAddClientCommand(cmdName string, newCmd func(newClient func(cmd *cobra.Command) (*client.APIClient, error)) *cobra.Command) *cobra.Command {
	// 构造延迟客户端工厂：在命令执行时才根据标志值创建客户端
	newClient := func(cmd *cobra.Command) (*client.APIClient, error) {
		cfg, err := config.Load(toConfigOptions())
		if err != nil {
			return nil, err
		}
		return newAPIClientFromConfig(cfg), nil
	}
	return newCmd(newClient)
}
