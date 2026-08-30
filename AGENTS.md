# sfc-cli Agent 指南

本文件为仓库级 AI Agent 工作约束，适用于本项目内的实现、文档和重构任务。

## 项目定位

- 本项目是一个 Go 实现的咸鱼云网盘 CLI 客户端。
- 目标能力以 [README.md](./README.md) 为准，系统分层与目录规划以 [docs/develop/architecture.md](./docs/develop/architecture.md) 为准。
- 如文档之间存在冲突，优先级为：用户明确要求 > `README.md` > `docs/develop/architecture.md` > 本文件。

## 工作原则

- 优先做最小可交付改动，不扩展未声明需求。
- 优先修复根因，不做只覆盖表象的补丁。
- 非必要不要引入新依赖；当前技术栈目标是 `cobra`、`viper`、`schollz/progressbar` 加 Go 标准库。
- 不擅自修改 CLI 对外语义，包括命令名、资源路径格式、配置优先级和输出约定。
- 涉及接口、命令、架构调整时，同步更新相关文档，避免实现与文档脱节。
- 函数和类型声明需要有完整的中文文档注释。函数执行流程内的关键阶段需要有中文行内注释。

## 实现边界

- 认证仅支持 OIDC 设备授权流程（`sfc-cli login`，public client，基于端点自动发现）。不实现手动 `apiTicket`、授权码流程、`logout`/`revoke` 命令。
- `login` 负责设备授权引导、令牌持久化与 `refresh_token` 自动刷新；不实现授权码流程、`logout`/`revoke` 命令。
- 资源路径格式固定为 `[resourceArea:]<path>`，支持 `local`、`private`、`public` 三种资源域。
- 未显式声明资源域时，按 `private` 处理。
- `private` 资源域的 `uid` 需要通过用户资料接口获取；`public` 固定为 `0`；`local` 不走远端接口。
- 目录上传、目录下载、`local <-> remote` 的 `cp`/`mv` 都属于 CLI 编排能力，不应假设后端存在单一专用接口。

## 架构约束

- 入口放在 `main.go`，只负责启动命令执行。
- `cmd/` 只做参数解析、输入校验、调用 service、格式化输出，不直接承载复杂业务逻辑。
- `internal/service/` 负责路径解析、远程与本地操作编排、递归处理和跨资源域复制移动。
- `internal/client/` 负责 HTTP 请求封装、认证头注入、响应解包和统一错误处理。
- `internal/oauth/` 负责 OIDC 端点发现、设备授权流程、刷新令牌和可自动刷新的令牌源。
- `internal/config/` 负责命令行参数、环境变量、配置文件合并，以及运行期 `uid` 缓存与 OAuth 登录态持久化。
- `internal/localfs/` 负责本地文件系统遍历与路径规范化。
- 新增代码优先落在上述分层中，不要把业务逻辑散落到命令层。

## 接口与数据约定

- 远端业务接口统一使用 `Authorization: Bearer {token}`（OIDC access token 鉴权）。
- 除 `/api/hello/feature` 外，JSON 接口默认从响应体的 `data` 字段读取业务数据。
- `/api/openApi/diskFile/download/v1` 返回二进制流，不按 JSON 解析。
- 业务错误需要尽量保留后端返回的 `businessCode` 与 `msg` 语义，输出对用户可读的错误信息。
- 路径解析、目录与文件名拆分、`uid` 映射应集中复用，避免各命令各自实现一套规则。

## 命令优先级

- P0：`ls`、`get`、`upload`、`rm`、`rename`
- P1：`cp`、`mv`、`version`、`login`
- P2：`remote-version`

如用户要求从零开始搭建或补全实现，默认优先保证 P0 链路完整，再处理 P1/P2。

## 输出与交互约定

- `ls` 默认输出表格信息：`type name size mtime`。
- `get`、`upload` 应提供进度反馈和最终结果提示。
- `rm`、`rename`、`cp`、`mv` 保持简洁、明确的成功或失败输出。
- 错误信息优先说明缺失配置、路径格式错误、网络错误或接口业务错误的具体原因。

## 配置约定

- 配置优先级固定为：命令行参数 > 环境变量 > `~/.config/sfc-cli/config.json`。
- 关键配置项为 `serviceUrl` 与 `clientId`/`accessToken`/`refreshToken`/`expiresAt`（OAuth 登录态）。
- 业务命令要求已通过 `sfc-cli login` 获得 `accessToken`。
- 缺失必要配置时，应尽早失败，并一次性指出缺少的项。

## 文档与变更同步

- 修改命令行为、参数语义、资源路径规则时，更新 [README.md](./README.md)。
- 修改模块划分、目录结构、职责边界时，更新 [docs/develop/architecture.md](./docs/develop/architecture.md)。
- 修改接口映射、认证方式或返回体解析规则时，检查 [docs/api.md](./docs/api.md) 和 [docs/require-api.md](./docs/require-api.md) 是否需要同步。

## 验证要求

- 涉及 Go 代码时，优先运行与改动范围匹配的验证命令。
- 常规最小验证为：`gofmt`、`go test ./...`。
- 仅修改文档时，至少自检链接、术语和与现有文档的一致性。
- 不要声称“已完成”或“可用”，除非已经完成与改动相称的验证。

## 提交规范

- Git 提交信息使用中文，遵循仓库约定格式。
- 详细规范见 [docs/develop/git-commit-convention.md](./docs/develop/git-commit-convention.md)。
