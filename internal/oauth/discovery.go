// Package oauth 实现 OIDC 设备授权流程（RFC 8628）所需的协议能力，
// 包括端点自动发现、设备码申请、轮询换票、刷新令牌，
// 以及供 client 层使用的可自动刷新 Bearer 令牌源。
package oauth

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// wellKnownPath 是 OIDC Discovery 元数据文档的固定路径（OpenID Connect Discovery 1.0）。
const wellKnownPath = "/.well-known/openid-configuration"

// httpClientTimeout 是 oauth 包内所有 HTTP 请求的默认单次超时时间。
const httpClientTimeout = 30 * time.Second

// Endpoints 保存从 OIDC Discovery 元数据中解析出的、设备授权流程所需的端点地址。
type Endpoints struct {
	// DeviceAuthorizationEndpoint 是设备授权端点的完整 URL。
	DeviceAuthorizationEndpoint string
	// TokenEndpoint 是令牌端点的完整 URL。
	TokenEndpoint string
}

// discoveryDocument 是 OIDC Discovery 文档中本工具关心的字段子集。
type discoveryDocument struct {
	DeviceAuthorizationEndpoint string `json:"device_authorization_endpoint"`
	TokenEndpoint               string `json:"token_endpoint"`
}

// Discover 请求 {serviceURL}/.well-known/openid-configuration 并解析出设备授权与令牌端点。
// serviceURL 会规范化（去除末尾斜杠）；两个端点任一缺失时返回错误，
// 通常意味着服务端未启用 OIDC 授权服务器。
func Discover(ctx context.Context, hc *http.Client, serviceURL string) (*Endpoints, error) {
	// 规范化服务地址并拼接发现文档路径
	base := strings.TrimRight(serviceURL, "/")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+wellKnownPath, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create discovery request: %w", err)
	}
	// 声明接受 JSON 响应，避免被授权服务器当作浏览器请求重定向到登录页
	req.Header.Set("Accept", "application/json")

	// 发起请求并校验 HTTP 状态
	if hc == nil {
		hc = &http.Client{Timeout: httpClientTimeout}
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to request OIDC discovery document: %w [%s]", err, req.URL)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("OIDC discovery failed: http error %d: %s [%s]", resp.StatusCode, resp.Status, req.URL)
	}

	// 解析发现文档中本工具关心的端点字段
	var doc discoveryDocument
	if err := json.NewDecoder(resp.Body).Decode(&doc); err != nil {
		return nil, fmt.Errorf("failed to decode OIDC discovery document: %w [%s]", err, req.URL)
	}

	// 校验必需端点存在，缺失时给出可操作的提示
	if doc.DeviceAuthorizationEndpoint == "" || doc.TokenEndpoint == "" {
		return nil, fmt.Errorf(
			"OIDC discovery document is missing required endpoints (device_authorization_endpoint or token_endpoint); is OIDC enabled on %s?",
			base,
		)
	}
	return &Endpoints{
		DeviceAuthorizationEndpoint: doc.DeviceAuthorizationEndpoint,
		TokenEndpoint:               doc.TokenEndpoint,
	}, nil
}

// discardAndClose 读取并丢弃响应体剩余内容后关闭响应，确保底层连接可复用。
func discardAndClose(resp *http.Response) {
	_, _ = io.Copy(io.Discard, resp.Body)
	_ = resp.Body.Close()
}
