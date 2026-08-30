package oauth

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/mjt233/saltedfishcloud-cli/internal/config"
)

// expiryMargin 是主动刷新令牌的安全边界：距过期不足该时长即提前刷新，
// 避免请求发出后令牌恰好在途中过期。
const expiryMargin = 60 * time.Second

// TokenSource 基于本地持久化的 OAuth 登录态提供 Bearer 令牌，
// 实现 client 包的 TokenSource 与 RefreshableTokenSource 接口：
// Token 在令牌临近过期时自动用 refresh_token 换新并回写持久化回调；
// RefreshToken 在令牌被服务端拒绝（401）时强制换新。
type TokenSource struct {
	// mu 串行化令牌状态访问与刷新，避免并发刷新造成 refresh_token 轮换丢失。
	mu sync.Mutex

	// hc 是用于发现与刷新请求的 HTTP 客户端，测试可注入替换。
	hc *http.Client
	// serviceURL 是服务根地址，用于懒加载 OIDC 端点发现。
	serviceURL string
	// clientID 是登录所用公共客户端的 client_id，刷新时必须携带。
	clientID string
	// state 是当前内存中的登录态，刷新成功后同步更新。
	state config.OAuthState
	// save 是持久化回调（写入配置文件），nil 表示不回写。
	save func(config.OAuthState) error
	// now 返回当前时间，测试可注入以模拟过期。
	now func() time.Time
	// endpoints 是懒加载的端点发现结果，首次刷新时才发起发现请求。
	endpoints *Endpoints
}

// NewTokenSource 构造一个基于 OAuth 登录态的令牌源。
// state 为本地持久化的登录状态；save 为刷新成功后的回写回调，可为 nil。
func NewTokenSource(serviceURL, clientID string, state config.OAuthState, save func(config.OAuthState) error) *TokenSource {
	return &TokenSource{
		hc:         &http.Client{Timeout: httpClientTimeout},
		serviceURL: strings.TrimRight(serviceURL, "/"),
		clientID:   clientID,
		state:      state,
		save:       save,
		now:        time.Now,
	}
}

// Token 返回当前可用的 Bearer 令牌（实现 client.TokenSource）。
// 令牌存在且距过期时间超过安全边界时直接返回；
// 否则（已过期或无令牌）尝试用 refresh_token 换新并回写。
func (s *TokenSource) Token() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// 快路径：令牌有效且未临近过期（expiresAt 未知时视为长期有效，交由 401 兜底）
	if s.state.AccessToken != "" &&
		(s.state.ExpiresAt.IsZero() || s.now().Before(s.state.ExpiresAt.Add(-expiryMargin))) {
		return s.state.AccessToken, nil
	}
	// 慢路径：过期或缺失时主动刷新
	if err := s.refreshLocked(); err != nil {
		return "", err
	}
	return s.state.AccessToken, nil
}

// RefreshToken 强制用 refresh_token 换取新令牌并回写（实现 client.RefreshableTokenSource）。
// 用于令牌已被服务端拒绝（HTTP 401）的场景。
func (s *TokenSource) RefreshToken() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if err := s.refreshLocked(); err != nil {
		return "", err
	}
	return s.state.AccessToken, nil
}

// refreshLocked 使用 refresh_token 换取新令牌并更新内存状态、回写持久化。
// 调用方必须已持有 s.mu。
func (s *TokenSource) refreshLocked() error {
	// 前置校验：没有刷新令牌说明从未登录过，给出可操作的提示
	if s.state.RefreshToken == "" {
		return fmt.Errorf("not logged in: run `sfc-cli login --client-id <id>` first")
	}
	if s.clientID == "" {
		return fmt.Errorf("missing clientId for token refresh; run `sfc-cli login` again")
	}

	// 懒加载端点发现结果，避免构造令牌源时就产生网络请求
	if s.endpoints == nil {
		endpoints, err := Discover(context.Background(), s.hc, s.serviceURL)
		if err != nil {
			return fmt.Errorf("failed to discover OIDC endpoints for token refresh: %w", err)
		}
		s.endpoints = endpoints
	}

	// 调用令牌端点换新
	ts, err := RefreshToken(context.Background(), s.hc, s.endpoints, s.clientID, s.state.RefreshToken)
	if err != nil {
		return fmt.Errorf("failed to refresh access token: %w", err)
	}

	// 更新内存状态：服务端轮换 refresh_token 时采用新值，否则沿用旧值
	s.state.AccessToken = ts.AccessToken
	if ts.RefreshToken != "" {
		s.state.RefreshToken = ts.RefreshToken
	}
	if ts.ExpiresIn > 0 {
		s.state.ExpiresAt = s.now().Add(time.Duration(ts.ExpiresIn) * time.Second)
	} else {
		s.state.ExpiresAt = time.Time{}
	}

	// 回写持久化；失败时令牌虽可用但刷新令牌可能已轮换丢失，必须报错提示
	if s.save != nil {
		if err := s.save(s.state); err != nil {
			return fmt.Errorf("token refreshed but failed to persist: %w", err)
		}
	}
	return nil
}
