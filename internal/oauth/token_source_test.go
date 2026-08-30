package oauth

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/mjt233/saltedfishcloud-cli/internal/config"
)

// urlForm 记录一次令牌请求的表单内容，供断言使用。
type urlForm struct {
	form map[string][]string
}

// get 读取指定表单字段的第一个值。
func (f urlForm) get(key string) string {
	if len(f.form[key]) == 0 {
		return ""
	}
	return f.form[key][0]
}

// TestTokenSource_ValidTokenFastPath 验证令牌未过期时直接返回且不发起任何网络请求。
func TestTokenSource_ValidTokenFastPath(t *testing.T) {
	// 指向不存在地址的服务器：若发生网络请求必然失败
	ts := NewTokenSource("http://127.0.0.1:1", "app-1", config.OAuthState{
		AccessToken:  "at-valid",
		RefreshToken: "rt-1",
		ExpiresAt:    time.Now().Add(10 * time.Minute),
	}, nil)

	token, err := ts.Token()
	if err != nil {
		t.Fatalf("Token returned error: %v", err)
	}
	if token != "at-valid" {
		t.Fatalf("token = %q, want %q", token, "at-valid")
	}
}

// TestTokenSource_UnknownExpiryFastPath 验证过期时间未知时视为长期有效（由 401 兜底刷新）。
func TestTokenSource_UnknownExpiryFastPath(t *testing.T) {
	ts := NewTokenSource("http://127.0.0.1:1", "app-1", config.OAuthState{
		AccessToken:  "at-unknown",
		RefreshToken: "rt-1",
	}, nil)

	token, err := ts.Token()
	if err != nil {
		t.Fatalf("Token returned error: %v", err)
	}
	if token != "at-unknown" {
		t.Fatalf("token = %q, want %q", token, "at-unknown")
	}
}

// TestTokenSource_ExpiredTokenRefreshes 验证令牌过期后自动刷新，
// 携带正确的刷新参数、更新内存状态并触发持久化回调。
func TestTokenSource_ExpiredTokenRefreshes(t *testing.T) {
	srv, forms := newExpiredRefreshServer(t)
	defer srv.Close()

	var saved config.OAuthState
	state := config.OAuthState{
		ClientID:     "app-1",
		AccessToken:  "at-old",
		RefreshToken: "rt-old",
		ExpiresAt:    time.Now().Add(-time.Minute),
	}
	source := NewTokenSource(srv.URL, "app-1", state, func(s config.OAuthState) error {
		saved = s
		return nil
	})

	token, err := source.Token()
	if err != nil {
		t.Fatalf("Token returned error: %v", err)
	}
	if token != "at-new" {
		t.Fatalf("token = %q, want %q", token, "at-new")
	}

	// 校验刷新请求参数
	if len(*forms) != 1 {
		t.Fatalf("token requests = %d, want 1", len(*forms))
	}
	form := (*forms)[0]
	if form.get("grant_type") != "refresh_token" || form.get("refresh_token") != "rt-old" || form.get("client_id") != "app-1" {
		t.Fatalf("unexpected refresh form: %+v", form)
	}

	// 校验持久化回调收到轮换后的刷新令牌与新的过期时间
	if saved.AccessToken != "at-new" || saved.RefreshToken != "rt-new" {
		t.Fatalf("unexpected saved state: %#v", saved)
	}
	if saved.ExpiresAt.Before(time.Now()) {
		t.Fatalf("saved ExpiresAt should be in the future, got %v", saved.ExpiresAt)
	}
}

// TestTokenSource_ForceRefresh 验证 RefreshToken 强制刷新并返回新令牌。
func TestTokenSource_ForceRefresh(t *testing.T) {
	srv, forms := newExpiredRefreshServer(t)
	defer srv.Close()

	source := NewTokenSource(srv.URL, "app-1", config.OAuthState{
		ClientID:     "app-1",
		AccessToken:  "at-valid",
		RefreshToken: "rt-old",
	}, nil)

	token, err := source.RefreshToken()
	if err != nil {
		t.Fatalf("RefreshToken returned error: %v", err)
	}
	if token != "at-new" {
		t.Fatalf("token = %q, want %q", token, "at-new")
	}
	if len(*forms) != 1 {
		t.Fatalf("token requests = %d, want 1", len(*forms))
	}
}

