# CLI 命令命名与交互反馈调整 Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 统一 CLI 的 kebab-case 命名、缺失配置指引、参数错误帮助输出，并让 `ls` 在无参数时默认列出 `/`。

**Architecture:** 继续沿用现有分层：`cmd/` 负责命令名、参数校验和帮助输出；`internal/config/` 负责配置加载与缺失配置报错；文档只同步对外语义，不调整 service/client 行为。实现上先补测试锁定 UX，再做最小代码修改，最后同步 README 与架构文档并跑全量测试。

**Tech Stack:** Go 1.24、Cobra、Viper、Go test、gofmt

---

## File Structure

- Modify: `internal/config/config.go`
  - 将缺失配置报错从内部键名改成用户可见的 kebab-case 文案，并附上 flag / 环境变量 / 配置文件指导。
- Modify: `internal/config/config_test.go`
  - 锁定新的缺失配置错误文本，覆盖“全部缺失”和“单项缺失”两类场景。
- Create: `cmd/args_help.go`
  - 提供统一的参数校验包装，在参数数量不满足时输出当前命令帮助后返回原始错误。
- Modify: `cmd/root.go`
  - 将 `remoteVersion` 注册名改为 `remote-version`。
- Modify: `cmd/ls.go`
  - 将 `ls` 形参改为可选；无参数时归一为 `/`；帮助文案改为 `ls [path]`。
- Modify: `cmd/get.go`
- Modify: `cmd/upload.go`
- Modify: `cmd/rm.go`
- Modify: `cmd/rename.go`
- Modify: `cmd/cp.go`
- Modify: `cmd/mv.go`
- Modify: `cmd/version.go`
- Modify: `cmd/remoteVersion.go`
  - 上述命令统一接入 `cmd/args_help.go` 的参数帮助包装；`remoteVersion` 的帮助文案和注释改成 `remote-version`。
- Modify: `cmd/ls_test.go`
  - 覆盖 `ls` 默认 `/` 与 `ls` 新帮助文案。
- Modify: `cmd/version_test.go`
  - 覆盖 `remote-version` 新命令名、额外参数时帮助输出。
- Modify: `README.md`
  - 对外命令名、`ls [path]` 默认值、配置指导文本同步更新。
- Modify: `docs/develop/architecture.md`
  - 命令名和命令层交互行为同步更新。

### Task 1: 锁定缺失配置提示的目标行为

**Files:**
- Modify: `internal/config/config_test.go`
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

- [ ] **Step 1: 先把缺失配置的期望写成失败测试**

```go
func TestLoad_ReportsAllMissingFields(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	unsetenv(t, "SFC_SERVICE_URL")
	unsetenv(t, "SFC_API_TICKET")

	_, err := Load(Options{})
	if err == nil {
		t.Fatal("expected missing config error, got nil")
	}

	msg := err.Error()
	for _, want := range []string{
		"missing required config: service-url, api-ticket",
		"--service-url",
		"--api-ticket",
		"SFC_SERVICE_URL",
		"SFC_API_TICKET",
		"~/.config/sfc-cli/config.json",
		"serviceUrl",
		"apiTicket",
	} {
		if !strings.Contains(msg, want) {
			t.Fatalf("error %q missing %q", msg, want)
		}
	}
}

func TestLoad_ReportsSingleMissingField(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("SFC_SERVICE_URL", "https://env")
	unsetenv(t, "SFC_API_TICKET")

	_, err := Load(Options{})
	if err == nil {
		t.Fatal("expected missing config error, got nil")
	}
	if !strings.Contains(err.Error(), "missing required config: api-ticket") {
		t.Fatalf("unexpected error: %v", err)
	}
}
```

- [ ] **Step 2: 运行定向测试，确认它们先失败**

Run: `go test ./internal/config -run "TestLoad_Reports(AllMissingFields|SingleMissingField)" -count=1`

Expected: FAIL，当前错误里仍是 `serviceUrl` / `apiTicket`，且没有配置指导文本。

- [ ] **Step 3: 在配置层实现新的缺失配置错误**

```go
func Load(opts Options) (Config, error) {
	// ... 保持现有 .env / 文件 / 环境变量 / flag 合并逻辑不变

	var missing []string
	if cfg.ServiceURL == "" {
		missing = append(missing, "service-url")
	}
	if cfg.APITicket == "" {
		missing = append(missing, "api-ticket")
	}
	if len(missing) > 0 {
		return Config{}, fmt.Errorf(
			"missing required config: %s\n"+
				"configure with flags: --service-url, --api-ticket\n"+
				"or environment: SFC_SERVICE_URL, SFC_API_TICKET\n"+
				"or config file: ~/.config/sfc-cli/config.json (keys: serviceUrl, apiTicket)",
			strings.Join(missing, ", "),
		)
	}

	return cfg, nil
}
```

- [ ] **Step 4: 再次运行定向测试，确认配置错误文案通过**

