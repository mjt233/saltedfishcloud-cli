// sfc-cli 是咸鱼云网盘的命令行客户端。
package main

import (
	"os"

	"github.com/mjt233/saltedfishcloud-cli/cmd"
)

// main 是程序入口，执行根命令并在出错时以非零状态退出。
func main() {
	// 执行根命令，出错时退出程序
	if err := cmd.Execute(); err != nil {
		os.Exit(1)
	}
}
