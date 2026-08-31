package oauth

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestRequestDeviceCode_SendsFormAndParses 验证设备授权请求携带正确的表单参数并解析响应，
// 包括始终附带的 PKCE S256 参数，以及返回结构中保存的 code_verifier。
func TestRequestDeviceCode_SendsFormAndParses(t *testing.T) {
	var gotClientID, gotScope, gotContentType string
	var gotChallenge, gotMethod string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Fatalf("expected POST, got %s", r.Method)
		}
		gotContentType = r.Header.Get("Content-Type")
		if err := r.ParseForm(); err != nil {
			t.Fatalf("failed to parse form: %v", err)
		}
		gotClientID = r.PostForm.Get("client_id")
		gotScope = r.PostForm.Get("scope")
		gotChallenge = r.PostForm.Get("code_challenge")
		gotMethod = r.PostForm.Get("code_challenge_method")
		_, _ = w.Write([]byte(`{
			"device_code":"dc-1","user_code":"ABCD-WXYZ",
			"verification_uri":"https://sfc/oauth/device",
			"verification_uri_complete":"https://sfc/oauth/device?user_code=ABCD-WXYZ",
			"expires_in":600,"interval":5
		}`))
	}))
	defer srv.Close()

	endpoints := &Endpoints{DeviceAuthorizationEndpoint: srv.URL, TokenEndpoint: srv.URL}
	da, err := RequestDeviceCode(context.Background(), nil, endpoints, "app-1", "profile storage_read")
	if err != nil {
		t.Fatalf("RequestDeviceCode returned error: %v", err)
	}
	if gotClientID != "app-1" {
		t.Fatalf("client_id = %q, want %q", gotClientID, "app-1")
	}
	if gotScope != "profile storage_read" {
		t.Fatalf("scope = %q, want %q", gotScope, "profile storage_read")
	}
	if gotMethod != "S256" {
		t.Fatalf("code_challenge_method = %q, want %q", gotMethod, "S256")
	}
	if gotChallenge == "" {
		t.Fatal("code_challenge should be sent")
	}
	if da.CodeVerifier == "" {
		t.Fatal("CodeVerifier should be retained for token polling")
	}
	// 校验 challenge 与本地 verifier 的 S256 对应关系
	sum := sha256.Sum256([]byte(da.CodeVerifier))
	wantChallenge := base64.RawURLEncoding.EncodeToString(sum[:])
	if gotChallenge != wantChallenge {
		t.Fatalf("code_challenge = %q, want S256(%s) = %q", gotChallenge, da.CodeVerifier, wantChallenge)
	}
	if !strings.HasPrefix(gotContentType, "application/x-www-form-urlencoded") {
		t.Fatalf("unexpected content type: %q", gotContentType)
	}
	if da.DeviceCode != "dc-1" || da.UserCode != "ABCD-WXYZ" || da.Interval != 5 || da.ExpiresIn != 600 {
		t.Fatalf("unexpected device authorization: %#v", da)
	}
	if da.VerificationURIComplete == "" {
		t.Fatal("verification_uri_complete should be parsed")
	}
}

// TestRequestDeviceCode_OAuthError 验证设备授权失败时解析标准 OAuth 错误。
func TestRequestDeviceCode_OAuthError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":"invalid_client","error_description":"unknown client"}`))
	}))
	defer srv.Close()

	endpoints := &Endpoints{DeviceAuthorizationEndpoint: srv.URL, TokenEndpoint: srv.URL}
	_, err := RequestDeviceCode(context.Background(), nil, endpoints, "bad", "profile")
	var oaErr *OAuthError
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.As(err, &oaErr) || oaErr.Code != "invalid_client" {
		t.Fatalf("expected OAuthError invalid_client, got: %v", err)
	}
}