Run: `go test ./internal/config -run "TestLoad_Reports(AllMissingFields|SingleMissingField)" -count=1`

Expected: PASS

- [ ] **Step 5: 提交配置提示变更**

```bash
git add internal/config/config.go internal/config/config_test.go
git commit -m "fix: 完善缺失配置提示" -m "统一使用 service-url 与 api-ticket 的 CLI 文案，并补充 flags、环境变量和配置文件指引。" -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

### Task 2: 锁定命令命名、`ls` 默认值与参数帮助输出

**Files:**
- Create: `cmd/args_help.go`
- Modify: `cmd/root.go`
- Modify: `cmd/ls.go`
- Modify: `cmd/get.go`
- Modify: `cmd/upload.go`
- Modify: `cmd/rm.go`
- Modify: `cmd/rename.go`
- Modify: `cmd/cp.go`
- Modify: `cmd/mv.go`
- Modify: `cmd/version.go`
- Modify: `cmd/remoteVersion.go`
- Modify: `cmd/ls_test.go`
- Modify: `cmd/version_test.go`
- Test: `cmd/ls_test.go`
- Test: `cmd/version_test.go`

- [ ] **Step 1: 先补会失败的命令层测试**

```go
func TestLSCommand_DefaultsToRootPath(t *testing.T) {
	apiTicket = ""
	serviceURL = ""
	t.Cleanup(func() { apiTicket = ""; serviceURL = "" })

	requestedPath := ""
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestedPath = r.URL.Query().Get("path")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"code": 200,
			"data": []map[string]any{},
			"msg":  "OK",
		})
	}))
	defer srv.Close()

	root := NewRootCommand()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"--service-url", srv.URL, "--api-ticket", "test-ticket", "ls"})

	if err := root.ExecuteContext(context.Background()); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if requestedPath != "/" {
		t.Fatalf("requested path = %q, want /", requestedPath)
	}
}

func TestRemoteVersionCommand_UsesKebabCaseName(t *testing.T) {
	root := NewRootCommand()
	var buf bytes.Buffer
	root.SetOut(&buf)
	root.SetErr(&buf)
	root.SetArgs([]string{"remote-version", "extra-arg"})

	err := root.Execute()
	if err == nil {
		t.Fatal("expected error for extra arguments, got nil")
	}
	if !strings.Contains(buf.String(), "Usage:\n  sfc-cli remote-version") {
		t.Fatalf("expected remote-version help text, got:\n%s", buf.String())
	}
}

func TestVersionCommand_NoArgsPrintsHelpOnArgError(t *testing.T) {
	cmd := newVersionCommand()
	buf := new(bytes.Buffer)
	cmd.SetOut(buf)
	cmd.SetErr(buf)
	cmd.SetArgs([]string{"extra-arg"})

	err := cmd.Execute()
	if err == nil {
		t.Fatal("expected error for extra arguments, got nil")
	}
	if !strings.Contains(buf.String(), "Usage:") {
		t.Fatalf("expected help output, got:\n%s", buf.String())
	}
}
```

- [ ] **Step 2: 运行命令层定向测试，确认当前实现失败**

Run: `go test ./cmd -run "Test(LSCommand_DefaultsToRootPath|RemoteVersionCommand_UsesKebabCaseName|VersionCommand_NoArgsPrintsHelpOnArgError)" -count=1`

Expected: FAIL，原因分别是 `ls` 仍要求必填路径、根命令还只注册 `remoteVersion`、参数错误时还未输出帮助文本。

- [ ] **Step 3: 用最小实现收敛命令层交互**

```go
// cmd/args_help.go
package cmd

import "github.com/spf13/cobra"

