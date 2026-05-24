// Package cmd 定义 sfc-cli 的全部 CLI 命令。
// 当前文件实现 rm 子命令，删除远端文件或目录。
package cmd

import (
	"fmt"

	"github.com/mjt233/saltedfishcloud-cli/internal/client"
	"github.com/mjt233/saltedfishcloud-cli/internal/config"
	"github.com/mjt233/saltedfishcloud-cli/internal/service"
	"github.com/spf13/cobra"
)

// newRmCommand 构造并返回 rm 子命令。
// rm 接受一个远端资源路径，删除对应的文件或目录。
func newRmCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <targetResourcePath>",
		Short: "删除远端文件或目录",
		Long:  "删除指定远端路径的文件或目录，支持 private 和 public 资源域。",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			// 加载运行时配置；命令行标志优先级最高
			cfg, err := config.Load(toConfigOptions())
			if err != nil {
				return err
			}

			// 构造服务依赖图
			cli := client.NewAPIClient(cfg.ServiceURL, cfg.APITicket)
			userSvc := service.NewUserService(cli)
			paths := service.NewPathService(userSvc.PrivateUID)
			diskSvc := service.NewDiskFileService(cli, paths)

			// 执行删除操作
			if err := diskSvc.Remove(cmd.Context(), args[0]); err != nil {
				return err
			}

			// 输出完成提示
			fmt.Fprintf(cmd.OutOrStdout(), "删除成功: %s\n", args[0])
			return nil
		},
	}
}
