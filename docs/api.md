# sfc-cli 开发所需接口整理

本文基于以下文档整理：

- `https://github.com/mjt233/saltedfishcloud-backend/tree/develop/docs/oauth/index.md`
- `https://github.com/mjt233/saltedfishcloud-backend/tree/develop/docs/oauth/api/**/*.md`

目标是从 README 中的 CLI 能力反推实现时需要接入的开放接口，并标明各命令依赖关系、权限范围和可落地方式。

当前实现前提已经简化：CLI 暂不实现 OAuth 授权码流程，而是由用户手动提供永久有效的 ApiTicket。

## 1. 认证与调用约定

CLI 暂不负责获取 ApiTicket，启动前由用户手动提供永久有效的 ApiTicket。业务接口统一要求：

```http
Authorization: ApiTicket {api_ticket}
Content-Type: application/json
```

返回体约定：

- 除 `/api/hello/feature` 外，所有 JSON 接口返回值都会被后端统一封装到 `data` 字段中。
- `/api/hello/feature` 为匿名接口，返回值不走 `data` 包装。
- `/api/openApi/diskFile/download/v1` 直接返回二进制文件流，不适用 JSON 包装规则。

当前 CLI 的最小认证链路如下：

1. 用户在 CLI 外部自行申请并保存永久有效的 ApiTicket
2. CLI 从命令行参数、环境变量或配置文件读取 `apiTicket`
3. 如需访问私人网盘，调用 `GET /api/openApi/user/profile/v1` 获取授权用户 ID
4. 携带 ApiTicket 调用开放接口

## 2. CLI 开发需要的接口清单

### 2.1 认证相关接口

| 用途 | 接口 | 方法 | 关键参数 | 备注 |
| --- | --- | --- | --- | --- |
| 获取授权用户基本信息 | `/api/openApi/user/profile/v1` | `GET` | 无 | 需要 `profile` 权限；CLI 可用返回的 `id` 作为私人网盘 `uid` |

### 2.2 非授权公共接口

| 用途 | 接口 | 方法 | 是否需要授权 | 返回示例 | 备注 |
| --- | --- | --- | --- | --- | --- |
| 获取远程服务版本 | `/api/hello/feature` | `GET` | 否 | `{ "version": "3.1.2.0-RELEASE" }` | 直接读取顶层 `version` 字段，不包在 `data` 中 |

### 2.3 存储相关接口

| 用途 | 接口 | 方法 | 权限 | 关键参数 |
| --- | --- | --- | --- | --- |
| 列出目录文件 | `/api/openApi/diskFile/fileList/v1` | `GET` | `storage_read` | `uid`、`path` |
| 下载单个文件 | `/api/openApi/diskFile/download/v1` | `GET` | `storage_read` | `uid`、`path` |
| 获取临时下载链接 | `/api/openApi/diskFile/downloadLink/v1` | `GET` | `storage_read` | `uid`、`path` |
| 上传单个文件 | `/api/openApi/diskFile/upload/v1` | `POST` | `storage_write` | `uid`、`path`、`file` |
| 创建目录 | `/api/openApi/diskFile/mkdir/v1` | `POST` | `storage_write` | `uid`、`path`、`name` |
| 复制文件或目录 | `/api/openApi/diskFile/copy/v1` | `POST` | `storage_write` | Body: `sourceUid`、`sourcePath`、`targetUid`、`files`、`targetPath`、`isOverwrite` |
| 移动文件或目录 | `/api/openApi/diskFile/move/v1` | `POST` | `storage_write` | Body: `sourceUid`、`sourcePath`、`targetUid`、`files`、`targetPath`、`isOverwrite` |
| 重命名文件或目录 | `/api/openApi/diskFile/rename/v1` | `POST` | `storage_write` | `uid`、`path`、`oldName`、`newName` |
| 删除文件或目录 | `/api/openApi/diskFile/delete/v1` | `DELETE` | `storage_write` | Query: `uid`、`path`；Body: `fileName[]` |

## 3. 资源域与 `uid` 映射

README 定义了 3 个资源域：

- `local`：本地文件系统，不走后端 API
- `private`：远程私人网盘
- `public`：远程公共网盘

根据开放接口文档，远程资源域需要映射到 `uid`：

- `public` -> `uid=0`
- `private` -> 授权用户的真实 `uid`

其中 `private` 资源域的 `uid` 可以通过 `GET /api/openApi/user/profile/v1` 返回的 `data.id` 获得。

