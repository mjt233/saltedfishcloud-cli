// Package cmd 定义 sfc-cli 的全部 CLI 命令。
// 当前文件实现 cp 子命令，复制远端文件或目录。
package cmd

import (
	"fmt"

	"github.com/mjt233/saltedfishcloud-cli/internal/client"
	"github.com/mjt233/saltedfishcloud-cli/internal/config"
	"github.com/mjt233/saltedfishcloud-cli/internal/service"
	"github.com/spf13/cobra"
)

// newCpCommand 构造并返回 cp 子命令。
// cp 接受源资源路径和目标资源路径，将远端文件或目录从源复制到目标。
func newCpCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "cp <sourceResourcePath> <targetResourcePath>",
		Short: "复制远端文件或目录",
		Long:  "将远端文件或目录从源路径复制到目标路径，支持 private 和 public 资源域。",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			// 加载运行时配置；命令行标志优先级最高
			cfg, err := config.Load(toConfigOptions())
			if err != nil {
				return err
			}

			sourcePath := args[0]
			targetPath := args[1]

			// 构造服务依赖图
			cli := client.NewAPIClient(cfg.ServiceURL, cfg.APITicket)
			userSvc := service.NewUserService(cli)
			paths := service.NewPathService(userSvc.PrivateUID)
			diskSvc := service.NewDiskFileService(cli, paths)
			copierSvc := service.NewCopierService(cli, diskSvc, paths)

			// 执行复制操作
			if err := copierSvc.Copy(cmd.Context(), sourcePath, targetPath); err != nil {
				return err
			}

			// 输出完成提示
			fmt.Fprintf(cmd.OutOrStdout(), "复制完成: %s -> %s\n", sourcePath, targetPath)
			return nil
		},
	}
}
