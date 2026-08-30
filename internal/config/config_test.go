package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// unsetenv 在测试期间临时取消设置环境变量，并在测试结束后恢复原始状态。
func unsetenv(t *testing.T, key string) {
	t.Helper()
	orig, wasSet := os.LookupEnv(key)
	if err := os.Unsetenv(key); err != nil {
		t.Fatalf("取消环境变量 %s 失败: %v", key, err)
	}
	t.Cleanup(func() {
		if wasSet {
			os.Setenv(key, orig)
		} else {
			os.Unsetenv(key)
		}
	})
}

// writeConfigFile 在指定路径创建配置文件，供测试使用。
func writeConfigFile(t *testing.T, path, content string) {
	t.Helper()
	// 确保父目录存在
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("创建配置目录失败: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("写入配置文件失败: %v", err)
	}
}

// TestLoad_PrefersFlagsOverEnvOverFile 验证加载优先级：命令行标志 > 环境变量 > 配置文件。
func TestLoad_PrefersFlagsOverEnvOverFile(t *testing.T) {
	home := t.TempDir()
	filePath := filepath.Join(home, ".config", "sfc-cli", "config.json")
	writeConfigFile(t, filePath, `{"serviceUrl":"https://file","clientId":"file-client","accessToken":"file-token"}`)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("SFC_SERVICE_URL", "https://env")
	t.Setenv("SFC_CLIENT_ID", "env-client")

	cfg, err := Load(Options{
		ServiceURL: "https://flag",
		ClientID:   "flag-client",
	})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.ServiceURL != "https://flag" || cfg.ClientID != "flag-client" || cfg.AccessToken != "file-token" {
		t.Fatalf("unexpected config: %#v", cfg)
	}
}

// TestLoad_PrefersEnvOverFile 验证环境变量优先于配置文件。
func TestLoad_PrefersEnvOverFile(t *testing.T) {
	home := t.TempDir()
	filePath := filepath.Join(home, ".config", "sfc-cli", "config.json")
	writeConfigFile(t, filePath, `{"serviceUrl":"https://file","clientId":"file-client","accessToken":"file-token"}`)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("SFC_SERVICE_URL", "https://env")
	t.Setenv("SFC_CLIENT_ID", "env-client")

	cfg, err := Load(Options{})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.ServiceURL != "https://env" || cfg.ClientID != "env-client" || cfg.AccessToken != "file-token" {
		t.Fatalf("unexpected config: %#v", cfg)
	}
}

// TestLoad_IgnoresEmptyEnvValues 验证空环境变量不会覆盖配置文件中的值。
func TestLoad_IgnoresEmptyEnvValues(t *testing.T) {
	home := t.TempDir()
	filePath := filepath.Join(home, ".config", "sfc-cli", "config.json")
	writeConfigFile(t, filePath, `{"serviceUrl":"https://file","clientId":"file-client","accessToken":"file-token"}`)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("SFC_SERVICE_URL", "")
	t.Setenv("SFC_CLIENT_ID", "")

	cfg, err := Load(Options{})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.ServiceURL != "https://file" || cfg.ClientID != "file-client" || cfg.AccessToken != "file-token" {
		t.Fatalf("unexpected config: %#v", cfg)
	}
}

// TestLoad_ReadsFromFile 验证可从配置文件中正确读取配置。
func TestLoad_ReadsFromFile(t *testing.T) {
	home := t.TempDir()
	filePath := filepath.Join(home, ".config", "sfc-cli", "config.json")
	writeConfigFile(t, filePath, `{"serviceUrl":"https://file","clientId":"file-client","accessToken":"file-token"}`)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	// 真正取消设置环境变量，避免空字符串干扰文件值读取
	unsetenv(t, "SFC_SERVICE_URL")
	unsetenv(t, "SFC_CLIENT_ID")

	cfg, err := Load(Options{})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.ServiceURL != "https://file" || cfg.ClientID != "file-client" || cfg.AccessToken != "file-token" {
		t.Fatalf("unexpected config: %#v", cfg)
	}
}

// TestLoad_ReportsAllMissingFields 验证所有必填字段缺失时返回包含全部缺失项的错误。
func TestLoad_ReportsAllMissingFields(t *testing.T) {
	// 隔离环境，确保不会从外部读到配置
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	unsetenv(t, "SFC_SERVICE_URL")
	unsetenv(t, "SFC_CLIENT_ID")

	_, err := Load(Options{})
	if err == nil {
		t.Fatal("expected missing config error, got nil")
	}

	msg := err.Error()
	for _, want := range []string{
		"missing required config: service-url, credentials",
		"sfc-cli login",
		"SFC_SERVICE_URL",
		"~/.config/sfc-cli/config.json",
		"serviceUrl",
		"accessToken",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error %q missing %q", msg, want)
		}
	}
	for _, notWant := range []string{"api-ticket", "apiTicket", "SFC_API_TICKET"} {
		if strings.Contains(msg, notWant) {
			t.Fatalf("error %q should not mention %q", msg, notWant)
		}
	}
}

