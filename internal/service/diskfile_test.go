// Package service 的磁盘文件服务测试文件。
package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mjt233/saltedfishcloud-cli/internal/client"
)

// TestDiskFileService_List_ForwardsUIDAndPath 验证 List 将解析后的 uid 和 path 作为查询参数转发给后端接口。
func TestDiskFileService_List_ForwardsUIDAndPath(t *testing.T) {
	var gotUID, gotPath string

	// 启动测试服务器，捕获请求中的查询参数
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUID = r.URL.Query().Get("uid")
		gotPath = r.URL.Query().Get("path")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 200,
			"data": []any{},
			"msg":  "OK",
		})
	}))
	defer srv.Close()

	// 构造使用测试服务器的服务图
	cli := client.NewAPIClient(srv.URL, "ticket-1")
	paths := NewPathService(func(ctx context.Context) (int64, error) {
		return 42, nil
	})
	svc := NewDiskFileService(cli, paths)

	// 使用 private 域路径，uid 应由 privateUID 回调提供
	_, err := svc.List(context.Background(), "private:/my/dir")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 验证 uid 参数
	if gotUID != "42" {
		t.Fatalf("expected uid=42, got %q", gotUID)
	}
	// 验证 path 参数
	if gotPath != "/my/dir" {
		t.Fatalf("expected path=/my/dir, got %q", gotPath)
	}
}

// TestDiskFileService_List_DecodesEntries 验证 List 将后端返回的 data 数组正确解码为 []DiskEntry。
func TestDiskFileService_List_DecodesEntries(t *testing.T) {
	// 启动测试服务器，返回两条 DiskEntry 记录
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 200,
			"data": []map[string]any{
				{"name": "file.txt", "dir": false, "size": "1024", "mtime": "1778581444799"},
				{"name": "folder", "dir": true, "size": "-1", "mtime": "1778674063900"},
			},
			"msg": "OK",
		})
	}))
	defer srv.Close()

	// 构造使用测试服务器的服务图；public 域 uid 固定为 0
	cli := client.NewAPIClient(srv.URL, "ticket-1")
	paths := NewPathService(func(ctx context.Context) (int64, error) {
		return 0, nil
	})
	svc := NewDiskFileService(cli, paths)

	result, err := svc.List(context.Background(), "public:/demo")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 验证条目数量
	if len(result) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(result))
	}
	// 验证第一条记录字段
	if result[0].Name != "file.txt" {
		t.Fatalf("expected entry[0].Name=file.txt, got %q", result[0].Name)
	}
	if result[0].Size != 1024 {
		t.Fatalf("expected entry[0].Size=1024, got %d", result[0].Size)
	}
	if result[0].Type != "file" {
		t.Fatalf("expected entry[0].Type=file, got %q", result[0].Type)
	}
	// 验证第二条记录字段
	if result[1].Name != "folder" {
		t.Fatalf("expected entry[1].Name=folder, got %q", result[1].Name)
	}
	if result[1].Type != "dir" {
		t.Fatalf("expected entry[1].Type=dir, got %q", result[1].Type)
	}
}

// TestDiskFileService_List_ReturnsErrorForLocalArea 验证 local 资源域在当前切片中返回明确错误。
func TestDiskFileService_List_ReturnsErrorForLocalArea(t *testing.T) {
	// local 域不走远端接口，List 应立即返回错误
	cli := client.NewAPIClient("http://localhost:9999", "t")
	paths := NewPathService(func(ctx context.Context) (int64, error) {
		return 0, nil
	})
	svc := NewDiskFileService(cli, paths)

	_, err := svc.List(context.Background(), "local:/some/path")
	if err == nil {
		t.Fatal("expected error for local area, got nil")
	}
	// 错误信息中应包含 "local" 关键字，方便用户理解
	if !strings.Contains(err.Error(), "local") {
		t.Fatalf("error should mention 'local', got: %s", err.Error())
	}
}