// TestTokenSource_NotLoggedIn 验证缺少刷新令牌时返回可操作的登录指引错误。
func TestTokenSource_NotLoggedIn(t *testing.T) {
	source := NewTokenSource("http://127.0.0.1:1", "app-1", config.OAuthState{}, nil)

	_, err := source.Token()
	if err == nil {
		t.Fatal("expected not-logged-in error, got nil")
	}
	if !strings.Contains(err.Error(), "sfc-cli login") {
		t.Fatalf("error should mention sfc-cli login, got: %v", err)
	}
}

// TestTokenSource_MissingClientID 验证缺少 client id 时刷新失败并提示重新登录。
func TestTokenSource_MissingClientID(t *testing.T) {
	source := NewTokenSource("http://127.0.0.1:1", "", config.OAuthState{
		RefreshToken: "rt-1",
	}, nil)

	_, err := source.RefreshToken()
	if err == nil {
		t.Fatal("expected missing clientId error, got nil")
	}
	if !strings.Contains(err.Error(), "clientId") {
		t.Fatalf("error should mention clientId, got: %v", err)
	}
}

// TestTokenSource_RefreshRejected 验证刷新令牌被服务端拒绝时错误向上传播。
func TestTokenSource_RefreshRejected(t *testing.T) {
	srv := newDiscoveryServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"token revoked"}`))
	}))
	defer srv.Close()

	source := NewTokenSource(srv.URL, "app-1", config.OAuthState{
		ClientID:     "app-1",
		RefreshToken: "rt-revoked",
	}, nil)

	_, err := source.Token()
	if err == nil {
		t.Fatal("expected refresh failure error, got nil")
	}
	if !strings.Contains(err.Error(), "invalid_grant") {
		t.Fatalf("error should mention invalid_grant, got: %v", err)
	}
}

// TestTokenSource_SaveFailurePropagates 验证持久化回调失败时返回错误，
// 避免刷新令牌轮换后新令牌只存在于内存而丢失。
func TestTokenSource_SaveFailurePropagates(t *testing.T) {
	srv := newDiscoveryServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"access_token":"at-new","refresh_token":"rt-new","token_type":"Bearer","expires_in":300}`))
	}))
	defer srv.Close()

	source := NewTokenSource(srv.URL, "app-1", config.OAuthState{
		ClientID:     "app-1",
		RefreshToken: "rt-old",
	}, func(config.OAuthState) error {
		return context.DeadlineExceeded
	})

	if _, err := source.Token(); err == nil {
		t.Fatal("expected persist failure error, got nil")
	}
}

// newDiscoveryServer 启动一个提供发现文档的服务器，令牌端点由 tokenHandler 处理。
func newDiscoveryServer(t *testing.T, tokenHandler http.HandlerFunc) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 发现文档与令牌端点按路径分流
		if strings.HasPrefix(r.URL.Path, "/.well-known/") {
			_, _ = w.Write([]byte(`{
				"token_endpoint": "` + srvURL + `/oauth2/token",
				"device_authorization_endpoint": "` + srvURL + `/oauth2/device_authorization"
			}`))
			return
		}
		tokenHandler(w, r)
	}))
	// 占位地址替换：handler 执行时 srv 已就绪，通过包级变量中转
	srvURL = srv.URL
	t.Cleanup(func() { srvURL = "" })
	return srv
}

// newExpiredRefreshServer 组合发现文档与成功刷新响应的标准测试服务器。
func newExpiredRefreshServer(t *testing.T) (*httptest.Server, *[]urlForm) {
	t.Helper()

	var tokenForms []urlForm
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/.well-known/") {
			_, _ = w.Write([]byte(`{
				"token_endpoint": "` + srvURL + `/oauth2/token",
				"device_authorization_endpoint": "` + srvURL + `/oauth2/device_authorization"
			}`))
			return
		}
		_ = r.ParseForm()
		tokenForms = append(tokenForms, urlForm{form: r.PostForm})
		_, _ = w.Write([]byte(`{"access_token":"at-new","refresh_token":"rt-new","token_type":"Bearer","expires_in":300}`))
	}))
	srvURL = srv.URL
	t.Cleanup(func() { srvURL = "" })
	return srv, &tokenForms
}

// srvURL 是测试服务器地址的中转变量，供 handler 构造发现文档时引用自身地址。
var srvURL string