// TestLoad_ReportsMissingCredentials 验证仅有 serviceUrl 而无 OAuth 登录态时，
// 错误提示 credentials 缺失并引导执行 sfc-cli login。
func TestLoad_ReportsMissingCredentials(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("SFC_SERVICE_URL", "https://env")
	unsetenv(t, "SFC_CLIENT_ID")

	_, err := Load(Options{})
	if err == nil {
		t.Fatal("expected missing credentials error, got nil")
	}
	msg := err.Error()
	for _, want := range []string{
		"missing required config: credentials",
		"sfc-cli login",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error %q missing %q", msg, want)
		}
	}
	if strings.Contains(msg, "api-ticket") {
		t.Fatalf("error should not mention api-ticket, got: %q", msg)
	}
}

// TestLoad_SucceedsWithOAuthState 验证配置文件中存在 OAuth 登录态时 Load 成功。
func TestLoad_SucceedsWithOAuthState(t *testing.T) {
	home := t.TempDir()
	filePath := filepath.Join(home, ".config", "sfc-cli", "config.json")
	writeConfigFile(t, filePath, `{"serviceUrl":"https://file","clientId":"app-1","accessToken":"at-1","refreshToken":"rt-1","expiresAt":"2030-01-01T00:00:00Z"}`)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	unsetenv(t, "SFC_SERVICE_URL")
	unsetenv(t, "SFC_CLIENT_ID")

	cfg, err := Load(Options{})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.ClientID != "app-1" || cfg.AccessToken != "at-1" || cfg.RefreshToken != "rt-1" {
		t.Fatalf("unexpected oauth state: %#v", cfg)
	}
	if cfg.ExpiresAt.IsZero() || cfg.ExpiresAt.Year() != 2030 {
		t.Fatalf("unexpected ExpiresAt: %v", cfg.ExpiresAt)
	}
}

// TestLoadBase_DoesNotRequireCredentials 验证 LoadBase 仅要求 serviceUrl，
// 无凭据时不报错；而 Load 在同样条件下报错。
func TestLoadBase_DoesNotRequireCredentials(t *testing.T) {
	home := t.TempDir()
	filePath := filepath.Join(home, ".config", "sfc-cli", "config.json")
	writeConfigFile(t, filePath, `{"serviceUrl":"https://file"}`)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	unsetenv(t, "SFC_SERVICE_URL")
	unsetenv(t, "SFC_CLIENT_ID")

	cfg, err := LoadBase(Options{})
	if err != nil {
		t.Fatalf("LoadBase returned error: %v", err)
	}
	if cfg.ServiceURL != "https://file" {
		t.Fatalf("unexpected ServiceURL: %q", cfg.ServiceURL)
	}
	if _, err := Load(Options{}); err == nil {
		t.Fatal("expected Load to fail without credentials, got nil")
	}
}

// TestLoad_ClientIDPriority 验证 clientId 的优先级：标志 > 环境变量 > 配置文件。
func TestLoad_ClientIDPriority(t *testing.T) {
	home := t.TempDir()
	filePath := filepath.Join(home, ".config", "sfc-cli", "config.json")
	writeConfigFile(t, filePath, `{"serviceUrl":"https://file","accessToken":"t","clientId":"file-client"}`)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	unsetenv(t, "SFC_SERVICE_URL")
	unsetenv(t, "SFC_CLIENT_ID")

	// 配置文件值
	cfg, err := Load(Options{})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.ClientID != "file-client" {
		t.Fatalf("expected file clientId, got %q", cfg.ClientID)
	}

	// 环境变量覆盖文件
	t.Setenv("SFC_CLIENT_ID", "env-client")
	cfg, err = Load(Options{})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.ClientID != "env-client" {
		t.Fatalf("expected env clientId, got %q", cfg.ClientID)
	}

	// 标志覆盖环境变量
	cfg, err = Load(Options{ClientID: "flag-client"})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.ClientID != "flag-client" {
		t.Fatalf("expected flag clientId, got %q", cfg.ClientID)
	}
}

