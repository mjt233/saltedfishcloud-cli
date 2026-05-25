// Package service 的跨资源域复制与移动服务测试文件。
package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mjt233/saltedfishcloud-cli/internal/client"
)

// TestCopierService_CopyRemoteToRemote 验证 Copy 将源路径和目标路径正确拆分，
// 并发送 POST 请求到 /api/openApi/diskFile/copy/v1，body 包含 sourceUid、targetUid、
// sourcePath、targetPath 和 files 数组。
func TestCopierService_CopyRemoteToRemote(t *testing.T) {
	var gotBody map[string]any

	// 启动测试服务器，捕获 copy 请求的 body
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/openApi/diskFile/fileList/v1":
			// 父目录 "/src" 的条目列表，目标文件以 dir=false 标识
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200,
				"data": []map[string]any{
					{"name": "file.txt", "dir": false, "size": "100", "mtime": "1778581444799"},
				},
				"msg": "OK",
			})
		case "/api/openApi/diskFile/copy/v1":
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200,
				"data": nil,
				"msg":  "OK",
			})
		default:
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	// 构造使用测试服务器的服务图
	cli := client.NewAPIClient(srv.URL, "ticket-1")
	paths := NewPathService(func(ctx context.Context) (int64, error) {
		return 42, nil
	})
	disk := NewDiskFileService(cli, paths)
	copier := NewCopierService(cli, disk, paths)

	// 复制 private:/src/file.txt 到 public:/dest/
	err := copier.Copy(context.Background(), "private:/src/file.txt", "public:/dest/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 验证 body 中的 sourceUid
	if gotBody["sourceUid"] != float64(42) {
		t.Fatalf("expected sourceUid=42, got %v", gotBody["sourceUid"])
	}
	// 验证 body 中的 targetUid（public 域为 0）
	if gotBody["targetUid"] != float64(0) {
		t.Fatalf("expected targetUid=0, got %v", gotBody["targetUid"])
	}
	// 验证 sourcePath
	if gotBody["sourcePath"] != "/src" {
		t.Fatalf("expected sourcePath=/src, got %v", gotBody["sourcePath"])
	}
	// 验证 targetPath
	if gotBody["targetPath"] != "/dest" {
		t.Fatalf("expected targetPath=/dest, got %v", gotBody["targetPath"])
	}
	// 验证 files 数组
	files, ok := gotBody["files"].([]any)
	if !ok || len(files) != 1 || files[0] != "file.txt" {
		t.Fatalf("expected files=[file.txt], got %v", gotBody["files"])
	}
}

// TestCopierService_MoveLocalToRemote_RemovesSourceOnSuccess 验证 local->remote 移动
// 先上传本地文件，成功后删除本地源文件。
func TestCopierService_MoveLocalToRemote_RemovesSourceOnSuccess(t *testing.T) {
	uploadCalled := false

	// 启动测试服务器，模拟上传成功
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/openApi/diskFile/upload/v1":
			uploadCalled = true
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200,
				"data": nil,
				"msg":  "OK",
			})
		default:
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	// 创建本地源文件
	base := t.TempDir()
	localFile := filepath.Join(base, "source.txt")
	if err := os.WriteFile(localFile, []byte("content"), 0644); err != nil {
		t.Fatalf("failed to create local file: %v", err)
	}

	// 构造服务图
	cli := client.NewAPIClient(srv.URL, "ticket-1")
	paths := NewPathService(func(ctx context.Context) (int64, error) { return 0, nil })
	disk := NewDiskFileService(cli, paths)
	copier := NewCopierService(cli, disk, paths)

	// 执行移动：local -> public:/dest/
	err := copier.Move(context.Background(), "local:"+localFile, "public:/dest/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 验证上传被调用
	if !uploadCalled {
		t.Fatal("expected upload endpoint to be called")
	}
	// 验证本地源文件已被 os.RemoveAll 删除
	if _, statErr := os.Stat(localFile); !os.IsNotExist(statErr) {
		t.Fatalf("expected local source %q to be removed, stat err=%v", localFile, statErr)
	}
}

