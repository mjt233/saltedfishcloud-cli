package oauth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// deviceCodeGrantType 是设备授权换票使用的标准 grant_type 值（RFC 8628 §3.4）。
const deviceCodeGrantType = "urn:ietf:params:oauth:grant-type:device_code"

// defaultPollInterval 是设备授权响应未给出 interval 时的默认轮询间隔（RFC 8628 §3.2）。
const defaultPollInterval = 5 * time.Second

// slowDownIncrement 是收到 slow_down 错误时需要在原间隔上增加的秒数（RFC 8628 §3.5）。
const slowDownIncrement = 5 * time.Second

// timeNow 返回当前时间，测试可替换以模拟设备码过期。
var timeNow = time.Now

// defaultDeviceCodeLifetime 是设备授权响应未给出 expires_in 时的默认有效时长。
const defaultDeviceCodeLifetime = 10 * time.Minute

// pkceCodeVerifierBytes 是生成 code_verifier 时读取的随机字节数。
// 32 字节经 base64url 无填充编码后为 43 个字符，满足 RFC 7636 §4.1 的 43–128 长度要求。
const pkceCodeVerifierBytes = 32

// DeviceAuthorization 是设备授权端点的响应结构（RFC 8628 §3.2），
// 并附带本端生成的 PKCE 状态（RFC 7636），不来自服务端 JSON。
type DeviceAuthorization struct {
	// DeviceCode 是设备确认码，轮询换票时携带。
	DeviceCode string `json:"device_code"`
	// UserCode 是展示给用户、需要在验证页面输入的用户码。
	UserCode string `json:"user_code"`
	// VerificationURI 是用户需要访问的验证页面地址。
	VerificationURI string `json:"verification_uri"`
	// VerificationURIComplete 是已预填用户码的验证页面地址，可直接访问。
	VerificationURIComplete string `json:"verification_uri_complete"`
	// ExpiresIn 是设备码的有效秒数。
	ExpiresIn int64 `json:"expires_in"`
	// Interval 是轮询令牌端点的最小间隔秒数。
	Interval int `json:"interval"`
	// CodeVerifier 是申请设备码时本地生成的 PKCE 校验串，仅用于后续换票，不参与 JSON 反序列化。
	CodeVerifier string `json:"-"`
}

// TokenSet 是令牌端点成功响应的结构（RFC 6749 §5.1）。
type TokenSet struct {
	// AccessToken 是用于调用 API 的短期访问令牌。
	AccessToken string `json:"access_token"`
	// RefreshToken 是用于换新的长期刷新令牌，可能随刷新轮换。
	RefreshToken string `json:"refresh_token"`
	// TokenType 是令牌类型，固定为 Bearer。
	TokenType string `json:"token_type"`
	// ExpiresIn 是访问令牌的有效秒数。
	ExpiresIn int64 `json:"expires_in"`
	// Scope 是实际授权的范围。
	Scope string `json:"scope"`
}

// OAuthError 是令牌/设备授权端点返回的标准 OAuth 2.0 错误（RFC 6749 §5.2）。
type OAuthError struct {
	// Code 是标准错误码，如 authorization_pending、slow_down、access_denied。
	Code string `json:"error"`
	// Description 是服务端附带的可读错误描述。
	Description string `json:"error_description"`
}

// Error 实现 error 接口，输出错误码与可选的错误描述。
func (e *OAuthError) Error() string {
	if e.Description == "" {
		return e.Code
	}
	return fmt.Sprintf("%s: %s", e.Code, e.Description)
}

