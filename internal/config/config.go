// Package config 负责从配置文件、环境变量和命令行标志中加载并校验运行时配置，
// 以及 OAuth 登录态的持久化保存。
// 优先级固定为：命令行标志 > 环境变量 > ~/.config/sfc-cli/config.json。
// 开发期间支持从当前工作目录的 .env 文件自动加载环境变量。
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/viper"
	"github.com/subosito/gotenv"
)

// Options 封装由命令行标志层传入的配置覆盖值。非空字段将以最高优先级覆盖环境变量和文件中的同名配置。
type Options struct {
	// ServiceURL 为命令行 --service-url 标志的值；留空则不覆盖低优先级来源。
	ServiceURL string
	// ClientID 为命令行 --client-id 标志的值；留空则不覆盖低优先级来源。
	ClientID string
}

// Config 是经过合并与校验后的运行时配置，供各子命令直接使用。
type Config struct {
	// ServiceURL 是咸鱼云服务的基础 URL。
	ServiceURL string
	// ClientID 是 OAuth 登录所用公共客户端的 client_id。
	ClientID string
	// AccessToken 是 OAuth 登录获得的短期访问令牌。
	AccessToken string
	// RefreshToken 是 OAuth 登录获得的长期刷新令牌。
	RefreshToken string
	// ExpiresAt 是访问令牌的过期时间；零值表示未知（由 401 兜底刷新）。
	ExpiresAt time.Time
}

// OAuthState 描述持久化在配置文件中的 OAuth 登录态。
type OAuthState struct {
	// ClientID 是登录所用公共客户端的 client_id，刷新令牌时必须携带。
	ClientID string `json:"clientId"`
	// AccessToken 是短期访问令牌。
	AccessToken string `json:"accessToken"`
	// RefreshToken 是长期刷新令牌，可能随刷新轮换。
	RefreshToken string `json:"refreshToken"`
	// ExpiresAt 是访问令牌的过期时间；零值表示未知。
	ExpiresAt time.Time `json:"expiresAt"`
}

// oauthStateFromConfig 从已合并的运行时配置中提取 OAuth 登录态。
func oauthStateFromConfig(cfg Config) OAuthState {
	return OAuthState{
		ClientID:     cfg.ClientID,
		AccessToken:  cfg.AccessToken,
		RefreshToken: cfg.RefreshToken,
		ExpiresAt:    cfg.ExpiresAt,
	}
}

// FilePath 返回配置文件的绝对路径（~/.config/sfc-cli/config.json）。
// 无法获取用户家目录时返回错误。
func FilePath() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to resolve user home directory: %w", err)
	}
	return filepath.Join(home, ".config", "sfc-cli", "config.json"), nil
}

// Load 按照"标志 > 环境变量 > 配置文件"的优先级合并配置，并校验全部必填字段：
// serviceUrl 必填；凭据要求已通过 sfc-cli login 获得 accessToken。
// 所有缺失项在同一个错误中一次性指出。
// 配置文件不存在时静默忽略；文件存在但解析失败时返回错误。
// 开发期间支持从当前工作目录的 .env 文件自动加载环境变量。
func Load(opts Options) (Config, error) {
	cfg, err := loadMerged(opts)
	if err != nil {
		return Config{}, err
	}

	// 校验必填字段，收集所有缺失项后一次性报错
	var missing []string
	if cfg.ServiceURL == "" {
		missing = append(missing, "service-url")
	}
	if cfg.AccessToken == "" {
		missing = append(missing, "credentials (OAuth login)")
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf(
			"missing required config: %s\n"+
				"run `sfc-cli login --client-id <id>` to obtain an OAuth token,\n"+
				"or configure with flags: --service-url\n"+
				"or environment: SFC_SERVICE_URL, SFC_CLIENT_ID\n"+
				"or config file: ~/.config/sfc-cli/config.json (keys: serviceUrl, accessToken, refreshToken, clientId)",
			strings.Join(missing, ", "),
		)
	}
	return cfg, nil
}

// LoadBase 按照"标志 > 环境变量 > 配置文件"的优先级合并配置，
// 但仅校验 serviceUrl，不要求已存在凭据。
// 供 login 等尚未持有凭据的命令使用。
func LoadBase(opts Options) (Config, error) {
	cfg, err := loadMerged(opts)
	if err != nil {
		return Config{}, err
	}

	// 仅校验 serviceUrl；凭据可在 login 完成后再具备
	if cfg.ServiceURL == "" {
		return Config{}, fmt.Errorf(
			"missing required config: service-url\n" +
				"configure with flags: --service-url\n" +
				"or environment: SFC_SERVICE_URL\n" +
				"or config file: ~/.config/sfc-cli/config.json (key: serviceUrl)",
		)
	}
	return cfg, nil
}

