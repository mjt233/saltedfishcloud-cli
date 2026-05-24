// Package cmd 的 version 与 remoteVersion 命令测试文件。
package cmd

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestVersionCommand_PrintsInjectedVersion 验证 version 命令输出注入的版本号。
func TestVersionCommand_PrintsInjectedVersion(t *testing.T) {
	// 备份原始值并在测试结束后恢复
	origVersion := version
	version = "1.2.3"
	t.Cleanup(func() { version = origVersion })

	cmd := newVersionCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetArgs(nil)

	// 直接执行 Run 函数，验证输出
	cmd.Run(cmd, nil)
	if got := strings.TrimSpace(buf.String()); got != "1.2.3" {
		t.Fatalf("unexpected version output: %q, want %q", got, "1.2.3")
	}
}

// TestRemoteVersionCommand_PrintsServerVersion 验证 remoteVersion 命令输出服务端版本号。
func TestRemoteVersionCommand_PrintsServerVersion(t *testing.T) {
	// 重置包级标志变量
	apiTicket = ""
	serviceURL = ""
	t.Cleanup(func() { apiTicket = ""; serviceURL = "" })

	// 启动测试服务器，模拟 /api/hello/feature 接口
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"version": "3.1.2.0-RELEASE"})
	}))
	defer srv.Close()

	// 通过根命令执行，验证端到端流程
	root := NewRootCommand()
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	root.SetArgs([]string{"--service-url", srv.URL, "--api-ticket", "dummy", "remoteVersion"})

	if err := root.Execute(); err != nil {
		t.Fatalf("Execute returned error: %v", err)
	}
	if got := strings.TrimSpace(buf.String()); got != "3.1.2.0-RELEASE" {
		t.Fatalf("unexpected remote version output: %q, want %q", got, "3.1.2.0-RELEASE")
	}
}

// TestRemoteVersionCommand_MissingArgsReturnsError 验证 remoteVersion 命令不接受额外参数。
func TestRemoteVersionCommand_MissingArgsReturnsError(t *testing.T) {
	// 重置包级标志变量
	apiTicket = ""
	serviceURL = ""
	t.Cleanup(func() { apiTicket = ""; serviceURL = "" })

	root := NewRootCommand()
	buf := new(bytes.Buffer)
	root.SetOut(buf)
	root.SetErr(buf)
	// 传入多余参数，应触发 NoArgs 校验
	root.SetArgs([]string{"--service-url", "http://localhost:9999", "remoteVersion", "extra-arg"})

	if err := root.Execute(); err == nil {
		t.Fatal("expected error for extra arguments, got nil")
	}
}

// TestVersionCommand_NoArgs 验证 version 命令不接受额外参数。
func TestVersionCommand_NoArgs(t *testing.T) {
	cmd := newVersionCommand()
	cmd.SetArgs([]string{"extra-arg"})

	if err := cmd.Execute(); err == nil {
		t.Fatal("expected error for extra arguments, got nil")
	}
}