// TestDiskFileService_Download_SingleFile_WritesContent 验证单文件下载将远端内容写入本地文件。
func TestDiskFileService_Download_SingleFile_WritesContent(t *testing.T) {
	// 启动测试服务器：fileList 接口返回业务错误（路径是文件而非目录），download 接口返回文件内容
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/openApi/diskFile/fileList/v1":
			// 返回业务错误，表明该路径不是目录
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":         200,
				"businessCode": 40001,
				"msg":          "path is not a directory",
				"data":         nil,
			})
		case "/api/openApi/diskFile/download/v1":
			w.Header().Set("Content-Length", "12")
			_, _ = w.Write([]byte("hello world!"))
		}
	}))
	defer srv.Close()

	// 构造使用测试服务器的服务图
	cli := client.NewAPIClient(srv.URL, "ticket-1")
	paths := NewPathService(func(ctx context.Context) (int64, error) { return 0, nil })
	svc := NewDiskFileService(cli, paths)

	// 在临时目录下创建下载目标路径
	base := t.TempDir()
	target := filepath.Join(base, "file.txt")

	var buf bytes.Buffer
	if err := svc.Download(context.Background(), "public:/file.txt", target, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 验证本地文件内容与远端相符
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("failed to read downloaded file: %v", err)
	}
	if string(content) != "hello world!" {
		t.Fatalf("expected content %q, got %q", "hello world!", string(content))
	}
}

// TestDiskFileService_Download_Directory_RecursivelyCreatesFiles 验证目录下载会递归创建所有文件。
func TestDiskFileService_Download_Directory_RecursivelyCreatesFiles(t *testing.T) {
	// 启动测试服务器：
	// - /dir 的 fileList 返回一个文件和一个子目录
	// - /dir/subdir 的 fileList 返回一个文件
	// - download 接口按路径返回不同内容
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/openApi/diskFile/fileList/v1":
			reqPath := r.URL.Query().Get("path")
			switch reqPath {
			case "/dir":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"code": 200,
					"data": []map[string]any{
						{"name": "file.txt", "dir": false, "size": "6", "mtime": "1778581444799"},
						{"name": "subdir", "dir": true, "size": "-1", "mtime": "1778581444799"},
					},
					"msg": "OK",
				})
			case "/dir/subdir":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"code": 200,
					"data": []map[string]any{
						{"name": "nested.txt", "dir": false, "size": "6", "mtime": "1778581444799"},
					},
					"msg": "OK",
				})
			default:
				// 其他路径视为文件，返回业务错误
				_ = json.NewEncoder(w).Encode(map[string]any{
					"code":         200,
					"businessCode": 40001,
					"msg":          "not a directory",
					"data":         nil,
				})
			}
		case "/api/openApi/diskFile/download/v1":
			reqPath := r.URL.Query().Get("path")
			switch reqPath {
			case "/dir/file.txt":
				_, _ = w.Write([]byte("cont1\n"))
			case "/dir/subdir/nested.txt":
				_, _ = w.Write([]byte("cont2\n"))
			default:
				http.Error(w, "not found", http.StatusNotFound)
			}
		}
	}))
	defer srv.Close()

	// 构造使用测试服务器的服务图
	cli := client.NewAPIClient(srv.URL, "ticket-1")
	paths := NewPathService(func(ctx context.Context) (int64, error) { return 0, nil })
	svc := NewDiskFileService(cli, paths)

	// 在临时目录中创建输出根目录
	base := t.TempDir()
	outputDir := filepath.Join(base, "output")

	var buf bytes.Buffer
	if err := svc.Download(context.Background(), "public:/dir", outputDir, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 验证 output/file.txt 内容正确
	c1, err := os.ReadFile(filepath.Join(outputDir, "file.txt"))
	if err != nil {
		t.Fatalf("failed to read output/file.txt: %v", err)
	}
	if string(c1) != "cont1\n" {
		t.Fatalf("expected output/file.txt=%q, got %q", "cont1\n", string(c1))
	}

	// 验证 output/subdir/nested.txt 内容正确
	c2, err := os.ReadFile(filepath.Join(outputDir, "subdir", "nested.txt"))
	if err != nil {
		t.Fatalf("failed to read output/subdir/nested.txt: %v", err)
	}
	if string(c2) != "cont2\n" {
		t.Fatalf("expected output/subdir/nested.txt=%q, got %q", "cont2\n", string(c2))
	}
}