// loadMerged 完成 .env 加载与"标志 > 环境变量 > 配置文件"的合并，不做任何校验。
func loadMerged(opts Options) (Config, error) {
	// 尝试从当前工作目录加载 .env 文件，开发期间便于配置环境变量
	// .env 文件不存在时静默忽略，不影响正常启动
	if err := gotenv.Load(); err != nil && !os.IsNotExist(err) {
		// .env 文件存在但解析失败时，记录警告但不阻断启动
		fmt.Fprintf(os.Stderr, "warning: failed to load .env file: %v\n", err)
	}

	// 第一阶段：分别初始化文件层和环境变量层的 Viper 实例。
	fileViper, err := newFileViper()
	if err != nil {
		return Config{}, fmt.Errorf("failed to read config file: %w", err)
	}
	envViper := newEnvViper()

	// 第二阶段：按优先级合并。文件值先进入基础配置；
	// 仅当环境变量非空时，才用 Viper 读取到的环境值覆盖文件值。
	cfg := Config{
		ServiceURL:   fileViper.GetString("serviceUrl"),
		ClientID:     fileViper.GetString("clientId"),
		AccessToken:  fileViper.GetString("accessToken"),
		RefreshToken: fileViper.GetString("refreshToken"),
		ExpiresAt:    parseExpiresAt(fileViper.GetString("expiresAt")),
	}
	if val, ok := os.LookupEnv("SFC_SERVICE_URL"); ok && val != "" {
		cfg.ServiceURL = envViper.GetString("serviceUrl")
	}
	if val, ok := os.LookupEnv("SFC_CLIENT_ID"); ok && val != "" {
		cfg.ClientID = envViper.GetString("clientId")
	}
	if opts.ServiceURL != "" {
		cfg.ServiceURL = opts.ServiceURL
	}
	if opts.ClientID != "" {
		cfg.ClientID = opts.ClientID
	}

	return cfg, nil
}

// State 返回当前配置中保存的 OAuth 登录态。
func (c Config) State() OAuthState {
	return oauthStateFromConfig(c)
}

// SaveOAuthState 将 OAuth 登录态合并写入配置文件，保留文件中的其他键。
// 采用"读取现有 JSON → 更新登录态键 → 原子写回"的方式，
// 文件不存在时创建（目录权限 0700、文件权限 0600），
// 文件存在但不是合法 JSON 时返回错误而不覆盖，避免破坏用户配置。
func SaveOAuthState(state OAuthState) error {
	// 解析配置文件路径
	path, err := FilePath()
	if err != nil {
		return err
	}

	// 读取现有配置内容到 map，保留未知键
	data := make(map[string]any)
	raw, readErr := os.ReadFile(path)
	if readErr == nil {
		if err := json.Unmarshal(raw, &data); err != nil {
			return fmt.Errorf("config file %s is not valid JSON, refusing to overwrite: %w", path, err)
		}
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return fmt.Errorf("failed to read config file: %w", readErr)
	}

	// 更新登录态键；expiresAt 零值存为空字符串
	data["clientId"] = state.ClientID
	data["accessToken"] = state.AccessToken
	data["refreshToken"] = state.RefreshToken
	if state.ExpiresAt.IsZero() {
		data["expiresAt"] = ""
	} else {
		data["expiresAt"] = state.ExpiresAt.Format(time.RFC3339)
	}
	// 清除已弃用的 apiTicket 键，避免旧配置残留
	delete(data, "apiTicket")

	// 序列化并确保配置目录存在
	content, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to encode config: %w", err)
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	// 原子写回：先写临时文件再重命名，避免中断导致配置损坏
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, content, 0o600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("failed to replace config file: %w", err)
	}
	return nil
}

// parseExpiresAt 解析以 RFC3339 存储的过期时间字符串；空串或解析失败时返回零值时间。
func parseExpiresAt(val string) time.Time {
	if val == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, val)
	if err != nil {
		return time.Time{}
	}
	return t
}

// newFileViper 构造并返回一个仅读取配置文件的 Viper 实例。
// 读取 ~/.config/sfc-cli/config.json；文件不存在时静默忽略，
// 文件存在但无法解析时返回错误。
func newFileViper() (*viper.Viper, error) {
	v := viper.New()

	home, err := os.UserHomeDir()
	if err != nil {
		// 无法获取家目录，跳过文件加载
		return v, nil
	}

	// 构造配置文件绝对路径并尝试加载
	cfgPath := filepath.Join(home, ".config", "sfc-cli", "config.json")
	v.SetConfigFile(cfgPath)
	if err := v.ReadInConfig(); err != nil {
		// 文件不存在属于正常情况，静默忽略
		if errors.Is(err, os.ErrNotExist) {
			return v, nil
		}
		// 其他错误（解析失败、权限不足等）需要向上传递
		return nil, err
	}

	return v, nil
}

// newEnvViper 构造并返回一个用于读取环境变量的 Viper 实例。
// 仅负责将 SFC_* 环境变量映射到内部配置键，不直接处理空值覆盖逻辑。
func newEnvViper() *viper.Viper {
	v := viper.New()
	v.SetEnvPrefix("SFC")
	v.SetEnvKeyReplacer(strings.NewReplacer(
		"SERVICEURL", "SERVICE_URL",
		"CLIENTID", "CLIENT_ID",
	))
	v.AutomaticEnv()
	_ = v.BindEnv("serviceUrl")
	_ = v.BindEnv("clientId")
	return v
}
