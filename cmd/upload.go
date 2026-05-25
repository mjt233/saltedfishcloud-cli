// Package cmd 定义 sfc-cli 的全部 CLI 命令。
// 当前文件实现 upload 子命令，支持上传单个本地文件或递归上传本地目录到远端。
package cmd

import (
	"fmt"

	"github.com/mjt233/saltedfishcloud-cli/internal/client"
	"github.com/mjt233/saltedfishcloud-cli/internal/config"
	"github.com/mjt233/saltedfishcloud-cli/internal/service"
	"github.com/spf13/cobra"
)

// newUploadCommand 构造并返回 upload 子命令。
// upload 接受一个本地路径和一个远端资源路径，将本地文件或目录上传到远端。
// 目录上传时会递归创建远端子目录并逐文件上传。
func newUploadCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "upload <localPath> <remoteResourcePath>",
		Short: "Upload local files or directories to remote",
		Long:  "Upload local files or directories to the specified remote resource path, supporting private and public resource areas. Directories are uploaded recursively.",
		Args:  withArgsHelp(cobra.ExactArgs(2)),
		RunE: func(cmd *cobra.Command, args []string) error {
			// 加载运行时配置；命令行标志优先级最高
			cfg, err := config.Load(toConfigOptions())
			if err != nil {
				return err
			}

			localPath := args[0]
			remotePath := args[1]

			// 构造服务依赖图
			cli := client.NewAPIClient(cfg.ServiceURL, cfg.APITicket)
			userSvc := service.NewUserService(cli)
			paths := service.NewPathService(userSvc.PrivateUID)
			diskSvc := service.NewDiskFileService(cli, paths)

			// 执行上传（单文件或目录均由 Upload 内部检测处理）
			if err := diskSvc.Upload(cmd.Context(), localPath, remotePath, cmd.OutOrStdout()); err != nil {
				return err
			}

			// 输出完成提示
			fmt.Fprintf(cmd.OutOrStdout(), "\nUpload complete: %s -> %s\n", localPath, remotePath)
			return nil
		},
	}
}
