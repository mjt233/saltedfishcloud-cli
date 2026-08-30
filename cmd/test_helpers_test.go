package cmd

import (
	"os"
	"path/filepath"
	"testing"
)

// isolateOAuthConfig 为命令测试写入隔离的 OAuth 登录态配置。
// 使用临时 HOME，避免依赖已弃用的 --api-ticket 标志，也不污染真实用户配置。
func isolateOAuthConfig(t *testing.T) {
	t.Helper()
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("SFC_SERVICE_URL", "")
	t.Setenv("SFC_CLIENT_ID", "")

	cfgDir := filepath.Join(home, ".config", "sfc-cli")
	if err := os.MkdirAll(cfgDir, 0o700); err != nil {
		t.Fatalf("创建配置目录失败: %v", err)
	}
	content := `{"clientId":"test-client","accessToken":"test-token","refreshToken":"test-refresh"}`
	if err := os.WriteFile(filepath.Join(cfgDir, "config.json"), []byte(content), 0o600); err != nil {
		t.Fatalf("写入配置文件失败: %v", err)
	}

	serviceURL = ""
	t.Cleanup(func() { serviceURL = "" })
}
