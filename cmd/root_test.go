package cmd

import "testing"

// TestNewRootCommand_RegistersGlobalFlags 验证根命令注册了必要的全局持久标志。
func TestNewRootCommand_RegistersGlobalFlags(t *testing.T) {
	root := NewRootCommand()
	// 逐一检查持久标志是否已注册
	for _, flagName := range []string{"api-ticket", "service-url"} {
		if root.PersistentFlags().Lookup(flagName) == nil {
			t.Fatalf("缺少持久标志 %q", flagName)
		}
	}
}

// TestNewRootCommand_FlagsBoundToPackageVars 验证标志值在解析后正确写入包级变量。
func TestNewRootCommand_FlagsBoundToPackageVars(t *testing.T) {
	// 重置包级变量，防止测试间相互污染
	apiTicket = ""
	serviceURL = ""

	root := NewRootCommand()
	root.SetArgs([]string{"--api-ticket", "my-ticket", "--service-url", "http://sfc.example.com"})
	// 执行根命令以触发标志解析
	_ = root.Execute()

	if apiTicket != "my-ticket" {
		t.Errorf("apiTicket = %q, want %q", apiTicket, "my-ticket")
	}
	if serviceURL != "http://sfc.example.com" {
		t.Errorf("serviceURL = %q, want %q", serviceURL, "http://sfc.example.com")
	}
}

// TestRootOptions_ToConfigOptions 验证 rootOptions 的 toConfigOptions 方法
// 能正确将自身字段转换为 config.Options。
func TestRootOptions_ToConfigOptions(t *testing.T) {
	ro := rootOptions{ServiceURL: "https://example.com", APITicket: "secret"}
	co := ro.toConfigOptions()
	if co.ServiceURL != "https://example.com" {
		t.Errorf("ServiceURL = %q, want %q", co.ServiceURL, "https://example.com")
	}
	if co.APITicket != "secret" {
		t.Errorf("APITicket = %q, want %q", co.APITicket, "secret")
	}
}

// TestToConfigOptions_UsesRootOptions 验证包级 toConfigOptions 函数使用 rootOptions 的方法进行转换。
func TestToConfigOptions_UsesRootOptions(t *testing.T) {
	// 重置包级变量，再写入已知值
	apiTicket = "pkg-ticket"
	serviceURL = "https://pkg.example.com"
	t.Cleanup(func() { apiTicket = ""; serviceURL = "" })

	co := toConfigOptions()
	if co.ServiceURL != "https://pkg.example.com" || co.APITicket != "pkg-ticket" {
		t.Fatalf("unexpected config options: %+v", co)
	}
}