// RequestDeviceCode 调用设备授权端点申请设备码与用户码。
// scope 为空时不携带 scope 参数（由服务端决定默认授权范围）。
// 始终附带 PKCE S256（RFC 7636）：生成 code_verifier，发送 code_challenge 与 code_challenge_method，
// 并将 code_verifier 保存在返回结构中供后续换票使用。
func RequestDeviceCode(ctx context.Context, hc *http.Client, endpoints *Endpoints, clientID, scope string) (*DeviceAuthorization, error) {
	// 生成本次设备授权绑定的 PKCE 参数
	verifier, challenge, err := generatePKCE()
	if err != nil {
		return nil, err
	}

	// 组装表单参数：公共客户端携带 client_id、scope 与 PKCE challenge
	form := url.Values{
		"client_id":             {clientID},
		"code_challenge":        {challenge},
		"code_challenge_method": {"S256"},
	}
	if scope != "" {
		form.Set("scope", scope)
	}
	resp, err := postForm(ctx, hc, endpoints.DeviceAuthorizationEndpoint, form)
	if err != nil {
		return nil, err
	}
	defer discardAndClose(resp)

	// 非成功状态时优先按标准 OAuth 错误解析
	if resp.StatusCode >= 400 {
		return nil, oauthErrorFromResponse(resp)
	}

	// 解析设备授权响应，并挂上本地 code_verifier（不来自服务端）
	var da DeviceAuthorization
	if err := json.NewDecoder(resp.Body).Decode(&da); err != nil {
		return nil, fmt.Errorf("failed to decode device authorization response: %w [%s]", err, endpoints.DeviceAuthorizationEndpoint)
	}
	if da.DeviceCode == "" || da.UserCode == "" {
		return nil, fmt.Errorf("device authorization response missing device_code or user_code [%s]", endpoints.DeviceAuthorizationEndpoint)
	}
	da.CodeVerifier = verifier
	return &da, nil
}

// WaitForAuthorization 按设备授权响应给定的间隔轮询令牌端点，
// 直到用户在浏览器完成授权并返回令牌，或发生终止性错误。
// authorization_pending 表示用户尚未完成授权，继续等待；
// slow_down 表示轮询过快，间隔增加 5 秒后继续；
// 其他错误（如 access_denied、expired_token、invalid_client）立即终止。
// sleep 参数可注入以便测试；为 nil 时使用可被 ctx 取消的真实等待。
func WaitForAuthorization(ctx context.Context, hc *http.Client, endpoints *Endpoints, clientID string, da *DeviceAuthorization, sleep func(time.Duration)) (*TokenSet, error) {
	// 未注入 sleep 时使用基于定时器的实现，等待期间可响应 ctx 取消
	if sleep == nil {
		sleep = func(d time.Duration) {
			timer := time.NewTimer(d)
			defer timer.Stop()
			select {
			case <-ctx.Done():
			case <-timer.C:
			}
		}
	}

	// 初始化轮询间隔与设备码有效期，缺失时采用 RFC 默认值
	interval := defaultPollInterval
	if da.Interval >= 1 {
		interval = time.Duration(da.Interval) * time.Second
	}
	lifetime := defaultDeviceCodeLifetime
	if da.ExpiresIn > 0 {
		lifetime = time.Duration(da.ExpiresIn) * time.Second
	}
	deadline := timeNow().Add(lifetime)

	// 轮询循环：先等待再请求，避免首次请求即违反服务端间隔要求
	for {
		sleep(interval)
		if err := ctx.Err(); err != nil {
			return nil, fmt.Errorf("device authorization canceled: %w", err)
		}
		if timeNow().After(deadline) {
			return nil, fmt.Errorf("device code expired before authorization was completed; please run login again")
		}

		ts, err := pollTokenOnce(ctx, hc, endpoints, clientID, da.DeviceCode, da.CodeVerifier)
		if err == nil {
			return ts, nil
		}

		// 非 OAuth 标准错误（网络错误、非 JSON 响应等）直接终止
		var oaErr *OAuthError
		if !errors.As(err, &oaErr) {
			return nil, err
		}

		// 按标准错误码决定继续等待或终止
		switch oaErr.Code {
		case "authorization_pending":
			// 用户尚未完成授权，继续按当前间隔轮询
			continue
		case "slow_down":
			// 服务端要求放慢轮询速度，间隔增加 5 秒
			interval += slowDownIncrement
			continue
		default:
			return nil, fmt.Errorf("device authorization failed: %s", oaErr.Error())
		}
	}
}

// RefreshToken 使用长期刷新令牌换取新的令牌集合（RFC 6749 §6）。
// 公共客户端仅需携带 client_id，无需客户端密钥。
func RefreshToken(ctx context.Context, hc *http.Client, endpoints *Endpoints, clientID, refreshToken string) (*TokenSet, error) {
	form := url.Values{
		"grant_type":    {"refresh_token"},
		"refresh_token": {refreshToken},
		"client_id":     {clientID},
	}
	resp, err := postForm(ctx, hc, endpoints.TokenEndpoint, form)
	if err != nil {
		return nil, err
	}
	defer discardAndClose(resp)

	if resp.StatusCode >= 400 {
		return nil, oauthErrorFromResponse(resp)
	}

	var ts TokenSet
	if err := json.NewDecoder(resp.Body).Decode(&ts); err != nil {
		return nil, fmt.Errorf("failed to decode token response: %w [%s]", err, endpoints.TokenEndpoint)
	}
	if ts.AccessToken == "" {
		return nil, fmt.Errorf("token response missing access_token [%s]", endpoints.TokenEndpoint)
	}
	return &ts, nil
}

