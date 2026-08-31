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
	"time"
)

// TestLoginCommand_Flags 验证 login 命令注册了 --client-id 与 --scope 标志，
// 且 scope 默认值覆盖 CLI 全功能所需的授权范围。
func TestLoginCommand_Flags(t *testing.T) {
	cmd := newLoginCommand()

	clientIDFlag := cmd.Flags().Lookup("client-id")
	if clientIDFlag == nil {
		t.Fatal("login command should register --client-id flag")
	}
	scopeFlag := cmd.Flags().Lookup("scope")
	if scopeFlag == nil {
		t.Fatal("login command should register --scope flag")
	}
	if scopeFlag.DefValue != defaultLoginScope {
		t.Fatalf("scope default = %q, want %q", scopeFlag.DefValue, defaultLoginScope)
	}
}

// TestRunLogin_MissingClientID 验证未提供且未配置 client id 时返回明确错误。
func TestRunLogin_MissingClientID(t *testing.T) {
	// 隔离 HOME 与包级标志变量，避免读到真实配置或其他测试残留
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("SFC_CLIENT_ID", "")
	origServiceURL := serviceURL
	t.Cleanup(func() { serviceURL = origServiceURL })
	serviceURL = "http://127.0.0.1:1"

	var out bytes.Buffer
	err := runLogin(context.Background(), &out, "", "profile", func(time.Duration) {})
	if err == nil {
		t.Fatal("expected missing client id error, got nil")
	}
	if !strings.Contains(err.Error(), "missing client id") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestRunLogin_DeviceFlowEndToEnd 验证完整设备授权流程：
// 发现端点、申请设备码、轮询换票、持久化登录态，并用新令牌确认身份。
func TestRunLogin_DeviceFlowEndToEnd(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	var srv *httptest.Server
	var profileAuth string
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/.well-known/openid-configuration":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"issuer":                        srv.URL,
				"token_endpoint":                srv.URL + "/oauth2/token",
				"device_authorization_endpoint": srv.URL + "/oauth2/device_authorization",
			})
		case r.URL.Path == "/oauth2/device_authorization":
			if err := r.ParseForm(); err != nil {
				t.Fatalf("failed to parse form: %v", err)
			}
			if got := r.PostForm.Get("client_id"); got != "app-1" {
				t.Fatalf("client_id = %q, want %q", got, "app-1")
			}
			if got := r.PostForm.Get("scope"); got != "profile storage_read" {
				t.Fatalf("scope = %q, want %q", got, "profile storage_read")
			}
			// 设备流应始终附带 PKCE S256
			if got := r.PostForm.Get("code_challenge_method"); got != "S256" {
				t.Fatalf("code_challenge_method = %q, want %q", got, "S256")
			}
			if got := r.PostForm.Get("code_challenge"); got == "" {
				t.Fatal("code_challenge should be present")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"device_code":               "dc-1",
				"user_code":                 "ABCD-WXYZ",
				"verification_uri":          srv.URL + "/oauth/device",
				"verification_uri_complete": srv.URL + "/oauth/device?user_code=ABCD-WXYZ",
				"expires_in":                600,
				"interval":                  1,
			})
		case r.URL.Path == "/oauth2/token":
			_ = r.ParseForm()
			if got := r.PostForm.Get("grant_type"); got != "urn:ietf:params:oauth:grant-type:device_code" {
				t.Fatalf("grant_type = %q", got)
			}
			// 换票应回传与申请阶段配对的 code_verifier
			if got := r.PostForm.Get("code_verifier"); got == "" {
				t.Fatal("code_verifier should be present on token poll")
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"access_token":  "at-1",
				"refresh_token": "rt-1",
				"token_type":    "Bearer",
				"expires_in":    300,
			})
		case r.URL.Path == "/api/openApi/user/profile/v1":
			profileAuth = r.Header.Get("Authorization")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 200,
				"data": map[string]any{"username": "admin", "id": "1"},
				"msg":  "OK",
			})
		default:
			t.Fatalf("unexpected request path: %s", r.URL.Path)
		}
	}))
	defer srv.Close()

	origServiceURL := serviceURL
	t.Cleanup(func() { serviceURL = origServiceURL })
	serviceURL = srv.URL

	var out bytes.Buffer
	err := runLogin(context.Background(), &out, "app-1", "profile storage_read", func(time.Duration) {})
	if err != nil {
		t.Fatalf("runLogin returned error: %v", err)
	}

	// 输出应包含引导信息与登录确认
	text := out.String()
	for _, want := range []string{"ABCD-WXYZ", "/oauth/device", "登录成功", "admin"} {
		if !strings.Contains(text, want) {
			t.Fatalf("output %q missing %q", text, want)
		}
	}

	// 身份确认请求应携带新获得的访问令牌
	if profileAuth != "Bearer at-1" {
		t.Fatalf("profile Authorization = %q, want %q", profileAuth, "Bearer at-1")
	}

	// 登录态应持久化到配置文件
	raw, err := os.ReadFile(filepath.Join(home, ".config", "sfc-cli", "config.json"))
	if err != nil {
		t.Fatalf("failed to read config file: %v", err)
	}
	var saved map[string]any
	if err := json.Unmarshal(raw, &saved); err != nil {
		t.Fatalf("config file is not valid JSON: %v", err)
	}
	for key, want := range map[string]string{
		"clientId":     "app-1",
		"accessToken":  "at-1",
		"refreshToken": "rt-1",
	} {
		if got := saved[key]; got != want {
			t.Fatalf("saved %s = %v, want %v", key, got, want)
		}
	}
	if saved["expiresAt"] == "" {
		t.Fatal("expiresAt should be persisted")
	}
}
