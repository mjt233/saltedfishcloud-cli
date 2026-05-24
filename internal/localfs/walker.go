// Package localfs 提供本地文件系统的遍历与路径规范化辅助函数。
package localfs

import (
	"os"
	"path/filepath"
)

// EnsureDownloadTarget 根据远端文件名和用户指定的本地目标路径，返回最终下载目标路径。
// 若 userTarget 为空，则以 remoteName 作为本地路径；否则直接使用 userTarget。
func EnsureDownloadTarget(remoteName, userTarget string) string {
	if userTarget == "" {
		return remoteName
	}
	return userTarget
}

// EnsureParentDir 确保文件路径的父目录存在；若父目录不存在，则递归创建（含所有中间目录）。
// 若 target 路径不含目录部分（仅文件名），则不做任何操作并返回 nil。
func EnsureParentDir(target string) error {
	// 提取父目录路径
	dir := filepath.Dir(target)
	// "." 或空字符串表示当前目录，无需创建
	if dir == "." || dir == "" {
		return nil
	}
	return os.MkdirAll(dir, 0755)
}