// TestWaitForAuthorization_PendingThenSuccess 验证轮询在 authorization_pending 后继续，
// 并在服务端返回令牌时成功结束；同时验证每次轮询的等待间隔符合响应给出的 interval，
// 以及换票请求携带申请阶段保存的 code_verifier。
func TestWaitForAuthorization_PendingThenSuccess(t *testing.T) {
	const wantVerifier = "test-code-verifier-value-xxxxxxxx"
	var polls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		polls++
		if err := r.ParseForm(); err != nil {
			t.Fatalf("failed to parse form: %v", err)
		}
		if got := r.PostForm.Get("grant_type"); got != deviceCodeGrantType {
			t.Fatalf("grant_type = %q, want %q", got, deviceCodeGrantType)
		}
		if got := r.PostForm.Get("device_code"); got != "dc-1" {
			t.Fatalf("device_code = %q, want %q", got, "dc-1")
		}
		if got := r.PostForm.Get("client_id"); got != "app-1" {
			t.Fatalf("client_id = %q, want %q", got, "app-1")
		}
		if got := r.PostForm.Get("code_verifier"); got != wantVerifier {
			t.Fatalf("code_verifier = %q, want %q", got, wantVerifier)
		}
		if polls == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"authorization_pending"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"access_token": "at-1", "refresh_token": "rt-1",
			"token_type": "Bearer", "expires_in": 300,
		})
	}))
	defer srv.Close()

	// 记录每次注入的等待时长，验证间隔来自 da.Interval
	var waits []time.Duration
	sleep := func(d time.Duration) { waits = append(waits, d) }

	endpoints := &Endpoints{DeviceAuthorizationEndpoint: srv.URL, TokenEndpoint: srv.URL}
	da := &DeviceAuthorization{DeviceCode: "dc-1", UserCode: "CODE", Interval: 3, CodeVerifier: wantVerifier}
	ts, err := WaitForAuthorization(context.Background(), nil, endpoints, "app-1", da, sleep)
	if err != nil {
		t.Fatalf("WaitForAuthorization returned error: %v", err)
	}
	if ts.AccessToken != "at-1" || ts.RefreshToken != "rt-1" || ts.ExpiresIn != 300 {
		t.Fatalf("unexpected token set: %#v", ts)
	}
	if polls != 2 {
		t.Fatalf("polls = %d, want 2", polls)
	}
	for _, d := range waits {
		if d != 3*time.Second {
			t.Fatalf("wait = %v, want 3s", d)
		}
	}
}

// TestGeneratePKCE 验证 PKCE 生成结果满足 RFC 7636：verifier 长度、challenge 为 S256。
func TestGeneratePKCE(t *testing.T) {
	verifier, challenge, err := generatePKCE()
	if err != nil {
		t.Fatalf("generatePKCE returned error: %v", err)
	}
	if len(verifier) != 43 {
		t.Fatalf("code_verifier length = %d, want 43", len(verifier))
	}
	// base64url 无填充字符集校验（字母数字与 -_）
	for _, c := range verifier {
		if (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '-' || c == '_' {
			continue
		}
		t.Fatalf("code_verifier contains invalid character %q", c)
	}
	sum := sha256.Sum256([]byte(verifier))
	want := base64.RawURLEncoding.EncodeToString(sum[:])
	if challenge != want {
		t.Fatalf("code_challenge = %q, want %q", challenge, want)
	}

	// 连续两次生成应得到不同的 verifier（高熵）
	v2, _, err := generatePKCE()
	if err != nil {
		t.Fatalf("second generatePKCE returned error: %v", err)
	}
	if verifier == v2 {
		t.Fatal("two consecutive code_verifiers should differ")
	}
}

// TestWaitForAuthorization_SlowDown 验证收到 slow_down 后轮询间隔增加 5 秒。
func TestWaitForAuthorization_SlowDown(t *testing.T) {
	var polls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		polls++
		if polls == 1 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte(`{"error":"slow_down"}`))
			return
		}
		_, _ = w.Write([]byte(`{"access_token":"at-2","token_type":"Bearer"}`))
	}))
	defer srv.Close()

	var waits []time.Duration
	sleep := func(d time.Duration) { waits = append(waits, d) }

	endpoints := &Endpoints{DeviceAuthorizationEndpoint: srv.URL, TokenEndpoint: srv.URL}
	da := &DeviceAuthorization{DeviceCode: "dc-2", Interval: 5}
	if _, err := WaitForAuthorization(context.Background(), nil, endpoints, "app-1", da, sleep); err != nil {
		t.Fatalf("WaitForAuthorization returned error: %v", err)
	}
	if len(waits) != 2 {
		t.Fatalf("waits = %v, want 2 entries", waits)
	}
	if waits[0] != 5*time.Second {
		t.Fatalf("first wait = %v, want 5s", waits[0])
	}
	if waits[1] != 10*time.Second {
		t.Fatalf("wait after slow_down = %v, want 10s", waits[1])
	}
}

