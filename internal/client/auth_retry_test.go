package client

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// fakeRefreshableSource 是可刷新令牌源的测试替身：
// Token 依次返回 tokens 中的值（耗尽后返回最后一个）；
// RefreshToken 依次返回 refreshTokens 中的值，refreshErr 非空时返回错误。
type fakeRefreshableSource struct {
	tokens        []string
	tokenCalls    int
	refreshCalls  int
	refreshTokens []string
	refreshErr    error
}

// Token 返回下一个令牌值，耗尽后重复最后一个。
func (f *fakeRefreshableSource) Token() (string, error) {
	if len(f.tokens) == 0 {
		return "", nil
	}
	idx := f.tokenCalls
	f.tokenCalls++
	if idx >= len(f.tokens) {
		idx = len(f.tokens) - 1
	}
	return f.tokens[idx], nil
}

// RefreshToken 模拟强制刷新：累计调用次数并返回预置的新令牌或错误。
func (f *fakeRefreshableSource) RefreshToken() (string, error) {
	f.refreshCalls++
	if f.refreshErr != nil {
		return "", f.refreshErr
	}
	idx := f.refreshCalls - 1
	if idx >= len(f.refreshTokens) {
		idx = len(f.refreshTokens) - 1
	}
	return f.refreshTokens[idx], nil
}

// TestDoJSONRequest_RetriesOnceAfterRefresh 验证 401 触发强制刷新并重试一次，
// 重试请求携带刷新后的新令牌并成功解包响应。
func TestDoJSONRequest_RetriesOnceAfterRefresh(t *testing.T) {
	var authHeaders []string
	var bodies []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeaders = append(authHeaders, r.Header.Get("Authorization"))
		body, _ := io.ReadAll(r.Body)
		bodies = append(bodies, string(body))
		if len(authHeaders) == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": map[string]any{"ok": true}, "msg": "OK"})
	}))
	defer srv.Close()

	source := &fakeRefreshableSource{
		tokens:        []string{"at-old", "at-new"},
		refreshTokens: []string{"at-new"},
	}
	cli := NewAPIClientWithTokenSource(srv.URL, source)

	var out struct {
		OK bool `json:"ok"`
	}
	err := cli.PostJSON(context.Background(), "/api/test", map[string]string{"k": "v"}, &out)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !out.OK {
		t.Fatalf("expected ok=true, got %#v", out)
	}

	// 共两次请求：首次 401 携带旧令牌，重试携带新令牌
	if len(authHeaders) != 2 {
		t.Fatalf("requests = %d, want 2", len(authHeaders))
	}
	if authHeaders[0] != "Bearer at-old" || authHeaders[1] != "Bearer at-new" {
		t.Fatalf("unexpected auth headers: %v", authHeaders)
	}
	// 重试请求应通过 GetBody 重建请求体，内容一致
	if len(bodies) != 2 || bodies[0] != bodies[1] || !strings.Contains(bodies[1], `"k":"v"`) {
		t.Fatalf("retry body mismatch: %v", bodies)
	}
	if source.refreshCalls != 1 {
		t.Fatalf("refresh calls = %d, want 1", source.refreshCalls)
	}
}

// TestDoJSONRequest_RefreshFailurePropagates 验证刷新失败时报错并提示重新登录，不再重试。
func TestDoJSONRequest_RefreshFailurePropagates(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	source := &fakeRefreshableSource{
		tokens:     []string{"at-old"},
		refreshErr: context.DeadlineExceeded,
	}
	cli := NewAPIClientWithTokenSource(srv.URL, source)

	var out any
	err := cli.GetJSON(context.Background(), "/api/test", nil, &out)
	if err == nil {
		t.Fatal("expected error when refresh fails, got nil")
	}
	if !strings.Contains(err.Error(), "sfc-cli login") {
		t.Fatalf("error should mention sfc-cli login, got: %v", err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1 (no retry after failed refresh)", requests)
	}
}

// TestDoJSONRequest_StaticSourceKeeps401Error 验证静态令牌源收到 401 时保持原有错误行为（不重试）。
func TestDoJSONRequest_StaticSourceKeeps401Error(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	cli := NewAPIClient(srv.URL, "static-ticket")
	var out any
	err := cli.GetJSON(context.Background(), "/api/test", nil, &out)
	if err == nil {
		t.Fatal("expected 401 error, got nil")
	}
	if !strings.Contains(err.Error(), "http error 401") {
		t.Fatalf("unexpected error: %v", err)
	}
	if requests != 1 {
		t.Fatalf("requests = %d, want 1", requests)
	}
}

// TestDoJSONRequest_Retried401Surfaces 验证重试后仍为 401 时返回原始状态码错误。
func TestDoJSONRequest_Retried401Surfaces(t *testing.T) {
	var requests int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	source := &fakeRefreshableSource{
		tokens:        []string{"at-old", "at-new"},
		refreshTokens: []string{"at-new"},
	}
	cli := NewAPIClientWithTokenSource(srv.URL, source)

	var out any
	err := cli.GetJSON(context.Background(), "/api/test", nil, &out)
	if err == nil {
		t.Fatal("expected 401 error after retry, got nil")
	}
	if !strings.Contains(err.Error(), "http error 401") {
		t.Fatalf("unexpected error: %v", err)
	}
	if requests != 2 {
		t.Fatalf("requests = %d, want 2", requests)
	}
}

// TestDownload_RetriesOnceAfterRefresh 验证下载请求 401 时同样触发刷新与重试。
func TestDownload_RetriesOnceAfterRefresh(t *testing.T) {
	var authHeaders []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authHeaders = append(authHeaders, r.Header.Get("Authorization"))
		if len(authHeaders) == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte("file-content"))
	}))
	defer srv.Close()

	source := &fakeRefreshableSource{
		tokens:        []string{"at-old", "at-new"},
		refreshTokens: []string{"at-new"},
	}
	cli := NewAPIClientWithTokenSource(srv.URL, source)

	resp, err := cli.Download(context.Background(), "/api/openApi/diskFile/download/v1", nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	defer resp.Body.Close()
	content, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("failed to read body: %v", err)
	}
	if string(content) != "file-content" {
		t.Fatalf("unexpected content: %q", content)
	}
	if len(authHeaders) != 2 || authHeaders[0] != "Bearer at-old" || authHeaders[1] != "Bearer at-new" {
		t.Fatalf("unexpected auth headers: %v", authHeaders)
	}
}

// TestTokenSource_NilSourceSendsNoAuthHeader 验证令牌源为 nil 时不携带鉴权头。
func TestTokenSource_NilSourceSendsNoAuthHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		_ = json.NewEncoder(w).Encode(map[string]any{"code": 200, "data": nil, "msg": "OK"})
	}))
	defer srv.Close()

	cli := NewAPIClientWithTokenSource(srv.URL, nil)
	var out any
	if err := cli.GetJSON(context.Background(), "/api/test", nil, &out); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAuth != "" {
		t.Fatalf("expected no Authorization header, got %q", gotAuth)
	}
}
