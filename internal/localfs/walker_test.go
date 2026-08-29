// Package localfs 的辅助函数测试文件。
package localfs_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mjt233/saltedfishcloud-cli/internal/localfs"
)

// TestEnsureDownloadTarget_EmptyTarget_ReturnsRemoteName 验证当用户目标路径为空时，使用远端文件名作为本地路径。
func TestEnsureDownloadTarget_EmptyTarget_ReturnsRemoteName(t *testing.T) {
	got := localfs.EnsureDownloadTarget("report.txt", "")
	if got != "report.txt" {
		t.Fatalf("expected %q, got %q", "report.txt", got)
	}
}

// TestEnsureDownloadTarget_WithTarget_ReturnsTarget 验证当用户指定目标路径时，直接使用该路径。
func TestEnsureDownloadTarget_WithTarget_ReturnsTarget(t *testing.T) {
	got := localfs.EnsureDownloadTarget("report.txt", "local.txt")
	if got != "local.txt" {
		t.Fatalf("expected %q, got %q", "local.txt", got)
	}
}

// TestEnsureParentDir_CreatesParentDirectory 验证父目录不存在时会自动创建。
func TestEnsureParentDir_CreatesParentDirectory(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "subdir", "file.txt")

	if err := localfs.EnsureParentDir(target); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 验证父目录已被创建
	if _, err := os.Stat(filepath.Join(base, "subdir")); os.IsNotExist(err) {
		t.Fatal("expected parent directory to be created, but it does not exist")
	}
}

// TestEnsureParentDir_AlreadyExists_NoError 验证父目录已存在时不报错。
func TestEnsureParentDir_AlreadyExists_NoError(t *testing.T) {
	base := t.TempDir()
	target := filepath.Join(base, "file.txt")

	if err := localfs.EnsureParentDir(target); err != nil {
		t.Fatalf("unexpected error when parent already exists: %v", err)
	}
}

// TestEnsureParentDir_NoParent_NoError 验证路径中不含目录分隔符时不报错。
func TestEnsureParentDir_NoParent_NoError(t *testing.T) {
	if err := localfs.EnsureParentDir("file.txt"); err != nil {
		t.Fatalf("unexpected error for flat path: %v", err)
	}
}

// TestWalk_NonExistent_ReturnsError 验证对不存在的路径调用 Walk 时返回明确错误。
func TestWalk_NonExistent_ReturnsError(t *testing.T) {
	_, err := localfs.Walk(filepath.Join(t.TempDir(), "nonexistent.txt"))
	if err == nil {
		t.Fatal("expected error for non-existent path, got nil")
	}
}

// TestWalk_SingleFile_ReturnsSingleEntry 验证对单个文件调用 Walk 时返回一个非目录条目，
// RelativePath 为文件基础名，AbsolutePath 为文件绝对路径。
func TestWalk_SingleFile_ReturnsSingleEntry(t *testing.T) {
	base := t.TempDir()
	filePath := filepath.Join(base, "hello.txt")
	if err := os.WriteFile(filePath, []byte("content"), 0644); err != nil {
		t.Fatalf("failed to create test file: %v", err)
	}

	entries, err := localfs.Walk(filePath)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 单文件应返回恰好一个条目
	if len(entries) != 1 {
		t.Fatalf("expected 1 entry, got %d", len(entries))
	}
	e := entries[0]
	if e.IsDir {
		t.Fatal("expected IsDir=false for file entry")
	}
	// RelativePath 应为文件基础名
	if e.RelativePath != "hello.txt" {
		t.Fatalf("expected RelativePath=%q, got %q", "hello.txt", e.RelativePath)
	}
	// AbsolutePath 应指向文件本身
	if e.AbsolutePath != filePath {
		t.Fatalf("expected AbsolutePath=%q, got %q", filePath, e.AbsolutePath)
	}
}

