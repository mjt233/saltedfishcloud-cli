// Package cmd 定义 sfc-cli 的全部 CLI 命令。
// 当前文件实现 mv 子命令，移动远端文件或目录。
package cmd

import (
	"fmt"

	"github.com/mjt233/saltedfishcloud-cli/internal/config"
	"github.com/mjt233/saltedfishcloud-cli/internal/service"
	"github.com/spf13/cobra"
)

// newMvCommand 构造并返回 mv 子命令。
// mv 接受源资源路径和目标资源路径，将文件或目录从源移动到目标。
// 支持远端到远端、本地到远端、远端到本地的移动。
func newMvCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "mv <sourceResourcePath> <targetResourcePath>",
		Short: "Move remote files or directories",
		Long:  "Move files or directories from source path to target path, supporting remote-to-remote, local-to-remote, and remote-to-local.",
		Args:  withArgsHelp(cobra.ExactArgs(2)),
		RunE: func(cmd *cobra.Command, args []string) error {
			// 加载运行时配置；命令行标志优先级最高
			cfg, err := config.Load(toConfigOptions())
			if err != nil {
				return err
			}

			sourcePath := args[0]
			targetPath := args[1]

			// 构造服务依赖图
			cli := newAPIClientFromConfig(cfg)
			userSvc := service.NewUserService(cli)
			paths := service.NewPathService(userSvc.PrivateUID)
			diskSvc := service.NewDiskFileService(cli, paths)
			copierSvc := service.NewCopierService(cli, diskSvc, paths)

			// 执行移动操作
			if err := copierSvc.Move(cmd.Context(), sourcePath, targetPath); err != nil {
				return err
			}

			// 输出完成提示
			fmt.Fprintf(cmd.OutOrStdout(), "Move complete: %s -> %s\n", sourcePath, targetPath)
			return nil
		},
	}
}
