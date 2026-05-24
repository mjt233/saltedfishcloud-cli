// Package cmd 定义 sfc-cli 的全部 CLI 命令。
// 当前文件实现 get 子命令，支持下载单个远端文件或递归下载远端目录。
package cmd

import (
	"fmt"
	"path"

	"github.com/mjt233/saltedfishcloud-cli/internal/client"
	"github.com/mjt233/saltedfishcloud-cli/internal/config"
	"github.com/mjt233/saltedfishcloud-cli/internal/localfs"
	"github.com/mjt233/saltedfishcloud-cli/internal/service"
	"github.com/spf13/cobra"
)

// newGetCommand 构造并返回 get 子命令。
// get 接受一个远端资源路径和可选的本地目标路径，将远端文件或目录下载到本地。
func newGetCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "get <remoteResourcePath> [localPath]",
		Short: "下载远端文件或目录",
		Long:  "下载指定远端路径的文件或目录到本地，支持 private 和 public 资源域。目录会被递归下载。",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			// 加载运行时配置；命令行标志优先级最高
			cfg, err := config.Load(toConfigOptions())
			if err != nil {
				return err
			}

			remotePath := args[0]

			// 确定本地目标路径：若未指定，则使用远端路径的基础名称
			var userTarget string
			if len(args) == 2 {
				userTarget = args[1]
			}
			// 提取远端路径的最后一段作为默认本地名称（使用正斜杠路径规则）
			localPath := localfs.EnsureDownloadTarget(path.Base(remotePath), userTarget)

			// 构造服务依赖图
			cli := client.NewAPIClient(cfg.ServiceURL, cfg.APITicket)
			userSvc := service.NewUserService(cli)
			paths := service.NewPathService(userSvc.PrivateUID)
			diskSvc := service.NewDiskFileService(cli, paths)

			// 执行下载（文件或目录均由 Download 内部检测处理）
			if err := diskSvc.Download(cmd.Context(), remotePath, localPath, cmd.OutOrStdout()); err != nil {
				return err
			}

			// 输出完成提示
			fmt.Fprintf(cmd.OutOrStdout(), "\n下载完成: %s\n", localPath)
			return nil
		},
	}
}
