// Package cmd 定义 sfc-cli 的全部 CLI 命令。
// 当前文件实现 remoteVersion 子命令，查询远端服务版本号。
package cmd

import (
	"fmt"

	"github.com/mjt233/saltedfishcloud-cli/internal/client"
	"github.com/spf13/cobra"
)

// newRemoteVersionCommand 创建 remoteVersion 子命令。
// 调用 /api/hello/feature 获取服务端版本号并输出。
// 注意：该接口返回体顶层有 version 字段，不在 data 中。
func newRemoteVersionCommand(newClient func(cmd *cobra.Command) (*client.APIClient, error)) *cobra.Command {
	return &cobra.Command{
		Use:   "remoteVersion",
		Short: "查询服务端版本号",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			// 通过工厂函数构造 API 客户端
			cli, err := newClient(cmd)
			if err != nil {
				return err
			}

			// 调用远端接口获取服务端版本号
			ver, err := cli.RemoteVersion(cmd.Context())
			if err != nil {
				return err
			}

			// 输出版本号到标准输出
			fmt.Fprintln(cmd.OutOrStdout(), ver)
			return nil
		},
	}
}
