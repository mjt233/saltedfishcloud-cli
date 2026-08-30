# sfc-cli 仍需注意的接口实现约束

当前 README 中声明的 CLI 能力，基于现有后端接口已可覆盖；目前没有必须新增的后端接口缺口。

需要注意的是，其中部分能力依赖客户端编排，且不同接口的返回体解析规则并不完全一致。认证方面，CLI 仅通过 OIDC 设备授权流程（`sfc-cli login`，基于 `/.well-known/openid-configuration` 端点发现）获取令牌，并支持 `refresh_token` 自动刷新。

## 1. 可实现但需要客户端补逻辑的能力

以下能力不是接口缺失，但不能靠单个接口直接完成，CLI 需要自行编排：

| README 能力 | 现状 | CLI 侧处理方式 |
| --- | --- | --- |
| 令牌获取 | 已可通过 OIDC 设备授权流程实现 | `login` 命令完成设备授权引导，`access_token`/`refresh_token` 持久化到配置文件，过期自动刷新并回写 |
| 私人网盘操作的 `uid` 获取 | 已可通过 `profile` 接口获取 | 使用已获取的令牌调用 `profile`，读取 `data.id` 作为私人网盘 `uid` |
| 目录下载 | 无专用目录下载接口 | 递归遍历远程目录树，组合 `fileList + download` 或 `fileList + downloadLink` |
| 目录上传 | 无专用目录上传接口 | 递归遍历本地目录，逐层 `mkdir`，逐文件 `upload` |
| `cp remote -> remote` 跨网盘 | 已有开放接口支持 | 使用 `copy` 接口的 `sourceUid` 与 `targetUid` 进行跨 `private/public` 复制 |
| `mv remote -> remote` 跨网盘 | 已有开放接口支持 | 使用 `move` 接口的 `sourceUid` 与 `targetUid` 进行跨 `private/public` 移动 |
| `cp local -> remote` | 无“本地复制到远程”专用接口 | 退化为上传 |
| `mv local -> remote` | 无“本地移动到远程”专用接口 | 先上传，成功后删除本地源文件 |

## 2. 返回体解析差异

| 接口类型 | 返回体形态 | CLI 解析注意点 |
| --- | --- | --- |
| OAuth / OpenAPI JSON 接口 | `{"code":200,"data":...,"msg":"OK"}` | 实际业务数据从 `data` 字段读取 |
| `/api/hello/feature` | 直接返回对象，例如 `{"version":"3.1.2.0-RELEASE"}` | 不包在 `data` 中，`remote-version` 直接读取顶层 `version` |
| `/api/openApi/diskFile/download/v1` | 二进制文件流 | 不按 JSON 解析，直接写入本地文件 |