// TestWaitForAuthorization_TerminalErrors 验证终止性错误（access_denied、expired_token）立即结束流程。
func TestWaitForAuthorization_TerminalErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		body string
		want string
	}{
		{"access_denied", `{"error":"access_denied"}`, "access_denied"},
		{"expired_token", `{"error":"expired_token"}`, "expired_token"},
		{"invalid_client", `{"error":"invalid_client","error_description":"nope"}`, "invalid_client"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.WriteHeader(http.StatusBadRequest)
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			endpoints := &Endpoints{DeviceAuthorizationEndpoint: srv.URL, TokenEndpoint: srv.URL}
			da := &DeviceAuthorization{DeviceCode: "dc-3", Interval: 1}
			_, err := WaitForAuthorization(context.Background(), nil, endpoints, "app-1", da, func(time.Duration) {})
			if err == nil {
				t.Fatal("expected terminal error, got nil")
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error %q should mention %q", err.Error(), tc.want)
			}
		})
	}
}

// TestWaitForAuthorization_DeviceCodeExpired 验证超过设备码有效期后返回过期错误。
func TestWaitForAuthorization_DeviceCodeExpired(t *testing.T) {
	// 模拟时间流逝：每次 sleep 后时钟前进 200 秒，使第二次轮询前超过 600 秒有效期
	fake := time.Unix(0, 0)
	originalNow := timeNow
	timeNow = func() time.Time { return fake }
	t.Cleanup(func() { timeNow = originalNow })

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"authorization_pending"}`))
	}))
	defer srv.Close()

	endpoints := &Endpoints{DeviceAuthorizationEndpoint: srv.URL, TokenEndpoint: srv.URL}
	da := &DeviceAuthorization{DeviceCode: "dc-4", Interval: 1, ExpiresIn: 600}
	sleep := func(time.Duration) { fake = fake.Add(200 * time.Second) }

	_, err := WaitForAuthorization(context.Background(), nil, endpoints, "app-1", da, sleep)
	if err == nil {
		t.Fatal("expected device code expiry error, got nil")
	}
	if !strings.Contains(err.Error(), "expired") {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestRefreshToken_Success 验证刷新令牌请求携带正确参数并解析响应。
func TestRefreshToken_Success(t *testing.T) {
	var gotGrant, gotRefresh, gotClientID string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		gotGrant = r.PostForm.Get("grant_type")
		gotRefresh = r.PostForm.Get("refresh_token")
		gotClientID = r.PostForm.Get("client_id")
		_, _ = w.Write([]byte(`{"access_token":"at-new","refresh_token":"rt-new","token_type":"Bearer","expires_in":180}`))
	}))
	defer srv.Close()

	endpoints := &Endpoints{TokenEndpoint: srv.URL}
	ts, err := RefreshToken(context.Background(), nil, endpoints, "app-7", "rt-old")
	if err != nil {
		t.Fatalf("RefreshToken returned error: %v", err)
	}
	if gotGrant != "refresh_token" || gotRefresh != "rt-old" || gotClientID != "app-7" {
		t.Fatalf("unexpected form: grant=%q refresh=%q client=%q", gotGrant, gotRefresh, gotClientID)
	}
	if ts.AccessToken != "at-new" || ts.RefreshToken != "rt-new" || ts.ExpiresIn != 180 {
		t.Fatalf("unexpected token set: %#v", ts)
	}
}

// TestRefreshToken_Error 验证刷新被拒绝时返回标准 OAuth 错误。
func TestRefreshToken_Error(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error":"invalid_grant","error_description":"revoked"}`))
	}))
	defer srv.Close()

	endpoints := &Endpoints{TokenEndpoint: srv.URL}
	_, err := RefreshToken(context.Background(), nil, endpoints, "app-7", "rt-bad")
	var oaErr *OAuthError
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !errors.As(err, &oaErr) || oaErr.Code != "invalid_grant" {
		t.Fatalf("expected OAuthError invalid_grant, got: %v", err)
	}
}
