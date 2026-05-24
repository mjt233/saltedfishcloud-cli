// sfc-cli 是咸鱼云网盘的命令行客户端。
package main

import (
	"fmt"
	"io"
	"os"

	"github.com/mjt233/saltedfishcloud-cli/cmd"
)

// run 执行给定的命令函数，并在出错时将错误信息写入 stderr，返回对应的退出码。
func run(stderr io.Writer, execFn func() error) int {
	// 执行命令函数，出错时输出错误并返回非零退出码
	if err := execFn(); err != nil {
		fmt.Fprintln(stderr, err)
		return 1
	}
	return 0
}

// main 是程序入口，调用 run 并以返回的退出码结束进程。
func main() {
	os.Exit(run(os.Stderr, cmd.Execute))
}