// TestDiskFileService_Download_LocalArea_ReturnsError 验证 local 资源域在 Download 中返回明确错误。
func TestDiskFileService_Download_LocalArea_ReturnsError(t *testing.T) {
	cli := client.NewAPIClient("http://localhost:9999", "t")
	paths := NewPathService(func(ctx context.Context) (int64, error) { return 0, nil })
	svc := NewDiskFileService(cli, paths)

	var buf bytes.Buffer
	err := svc.Download(context.Background(), "local:/some/path", "output.txt", &buf)
	if err == nil {
		t.Fatal("expected error for local area, got nil")
	}
	// 错误信息中应包含 "local" 关键字
	if !strings.Contains(err.Error(), "local") {
		t.Fatalf("error should mention 'local', got: %s", err.Error())
	}
}

// TestDiskFileService_Download_ListHTTPError_DoesNotFallback 验证当 fileList 接口返回
// 非"路径非目录"错误（如 HTTP 500）时，Download 立即返回该错误，不回退到文件下载。
func TestDiskFileService_Download_ListHTTPError_DoesNotFallback(t *testing.T) {
	downloadCalled := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/openApi/diskFile/fileList/v1":
			// HTTP 500 不是业务错误，不应触发回退
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte("server error"))
		case "/api/openApi/diskFile/download/v1":
			downloadCalled = true
			_, _ = w.Write([]byte("should not reach here"))
		}
	}))
	defer srv.Close()

	cli := client.NewAPIClient(srv.URL, "t")
	paths := NewPathService(func(ctx context.Context) (int64, error) { return 0, nil })
	svc := NewDiskFileService(cli, paths)

	base := t.TempDir()
	target := filepath.Join(base, "output.txt")

	var buf bytes.Buffer
	err := svc.Download(context.Background(), "public:/file.txt", target, &buf)
	if err == nil {
		t.Fatal("expected error when fileList returns HTTP 500, got nil")
	}
	if downloadCalled {
		t.Fatal("download endpoint should not be called when fileList fails with HTTP error")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("error should mention HTTP 500, got: %s", err.Error())
	}
}

// TestDiskFileService_Download_PartialFileCleanedUpOnFailure 验证下载失败（连接中断）时
// 已创建的不完整本地文件会被自动删除。
func TestDiskFileService_Download_PartialFileCleanedUpOnFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/openApi/diskFile/fileList/v1":
			// 路径是文件，返回"非目录"业务错误以触发文件下载逻辑
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":         200,
				"businessCode": 40001,
				"msg":          "path is not a directory",
				"data":         nil,
			})
		case "/api/openApi/diskFile/download/v1":
			// 劫持连接：声明 Content-Length=100 但只写入少量数据后关闭（模拟下载中断）
			hj, ok := w.(http.Hijacker)
			if !ok {
				http.Error(w, "hijacking unsupported", http.StatusInternalServerError)
				return
			}
			conn, bufW, _ := hj.Hijack()
			// 发送声明长度为 100 的响应头，但 body 只写 7 字节
			_, _ = bufW.WriteString("HTTP/1.1 200 OK\r\nContent-Type: application/octet-stream\r\nContent-Length: 100\r\n\r\npartial")
			_ = bufW.Flush()
			conn.Close() // 中断连接，触发客户端读取 body 时发生 unexpected EOF
		}
	}))
	defer srv.Close()

	cli := client.NewAPIClient(srv.URL, "t")
	paths := NewPathService(func(ctx context.Context) (int64, error) { return 0, nil })
	svc := NewDiskFileService(cli, paths)

	base := t.TempDir()
	target := filepath.Join(base, "partial.bin")

	var buf bytes.Buffer
	err := svc.Download(context.Background(), "public:/file.bin", target, &buf)
	if err == nil {
		t.Fatal("expected error for interrupted download, got nil")
	}

	// 验证不完整文件已被删除，不遗留损坏数据
	if _, statErr := os.Stat(target); !os.IsNotExist(statErr) {
		t.Fatalf("partial file should be cleaned up on failure, but it still exists at %s", target)
	}
}

