// Package service 的磁盘文件服务测试文件。
package service

import (
	"bytes"
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
				{"name": "file.txt", "type": "file", "size": 1024, "mtime": "2024-01-01 00:00:00"},
				{"name": "folder", "type": "dir", "size": 0, "mtime": "2024-01-02 00:00:00"},
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
						{"name": "file.txt", "type": "file", "size": 6, "mtime": "2024-01-01"},
						{"name": "subdir", "type": "dir", "size": 0, "mtime": "2024-01-01"},
					},
					"msg": "OK",
				})
			case "/dir/subdir":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"code": 200,
					"data": []map[string]any{
						{"name": "nested.txt", "type": "file", "size": 6, "mtime": "2024-01-01"},
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
