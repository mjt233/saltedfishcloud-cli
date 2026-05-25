// Package cmd 定义 sfc-cli 的全部 CLI 命令。
// 当前文件实现 rename 子命令，重命名远端文件或目录。
package cmd

import (
	"fmt"

	"github.com/mjt233/saltedfishcloud-cli/internal/client"
	"github.com/mjt233/saltedfishcloud-cli/internal/config"
	"github.com/mjt233/saltedfishcloud-cli/internal/service"
	"github.com/spf13/cobra"
)

// newRenameCommand 构造并返回 rename 子命令。
// rename 接受一个远端资源路径和一个新名称，将远端文件或目录重命名。
func newRenameCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "rename <sourceResourcePath> <newName>",
		Short: "Rename remote files or directories",
		Long:  "Rename the file or directory at the specified remote path to a new name, supporting private and public resource areas.",
		Args:  withArgsHelp(cobra.ExactArgs(2)),
		RunE: func(cmd *cobra.Command, args []string) error {
			// 加载运行时配置；命令行标志优先级最高
			cfg, err := config.Load(toConfigOptions())
			if err != nil {
				return err
			}

			sourcePath := args[0]
			newName := args[1]

			// 构造服务依赖图
			cli := client.NewAPIClient(cfg.ServiceURL, cfg.APITicket)
			userSvc := service.NewUserService(cli)
			paths := service.NewPathService(userSvc.PrivateUID)
			diskSvc := service.NewDiskFileService(cli, paths)

			// 执行重命名操作
			if err := diskSvc.Rename(cmd.Context(), sourcePath, newName); err != nil {
				return err
			}

			// 输出完成提示
			fmt.Fprintf(cmd.OutOrStdout(), "Rename successful: %s -> %s\n", sourcePath, newName)
			return nil
		},
	}
}
