# 咸鱼云网盘 CLI 客户端程序

通过cli命令行的方式与咸鱼云网盘服务进行交互，实现文件的上传、下载、删除、重命名、复制、移动、获取文件列表操作

## 快速开始

### 开发期间使用 .env 文件（推荐）

开发期间可以通过 `.env` 文件自动加载环境变量，避免每次手动配置：

1. 复制 `.env.example` 文件为 `.env`：
   ```bash
   cp .env.example .env
   ```

2. 编辑 `.env` 文件，填入实际的配置值：
   ```bash
   SFC_SERVICE_URL=http://saltedfishcloud-server-host
   SFC_CLIENT_ID=your_oauth_client_id
   ```

3. 程序启动时会自动从当前工作目录加载 `.env` 文件中的环境变量

**注意**：`.env` 文件包含敏感信息，已被 `.gitignore` 忽略，不会提交到版本控制。

### 1. 配置服务地址

- 方式1：使用命令行参数 `--service-url=<serviceUrl>` 手动指定
- 方式2：配置环境变量 `SFC_SERVICE_URL`
- 方式3：手动修改配置文件 `~/.config/sfc-cli/config.json`（如果没有可手动创建）
  配置文件中的键名保持为 `serviceUrl`：
  ```json
  {
    "serviceUrl": "service http url"
  }
  ```


### 2. 账号认证

通过 OIDC 设备授权流程（RFC 8628）登录，无需手动配置令牌：

```bash
sfc-cli login --client-id=<你的应用client_id>
```

流程说明：

1. CLI 从 `{serviceUrl}/.well-known/openid-configuration` 自动发现授权端点
2. 申请设备码后，终端展示验证页面地址与用户码，请自行在浏览器打开该地址
3. 在浏览器登录网盘并确认授权后，CLI 自动获得令牌并写入配置文件
4. 登录成功后打印当前登录用户；`access_token` 过期时 CLI 使用 `refresh_token` 自动刷新并回写配置文件

前置条件：需管理员在网盘管理后台创建第三方 OAuth 应用，并将令牌端点认证方式设为 `none`（public client），把应用 ID 作为 `--client-id` 传入。

可用参数：

| 参数 | 说明 |
| --- | --- |
| `--client-id` | OAuth 应用 ID；也可通过环境变量 `SFC_CLIENT_ID` 或配置文件键 `clientId` 提供 |
| `--scope` | 授权范围，空格分隔；默认 `profile storage_read storage_write` |

当缺少 `service-url` 或尚未登录时，CLI 会提示先执行 `sfc-cli login`。


### 3. 命令与参数

命令格式参考：
```
sfc-cli
  [--service-url=<serviceUrl>]
  <command> [<args>]
```

#### 资源路径参数 path 约定

- 路径格式: `[resourceArea:]<path>`
- `resourceArea`表示资源域，默认为`private`表示远程“我的网盘/私人网盘”的资源。即：`private:/my-files`与`/my-files`等价

当前可用的资源域有：

| 资源域 | 含义 |
| ----- | -----|
| private | 默认值，远程，我的网盘 |
| public | 远程，公共网盘 |
| local | 本地文件系统 |

#### command 与 args 参考

- 命令层在参数个数不符合要求时，会先输出当前命令帮助文本，再返回参数错误。

##### 文件操作

- `ls [path]` - 列出指定目录下的文件列表，未指定时默认使用 `/`
- `get <remoteResourcePath> [localPath]` - 把远程网盘资源下载到本地（支持文件夹/单文件）。`localPath`未指定时，文件下载到当前工作目录。
- `upload <localPath> <remoteResourcePath>` - 把本地文件/文件夹上传到远程。`remoteResourcePath`只能接受远程资源域；单文件上传时`remoteResourcePath`需以目标文件名结尾。文件夹上传采用流式传输：目录内的符号链接和空文件会被跳过并输出警告；单个文件失败不中止整体上传，结束时输出汇总，存在失败时以非零码退出。
- `cp <sourceResourcePath> <targetResourcePath>` - 复制文件，支持跨资源域操作。当`sourceResourcePath`的资源域为`local`时，`targetResourcePath`为`public`或`private`时，则等价于`upload`操作。
- `mv <sourceResourcePath> <targetResourcePath>` - 移动文件，支持跨资源域操作，参数逻辑同`copy`。
- `rm <targetResourcePath>` - 删除文件
- `rename <sourceResourcePath> <newName>` - 重命名文件，不能修改文件位置。

##### 账号操作

- `login` - 通过 OIDC 设备授权流程登录网盘，将令牌持久化到配置文件。参数见「账号认证」章节

##### 其他操作

- `version` - 查看当前cli程序版本
- `remote-version` - 查询远端服务端版本号

## 配置文件参考

`~/.config/sfc-cli/config.json` 的完整键名：

| 键名 | 说明 |
| --- | --- |
| `serviceUrl` | 服务基础地址（必填） |
| `clientId` | OAuth 登录所用应用的 client_id |
| `accessToken` | OAuth 登录获得的访问令牌（由 `login` 写入） |
| `refreshToken` | OAuth 登录获得的刷新令牌（由 `login` 写入） |
| `expiresAt` | 访问令牌过期时间，RFC3339 格式（由 `login` 写入） |