// generatePKCE 按 RFC 7636 生成 code_verifier 与 S256 code_challenge。
// code_verifier 为 43 字符的 base64url 无填充高熵串；
// code_challenge 为 BASE64URL-ENCODE(SHA256(code_verifier))（无填充）。
func generatePKCE() (verifier, challenge string, err error) {
	raw := make([]byte, pkceCodeVerifierBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", "", fmt.Errorf("failed to generate PKCE code_verifier: %w", err)
	}
	verifier = base64.RawURLEncoding.EncodeToString(raw)
	sum := sha256.Sum256([]byte(verifier))
	challenge = base64.RawURLEncoding.EncodeToString(sum[:])
	return verifier, challenge, nil
}

// pollTokenOnce 执行一次设备授权换票请求，成功返回令牌集合，
// 失败时若服务端返回标准 OAuth 错误则返回 *OAuthError 供调用方分支处理。
// codeVerifier 非空时按 RFC 7636 附带 code_verifier（与申请设备码时的 challenge 配对）。
func pollTokenOnce(ctx context.Context, hc *http.Client, endpoints *Endpoints, clientID, deviceCode, codeVerifier string) (*TokenSet, error) {
	form := url.Values{
		"grant_type":  {deviceCodeGrantType},
		"device_code": {deviceCode},
		"client_id":   {clientID},
	}
	// 申请阶段启用了 PKCE 时，换票必须回传同一 code_verifier
	if codeVerifier != "" {
		form.Set("code_verifier", codeVerifier)
	}
	resp, err := postForm(ctx, hc, endpoints.TokenEndpoint, form)
	if err != nil {
		return nil, err
	}
	defer discardAndClose(resp)

	if resp.StatusCode >= 400 {
		return nil, oauthErrorFromResponse(resp)
	}

	var ts TokenSet
	if err := json.NewDecoder(resp.Body).Decode(&ts); err != nil {
		return nil, fmt.Errorf("failed to decode token response: %w [%s]", err, endpoints.TokenEndpoint)
	}
	if ts.AccessToken == "" {
		return nil, fmt.Errorf("token response missing access_token [%s]", endpoints.TokenEndpoint)
	}
	return &ts, nil
}

// postForm 向指定端点发送 application/x-www-form-urlencoded 的 POST 请求并返回响应。
// hc 为 nil 时使用带默认超时的客户端。
func postForm(ctx context.Context, hc *http.Client, endpoint string, form url.Values) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// 声明接受 JSON 响应：Spring 授权服务器对未声明 Accept 的请求按浏览器处理，
	// 客户端认证失败时会重定向到登录页而非返回标准 OAuth 错误
	req.Header.Set("Accept", "application/json")

	if hc == nil {
		hc = &http.Client{Timeout: httpClientTimeout}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("http request failed: %w [%s]", err, req.URL)
	}
	return resp, nil
}

// oauthErrorFromResponse 尝试将非 2xx 响应体解析为标准 OAuth 错误；
// 解析失败时回退为包含响应片段的通用 HTTP 错误，
// 响应体为空时按状态码给出可操作的排查提示。
func oauthErrorFromResponse(resp *http.Response) error {
	// 读取响应体并限制大小，防止异常服务端返回超大内容
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	trimmed := strings.TrimSpace(string(body))

	// 优先按标准 OAuth 错误 JSON 解析
	var oaErr OAuthError
	if err := json.Unmarshal([]byte(trimmed), &oaErr); err == nil && oaErr.Code != "" {
		return &oaErr
	}

	// 无响应体时的兜底提示：401 通常意味着客户端认证失败
	if trimmed == "" {
		if resp.StatusCode == http.StatusUnauthorized {
			return fmt.Errorf(
				"http error 401: client authentication failed; check client_id, whether the app is enabled with OIDC support, and whether its token endpoint auth method is set to none [%s]",
				resp.Request.URL,
			)
		}
		return fmt.Errorf("http error %d: %s [%s]", resp.StatusCode, resp.Status, resp.Request.URL)
	}
	return fmt.Errorf("http error %d: %s [%s]", resp.StatusCode, trimmed, resp.Request.URL)
}
