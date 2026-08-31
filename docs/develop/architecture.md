# sfc-cli 技术栈与系统总体架构设计

> 本文档描述 sfc-cli Go 语言实现的技术栈选型、模块划分、数据流和构建发布策略。

## 1. 技术栈

| 层次 | 技术选型 | 说明 |
| --- | --- | --- |
| 语言 | Go 1.24+ | 天然交叉编译，单二进制分发 |
| CLI 框架 | [cobra](https://github.com/spf13/cobra) | 业界标准 CLI 框架，支持子命令、flag 解析、自动生成帮助 |
| 配置管理 | [viper](https://github.com/spf13/viper) | 与 cobra 配合，统一读取环境变量、命令行参数和 JSON 配置文件 |
| HTTP 客户端 | `net/http`（标准库） | 足够覆盖当前所有接口调用，零外部依赖 |
| 进度条 | [schollz/progressbar](https://github.com/schollz/progressbar) | 上传/下载时的终端进度反馈 |
| 构建 | GoReleaser | 一键交叉编译、打包、发布到 GitHub Releases |

### 外部依赖最小清单

```
github.com/spf13/cobra        # CLI 框架
github.com/spf13/viper         # 配置管理
github.com/schollz/progressbar # 终端进度条
```

仅 3 个外部依赖。`net/http`、`encoding/json`、`os`、`path/filepath` 等均来自标准库。

## 2. 系统总体架构

```
┌──────────────────────────────────────────────────────┐
│                      cmd/                             │
│  root.go  login.go  ls.go  get.go  upload.go         │
│  cp.go  mv.go  rm.go  rename.go  version.go          │
│  remoteVersion.go   (cobra 命令定义层)                 │
├──────────────────────────────────────────────────────┤
│                    internal/                          │
│  ┌─────────┐  ┌─────────┐  ┌─────────┐  ┌─────────┐ │
│  │  config  │  │  client  │  │  oauth  │  │ service │ │
│  │ 配置解析 │  │ HTTP 封装│  │ OIDC设备│  │ 业务编排 │ │
│  │ uid 缓存│  │ Bearer   │  │ 授权流程 │  │         │ │
│  └─────────┘  └─────────┘  └─────────┘  └─────────┘ │
├──────────────────────────────────────────────────────┤
│                    main.go                            │
│                  (入口，调用 cmd.Execute)              │
└──────────────────────────────────────────────────────┘
```

### 分层职责

| 层 | 目录 | 职责 | 依赖方向 |
| --- | --- | --- | --- |
| 入口层 | `main.go` | 初始化 cobra 根命令，调用 `Execute()` | → cmd |
| 命令层 | `cmd/` | 解析参数、校验输入、调用 service 层、格式化输出；参数个数错误时先输出当前命令帮助文本再返回错误 | → internal |
| 服务层 | `internal/service/` | 实现业务编排：路径解析、uid 映射、递归上传/下载、跨资源域复制 | → client, localfs |
| 客户端层 | `internal/client/` | 封装 HTTP 请求，经 TokenSource 统一注入 `Authorization: Bearer`，处理响应体解包；401 时触发令牌刷新重试 | 无外部依赖 |
| 授权层 | `internal/oauth/` | OIDC Discovery 端点发现、设备授权流程（RFC 8628）、刷新令牌、可自动刷新的令牌源 | → config |
| 配置层 | `internal/config/` | 读取并合并标志 / `SFC_*` 环境变量 / `~/.config/sfc-cli/config.json`；缓存 `uid`；持久化 OAuth 登录态 | → client |
| 文件系统层 | `internal/localfs/` | 本地目录遍历、路径规范化 | 无外部依赖 |

## 3. 关键模块设计

### 3.1 配置与认证 (`internal/config`)

```
优先级（高 → 低）：
  命令行参数 --service-url / --client-id
  > 环境变量 SFC_SERVICE_URL / SFC_CLIENT_ID
  > 配置文件 ~/.config/sfc-cli/config.json
```

配置文件示例（OAuth 登录后由 CLI 自动写入）：
```json
{
  "serviceUrl": "https://cloud.example.com",
  "clientId": "3",
  "accessToken": "eyJhbGci...",
  "refreshToken": "eyJhbGci...",
  "expiresAt": "2030-01-01T00:00:00Z"
}
```

认证仅使用 OAuth 登录态（`accessToken`/`refreshToken`）。
当缺少 `service-url` 或尚未登录（无 `accessToken`）时，配置层会一次性指出缺失项。

OAuth 登录态持久化：`SaveOAuthState` 以"读取现有 JSON → 更新登录态键 → 原子写回（临时文件 + 重命名）"的方式合并保存，
保留文件中的其他键；目录权限 0700、文件权限 0600；文件不是合法 JSON 时拒绝覆盖。

uid 缓存策略：首次需要私人网盘 `uid` 时调用 `/api/openApi/user/profile/v1`，结果缓存在内存中，整个命令生命周期内复用。

### 3.2 HTTP 客户端 (`internal/client`)

```go
type APIClient struct {
    baseURL     string
    tokenSource TokenSource   // Bearer 令牌来源抽象
    httpClient  *http.Client
}
```

职责：
- 所有请求经 `TokenSource` 获取令牌并自动注入 `Authorization: Bearer {token}`（当前后端为 OIDC access token 鉴权）
- 收到 HTTP 401 且令牌源支持刷新（`RefreshableTokenSource`）时，强制刷新令牌并重试一次；
  流式上传等无法重建请求体的场景不重试，直接返回错误
- 统一解析 JSON 响应，抽取 `data` 字段（除 `/api/hello/feature`）
- 统一错误处理：解析 `businessCode` + `msg`，转换为 Go error
- 下载接口返回原始 `*http.Response`，由上层写入文件

### 3.3 OIDC 设备授权 (`internal/oauth`)

实现 OIDC 设备授权流程（RFC 8628），全部基于标准库：

| 文件 | 职责 |
| --- | --- |
| `discovery.go` | 请求 `{serviceUrl}/.well-known/openid-configuration` 自动发现设备授权与令牌端点，不硬编码路径 |
| `device.go` | 申请设备码（`POST device_authorization_endpoint`，附带 PKCE S256）、按 `interval` 轮询换票（携带 `code_verifier`，处理 `authorization_pending`/`slow_down`/终止性错误与设备码过期）、`refresh_token` 换新 |
| `token_source.go` | 实现 client 层的 `TokenSource`/`RefreshableTokenSource`：令牌临近过期自动刷新并回写配置；刷新令牌轮换时采用新值 |

`login` 命令流程：端点发现 → 生成 PKCE → 申请设备码 → 输出验证地址与用户码到控制台 → 轮询换票（附 `code_verifier`）→ 持久化 → 调用 profile 接口确认身份。

### 3.4 路径解析 (`internal/service/path.go`)

将 `[resourceArea:]<path>` 解析为：

```go
type ResolvedPath struct {
    Area string // "local" | "private" | "public"
    UID  int64  // public=0, private=缓存的用户ID
    Path string // 纯路径，如 "/my-files/docs"
}
```

所有命令统一使用此结构做参数预处理。

### 3.5 递归操作编排

| 操作 | 策略 |
| --- | --- |
| 目录上传 | 本地 `filepath.WalkDir` 遍历（符号链接/空文件跳过并警告）→ 逐层 `mkdir` → 逐文件流式 `upload`（单条目失败收集汇总，不中止整体；请求使用无全局超时客户端，超时由 `ctx` 控制） |
| 目录下载 | 远程 `fileList` 递归 → 逐文件 `download` + 本地 `os.MkdirAll` |
| 跨资源域 `cp`/`mv` | 直接调用 `copy`/`move` 接口的 `sourceUid` / `targetUid` |
| `local → remote` 的 `cp`/`mv` | 退化为上传；`mv` 在上传成功后删除本地源文件 |

### 3.6 命令层输出格式

| 命令 | 默认输出 |
| --- | --- |
| `login` | 验证页面地址 + 用户码 + 等待提示 + 登录成功确认（用户名与 uid） |
| `ls [path]` | 表格式：`type  name  size  mtime`；未传 `path` 时默认列出 `/` |
| `get` / `upload` | 进度条 + 完成提示 |
| `rm` / `rename` / `cp` / `mv` | 简单的成功/失败文本 |
| `version` | 版本号（构建时注入） |
| `remote-version` | 远程版本号 |

## 4. 项目目录结构

```
sfc-cli/
├── main.go                          # 入口
├── go.mod
├── go.sum
├── cmd/                             # cobra 命令定义
│   ├── root.go                      # 根命令 + 全局 flag + 客户端工厂（认证来源选择）
│   ├── login.go                     # OIDC 设备授权登录
│   ├── ls.go
│   ├── get.go
│   ├── upload.go
│   ├── cp.go
│   ├── mv.go
│   ├── rm.go
│   ├── rename.go
│   ├── version.go
│   └── remoteVersion.go
├── internal/
│   ├── config/
│   │   └── config.go                # 配置加载、合并、uid 缓存、OAuth 登录态持久化
│   ├── oauth/
│   │   ├── discovery.go             # OIDC Discovery 端点发现
│   │   ├── device.go                # 设备授权申请、轮询换票、刷新令牌
│   │   └── token_source.go          # 可自动刷新的 Bearer 令牌源
│   ├── client/
│   │   └── api.go                   # HTTP 客户端封装
│   ├── service/
│   │   ├── path.go                  # 资源路径解析
│   │   ├── diskfile.go              # 文件操作服务（ls/get/upload/rm/rename）
│   │   ├── copier.go                # cp/mv 编排
│   │   └── user.go                  # profile → uid
│   └── localfs/
│       └── walker.go                # 本地目录遍历
├── docs/
│   ├── api.md
│   ├── require-api.md
│   └── develop/
│       └── architecture.md          # 本文档
└── .goreleaser.yml                  # 交叉编译与发布配置
```

## 5. 交叉编译与发布

GoReleaser 配置要点：

```yaml
builds:
  - env:
      - CGO_ENABLED=0          # 纯 Go，无 CGO，确保交叉编译无阻碍
    goos:
      - windows
      - linux
      - darwin
    goarch:
      - amd64
      - arm64
    ldflags:
      - -s -w -X main.version={{.Version}}
```

产物矩阵（6 个目标平台）：

| OS | Arch | 产物名 |
| --- | --- | --- |
| Windows | amd64 | `sfc-cli_windows_amd64.exe` |
| Windows | arm64 | `sfc-cli_windows_arm64.exe` |
| Linux | amd64 | `sfc-cli_linux_amd64` |
| Linux | arm64 | `sfc-cli_linux_arm64` |
| macOS | amd64 | `sfc-cli_darwin_amd64` |
| macOS | arm64 | `sfc-cli_darwin_arm64` |

关键约束：`CGO_ENABLED=0` 确保纯 Go 编译，不依赖任何目标平台的 C 工具链。

## 6. 首版实现范围

按 README 命令和接口映射，首版必须实现：

| 命令 | 优先级 | 说明 |
| --- | --- | --- |
| `ls [path]` | P0 | 基础能力，验证整个调用链路；未传 `path` 时默认列出 `/` |
| `get`（单文件 + 目录） | P0 | CLI 核心价值 |
| `upload`（单文件 + 目录） | P0 | CLI 核心价值 |
| `rm` | P0 | 基础文件操作 |
| `rename` | P0 | 基础文件操作 |
| `login` | P1 | OIDC 设备授权登录引导，令牌持久化与自动刷新 |
| `cp` | P1 | 跨资源域是差异化能力 |
| `mv` | P1 | 跨资源域是差异化能力 |
| `version` | P1 | 简单，构建时注入 |
| `remote-version` | P2 | 匿名接口，验证连通性 |

## 7. 错误处理策略

| 错误类型 | 处理方式 |
| --- | --- |
| 配置缺失（无 `service-url` 或凭据） | 启动时一次性检查，缺啥报啥，提示 flags / 环境变量 / 配置文件路径与键名 / `sfc-cli login`，随后立即退出 |
| OIDC Discovery / 设备授权失败 | 返回包含端点与错误码的可读错误；`access_denied`、`expired_token` 等终止性错误立即结束，`authorization_pending` 继续等待，`slow_down` 自动放慢轮询 |
| 访问令牌过期 | 令牌临近过期主动刷新；HTTP 401 时强制刷新并重试一次；刷新失败提示重新执行 `sfc-cli login` |
| 网络超时 / 连接拒绝 | 透传 Go 原生错误，附带目标 URL 提示 |
| 业务错误（businessCode != 0） | 解析 `msg` 字段，格式化为用户可读的错误消息 |
| HTTP 状态码 >= 400 | 直接返回 HTTP 错误，不尝试解析 JSON 信封 |
| 服务端错误（code != 200，无 businessCode） | 解析 `msg` 字段，格式化为用户可读的错误消息 |
| 路径格式错误 | 在路径解析阶段即报错，附带正确的格式示例 |
| 部分文件失败（递归操作） | 收集失败列表，命令结束时汇总报告，不因单文件失败中断整个操作 |