func withArgsHelp(validate cobra.PositionalArgs) cobra.PositionalArgs {
	return func(cmd *cobra.Command, args []string) error {
		if err := validate(cmd, args); err != nil {
			_ = cmd.Help()
			return err
		}
		return nil
	}
}
```

```go
// cmd/ls.go
func newLSCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "ls [path]",
		Short: "List remote directory contents",
		Long:  "List files and directories under the specified remote path, supporting private and public resource areas. Defaults to / when path is omitted.",
		Args:  withArgsHelp(cobra.MaximumNArgs(1)),
		RunE: func(cmd *cobra.Command, args []string) error {
			targetPath := "/"
			if len(args) == 1 {
				targetPath = args[0]
			}

			cfg, err := config.Load(toConfigOptions())
			if err != nil {
				return err
			}

			cli := client.NewAPIClient(cfg.ServiceURL, cfg.APITicket)
			userSvc := service.NewUserService(cli)
			paths := service.NewPathService(userSvc.PrivateUID)
			diskSvc := service.NewDiskFileService(cli, paths)

			entries, err := diskSvc.List(cmd.Context(), targetPath)
			if err != nil {
				return err
			}

			fmt.Fprintln(cmd.OutOrStdout(), "type\tname\tsize\tmtime")
			for _, e := range entries {
				fmt.Fprintf(cmd.OutOrStdout(), "%s\t%s\t%d\t%s\n", e.Type, e.Name, e.Size, e.Mtime)
			}
			return nil
		},
	}
}
```

```go
// cmd/root.go
root.AddCommand(rootAddClientCommand("remote-version", newRemoteVersionCommand))
```

```go
// 其余命令把 Args 换成带帮助包装的形式
Args: withArgsHelp(cobra.RangeArgs(1, 2))
Args: withArgsHelp(cobra.ExactArgs(2))
Args: withArgsHelp(cobra.NoArgs)
```

```go
// cmd/remoteVersion.go
func newRemoteVersionCommand(newClient func(cmd *cobra.Command) (*client.APIClient, error)) *cobra.Command {
	return &cobra.Command{
		Use:   "remote-version",
		Short: "Query server version",
		Args:  withArgsHelp(cobra.NoArgs),
		RunE:  // 保持原有调用逻辑
	}
}
```

- [ ] **Step 4: 运行命令层测试，确认交互行为通过**

Run: `go test ./cmd -run "Test(LSCommand_DefaultsToRootPath|RemoteVersionCommand_UsesKebabCaseName|VersionCommand_NoArgsPrintsHelpOnArgError|LSCommand_OutputsTableHeaderAndRow|LSCommand_RespectsContextCancellation|RemoteVersionCommand_PrintsServerVersion)" -count=1`

Expected: PASS

- [ ] **Step 5: 提交命令层 UX 变更**

```bash
git add cmd/args_help.go cmd/root.go cmd/ls.go cmd/get.go cmd/upload.go cmd/rm.go cmd/rename.go cmd/cp.go cmd/mv.go cmd/version.go cmd/remoteVersion.go cmd/ls_test.go cmd/version_test.go
git commit -m "fix: 统一命令交互反馈" -m "将 remote-version 设为正式命令名，为参数数量错误输出命令帮助，并让 ls 默认列出根目录。" -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

### Task 3: 同步 README / 架构文档并完成全量验证

**Files:**
- Modify: `README.md`
- Modify: `docs/develop/architecture.md`
- Modify: `cmd/remoteVersion.go`
- Modify: `cmd/ls.go`
- Modify: `internal/config/config.go`
- Test: `./...`

- [ ] **Step 1: 更新 README 与架构文档中的对外语义**

```md
- `ls [path]` - 列出指定目录下的文件列表；未传 path 时默认使用 `/`
- `remote-version` - 查询远端服务端版本号

缺失 `service-url` / `api-ticket` 时，可通过以下方式配置：
- 命令行参数：`--service-url`、`--api-ticket`
- 环境变量：`SFC_SERVICE_URL`、`SFC_API_TICKET`
- 配置文件：`~/.config/sfc-cli/config.json`（键名仍为 `serviceUrl`、`apiTicket`）
```

- [ ] **Step 2: 格式化本次改动涉及的 Go 文件**

Run: `gofmt -w cmd/args_help.go cmd/root.go cmd/ls.go cmd/get.go cmd/upload.go cmd/rm.go cmd/rename.go cmd/cp.go cmd/mv.go cmd/version.go cmd/remoteVersion.go cmd/ls_test.go cmd/version_test.go internal/config/config.go internal/config/config_test.go`

Expected: 命令成功返回，无输出或仅覆盖写回文件。

- [ ] **Step 3: 跑全量测试确认没有回归**

Run: `go test ./...`

Expected: PASS，`cmd`、`internal/config` 以及其余包测试全部通过。

- [ ] **Step 4: 检查工作区只包含预期变更**

Run: `git --no-pager status --short`

Expected: 仅看到本任务涉及的代码与文档文件变更，没有额外临时文件。

- [ ] **Step 5: 提交文档与收尾修改**

```bash
git add README.md docs/develop/architecture.md cmd/remoteVersion.go cmd/ls.go internal/config/config.go
git commit -m "docs: 同步 CLI 命令与配置说明" -m "更新 remote-version、ls 默认路径以及缺失配置指引，确保 README 与架构文档和实现一致。" -m "Co-authored-by: Copilot <223556219+Copilot@users.noreply.github.com>"
```

## Self-Review

- **Spec coverage:** 规格里的四项要求都映射到了任务：Task 1 覆盖缺失配置指引；Task 2 覆盖 `ls` 默认 `/`、参数错误帮助、`remote-version` 命名；Task 3 覆盖 README 与架构文档同步和全量验证。
- **Placeholder scan:** 计划中没有使用 TBD / TODO / “适当处理”等占位词，每个代码步骤都给了明确文件、代码片段或命令。
- **Type consistency:** 计划中统一使用 `withArgsHelp` 作为 Cobra 参数包装名称，`remote-version` 作为唯一新命令名，配置文件键名始终保持 `serviceUrl` / `apiTicket`。