// TestSaveOAuthState_RoundTripAndPreservesOtherKeys 验证 SaveOAuthState 写入登录态，
// 保留文件中的其他键，且随后 Load 能读回登录态。
func TestSaveOAuthState_RoundTripAndPreservesOtherKeys(t *testing.T) {
	home := t.TempDir()
	filePath := filepath.Join(home, ".config", "sfc-cli", "config.json")
	writeConfigFile(t, filePath, `{"serviceUrl":"https://file","customKey":"keep-me"}`)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	expires := time.Date(2030, 5, 1, 12, 0, 0, 0, time.UTC)
	state := OAuthState{ClientID: "app-9", AccessToken: "at-9", RefreshToken: "rt-9", ExpiresAt: expires}
	if err := SaveOAuthState(state); err != nil {
		t.Fatalf("SaveOAuthState returned error: %v", err)
	}

	// 原始文件应保留未知键
	raw, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read config file: %v", err)
	}
	if !strings.Contains(string(raw), "keep-me") {
		t.Fatalf("expected customKey to be preserved, got: %s", raw)
	}

	// Load 读回登录态
	cfg, err := Load(Options{})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	got := cfg.State()
	if got.ClientID != "app-9" || got.AccessToken != "at-9" || got.RefreshToken != "rt-9" {
		t.Fatalf("unexpected state: %#v", got)
	}
	if !got.ExpiresAt.Equal(expires) {
		t.Fatalf("ExpiresAt = %v, want %v", got.ExpiresAt, expires)
	}
}

// TestSaveOAuthState_RemovesLegacyApiTicketKey 验证保存登录态时会清除已弃用的 apiTicket 键。
func TestSaveOAuthState_RemovesLegacyApiTicketKey(t *testing.T) {
	home := t.TempDir()
	filePath := filepath.Join(home, ".config", "sfc-cli", "config.json")
	writeConfigFile(t, filePath, `{"serviceUrl":"https://file","apiTicket":"legacy"}`)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	state := OAuthState{ClientID: "app-1", AccessToken: "at", RefreshToken: "rt"}
	if err := SaveOAuthState(state); err != nil {
		t.Fatalf("SaveOAuthState returned error: %v", err)
	}
	raw, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read config file: %v", err)
	}
	if strings.Contains(string(raw), "apiTicket") {
		t.Fatalf("legacy apiTicket key should be removed, got: %s", raw)
	}
}

// TestSaveOAuthState_ZeroExpiresAt 验证零值过期时间写为空字符串且读回为零值。
func TestSaveOAuthState_ZeroExpiresAt(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	state := OAuthState{ClientID: "app-1", AccessToken: "at", RefreshToken: "rt"}
	if err := SaveOAuthState(state); err != nil {
		t.Fatalf("SaveOAuthState returned error: %v", err)
	}
	cfg, err := Load(Options{ServiceURL: "https://file"})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if !cfg.ExpiresAt.IsZero() {
		t.Fatalf("expected zero ExpiresAt, got %v", cfg.ExpiresAt)
	}
}

// TestSaveOAuthState_RefusesMalformedFile 验证配置文件不是合法 JSON 时
// SaveOAuthState 返回错误而不覆盖文件。
func TestSaveOAuthState_RefusesMalformedFile(t *testing.T) {
	home := t.TempDir()
	filePath := filepath.Join(home, ".config", "sfc-cli", "config.json")
	writeConfigFile(t, filePath, `not json {{{`)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	if err := SaveOAuthState(OAuthState{ClientID: "a", AccessToken: "b", RefreshToken: "c"}); err == nil {
		t.Fatal("expected error for malformed config file, got nil")
	}
	raw, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read config file: %v", err)
	}
	if string(raw) != `not json {{{` {
		t.Fatalf("malformed file should remain untouched, got: %s", raw)
	}
}

// TestLoad_MalformedConfigReturnsError 验证配置文件存在但格式错误时，Load 返回错误而非静默忽略。
func TestLoad_MalformedConfigReturnsError(t *testing.T) {
	home := t.TempDir()
	filePath := filepath.Join(home, ".config", "sfc-cli", "config.json")
	writeConfigFile(t, filePath, `this is not valid json {{{`)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	unsetenv(t, "SFC_SERVICE_URL")

	_, err := Load(Options{ServiceURL: "https://x"})
	if err == nil {
		t.Fatal("Load should return an error for malformed config file, got nil")
	}
	if !strings.Contains(err.Error(), "failed to read config file") {
		t.Fatalf("error should mention config file read failure, got: %v", err)
	}
}
