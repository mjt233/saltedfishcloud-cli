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
