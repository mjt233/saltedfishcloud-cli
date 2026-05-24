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
