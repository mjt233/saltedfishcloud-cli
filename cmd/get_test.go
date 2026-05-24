// Package cmd 的 get 命令测试文件。
package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// TestGetCommand_DownloadsFile_ContentMatchesExpected 验证 get 命令从远端下载单个文件到本地，内容与远端相符。
func TestGetCommand_DownloadsFile_ContentMatchesExpected(t *testing.T) {
	// 重置包级标志变量，避免其他测试的残留值干扰
	apiTicket = ""
	serviceURL = ""
	t.Cleanup(func() { apiTicket = ""; serviceURL = "" })

	// 启动测试服务器：fileList 返回业务错误（路径是文件），download 返回文件内容
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
			w.Header().Set("Content-Length", "12")
			_, _ = w.Write([]byte("hello world!"))
		}
	}))
	defer srv.Close()

	// 在临时目录中设置下载目标
	base := t.TempDir()
	target := filepath.Join(base, "downloaded.txt")

	// 构造根命令，捕获输出到 buf
	root := NewRootCommand()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{
		"--service-url", srv.URL,
		"--api-ticket", "test-ticket",
		"get", "public:/file.txt", target,
	})

	// 执行命令；不应返回错误
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 验证本地文件已创建且内容正确
	content, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("failed to read downloaded file: %v", err)
	}
	if string(content) != "hello world!" {
		t.Fatalf("expected content %q, got %q", "hello world!", string(content))
	}
}

// TestGetCommand_DownloadsDirectory_CreatesNestedFiles 验证 get 命令递归下载目录并在本地重建目录树。
func TestGetCommand_DownloadsDirectory_CreatesNestedFiles(t *testing.T) {
	apiTicket = ""
	serviceURL = ""
	t.Cleanup(func() { apiTicket = ""; serviceURL = "" })

	// 启动测试服务器：/dir 有一个文件和一个子目录
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/openApi/diskFile/fileList/v1":
			reqPath := r.URL.Query().Get("path")
			switch reqPath {
			case "/dir":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"code": 200,
					"data": []map[string]any{
						{"name": "a.txt", "type": "file", "size": 4, "mtime": "2024-01-01"},
						{"name": "sub", "type": "dir", "size": 0, "mtime": "2024-01-01"},
					},
					"msg": "OK",
				})
			case "/dir/sub":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"code": 200,
					"data": []map[string]any{
						{"name": "b.txt", "type": "file", "size": 4, "mtime": "2024-01-01"},
					},
					"msg": "OK",
				})
			default:
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
			case "/dir/a.txt":
				_, _ = w.Write([]byte("aaaa"))
			case "/dir/sub/b.txt":
				_, _ = w.Write([]byte("bbbb"))
			default:
				http.Error(w, "not found", http.StatusNotFound)
			}
		}
	}))
	defer srv.Close()

	base := t.TempDir()
	outputDir := filepath.Join(base, "mydir")

	root := NewRootCommand()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{
		"--service-url", srv.URL,
		"--api-ticket", "test-ticket",
		"get", "public:/dir", outputDir,
	})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 验证 a.txt
	c1, err := os.ReadFile(filepath.Join(outputDir, "a.txt"))
	if err != nil {
		t.Fatalf("failed to read a.txt: %v", err)
	}
	if string(c1) != "aaaa" {
		t.Fatalf("expected a.txt=%q, got %q", "aaaa", string(c1))
	}

	// 验证 sub/b.txt
	c2, err := os.ReadFile(filepath.Join(outputDir, "sub", "b.txt"))
	if err != nil {
		t.Fatalf("failed to read sub/b.txt: %v", err)
	}
	if string(c2) != "bbbb" {
		t.Fatalf("expected sub/b.txt=%q, got %q", "bbbb", string(c2))
	}
}

// TestGetCommand_MissingArgs_ReturnsError 验证 get 命令在缺少路径参数时返回错误。
func TestGetCommand_MissingArgs_ReturnsError(t *testing.T) {
	apiTicket = ""
	serviceURL = ""
	t.Cleanup(func() { apiTicket = ""; serviceURL = "" })

	root := NewRootCommand()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{
		"--service-url", "http://localhost:9999",
		"--api-ticket", "test-ticket",
		"get",
	})

	if err := root.ExecuteContext(context.Background()); err == nil {
		t.Fatal("expected error for missing path argument, got nil")
	}
}
