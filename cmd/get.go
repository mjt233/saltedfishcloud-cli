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

			// 构造服务依赖图
			cli := client.NewAPIClient(cfg.ServiceURL, cfg.APITicket)
			userSvc := service.NewUserService(cli)
			paths := service.NewPathService(userSvc.PrivateUID)
			diskSvc := service.NewDiskFileService(cli, paths)

			// 确定本地目标路径：若未指定，从解析后的远端路径中提取基础名称，
			// 避免原始字符串含资源域前缀（如 "public:file.bin"）导致名称错误。
			var localPath string
			if len(args) == 2 {
				localPath = args[1]
			} else {
				rp, err := paths.Resolve(cmd.Context(), remotePath)
				if err != nil {
					return err
				}
				localPath = localfs.EnsureDownloadTarget(path.Base(rp.Path), "")
				if localPath == "/" || localPath == "." || localPath == "" {
					return fmt.Errorf("无法为远端路径 %q 推导默认本地目标，请显式指定 localPath", rp.Path)
				}
			}

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
