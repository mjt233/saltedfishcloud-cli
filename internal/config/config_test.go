package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
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
	writeConfigFile(t, filePath, `{"serviceUrl":"https://file","apiTicket":"file-ticket"}`)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("SFC_SERVICE_URL", "https://env")
	t.Setenv("SFC_API_TICKET", "env-ticket")

	cfg, err := Load(Options{
		ServiceURL: "https://flag",
		APITicket:  "flag-ticket",
	})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.ServiceURL != "https://flag" || cfg.APITicket != "flag-ticket" {
		t.Fatalf("unexpected config: %#v", cfg)
	}
}

// TestLoad_PrefersEnvOverFile 验证环境变量优先于配置文件。
func TestLoad_PrefersEnvOverFile(t *testing.T) {
	home := t.TempDir()
	filePath := filepath.Join(home, ".config", "sfc-cli", "config.json")
	writeConfigFile(t, filePath, `{"serviceUrl":"https://file","apiTicket":"file-ticket"}`)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("SFC_SERVICE_URL", "https://env")
	t.Setenv("SFC_API_TICKET", "env-ticket")

	cfg, err := Load(Options{})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.ServiceURL != "https://env" || cfg.APITicket != "env-ticket" {
		t.Fatalf("unexpected config: %#v", cfg)
	}
}

// TestLoad_IgnoresEmptyEnvValues 验证空环境变量不会覆盖配置文件中的值。
func TestLoad_IgnoresEmptyEnvValues(t *testing.T) {
	home := t.TempDir()
	filePath := filepath.Join(home, ".config", "sfc-cli", "config.json")
	writeConfigFile(t, filePath, `{"serviceUrl":"https://file","apiTicket":"file-ticket"}`)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("SFC_SERVICE_URL", "")
	t.Setenv("SFC_API_TICKET", "")

	cfg, err := Load(Options{})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.ServiceURL != "https://file" || cfg.APITicket != "file-ticket" {
		t.Fatalf("unexpected config: %#v", cfg)
	}
}

// TestLoad_ReadsFromFile 验证可从配置文件中正确读取配置。
func TestLoad_ReadsFromFile(t *testing.T) {
	home := t.TempDir()
	filePath := filepath.Join(home, ".config", "sfc-cli", "config.json")
	writeConfigFile(t, filePath, `{"serviceUrl":"https://file","apiTicket":"file-ticket"}`)
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	// 真正取消设置环境变量，避免空字符串干扰文件值读取
	unsetenv(t, "SFC_SERVICE_URL")
	unsetenv(t, "SFC_API_TICKET")

	cfg, err := Load(Options{})
	if err != nil {
		t.Fatalf("Load returned error: %v", err)
	}
	if cfg.ServiceURL != "https://file" || cfg.APITicket != "file-ticket" {
		t.Fatalf("unexpected config: %#v", cfg)
	}
}

// TestLoad_ReportsAllMissingFields 验证所有必填字段缺失时返回包含全部字段名的错误。
func TestLoad_ReportsAllMissingFields(t *testing.T) {
	// 隔离环境，确保不会从外部读到配置
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	unsetenv(t, "SFC_SERVICE_URL")
	unsetenv(t, "SFC_API_TICKET")

	_, err := Load(Options{})
	if err == nil || err.Error() != "missing required config: serviceUrl, apiTicket" {
		t.Fatalf("unexpected error: %v", err)
	}
}

// TestLoad_ReportsSingleMissingField 验证仅单个字段缺失时错误信息只列出该字段。
func TestLoad_ReportsSingleMissingField(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("SFC_SERVICE_URL", "https://env")
	unsetenv(t, "SFC_API_TICKET")

	_, err := Load(Options{})
	if err == nil || err.Error() != "missing required config: apiTicket" {
		t.Fatalf("unexpected error: %v", err)
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
	unsetenv(t, "SFC_API_TICKET")

	_, err := Load(Options{ServiceURL: "https://x", APITicket: "t"})
	if err == nil {
		t.Fatal("Load should return an error for malformed config file, got nil")
	}
	if !strings.Contains(err.Error(), "failed to read config file") {
		t.Fatalf("error should mention config file read failure, got: %v", err)
	}
}