// TestDiskFileService_Download_PartialFileCleanupFailureIsReported 验证当下载失败且清理失败时，
// 返回错误会同时包含写入失败和清理失败信息。
func TestDiskFileService_Download_PartialFileCleanupFailureIsReported(t *testing.T) {
	oldRemoveLocalFile := removeLocalFile
	removeLocalFile = func(path string) error {
		return errors.New("remove failed")
	}
	t.Cleanup(func() { removeLocalFile = oldRemoveLocalFile })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/openApi/diskFile/fileList/v1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":         200,
				"businessCode": 40001,
				"msg":          "path is not a directory",
				"data":         nil,
			})
		case "/api/openApi/diskFile/download/v1":
			hj, ok := w.(http.Hijacker)
			if !ok {
				http.Error(w, "hijacking unsupported", http.StatusInternalServerError)
				return
			}
			conn, bufW, _ := hj.Hijack()
			_, _ = bufW.WriteString("HTTP/1.1 200 OK\r\nContent-Type: application/octet-stream\r\nContent-Length: 100\r\n\r\npartial")
			_ = bufW.Flush()
			conn.Close()
		}
	}))
	defer srv.Close()

	cli := client.NewAPIClient(srv.URL, "t")
	paths := NewPathService(func(ctx context.Context) (int64, error) { return 0, nil })
	svc := NewDiskFileService(cli, paths)

	base := t.TempDir()
	target := filepath.Join(base, "partial.bin")

	var buf bytes.Buffer
	err := svc.Download(context.Background(), "public:/file.bin", target, &buf)
	if err == nil {
		t.Fatal("expected error for interrupted download, got nil")
	}
	if !strings.Contains(err.Error(), "failed to clean up incomplete file") {
		t.Fatalf("error should mention cleanup failure, got: %s", err.Error())
	}
	if !strings.Contains(err.Error(), "remove failed") {
		t.Fatalf("error should include underlying cleanup failure, got: %s", err.Error())
	}
}

// TestDiskFileService_Download_ExistingDirectoryTarget_DownloadsIntoDir 验证当本地目标路径
// 已是已存在目录时，单文件下载会进入该目录并使用远端文件的基础名称。
func TestDiskFileService_Download_ExistingDirectoryTarget_DownloadsIntoDir(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/openApi/diskFile/fileList/v1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code":         200,
				"businessCode": 40001,
				"msg":          "path is not a directory",
				"data":         nil,
			})
		case "/api/openApi/diskFile/download/v1":
			_, _ = w.Write([]byte("file-content"))
		}
	}))
	defer srv.Close()

	cli := client.NewAPIClient(srv.URL, "t")
	paths := NewPathService(func(ctx context.Context) (int64, error) { return 0, nil })
	svc := NewDiskFileService(cli, paths)

	// 创建一个已存在的目录作为下载目标
	base := t.TempDir()
	existingDir := filepath.Join(base, "downloads")
	if err := os.MkdirAll(existingDir, 0755); err != nil {
		t.Fatalf("failed to create existing dir: %v", err)
	}

	var buf bytes.Buffer
	// 以已存在目录作为 localPath（而非文件路径）
	if err := svc.Download(context.Background(), "public:/remote.txt", existingDir, &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 验证文件被下载到 existingDir/remote.txt
	expectedFile := filepath.Join(existingDir, "remote.txt")
	content, err := os.ReadFile(expectedFile)
	if err != nil {
		t.Fatalf("expected file at %s, got error: %v", expectedFile, err)
	}
	if string(content) != "file-content" {
		t.Fatalf("expected content %q, got %q", "file-content", string(content))
	}
}

