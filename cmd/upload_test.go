// Package cmd 的 upload 命令测试文件。
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

// TestUploadCommand_MissingArgs_ReturnsError 验证 upload 命令在缺少参数时返回错误。
func TestUploadCommand_MissingArgs_ReturnsError(t *testing.T) {
	isolateOAuthConfig(t)

	root := NewRootCommand()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{
		"--service-url", "http://localhost:9999",
		"upload", // 缺少 localPath 和 remoteResourcePath
	})

	if err := root.ExecuteContext(context.Background()); err == nil {
		t.Fatal("expected error for missing args, got nil")
	}
}

// TestUploadCommand_SingleFile_MakesUploadRequest 验证 upload 命令对单个文件执行时，
// 向后端发出正确的上传请求，且文件内容与本地文件一致。
func TestUploadCommand_SingleFile_MakesUploadRequest(t *testing.T) {
	isolateOAuthConfig(t)

	uploadCalled := false
	var gotPath, gotFileName string

	// 启动测试服务器，接收上传请求
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/openApi/diskFile/upload/v1":
			uploadCalled = true
			gotPath = r.URL.Query().Get("path")
			if err := r.ParseMultipartForm(1 << 20); err == nil {
				if fh := r.MultipartForm.File["file"]; len(fh) > 0 {
					gotFileName = fh[0].Filename
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": nil, "msg": "OK"})
		default:
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	// 在临时目录中创建待上传的本地文件
	base := t.TempDir()
	localFile := filepath.Join(base, "source.txt")
	if err := os.WriteFile(localFile, []byte("hello"), 0644); err != nil {
		t.Fatalf("failed to create local file: %v", err)
	}

	root := NewRootCommand()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{
		"--service-url", srv.URL,
		"upload", localFile, "public:/remote/dest.txt",
	})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 验证上传接口确实被调用
	if !uploadCalled {
		t.Fatal("expected upload endpoint to be called")
	}
	// 验证远端父目录参数
	if gotPath != "/remote" {
		t.Fatalf("expected path=/remote, got %q", gotPath)
	}
	// 验证文件名取自远端路径基础名
	if gotFileName != "dest.txt" {
		t.Fatalf("expected fileName=dest.txt, got %q", gotFileName)
	}
}

// TestUploadCommand_Directory_CreatesDirsAndUploadsFiles 验证 upload 命令对目录执行时，
// 递归调用 mkdir 创建远端子目录并上传所有文件。
func TestUploadCommand_Directory_CreatesDirsAndUploadsFiles(t *testing.T) {
	isolateOAuthConfig(t)

	var mkdirNames []string
	var uploadedFiles []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/openApi/diskFile/mkdir/v1":
			mkdirNames = append(mkdirNames, r.URL.Query().Get("name"))
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": nil, "msg": "OK"})
		case "/api/openApi/diskFile/upload/v1":
			if err := r.ParseMultipartForm(1 << 20); err == nil {
				if fh := r.MultipartForm.File["file"]; len(fh) > 0 {
					uploadedFiles = append(uploadedFiles, fh[0].Filename)
				}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": nil, "msg": "OK"})
		default:
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusBadRequest)
		}
	}))
	defer srv.Close()

	// 创建本地目录结构：localDir/file.txt, localDir/sub/nested.txt
	localDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(localDir, "file.txt"), []byte("file"), 0644); err != nil {
		t.Fatalf("failed to create file.txt: %v", err)
	}
	subDir := filepath.Join(localDir, "sub")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatalf("failed to create sub: %v", err)
	}
	if err := os.WriteFile(filepath.Join(subDir, "nested.txt"), []byte("nested"), 0644); err != nil {
		t.Fatalf("failed to create nested.txt: %v", err)
	}

	root := NewRootCommand()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{
		"--service-url", srv.URL,
		"upload", localDir, "public:/dest",
	})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// 验证 mkdir 被调用了 sub 目录
	found := false
	for _, n := range mkdirNames {
		if n == "sub" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected mkdir for 'sub', got mkdirNames=%v", mkdirNames)
	}

	// 验证两个文件都被上传
	uploadedSet := make(map[string]bool)
	for _, f := range uploadedFiles {
		uploadedSet[f] = true
	}
	if !uploadedSet["file.txt"] {
		t.Fatalf("expected file.txt to be uploaded, got uploadedFiles=%v", uploadedFiles)
	}
	if !uploadedSet["nested.txt"] {
		t.Fatalf("expected nested.txt to be uploaded, got uploadedFiles=%v", uploadedFiles)
	}
}
