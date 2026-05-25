# sfc-cli 仍需注意的接口实现约束

当前 README 中声明的 CLI 能力，基于现有后端接口已可覆盖；目前没有必须新增的后端接口缺口。

需要注意的是，其中部分能力依赖客户端编排，且不同接口的返回体解析规则并不完全一致。当前实现前提也已简化为：用户手动提供永久有效的 ApiTicket，CLI 暂不负责 OAuth 授权与换票。

## 1. 可实现但需要客户端补逻辑的能力

以下能力不是接口缺失，但不能靠单个接口直接完成，CLI 需要自行编排：

| README 能力 | 现状 | CLI 侧处理方式 |
| --- | --- | --- |
| ApiTicket 获取 | CLI 不负责 | 由用户在 CLI 外部手动申请永久 ApiTicket，并通过参数、环境变量或配置文件提供给 CLI |
| 私人网盘操作的 `uid` 获取 | 已可通过 `profile` 接口获取 | 直接使用已有 ApiTicket 调用 `profile`，读取 `data.id` 作为私人网盘 `uid` |
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