// TestDiskFileService_Remove_SplitsParentAndName 验证 Remove 将路径拆分为父目录和文件名，
// 并发送 DELETE 请求到 /api/openApi/diskFile/delete/v1，body 包含文件名数组。
func TestDiskFileService_Remove_SplitsParentAndName(t *testing.T) {
	var gotMethod, gotPath, gotUID string
	var gotNames []string

	// 启动测试服务器，捕获 DELETE 请求的参数和 body
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotPath = r.URL.Query().Get("path")
		gotUID = r.URL.Query().Get("uid")
		_ = json.NewDecoder(r.Body).Decode(&gotNames)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 200,
			"data": nil,
			"msg":  "OK",
		})
	}))
	defer srv.Close()

	// 构造使用测试服务器的服务图
	cli := client.NewAPIClient(srv.URL, "ticket-1")
	paths := NewPathService(func(ctx context.Context) (int64, error) {
		return 42, nil
	})
	svc := NewDiskFileService(cli, paths)

	// 删除 /my/dir 下的 file.txt
	err := svc.Remove(context.Background(), "private:/my/dir/file.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 验证请求方法为 DELETE
	if gotMethod != "DELETE" {
		t.Fatalf("expected method DELETE, got %q", gotMethod)
	}
	// 验证 path 参数为父目录
	if gotPath != "/my/dir" {
		t.Fatalf("expected path=/my/dir, got %q", gotPath)
	}
	// 验证 uid 参数
	if gotUID != "42" {
		t.Fatalf("expected uid=42, got %q", gotUID)
	}
	// 验证 body 包含文件名数组
	if len(gotNames) != 1 || gotNames[0] != "file.txt" {
		t.Fatalf("expected names=[file.txt], got %v", gotNames)
	}
}

// TestDiskFileService_Remove_RejectsLocalArea 验证 Remove 拒绝 local 资源域。
func TestDiskFileService_Remove_RejectsLocalArea(t *testing.T) {
	cli := client.NewAPIClient("http://localhost:9999", "t")
	paths := NewPathService(func(ctx context.Context) (int64, error) { return 0, nil })
	svc := NewDiskFileService(cli, paths)

	err := svc.Remove(context.Background(), "local:/some/path")
	if err == nil {
		t.Fatal("expected error for local area, got nil")
	}
	if !strings.Contains(err.Error(), "local") {
		t.Fatalf("error should mention 'local', got: %s", err.Error())
	}
}

// TestDiskFileService_Rename_UsesParentAndOldName 验证 Rename 将路径拆分为父目录和旧名称，
// 并发送 POST 请求到 /api/openApi/diskFile/rename/v1，body 包含 path、oldName、newName。
func TestDiskFileService_Rename_UsesParentAndOldName(t *testing.T) {
	var gotUID string
	var gotBody map[string]string

	// 启动测试服务器，捕获 POST 请求的 body
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUID = r.URL.Query().Get("uid")
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 200,
			"data": nil,
			"msg":  "OK",
		})
	}))
	defer srv.Close()

	// 构造使用测试服务器的服务图
	cli := client.NewAPIClient(srv.URL, "ticket-1")
	paths := NewPathService(func(ctx context.Context) (int64, error) {
		return 42, nil
	})
	svc := NewDiskFileService(cli, paths)

	// 重命名 /docs/old.txt 为 new.txt
	err := svc.Rename(context.Background(), "private:/docs/old.txt", "new.txt")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 验证 uid 参数
	if gotUID != "42" {
		t.Fatalf("expected uid=42, got %q", gotUID)
	}
	// 验证 body 中的 path 为父目录
	if gotBody["path"] != "/docs" {
		t.Fatalf("expected path=/docs, got %q", gotBody["path"])
	}
	// 验证 body 包含正确的 oldName 和 newName
	if gotBody["oldName"] != "old.txt" {
		t.Fatalf("expected oldName=old.txt, got %q", gotBody["oldName"])
	}
	if gotBody["newName"] != "new.txt" {
		t.Fatalf("expected newName=new.txt, got %q", gotBody["newName"])
	}
}