因此 CLI 在构造远程请求时，至少要先解析资源路径中的资源域，再把它转换成对应的 `uid` 和远程路径。

## 4. README 命令与接口映射

| CLI 命令 | 是否可基于现有开放接口实现 | 所需接口 | 实现说明 |
| --- | --- | --- | --- |
| `ls <path>` | 是 | `profile`（仅 private）、`fileList` | 远程资源域转换为 `uid` 后直接调用 |
| `get <remoteResourcePath> [localPath]`（单文件） | 是 | `profile`（仅 private）、`download` 或 `downloadLink` | `download` 可直接取二进制流；`downloadLink` 适合先拿临时链接再下载 |
| `get <remoteResourcePath> [localPath]`（目录） | 是 | `profile`（仅 private）、`fileList`、`download` 或 `downloadLink` | 由 CLI 递归遍历目录树并逐文件下载，目录下载是客户端组合能力 |
| `upload <localPath> <remoteResourcePath>` | 是 | `profile`（仅 private）、`mkdir`、`upload` | 单文件直接上传；目录上传由 CLI 递归遍历本地目录，逐层 `mkdir` 后逐文件 `upload` |
| `rm <targetResourcePath>` | 是 | `profile`（仅 private）、`delete` | 需把目标拆成父目录 `path` 和文件名数组 `fileName[]` |
| `rename <sourceResourcePath> <newName>` | 是 | `profile`（仅 private）、`rename` | 只能改名，不能改位置，和 README 约束一致 |
| `cp <remoteResourcePath> <remoteResourcePath>` | 是 | `profile`（涉及 private 时）、`copy` | 通过 `sourceUid` / `targetUid` 可覆盖 `private` 与 `public` 间的跨网盘复制 |
| `mv <remoteResourcePath> <remoteResourcePath>` | 是 | `profile`（涉及 private 时）、`move` | 通过 `sourceUid` / `targetUid` 可覆盖 `private` 与 `public` 间的跨网盘移动 |
| `cp <localPath> <remoteResourcePath>` | 基本可行 | `profile`（仅 private）、`mkdir`、`upload` | 可在客户端把复制退化为上传 |
| `mv <localPath> <remoteResourcePath>` | 基本可行 | `profile`（仅 private）、`mkdir`、`upload` | 可先上传，成功后由 CLI 删除本地源文件 |
| `version` | 是 | 无 | 读取 CLI 本地版本，不依赖远程接口 |
| `remote-version` | 是 | `/api/hello/feature` | 匿名请求即可；直接读取响应体中的 `version` 字段 |

## 5. 建议的最小实现范围

如果按当前开放接口优先落一版 CLI，建议最小可交付命令为：

- `ls`
- `get`
- `upload`
- `rm`
- `rename`
- `cp`
- `mv`
- `version`
- `remote-version`

## 6. ApiTicket 权限要求

虽然 CLI 暂不实现 OAuth 流程，但用户手动提供的永久 ApiTicket 至少需要覆盖以下 scope：

- `profile`
- `storage_read`
- `storage_write`

如果 CLI 只操作公共网盘，可按需降低 scope；但 README 当前目标包含私人网盘，因此 `profile` 实际上已成为必要依赖。

## 7. 开发时的关键注意事项

1. CLI 当前只接受用户手动提供的永久 ApiTicket，不负责申请或刷新票据。
2. 业务接口统一使用 `ApiTicket`，不能把 AccessToken 当作业务接口认证头。
3. 私人网盘操作依赖 `profile` 接口返回的用户 `id` 作为后续存储接口的 `uid`。
4. 目录上传和目录下载都不是单独接口能力，需要 CLI 分别递归组合 `mkdir + upload` 与 `fileList + download`。
5. 删除、复制、移动、重命名都依赖“目录路径 + 文件名”的参数拆分，CLI 需要统一的远程路径解析逻辑。
6. `copy` / `move` 已支持 `sourceUid` 和 `targetUid`，旧的 Query `uid` 仅作为兼容参数。
7. 公共网盘与私人网盘通过 `uid` 区分，而不是通过不同接口路径区分。
8. `download.md` 和 `download-link.md` 的示例请求头仍写的是 `Bearer YOUR_ACCESS_TOKEN`，但开放接口总说明要求统一使用 `Authorization: ApiTicket {api_ticket}`，CLI 实现应以总说明为准。
9. `remote-version` 走 `/api/hello/feature`，它既不需要授权，也不使用 `data` 包装，解析方式与 OpenAPI 接口不同。
