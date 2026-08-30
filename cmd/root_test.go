package cmd

import "testing"

// TestNewRootCommand_RegistersGlobalFlags 验证根命令注册了必要的全局持久标志。
func TestNewRootCommand_RegistersGlobalFlags(t *testing.T) {
	root := NewRootCommand()
	if root.PersistentFlags().Lookup("service-url") == nil {
		t.Fatal("缺少持久标志 service-url")
	}
	if root.PersistentFlags().Lookup("api-ticket") != nil {
		t.Fatal("已弃用的 api-ticket 标志不应再注册")
	}
}

// TestNewRootCommand_FlagsBoundToPackageVars 验证标志值在解析后正确写入包级变量。
func TestNewRootCommand_FlagsBoundToPackageVars(t *testing.T) {
	// 重置包级变量，防止测试间相互污染
	serviceURL = ""

	root := NewRootCommand()
	root.SetArgs([]string{"--service-url", "http://sfc.example.com"})
	// 执行根命令以触发标志解析
	_ = root.Execute()

	if serviceURL != "http://sfc.example.com" {
		t.Errorf("serviceURL = %q, want %q", serviceURL, "http://sfc.example.com")
	}
}

// TestRootOptions_ToConfigOptions 验证 rootOptions 的 toConfigOptions 方法
// 能正确将自身字段转换为 config.Options。
func TestRootOptions_ToConfigOptions(t *testing.T) {
	ro := rootOptions{ServiceURL: "https://example.com"}
	co := ro.toConfigOptions()
	if co.ServiceURL != "https://example.com" {
		t.Errorf("ServiceURL = %q, want %q", co.ServiceURL, "https://example.com")
	}
}

// TestToConfigOptions_UsesRootOptions 验证包级 toConfigOptions 函数使用 rootOptions 的方法进行转换。
func TestToConfigOptions_UsesRootOptions(t *testing.T) {
	// 重置包级变量，再写入已知值
	serviceURL = "https://pkg.example.com"
	t.Cleanup(func() { serviceURL = "" })

	co := toConfigOptions()
	if co.ServiceURL != "https://pkg.example.com" {
		t.Fatalf("unexpected config options: %+v", co)
	}
}