// TestDiskFileService_Rename_RejectsLocalArea 验证 Rename 拒绝 local 资源域。
func TestDiskFileService_Rename_RejectsLocalArea(t *testing.T) {
	cli := client.NewAPIClient("http://localhost:9999", "t")
	paths := NewPathService(func(ctx context.Context) (int64, error) { return 0, nil })
	svc := NewDiskFileService(cli, paths)

	err := svc.Rename(context.Background(), "local:/some/path", "newname")
	if err == nil {
		t.Fatal("expected error for local area, got nil")
	}
	if !strings.Contains(err.Error(), "local") {
		t.Fatalf("error should mention 'local', got: %s", err.Error())
	}
}

// TestDiskFileService_Upload_LocalArea_ReturnsError 验证 Upload 在 local 资源域时返回明确错误。
func TestDiskFileService_Upload_LocalArea_ReturnsError(t *testing.T) {
	cli := client.NewAPIClient("http://localhost:9999", "t")
	paths := NewPathService(func(ctx context.Context) (int64, error) { return 0, nil })
	svc := NewDiskFileService(cli, paths)

	var buf bytes.Buffer
	err := svc.Upload(context.Background(), ".", "local:/some/path", &buf)
	if err == nil {
		t.Fatal("expected error for local area, got nil")
	}
	// 错误信息中应包含 "local" 关键字
	if !strings.Contains(err.Error(), "local") {
		t.Fatalf("error should mention 'local', got: %s", err.Error())
	}
}

// TestDiskFileService_Upload_NonExistentLocal_ReturnsError 验证本地路径不存在时 Upload 返回明确错误。
func TestDiskFileService_Upload_NonExistentLocal_ReturnsError(t *testing.T) {
	cli := client.NewAPIClient("http://localhost:9999", "t")
	paths := NewPathService(func(ctx context.Context) (int64, error) { return 0, nil })
	svc := NewDiskFileService(cli, paths)

	var buf bytes.Buffer
	nonExistent := filepath.Join(t.TempDir(), "no_such_file.txt")
	err := svc.Upload(context.Background(), nonExistent, "public:/dest/file.txt", &buf)
	if err == nil {
		t.Fatal("expected error for non-existent local path, got nil")
	}
}

// TestDiskFileService_Upload_SingleFile_HitsUploadEndpoint 验证单文件上传调用 /upload/v1 接口，
// 并将正确的 uid、path 查询参数和文件内容发送到后端。
func TestDiskFileService_Upload_SingleFile_HitsUploadEndpoint(t *testing.T) {
	var gotUID, gotPath, gotFileName string
	var gotContent []byte

	// 启动测试服务器，捕获上传请求的参数和内容
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/openApi/diskFile/upload/v1" {
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusBadRequest)
			return
		}
		// 读取查询参数
		gotUID = r.URL.Query().Get("uid")
		gotPath = r.URL.Query().Get("path")

		// 解析 multipart body，读取文件字段
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			http.Error(w, "parse multipart failed: "+err.Error(), http.StatusBadRequest)
			return
		}
		if fh := r.MultipartForm.File["file"]; len(fh) > 0 {
			gotFileName = fh[0].Filename
			f, _ := fh[0].Open()
			defer f.Close()
			gotContent, _ = io.ReadAll(f)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": nil, "msg": "OK"})
	}))
	defer srv.Close()

	// 在临时目录中创建待上传的本地文件
	base := t.TempDir()
	localFile := filepath.Join(base, "myfile.txt")
	if err := os.WriteFile(localFile, []byte("upload-content"), 0644); err != nil {
		t.Fatalf("failed to create local file: %v", err)
	}

	// 构造使用测试服务器的服务图；public 域 uid 固定为 0
	cli := client.NewAPIClient(srv.URL, "ticket-1")
	paths := NewPathService(func(ctx context.Context) (int64, error) { return 0, nil })
	svc := NewDiskFileService(cli, paths)

	var buf bytes.Buffer
	// 远端路径指定到具体文件，期望上传到父目录 /dest 并使用文件名 report.txt
	if err := svc.Upload(context.Background(), localFile, "public:/dest/report.txt", &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 验证查询参数
	if gotUID != "0" {
		t.Fatalf("expected uid=0, got %q", gotUID)
	}
	// path 应为远端父目录
	if gotPath != "/dest" {
		t.Fatalf("expected path=/dest, got %q", gotPath)
	}
	// 文件名应取自远端路径基础名
	if gotFileName != "report.txt" {
		t.Fatalf("expected fileName=report.txt, got %q", gotFileName)
	}
	// 文件内容应与本地文件一致
	if string(gotContent) != "upload-content" {
		t.Fatalf("expected content=%q, got %q", "upload-content", string(gotContent))
	}
}

