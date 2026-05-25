// Package cmd 的文件操作命令（rm、rename、cp、mv）测试文件。
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

// TestRmCommand_ExecutesDelete 验证 rm 命令向后端发送删除请求。
func TestRmCommand_ExecutesDelete(t *testing.T) {
	apiTicket = ""
	serviceURL = ""
	t.Cleanup(func() { apiTicket = ""; serviceURL = "" })

	deleteCalled := false
	var gotPath, gotUID string

	// 启动测试服务器，捕获删除请求
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/openApi/diskFile/delete/v1":
			deleteCalled = true
			gotPath = r.URL.Query().Get("path")
			gotUID = r.URL.Query().Get("uid")
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

	root := NewRootCommand()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{
		"--service-url", srv.URL,
		"--api-ticket", "test-ticket",
		"rm", "public:/dir/file.txt",
	})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 验证删除接口被调用
	if !deleteCalled {
		t.Fatal("expected delete endpoint to be called")
	}
	// 验证 path 参数为父目录
	if gotPath != "/dir" {
		t.Fatalf("expected path=/dir, got %q", gotPath)
	}
	// 验证 uid 参数（public 域为 0）
	if gotUID != "0" {
		t.Fatalf("expected uid=0, got %q", gotUID)
	}
}

// TestRmCommand_MissingArgsReturnsError 验证 rm 命令在缺少参数时返回错误。
func TestRmCommand_MissingArgsReturnsError(t *testing.T) {
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
		"rm",
	})

	if err := root.ExecuteContext(context.Background()); err == nil {
		t.Fatal("expected error for missing args, got nil")
	}
}

// TestRenameCommand_ExecutesRename 验证 rename 命令向后端发送重命名请求。
func TestRenameCommand_ExecutesRename(t *testing.T) {
	apiTicket = ""
	serviceURL = ""
	t.Cleanup(func() { apiTicket = ""; serviceURL = "" })

	renameCalled := false
	var gotBody map[string]string

	// 启动测试服务器，捕获重命名请求
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/openApi/diskFile/rename/v1":
			renameCalled = true
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

	root := NewRootCommand()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{
		"--service-url", srv.URL,
		"--api-ticket", "test-ticket",
		"rename", "public:/docs/old.txt", "new.txt",
	})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 验证重命名接口被调用
	if !renameCalled {
		t.Fatal("expected rename endpoint to be called")
	}
	// 验证 body 中的字段
	if gotBody["oldName"] != "old.txt" {
		t.Fatalf("expected oldName=old.txt, got %q", gotBody["oldName"])
	}
	if gotBody["newName"] != "new.txt" {
		t.Fatalf("expected newName=new.txt, got %q", gotBody["newName"])
	}
}

// TestRenameCommand_MissingArgsReturnsError 验证 rename 命令在缺少参数时返回错误。
func TestRenameCommand_MissingArgsReturnsError(t *testing.T) {
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
		"rename", "public:/file.txt", // 缺少 newName
	})

	if err := root.ExecuteContext(context.Background()); err == nil {
		t.Fatal("expected error for missing args, got nil")
	}
}

// TestCpCommand_ExecutesCopy 验证 cp 命令向后端发送复制请求。
func TestCpCommand_ExecutesCopy(t *testing.T) {
	apiTicket = ""
	serviceURL = ""
	t.Cleanup(func() { apiTicket = ""; serviceURL = "" })

	copyCalled := false
	var gotBody map[string]any

	// 启动测试服务器：fileList 返回父目录条目，copy 捕获请求
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
			copyCalled = true
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

	root := NewRootCommand()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{
		"--service-url", srv.URL,
		"--api-ticket", "test-ticket",
		"cp", "public:/src/file.txt", "public:/dest/",
	})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 验证复制接口被调用
	if !copyCalled {
		t.Fatal("expected copy endpoint to be called")
	}
	// 验证 files 数组包含文件名
	files, ok := gotBody["files"].([]any)
	if !ok || len(files) != 1 || files[0] != "file.txt" {
		t.Fatalf("expected files=[file.txt], got %v", gotBody["files"])
	}
}

// TestCpCommand_MissingArgsReturnsError 验证 cp 命令在缺少参数时返回错误。
func TestCpCommand_MissingArgsReturnsError(t *testing.T) {
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
		"cp", "public:/src/file.txt", // 缺少 targetPath
	})

	if err := root.ExecuteContext(context.Background()); err == nil {
		t.Fatal("expected error for missing args, got nil")
	}
}

// TestMvCommand_ExecutesMove 验证 mv 命令向后端发送移动请求。
func TestMvCommand_ExecutesMove(t *testing.T) {
	apiTicket = ""
	serviceURL = ""
	t.Cleanup(func() { apiTicket = ""; serviceURL = "" })

	moveCalled := false
	var gotBody map[string]any

	// 启动测试服务器：fileList 返回父目录条目，move 捕获请求
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
			moveCalled = true
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

	root := NewRootCommand()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{
		"--service-url", srv.URL,
		"--api-ticket", "test-ticket",
		"mv", "public:/src/file.txt", "public:/dest/",
	})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 验证移动接口被调用
	if !moveCalled {
		t.Fatal("expected move endpoint to be called")
	}
	// 验证 files 数组包含文件名
	files, ok := gotBody["files"].([]any)
	if !ok || len(files) != 1 || files[0] != "file.txt" {
		t.Fatalf("expected files=[file.txt], got %v", gotBody["files"])
	}
}

// TestMvCommand_MissingArgsReturnsError 验证 mv 命令在缺少参数时返回错误。
func TestMvCommand_MissingArgsReturnsError(t *testing.T) {
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
		"mv", "public:/src/file.txt", // 缺少 targetPath
	})

	if err := root.ExecuteContext(context.Background()); err == nil {
		t.Fatal("expected error for missing args, got nil")
	}
}

// TestMvCommand_LocalToRemote_RemovesLocalSource 验证 mv 命令从 local 移动到 remote 时，
// 上传成功后会删除本地源文件。
func TestMvCommand_LocalToRemote_RemovesLocalSource(t *testing.T) {
	apiTicket = ""
	serviceURL = ""
	t.Cleanup(func() { apiTicket = ""; serviceURL = "" })

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

	root := NewRootCommand()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{
		"--service-url", srv.URL,
		"--api-ticket", "test-ticket",
		"mv", "local:" + localFile, "public:/dest/",
	})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 验证上传被调用
	if !uploadCalled {
		t.Fatal("expected upload endpoint to be called")
	}
	// 验证本地源文件已被删除
	if _, statErr := os.Stat(localFile); !os.IsNotExist(statErr) {
		t.Fatalf("expected local source %q to be removed after move, but it still exists", localFile)
	}
}

// TestMvCommand_RemoteToLocal_RemovesRemoteSource 验证 mv 命令从 remote 移动到 local 时，
// 下载成功后会删除远端源文件。
func TestMvCommand_RemoteToLocal_RemovesRemoteSource(t *testing.T) {
	apiTicket = ""
	serviceURL = ""
	t.Cleanup(func() { apiTicket = ""; serviceURL = "" })

	deleteCalled := false

	// 启动测试服务器：fileList 返回父目录条目，download 返回内容，delete 记录调用
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

	// 创建本地目标路径
	base := t.TempDir()
	localTarget := filepath.Join(base, "downloaded.txt")

	root := NewRootCommand()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{
		"--service-url", srv.URL,
		"--api-ticket", "test-ticket",
		"mv", "public:/remote.txt", "local:" + localTarget,
	})

	if err := root.ExecuteContext(context.Background()); err != nil {
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
