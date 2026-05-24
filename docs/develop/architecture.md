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
│  root.go  ls.go  get.go  upload.go  cp.go  mv.go    │
│  rm.go  rename.go  version.go  remoteVersion.go      │
│               (cobra 命令定义层)                       │
├──────────────────────────────────────────────────────┤
│                    internal/                          │
│  ┌─────────┐  ┌─────────┐  ┌────────────────────┐   │
│  │  config  │  │  client  │  │     service        │   │
│  │         │  │         │  │  diskfile.go         │   │
│  │ 配置解析 │  │ HTTP 封装│  │  user.go            │   │
│  │ uid 缓存│  │ ApiTicket│  │  path.go            │   │
│  └─────────┘  └─────────┘  └────────────────────┘   │
│                                  │                    │
│                    ┌─────────────┼─────────┐         │
│                    ▼             ▼          ▼         │
│              localfs.go    remotefs.go   copier.go    │
│             (本地遍历)     (远端调用)   (编排复制/移动) │
├──────────────────────────────────────────────────────┤
│                    main.go                            │
│                  (入口，调用 cmd.Execute)              │
└──────────────────────────────────────────────────────┘
```

### 分层职责

| 层 | 目录 | 职责 | 依赖方向 |
| --- | --- | --- | --- |
| 入口层 | `main.go` | 初始化 cobra 根命令，调用 `Execute()` | → cmd |
| 命令层 | `cmd/` | 解析参数、校验输入、调用 service 层、格式化输出 | → internal |
| 服务层 | `internal/service/` | 实现业务编排：路径解析、uid 映射、递归上传/下载、跨资源域复制 | → client, localfs |
| 客户端层 | `internal/client/` | 封装 HTTP 请求，统一注入 `Authorization: ApiTicket`，处理响应体解包 | 无外部依赖 |
| 配置层 | `internal/config/` | 读取并合并 `--api-ticket` / `SFC_API_TICKET` / `~/.config/sfc-cli/config.json`；缓存 `uid` | → client |
| 文件系统层 | `internal/localfs/` | 本地目录遍历、路径规范化 | 无外部依赖 |

## 3. 关键模块设计

### 3.1 配置与认证 (`internal/config`)

```
优先级（高 → 低）：
  命令行参数 --api-ticket / --service-url
  > 环境变量 SFC_API_TICKET / SFC_SERVICE_URL
  > 配置文件 ~/.config/sfc-cli/config.json
```

配置文件示例：
```json
{
  "serviceUrl": "https://cloud.example.com",
  "apiTicket": "eyJhbGci..."
}
```

uid 缓存策略：首次需要私人网盘 `uid` 时调用 `/api/openApi/user/profile/v1`，结果缓存在内存中，整个命令生命周期内复用。

### 3.2 HTTP 客户端 (`internal/client`)

```go
type APIClient struct {
    baseURL    string
    apiTicket  string
    httpClient *http.Client
}
```

职责：
- 所有请求自动注入 `Authorization: ApiTicket {ticket}`
- 统一解析 JSON 响应，抽取 `data` 字段（除 `/api/hello/feature`）
- 统一错误处理：解析 `businessCode` + `msg`，转换为 Go error
- 下载接口返回原始 `*http.Response`，由上层写入文件

### 3.3 路径解析 (`internal/service/path.go`)

将 `[resourceArea:]<path>` 解析为：

```go
type ResolvedPath struct {
    Area string // "local" | "private" | "public"
    UID  int64  // public=0, private=缓存的用户ID
    Path string // 纯路径，如 "/my-files/docs"
}
```

所有命令统一使用此结构做参数预处理。

### 3.4 递归操作编排

| 操作 | 策略 |
| --- | --- |
| 目录上传 | 本地 `filepath.WalkDir` 遍历 → 逐层 `mkdir` → 逐文件 `upload` |
| 目录下载 | 远程 `fileList` 递归 → 逐文件 `download` + 本地 `os.MkdirAll` |
| 跨资源域 `cp`/`mv` | 直接调用 `copy`/`move` 接口的 `sourceUid` / `targetUid` |
| `local → remote` 的 `cp`/`mv` | 退化为上传；`mv` 在上传成功后删除本地源文件 |

### 3.5 命令层输出格式

| 命令 | 默认输出 |
| --- | --- |
| `ls` | 表格式：`type  name  size  mtime` |
| `get` / `upload` | 进度条 + 完成提示 |
| `rm` / `rename` / `cp` / `mv` | 简单的成功/失败文本 |
| `version` | 版本号（构建时注入） |
| `remoteVersion` | 远程版本号 |

## 4. 项目目录结构

```
sfc-cli/
├── main.go                          # 入口
├── go.mod
├── go.sum
├── cmd/                             # cobra 命令定义
│   ├── root.go                      # 根命令 + 全局 flag
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
│   │   └── config.go                # 配置加载、合并、uid 缓存
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
| `ls` | P0 | 基础能力，验证整个调用链路 |
| `get`（单文件 + 目录） | P0 | CLI 核心价值 |
| `upload`（单文件 + 目录） | P0 | CLI 核心价值 |
| `rm` | P0 | 基础文件操作 |
| `rename` | P0 | 基础文件操作 |
| `cp` | P1 | 跨资源域是差异化能力 |
| `mv` | P1 | 跨资源域是差异化能力 |
| `version` | P1 | 简单，构建时注入 |
| `remoteVersion` | P2 | 匿名接口，验证连通性 |

## 7. 错误处理策略

| 错误类型 | 处理方式 |
| --- | --- |
| 配置缺失（无 serviceUrl / apiTicket） | 启动时一次性检查，缺啥报啥，立即退出 |
| 网络超时 / 连接拒绝 | 透传 Go 原生错误，附带目标 URL 提示 |
| 业务错误（businessCode != 0） | 解析 `msg` 字段，格式化为用户可读的错误消息 |
| HTTP 状态码 >= 400 | 直接返回 HTTP 错误，不尝试解析 JSON 信封 |
| 服务端错误（code != 200，无 businessCode） | 解析 `msg` 字段，格式化为用户可读的错误消息 |
| 路径格式错误 | 在路径解析阶段即报错，附带正确的格式示例 |
| 部分文件失败（递归操作） | 收集失败列表，命令结束时汇总报告，不因单文件失败中断整个操作 |