// TestWalk_Directory_IncludesAllEntriesWithRelativePaths 验证对目录调用 Walk 时，
// 返回所有文件和子目录条目，RelativePath 相对于 root 计算，不包含 root 本身。
func TestWalk_Directory_IncludesAllEntriesWithRelativePaths(t *testing.T) {
	base := t.TempDir()

	// 创建目录结构：base/a.txt, base/sub/(dir), base/sub/b.txt
	if err := os.WriteFile(filepath.Join(base, "a.txt"), []byte("a"), 0644); err != nil {
		t.Fatalf("failed to create a.txt: %v", err)
	}
	subDir := filepath.Join(base, "sub")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("failed to create sub dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "b.txt"), []byte("b"), 0644); err != nil {
		t.Fatalf("failed to create b.txt: %v", err)
	}

	entries, err := localfs.Walk(base)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 预期 3 个条目：a.txt, sub, sub/b.txt（不含 root 本身）
	if len(entries) != 3 {
		t.Fatalf("expected 3 entries, got %d: %v", len(entries), entries)
	}

	// 用 map 方便按 RelativePath 查找
	byRel := make(map[string]localfs.WalkEntry)
	for _, e := range entries {
		byRel[e.RelativePath] = e
	}

	// 验证 a.txt 存在且为文件
	if e, ok := byRel["a.txt"]; !ok || e.IsDir {
		t.Fatalf("expected file entry for a.txt, got: %v, ok=%v", byRel["a.txt"], ok)
	}
	// 验证 sub 存在且为目录
	if e, ok := byRel["sub"]; !ok || !e.IsDir {
		t.Fatalf("expected dir entry for sub, got: %v, ok=%v", byRel["sub"], ok)
	}
	// 验证 sub/b.txt 存在（使用 OS 原生路径分隔符）
	subBTxt := filepath.Join("sub", "b.txt")
	if e, ok := byRel[subBTxt]; !ok || e.IsDir {
		t.Fatalf("expected file entry for %s, got: %v, ok=%v", subBTxt, byRel[subBTxt], ok)
	}
}

// TestWalk_Directory_DirsAppearBeforeContents 验证目录条目在其内容之前出现，
// 以确保 mkdir 编排时父目录先于子条目被处理。
func TestWalk_Directory_DirsAppearBeforeContents(t *testing.T) {
	base := t.TempDir()
	subDir := filepath.Join(base, "sub")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("failed to create sub dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "file.txt"), []byte("f"), 0644); err != nil {
		t.Fatalf("failed to create file.txt: %v", err)
	}

	entries, err := localfs.Walk(base)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 找出 sub 和 sub/file.txt 的位置
	subIdx, fileIdx := -1, -1
	subFile := filepath.Join("sub", "file.txt")
	for i, e := range entries {
		if e.RelativePath == "sub" {
			subIdx = i
		}
		if e.RelativePath == subFile {
			fileIdx = i
		}
	}
	if subIdx == -1 || fileIdx == -1 {
		t.Fatalf("missing expected entries; got: %v", entries)
	}
	// 目录必须先于其子文件出现
	if subIdx >= fileIdx {
		t.Fatalf("expected sub (idx=%d) to appear before sub/file.txt (idx=%d)", subIdx, fileIdx)
	}
}

// TestWalk_MarksSymlinksAndSizes 验证 Walk 为符号链接条目标记 IsSymlink，
// 并为普通文件填充 Size（空文件为 0）。符号链接不可创建时跳过该部分断言。
func TestWalk_MarksSymlinksAndSizes(t *testing.T) {
	base := t.TempDir()

	// a.txt 内容 7 字节；empty.bin 为 0 字节空文件
	if err := os.WriteFile(filepath.Join(base, "a.txt"), []byte("1234567"), 0644); err != nil {
		t.Fatalf("failed to create a.txt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(base, "empty.bin"), nil, 0644); err != nil {
		t.Fatalf("failed to create empty.bin: %v", err)
	}

	// link.txt 指向 a.txt；无符号链接权限的环境跳过该断言
	haveSymlink := true
	if err := os.Symlink(filepath.Join(base, "a.txt"), filepath.Join(base, "link.txt")); err != nil {
		t.Logf("symlink unavailable, skipping symlink assertions: %v", err)
		haveSymlink = false
	}

	entries, err := localfs.Walk(base)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	byRel := make(map[string]localfs.WalkEntry)
	for _, e := range entries {
		byRel[e.RelativePath] = e
	}

	// a.txt：普通文件，Size 为 7
	if e, ok := byRel["a.txt"]; !ok || e.IsSpecial || e.Size != 7 {
		t.Fatalf("unexpected entry for a.txt: %+v, ok=%v", byRel["a.txt"], ok)
	}
	// empty.bin：普通文件，Size 为 0
	if e, ok := byRel["empty.bin"]; !ok || e.IsSpecial || e.Size != 0 {
		t.Fatalf("unexpected entry for empty.bin: %+v, ok=%v", byRel["empty.bin"], ok)
	}
	// link.txt：标记为特殊条目（符号链接）
	if haveSymlink {
		if e, ok := byRel["link.txt"]; !ok || !e.IsSpecial {
			t.Fatalf("expected link.txt to be marked as special: %+v, ok=%v", byRel["link.txt"], ok)
		}
	}
}
