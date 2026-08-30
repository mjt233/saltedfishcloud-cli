package oauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestDiscover_ParsesEndpoints 验证 Discover 正确解析发现文档中的端点。
func TestDiscover_ParsesEndpoints(t *testing.T) {
	// 先声明再赋值，允许 handler 闭包引用自身服务器地址
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != wellKnownPath {
			t.Fatalf("unexpected discovery path: %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{
			"issuer": "` + srv.URL + `",
			"token_endpoint": "` + srv.URL + `/oauth2/token",
			"device_authorization_endpoint": "` + srv.URL + `/oauth2/device_authorization"
		}`))
	}))
	defer srv.Close()

	endpoints, err := Discover(context.Background(), nil, srv.URL)
	if err != nil {
		t.Fatalf("Discover returned error: %v", err)
	}
	if endpoints.TokenEndpoint != srv.URL+"/oauth2/token" {
		t.Fatalf("unexpected token endpoint: %q", endpoints.TokenEndpoint)
	}
	if endpoints.DeviceAuthorizationEndpoint != srv.URL+"/oauth2/device_authorization" {
		t.Fatalf("unexpected device authorization endpoint: %q", endpoints.DeviceAuthorizationEndpoint)
	}
}

// TestDiscover_MissingEndpoints 验证发现文档缺少必需端点时返回明确错误。
func TestDiscover_MissingEndpoints(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"issuer":"https://example.com"}`))
	}))
	defer srv.Close()

	_, err := Discover(context.Background(), nil, srv.URL)
	if err == nil {
		t.Fatal("expected error for missing endpoints, got nil")
	}
	if !strings.Contains(err.Error(), "missing required endpoints") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestDiscover_HTTPError 验证发现请求返回非 2xx 时报错并包含状态码。
func TestDiscover_HTTPError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, err := Discover(context.Background(), nil, srv.URL)
	if err == nil {
		t.Fatal("expected error for 404 discovery, got nil")
	}
	if !strings.Contains(err.Error(), "404") {
		t.Fatalf("error should mention status 404, got: %v", err)
	}
}

// TestDiscover_BadJSON 验证发现文档不是合法 JSON 时返回解码错误。
func TestDiscover_BadJSON(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`not json`))
	}))
	defer srv.Close()

	_, err := Discover(context.Background(), nil, srv.URL)
	if err == nil {
		t.Fatal("expected error for malformed discovery document, got nil")
	}
	if !strings.Contains(err.Error(), "decode") {
		t.Fatalf("unexpected error: %v", err)
	}
}
