// Package cmd 的 ls 命令测试文件。
package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestLSCommand_OutputsTableHeaderAndRow 验证 ls 命令在标准输出写出正确的表头和数据行。
func TestLSCommand_OutputsTableHeaderAndRow(t *testing.T) {
	// 重置包级标志变量，防止其他测试的残留值干扰
	apiTicket = ""
	serviceURL = ""
	t.Cleanup(func() { apiTicket = ""; serviceURL = "" })

	// 启动测试服务器，模拟文件列表接口
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 200,
			"data": []map[string]any{
				{"name": "demo.txt", "type": "file", "size": 512, "mtime": "2024-06-01 12:00:00"},
			},
			"msg": "OK",
		})
	}))
	defer srv.Close()

	// 构造根命令，捕获输出到 buf
	root := NewRootCommand()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{
		"--service-url", srv.URL,
		"--api-ticket", "test-ticket",
		"ls", "public:/demo",
	})

	// 执行命令；ls 子命令不应返回错误
	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	output := buf.String()

	// 验证表头行
	if !strings.Contains(output, "type\tname\tsize\tmtime") {
		t.Fatalf("output missing expected header, got:\n%s", output)
	}
	// 验证条目数据行包含关键字段
	if !strings.Contains(output, "demo.txt") {
		t.Fatalf("output missing entry name 'demo.txt', got:\n%s", output)
	}
	if !strings.Contains(output, "512") {
		t.Fatalf("output missing entry size '512', got:\n%s", output)
	}
	if !strings.Contains(output, "file") {
		t.Fatalf("output missing entry type 'file', got:\n%s", output)
	}
}

// TestLSCommand_MissingArgsReturnsError 验证 ls 命令在缺少路径参数时返回错误。
func TestLSCommand_MissingArgsReturnsError(t *testing.T) {
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
		"ls",
	})

	// 缺少路径参数时应返回错误
	if err := root.ExecuteContext(context.Background()); err == nil {
		t.Fatal("expected error for missing path argument, got nil")
	}
}

// TestLSCommand_RespectsContextCancellation 验证 ls 命令会透传执行上下文。
func TestLSCommand_RespectsContextCancellation(t *testing.T) {
	apiTicket = ""
	serviceURL = ""
	t.Cleanup(func() { apiTicket = ""; serviceURL = "" })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("request should not be sent when command context is canceled")
	}))
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	root := NewRootCommand()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{
		"--service-url", srv.URL,
		"--api-ticket", "test-ticket",
		"ls", "public:/demo",
	})

	err := root.ExecuteContext(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context canceled error, got %v", err)
	}
}
