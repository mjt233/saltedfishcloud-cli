// Package service 的用户服务测试文件。
package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mjt233/saltedfishcloud-cli/internal/client"
)

// TestUserService_PrivateUID_FetchesUID 验证 PrivateUID 正确调用用户资料接口并返回 id 字段。
func TestUserService_PrivateUID_FetchesUID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/openApi/user/profile/v1" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 200,
			"data": map[string]any{"id": 42},
			"msg":  "OK",
		})
	}))
	defer srv.Close()

	cli := client.NewAPIClient(srv.URL, "test-ticket")
	svc := NewUserService(cli)

	uid, err := svc.PrivateUID(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if uid != 42 {
		t.Fatalf("expected uid=42, got %d", uid)
	}
}

// TestUserService_PrivateUID_CachesResult 验证 PrivateUID 对同一实例只调用一次接口，后续调用使用缓存。
func TestUserService_PrivateUID_CachesResult(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 200,
			"data": map[string]any{"id": 99},
			"msg":  "OK",
		})
	}))
	defer srv.Close()

	cli := client.NewAPIClient(srv.URL, "ticket")
	svc := NewUserService(cli)

	// 连续两次调用，服务端应只被请求一次
	_, _ = svc.PrivateUID(context.Background())
	_, _ = svc.PrivateUID(context.Background())

	if callCount != 1 {
		t.Fatalf("expected server called once, got %d times", callCount)
	}
}

// TestUserService_PrivateUID_PropagatesError 验证接口返回错误时 PrivateUID 正确透传错误。
func TestUserService_PrivateUID_PropagatesError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte("Unauthorized"))
	}))
	defer srv.Close()

	cli := client.NewAPIClient(srv.URL, "bad-ticket")
	svc := NewUserService(cli)

	_, err := svc.PrivateUID(context.Background())
	if err == nil {
		t.Fatal("expected error for unauthorized response, got nil")
	}
}
