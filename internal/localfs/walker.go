// Package localfs 提供本地文件系统的遍历与路径规范化辅助函数。
package localfs

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// WalkEntry 表示本地文件系统遍历过程中的一个条目。
type WalkEntry struct {
	// AbsolutePath 是该条目在操作系统中的绝对路径，使用 OS 原生路径分隔符。
	AbsolutePath string
	// RelativePath 是相对于 Walk 传入的根路径的相对路径，使用 OS 原生路径分隔符。
	// 若根路径为文件，则 RelativePath 为文件名（不含路径）。
	RelativePath string
	// IsDir 为 true 时表示此条目是目录，false 时表示文件。
	IsDir bool
}

// Walk 遍历指定根路径并返回所有条目。
//
// 若 root 是文件，返回单个 WalkEntry，RelativePath 为文件名。
// 若 root 是目录，递归返回所有子文件和子目录条目，RelativePath 相对于 root 计算；
// 目录条目先于其内容条目出现，以保证 mkdir 编排顺序正确。
// root 本身（当为目录时）不包含在返回结果中。
// 若 root 不存在，返回明确错误。
func Walk(root string) ([]WalkEntry, error) {
	// 获取规范化绝对路径，消除相对路径和符号链接中的 ".." 等
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("无法解析路径 %q: %w", root, err)
	}

	// 检查路径是否存在并获取元信息
	rootInfo, err := os.Stat(absRoot)
	if err != nil {
		return nil, fmt.Errorf("路径 %q 不存在或无法访问: %w", root, err)
	}

	// 若根路径是文件，直接返回单个条目
	if !rootInfo.IsDir() {
		return []WalkEntry{
			{
				AbsolutePath: absRoot,
				RelativePath: filepath.Base(absRoot),
				IsDir:        false,
			},
		}, nil
	}

	// 若根路径是目录，递归遍历所有子条目
	var entries []WalkEntry
	err = filepath.WalkDir(absRoot, func(p string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		// 跳过根目录本身
		if p == absRoot {
			return nil
		}
		// 计算相对于根目录的路径
		relPath, err := filepath.Rel(absRoot, p)
		if err != nil {
			return fmt.Errorf("计算相对路径失败: %w", err)
		}
		entries = append(entries, WalkEntry{
			AbsolutePath: p,
			RelativePath: relPath,
			IsDir:        d.IsDir(),
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("遍历目录 %q 失败: %w", root, err)
	}
	return entries, nil
}

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
