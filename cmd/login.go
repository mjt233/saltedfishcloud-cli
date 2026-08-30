// Package cmd 定义 sfc-cli 的全部 CLI 命令。
// 当前文件实现 login 子命令，通过 OIDC 设备授权流程（RFC 8628）引导用户完成登录，
// 并将获得的令牌持久化到本地配置文件。
package cmd

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/mjt233/saltedfishcloud-cli/internal/client"
	"github.com/mjt233/saltedfishcloud-cli/internal/config"
	"github.com/mjt233/saltedfishcloud-cli/internal/oauth"
	"github.com/spf13/cobra"
)

// defaultLoginScope 是 login 默认请求的授权范围，
// 覆盖 CLI 全部功能：profile 用于获取用户标识，storage_read/write 用于读写网盘。
const defaultLoginScope = "profile storage_read storage_write"

// oauthRequestTimeout 是 login 流程中单次 HTTP 请求（发现、设备授权、轮询）的超时时间。
const oauthRequestTimeout = 30 * time.Second

// newLoginCommand 构造并返回 login 子命令。
// login 通过 OIDC 设备授权流程完成登录：向服务端申请设备码，
// 引导用户在浏览器完成授权，轮询获得令牌后写入配置文件。
func newLoginCommand() *cobra.Command {
	var (
		clientID string
		scope    string
	)
	cmd := &cobra.Command{
		Use:   "login",
		Short: "Log in via OIDC device authorization flow",
		Long: "Request a device code from the Salted Fish Cloud OIDC authorization server, " +
			"guide the user to complete authorization in a browser, and persist the obtained tokens " +
			"to ~/.config/sfc-cli/config.json. Requires a public OAuth app (token endpoint auth method: none) " +
			"created in the admin console.",
		Args: withArgsHelp(cobra.NoArgs),
		RunE: func(cmd *cobra.Command, args []string) error {
			// 具体流程委托给 runLogin，便于单元测试注入输出
			return runLogin(cmd.Context(), cmd.OutOrStdout(), clientID, scope, nil)
		},
	}

	// 注册 login 专属标志：client id 与授权范围
	cmd.Flags().StringVar(&clientID, "client-id", "", "OAuth client id of your registered public app (required unless set via SFC_CLIENT_ID or config file)")
	cmd.Flags().StringVar(&scope, "scope", defaultLoginScope, "OAuth scopes to request, separated by spaces")
	return cmd
}

// runLogin 执行设备授权登录的完整流程：
// 端点发现 → 申请设备码 → 展示引导信息 → 轮询换票 → 持久化 → 确认身份。
// sleep 为轮询等待函数，nil 表示使用真实等待（测试可注入空实现以加速）。
func runLogin(ctx context.Context, out io.Writer, clientID, scope string, sleep func(time.Duration)) error {
	// 1. 加载基础配置（login 不要求已有凭据，仅要求 serviceUrl）
	cfg, err := config.LoadBase(toConfigOptions())
	if err != nil {
		return err
	}

	// 2. 解析 client id：标志 > 环境变量 > 配置文件（LoadBase 已完成合并）
	if clientID == "" {
		clientID = cfg.ClientID
	}
	if clientID == "" {
		return fmt.Errorf(
			"missing client id: specify --client-id, set SFC_CLIENT_ID, or store clientId in ~/.config/sfc-cli/config.json",
		)
	}

	// 3. OIDC 端点自动发现，不硬编码授权端点路径
	hc := &http.Client{Timeout: oauthRequestTimeout}
	endpoints, err := oauth.Discover(ctx, hc, cfg.ServiceURL)
	if err != nil {
		return err
	}

	// 4. 申请设备码与用户码
	da, err := oauth.RequestDeviceCode(ctx, hc, endpoints, clientID, scope)
	if err != nil {
		return err
	}

	// 5. 展示授权引导信息：同时输出验证地址与用户码，不自动打开浏览器
	fmt.Fprintln(out, "请在浏览器中打开以下地址完成授权：")
	if da.VerificationURIComplete != "" {
		fmt.Fprintf(out, "  %s\n", da.VerificationURIComplete)
	} else {
		fmt.Fprintf(out, "  %s\n", da.VerificationURI)
	}
	fmt.Fprintf(out, "  用户码: %s\n", da.UserCode)
	fmt.Fprintln(out, "等待授权确认…（完成授权后本命令将继续）")

	// 6. 按服务端给定的间隔轮询令牌端点，直到授权完成或流程终止
	ts, err := oauth.WaitForAuthorization(ctx, hc, endpoints, clientID, da, sleep)
	if err != nil {
		return err
	}

	// 7. 持久化登录态到配置文件
	state := config.OAuthState{
		ClientID:     clientID,
		AccessToken:  ts.AccessToken,
		RefreshToken: ts.RefreshToken,
	}
	if ts.ExpiresIn > 0 {
		// 记录过期时间，供后续命令提前刷新令牌
		state.ExpiresAt = time.Now().Add(time.Duration(ts.ExpiresIn) * time.Second)
	}
	if err := config.SaveOAuthState(state); err != nil {
		return err
	}

	// 8. 调用用户资料接口确认身份；失败不阻断登录成功（如未授予 profile 范围）
	source := oauth.NewTokenSource(cfg.ServiceURL, clientID, state, nil)
	cli := client.NewAPIClientWithTokenSource(cfg.ServiceURL, source)
	var profile struct {
		Username string `json:"username"`
		ID       string `json:"id"`
	}
	if err := cli.GetJSON(ctx, "/api/openApi/user/profile/v1", nil, &profile); err == nil && profile.Username != "" {
		fmt.Fprintf(out, "登录成功：%s (uid %s)，令牌已写入 %s\n", profile.Username, profile.ID, configFilePathForDisplay())
	} else {
		fmt.Fprintf(out, "登录成功，令牌已写入 %s\n", configFilePathForDisplay())
	}
	return nil
}

// configFilePathForDisplay 返回配置文件路径用于输出展示；路径解析失败时返回占位描述。
func configFilePathForDisplay() string {
	path, err := config.FilePath()
	if err != nil {
		return "~/.config/sfc-cli/config.json"
	}
	return path
}
