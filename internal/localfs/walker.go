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
	// IsSpecial 为 true 表示此条目不是普通文件或目录：包括符号链接、
	// Windows 目录联接（mklink /J）等重解析点，以及 FIFO、设备文件等特殊文件。
	// Walk 不跟随此类条目；上传等场景应跳过并警告，根路径本身不标记（根路径按 os.Stat 跟随链接）。
	IsSpecial bool
	// Size 是条目大小（字节），仅对普通文件有意义；无法获取时为 -1（未知）。
	Size int64
}

// Walk 遍历指定根路径并返回所有条目。
//
// 若 root 是文件，返回单个 WalkEntry，RelativePath 为文件名。
// 若 root 是目录，递归返回所有子文件和子目录条目，RelativePath 相对于 root 计算；
// 目录条目先于其内容条目出现，以保证 mkdir 编排顺序正确。
// root 本身（当为目录时）不包含在返回结果中。
// 目录内的符号链接、目录联接等非普通条目不跟随，以 IsSpecial 标记，由调用方决定跳过或其他处理。
// 若 root 不存在，返回明确错误。
func Walk(root string) ([]WalkEntry, error) {
	// 获取规范化绝对路径，消除相对路径和符号链接中的 ".." 等
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("cannot resolve path %q: %w", root, err)
	}

	// 检查路径是否存在并获取元信息
	rootInfo, err := os.Stat(absRoot)
	if err != nil {
		return nil, fmt.Errorf("path %q does not exist or is inaccessible: %w", root, err)
	}

	// 若根路径是文件，直接返回单个条目（根路径按 os.Stat 跟随符号链接，不标记 IsSymlink）
	if !rootInfo.IsDir() {
		return []WalkEntry{
			{
				AbsolutePath: absRoot,
				RelativePath: filepath.Base(absRoot),
				IsDir:        false,
				Size:         rootInfo.Size(),
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
			return fmt.Errorf("failed to compute relative path: %w", err)
		}
		// 识别非普通条目：符号链接与 Windows 目录联接等重解析点（Go 1.24+ 在目录遍历中
		// 对联接报告 ModeIrregular 而非 ModeSymlink），以及 FIFO、设备文件等特殊文件。
		// 这类条目不能按普通文件读取内容，统一标记为 IsSpecial 由调用方跳过处理。
		isSpecial := d.Type()&(fs.ModeSymlink|fs.ModeIrregular) != 0
		// 获取条目大小；获取失败时以 -1 表示未知
		var size int64 = -1
		if info, infoErr := d.Info(); infoErr == nil {
			size = info.Size()
		}
		entries = append(entries, WalkEntry{
			AbsolutePath: p,
			RelativePath: relPath,
			IsDir:        d.IsDir(),
			IsSpecial:    isSpecial,
			Size:         size,
		})
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("failed to walk directory %q: %w", root, err)
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