// TestDiskFileService_Upload_Directory_CreatesDirsBeforeUploadingFiles 验证目录上传先通过
// /mkdir/v1 创建远端子目录，再通过 /upload/v1 上传嵌套文件，且顺序正确。
func TestDiskFileService_Upload_Directory_CreatesDirsBeforeUploadingFiles(t *testing.T) {
	// seq 记录服务端接收请求的顺序
	var seq []string
	var mkdirUID, mkdirPath, mkdirName string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/openApi/diskFile/mkdir/v1":
			// 记录 mkdir 调用及顺序
			mkdirUID = r.URL.Query().Get("uid")
			mkdirPath = r.URL.Query().Get("path")
			mkdirName = r.URL.Query().Get("name")
			seq = append(seq, "mkdir:"+mkdirName)
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": nil, "msg": "OK"})
		case "/api/openApi/diskFile/upload/v1":
			// 记录 upload 调用的文件名及顺序
			if err := r.ParseMultipartForm(1 << 20); err == nil {
				if fh := r.MultipartForm.File["file"]; len(fh) > 0 {
					seq = append(seq, "upload:"+fh[0].Filename)
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": nil, "msg": "OK"})
		default:
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	// 创建本地目录结构：root/top.txt, root/sub/, root/sub/nested.txt
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "top.txt"), []byte("top"), 0644); err != nil {
		t.Fatalf("failed to create top.txt: %v", err)
	}
	subDir := filepath.Join(root, "sub")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("failed to create sub: %v", err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "nested.txt"), []byte("nested"), 0644); err != nil {
		t.Fatalf("failed to create nested.txt: %v", err)
	}

	cli := client.NewAPIClient(srv.URL, "ticket-1")
	// private uid = 42
	paths := NewPathService(func(ctx context.Context) (int64, error) { return 42, nil })
	svc := NewDiskFileService(cli, paths)

	var buf bytes.Buffer
	if err := svc.Upload(context.Background(), root, "private:/remote", &buf); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 验证 mkdir 被调用且参数正确
	if mkdirUID != "42" {
		t.Fatalf("expected mkdir uid=42, got %q", mkdirUID)
	}
	if mkdirPath != "/remote" {
		t.Fatalf("expected mkdir path=/remote, got %q", mkdirPath)
	}
	if mkdirName != "sub" {
		t.Fatalf("expected mkdir name=sub, got %q", mkdirName)
	}

	// 找到 mkdir:sub 和 upload:nested.txt 在序列中的位置
	mkdirIdx, nestedIdx := -1, -1
	for i, s := range seq {
		if s == "mkdir:sub" {
			mkdirIdx = i
		}
		if s == "upload:nested.txt" {
			nestedIdx = i
		}
	}
	if mkdirIdx == -1 {
		t.Fatalf("mkdir:sub not found in call sequence: %v", seq)
	}
	if nestedIdx == -1 {
		t.Fatalf("upload:nested.txt not found in call sequence: %v", seq)
	}
	// mkdir 必须在嵌套文件上传之前调用
	if mkdirIdx >= nestedIdx {
		t.Fatalf("expected mkdir:sub (idx=%d) before upload:nested.txt (idx=%d); seq=%v",
			mkdirIdx, nestedIdx, seq)
	}
}
