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
	"strings"
	"testing"
)

// TestGetCommand_DownloadsFile_ContentMatchesExpected 验证 get 命令从远端下载单个文件到本地，内容与远端相符。
func TestGetCommand_DownloadsFile_ContentMatchesExpected(t *testing.T) {
	// 重置包级标志变量，避免其他测试的残留值干扰
	apiTicket = ""
	serviceURL = ""
	t.Cleanup(func() { apiTicket = ""; serviceURL = "" })

	// 启动测试服务器：fileList 返回父目录条目（路径是文件），download 返回文件内容
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/openApi/diskFile/fileList/v1":
			// 父目录 "/" 的条目列表，目标文件以 dir=false 标识
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200,
				"data": []map[string]any{
					{"name": "file.txt", "dir": false, "size": "12", "mtime": "1778581444799"},
				},
				"msg": "OK",
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

	// 启动测试服务器：/ 有 dir 目录，/dir 有一个文件和一个子目录
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/openApi/diskFile/fileList/v1":
			reqPath := r.URL.Query().Get("path")
			switch reqPath {
			case "/":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"code": 200,
					"data": []map[string]any{
						{"name": "dir", "dir": true, "size": "-1", "mtime": "1778581444799"},
					},
					"msg": "OK",
				})
			case "/dir":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"code": 200,
					"data": []map[string]any{
						{"name": "a.txt", "dir": false, "size": "4", "mtime": "1778581444799"},
						{"name": "sub", "dir": true, "size": "-1", "mtime": "1778581444799"},
					},
					"msg": "OK",
				})
			case "/dir/sub":
				_ = json.NewEncoder(w).Encode(map[string]any{
					"code": 200,
					"data": []map[string]any{
						{"name": "b.txt", "dir": false, "size": "4", "mtime": "1778581444799"},
					},
					"msg": "OK",
				})
			default:
				http.Error(w, "unexpected path: "+reqPath, http.StatusBadRequest)
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

// TestGetCommand_DefaultLocalName_DerivedFromResolvedPath 验证当未指定本地路径时，
// 下载目标名称从解析后的远端路径中提取，而非原始路径字符串；
// 确保含资源域前缀但无前导斜杠的路径（如 "public:file.bin"）能正确派生出文件名。
func TestGetCommand_DefaultLocalName_DerivedFromResolvedPath(t *testing.T) {
	apiTicket = ""
	serviceURL = ""
	t.Cleanup(func() { apiTicket = ""; serviceURL = "" })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/openApi/diskFile/fileList/v1":
			// 父目录 "/" 的条目列表，目标文件以 dir=false 标识
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200,
				"data": []map[string]any{
					{"name": "remote-name.bin", "dir": false, "size": "7", "mtime": "1778581444799"},
				},
				"msg": "OK",
			})
		case "/api/openApi/diskFile/download/v1":
			_, _ = w.Write([]byte("content"))
		}
	}))
	defer srv.Close()

	// 切换到临时目录，使默认下载路径落在此处
	base := t.TempDir()
	origDir, err := os.Getwd()
	if err != nil {
		t.Fatalf("failed to get working dir: %v", err)
	}
	if err := os.Chdir(base); err != nil {
		t.Fatalf("failed to chdir: %v", err)
	}
	t.Cleanup(func() { _ = os.Chdir(origDir) })

	root := NewRootCommand()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	// "public:remote-name.bin"（无前导斜杠）是触发 bug 的场景：
	//   修复前：path.Base("public:remote-name.bin") = "public:remote-name.bin"（Windows 上因冒号非法而失败）
	//   修复后：解析路径得 /remote-name.bin，path.Base 取得 "remote-name.bin"
	root.SetArgs([]string{
		"--service-url", srv.URL,
		"--api-ticket", "test-ticket",
		"get", "public:remote-name.bin", // 无本地目标路径
	})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 验证文件以正确名称（不含资源域前缀）存在于临时目录
	expectedFile := filepath.Join(base, "remote-name.bin")
	if _, statErr := os.Stat(expectedFile); statErr != nil {
		t.Fatalf("expected file at %s, got: %v", expectedFile, statErr)
	}
}

// TestGetCommand_DefaultLocalName_RejectsUnsafeRootPath 验证当远端路径无法推导安全默认文件名时，
// get 命令会要求用户显式指定本地目标路径。
func TestGetCommand_DefaultLocalName_RejectsUnsafeRootPath(t *testing.T) {
	apiTicket = ""
	serviceURL = ""
	t.Cleanup(func() { apiTicket = ""; serviceURL = "" })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/openApi/diskFile/fileList/v1":
			// 根目录 "/" 的条目列表
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200,
				"data": []map[string]any{},
				"msg":  "OK",
			})
		case "/api/openApi/diskFile/download/v1":
			_, _ = w.Write([]byte("content"))
		}
	}))
	defer srv.Close()

	root := NewRootCommand()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{
		"--service-url", srv.URL,
		"--api-ticket", "test-ticket",
		"get", "public:/",
	})

	err := root.ExecuteContext(context.Background())
	if err == nil {
		t.Fatal("expected error for unsafe default local path, got nil")
	}
	// 根路径无法推导文件名，错误可能来自 Download（"cannot download root path"）
	// 或来自本地路径推导（"specify localPath explicitly"）
	if !strings.Contains(err.Error(), "root path") && !strings.Contains(err.Error(), "specify localPath explicitly") {
		t.Fatalf("error should mention root path issue or request explicit localPath, got: %s", err.Error())
	}
}
