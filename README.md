# 咸鱼云网盘 CLI 客户端程序

通过cli命令行的方式与咸鱼云网盘服务进行交互，实现文件的上传、下载、删除、重命名、复制、移动、获取文件列表操作

## 快速开始

### 1. 配置服务地址

- 方式1：配置环境变量`SFC_SERVICE_URL`
- 方式2：使用命令行参数`--service-url=<serviceUrl>`手动指定
- 方式3：手动修改配置文件
  文件配置: `~/.config/sfc-cli/config.json`（如果没有可手动创建）
  配置内容：
  ```json
  {
    "serviceUrl": "service http url"
  }
  ```


### 2. 账号认证

#### 手动配置永久有效的 ApiTicket

- 方式1：配置环境变量`SFC_API_TICKET`
- 方式2：使用命令行参数`--api-ticket=<apiTicket>`手动指定
- 方式3：手动修改配置文件
  文件配置: `~/.config/sfc-cli/config.json`（如果没有可手动创建）
  配置内容：
  ```json
  {
    "apiTicket": "your permanent api ticket"
  }
  ```


### 3. 命令与参数

命令格式参考：
```
sfc-cli
  [--api-ticket=<apiTicket>]
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

#### command 与 args参考

##### 文件操作

- `ls <path>` - 列出指定目录下的文件列表
- `get <remoteResourcePath> [localPath]` - 把远程网盘资源下载到本地（支持文件夹/单文件）。`localPath`未指定时，文件下载到当前工作目录。
- `upload <localPath> <remoteResourcePath>` - 把本地文件/文件夹上传到远程。`remoteResourcePath`只能接受远程资源域。
- `cp <sourceResourcePath> <targetResourcePath>` - 复制文件，支持跨资源域操作。当`sourceResourcePath`的资源域为`local`时，`targetResourcePath`为`public`或`private`时，则等价于`upload`操作。
- `mv <sourceResourcePath> <targetResourcePath>` - 移动文件，支持跨资源域操作，参数逻辑同`copy`。
- `rm <targetResourcePath>` - 删除文件
- `rename <sourceResourcePath> <newName>` - 重命名文件，不能修改文件位置。

##### 其他操作

- `version` - 查看当前cli程序版本