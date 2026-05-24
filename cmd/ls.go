// Package cmd 定义 sfc-cli 的全部 CLI 命令。
// 当前文件实现 ls 子命令，列出远端目录的文件条目。
package cmd

import (
	"fmt"

	"github.com/mjt233/saltedfishcloud-cli/internal/client"
	"github.com/mjt233/saltedfishcloud-cli/internal/config"
	"github.com/mjt233/saltedfishcloud-cli/internal/service"
	"github.com/spf13/cobra"
)

// newLSCommand 构造并返回 ls 子命令。
// ls 接受一个路径参数，以制表符分隔的表格形式列出远端目录内容。
func newLSCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "ls <path>",
		Short: "列出远端目录内容",
		Long:  "列出指定远端路径下的文件和目录，支持 private 和 public 资源域。",
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

			// 查询远端目录列表
			entries, err := diskSvc.List(cmd.Context(), args[0])
			if err != nil {
				return err
			}

			// 输出表头行
			fmt.Fprintln(cmd.OutOrStdout(), "type\tname\tsize\tmtime")

			// 每条记录输出一行，字段间以制表符分隔
			for _, e := range entries {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%d\t%s\n", e.Type, e.Name, e.Size, e.Mtime)
			}
			return nil
		},
	}
}