// TestCopierService_MoveLocalDirToRemote_RemovesSourceDir 验证 local 目录->remote 移动
// 上传本地目录后，整个目录树被删除。
func TestCopierService_MoveLocalDirToRemote_RemovesSourceDir(t *testing.T) {
	// 启动测试服务器，模拟上传和创建目录成功
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 200,
			"data": nil,
			"msg":  "OK",
		})
	}))
	defer srv.Close()

	// 创建本地目录结构：base/sub/file.txt
	base := t.TempDir()
	subDir := filepath.Join(base, "sub")
	if err := os.MkdirAll(subDir, 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "file.txt"), []byte("data"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	// 构造服务图
	cli := client.NewAPIClient(srv.URL, "ticket-1")
	paths := NewPathService(func(ctx context.Context) (int64, error) { return 0, nil })
	disk := NewDiskFileService(cli, paths)
	copier := NewCopierService(cli, disk, paths)

	// 执行移动：local 目录 -> public:/dest/
	err := copier.Move(context.Background(), "local:"+base, "public:/dest/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 验证整个本地目录树已被删除
	if _, statErr := os.Stat(base); !os.IsNotExist(statErr) {
		t.Fatalf("expected local dir %q to be removed, stat err=%v", base, statErr)
	}
}

// TestCopierService_MoveRemoteToLocal_RemovesSourceOnSuccess 验证 remote->local 移动
// 先下载远端文件到本地，成功后删除远端源文件。
func TestCopierService_MoveRemoteToLocal_RemovesSourceOnSuccess(t *testing.T) {
	deleteCalled := false

	// 启动测试服务器：fileList 返回父目录条目，download 返回文件内容，delete 记录调用
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/openApi/diskFile/fileList/v1":
			// 父目录 "/" 的条目列表，目标文件以 dir=false 标识
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200,
				"data": []map[string]any{
					{"name": "remote.txt", "dir": false, "size": "14", "mtime": "1778581444799"},
				},
				"msg": "OK",
			})
		case "/api/openApi/diskFile/download/v1":
			_, _ = w.Write([]byte("remote content"))
		case "/api/openApi/diskFile/delete/v1":
			deleteCalled = true
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200,
				"data": nil,
				"msg":  "OK",
			})
		default:
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	// 创建本地目标目录
	base := t.TempDir()
	localTarget := filepath.Join(base, "downloaded.txt")

	// 构造服务图
	cli := client.NewAPIClient(srv.URL, "ticket-1")
	paths := NewPathService(func(ctx context.Context) (int64, error) { return 0, nil })
	disk := NewDiskFileService(cli, paths)
	copier := NewCopierService(cli, disk, paths)

	// 执行移动：public:/remote.txt -> local 目标文件
	err := copier.Move(context.Background(), "public:/remote.txt", "local:"+localTarget)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 验证本地文件已创建
	content, err := os.ReadFile(localTarget)
	if err != nil {
		t.Fatalf("failed to read local target: %v", err)
	}
	if string(content) != "remote content" {
		t.Fatalf("expected content %q, got %q", "remote content", string(content))
	}

	// 验证远端源文件被删除
	if !deleteCalled {
		t.Fatal("expected delete endpoint to be called for remote source removal")
	}
}

// TestCopierService_CopyRejectsLocalArea 验证 Copy 拒绝 local 资源域。
func TestCopierService_CopyRejectsLocalArea(t *testing.T) {
	cli := client.NewAPIClient("http://localhost:9999", "t")
	paths := NewPathService(func(ctx context.Context) (int64, error) { return 0, nil })
	disk := NewDiskFileService(cli, paths)
	copier := NewCopierService(cli, disk, paths)

	err := copier.Copy(context.Background(), "local:/some/path", "public:/dest")
	if err == nil {
		t.Fatal("expected error for local area, got nil")
	}
	if !strings.Contains(err.Error(), "local") {
		t.Fatalf("error should mention 'local', got: %s", err.Error())
	}
}

// TestCopierService_MoveRemoteToRemote_SendsCorrectBody 验证远端到远端移动发送正确的请求体。
func TestCopierService_MoveRemoteToRemote_SendsCorrectBody(t *testing.T) {
	var gotBody map[string]any

	// 启动测试服务器，捕获 move 请求的 body
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/openApi/diskFile/fileList/v1":
			// 父目录 "/src" 的条目列表，目标文件以 dir=false 标识
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200,
				"data": []map[string]any{
					{"name": "file.txt", "dir": false, "size": "100", "mtime": "1778581444799"},
				},
				"msg": "OK",
			})
		case "/api/openApi/diskFile/move/v1":
			_ = json.NewDecoder(r.Body).Decode(&gotBody)
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200,
				"data": nil,
				"msg":  "OK",
			})
		default:
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	// 构造服务图
	cli := client.NewAPIClient(srv.URL, "ticket-1")
	paths := NewPathService(func(ctx context.Context) (int64, error) { return 42, nil })
	disk := NewDiskFileService(cli, paths)
	copier := NewCopierService(cli, disk, paths)

	// 执行移动：private:/src/file.txt -> public:/dest/
	err := copier.Move(context.Background(), "private:/src/file.txt", "public:/dest/")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 验证 body 中的字段
	if gotBody["sourceUid"] != float64(42) {
		t.Fatalf("expected sourceUid=42, got %v", gotBody["sourceUid"])
	}
	if gotBody["targetUid"] != float64(0) {
		t.Fatalf("expected targetUid=0, got %v", gotBody["targetUid"])
	}
	if gotBody["sourcePath"] != "/src" {
		t.Fatalf("expected sourcePath=/src, got %v", gotBody["sourcePath"])
	}
	if gotBody["targetPath"] != "/dest" {
		t.Fatalf("expected targetPath=/dest, got %v", gotBody["targetPath"])
	}
	files, ok := gotBody["files"].([]any)
	if !ok || len(files) != 1 || files[0] != "file.txt" {
		t.Fatalf("expected files=[file.txt], got %v", gotBody["files"])
	}
}
