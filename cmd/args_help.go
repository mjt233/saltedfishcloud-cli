// Package cmd 定义 sfc-cli 的全部 CLI 命令。
// 当前文件提供命令参数校验的统一帮助输出包装。
package cmd

import "github.com/spf13/cobra"

// withArgsHelp 将 Cobra 位置参数校验器包装为带帮助输出的版本。
// 当参数数量不满足校验条件时，先向标准输出写入当前命令的帮助文本，
// 再返回原始校验错误，使用户能同时看到错误原因和正确用法。
func withArgsHelp(validate cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := validate(cmd, args); err != nil {
			// 输出当前命令的帮助文本，辅助用户了解正确用法
			_ = cmd.Help()
			return err
		}
		return nil
	}
}
