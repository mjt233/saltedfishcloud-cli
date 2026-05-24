# sfc-cli 分阶段实现设计

## 背景

仓库当前仅有 README、架构与接口说明文档，尚未开始 Go 代码实现。目标是基于现有文档，从零实现一个 Go 版咸鱼云网盘 CLI，并保持以下约束：

- 对外命令语义以 `README.md` 为准
- 分层职责以 `docs/develop/architecture.md` 为准
- 认证方式仅支持用户手动提供永久有效的 `ApiTicket`
- 路径格式固定为 `[resourceArea:]<path>`
- 每完成一个关键步骤或模块即进行一次 Git 提交

## 范围

本次开发按优先级分阶段完成全部命令：

1. P0：`ls`、`get`、`upload`、`rm`、`rename`
2. P1：`cp`、`mv`、`version`
3. P2：`remoteVersion`

不纳入首轮范围：

- OAuth 授权码流程、换票、刷新逻辑
- README 未声明的增强功能
- 依赖真实线上环境的仓库内联调脚本

## 推荐实现策略

采用“共享基础设施 + 纵向切片”推进：

1. 先搭建 `main`、`cmd/root`、`config`、`client`、`service/path`、`service/user`
2. 先纵向打通 `ls`，验证配置、鉴权、路径解析、UID 映射、远端调用与表格输出链路
3. 再扩展 `get`、`upload`、`rm`、`rename`
4. 最后补齐 `cp`、`mv`、`version`、`remoteVersion`

这样可以尽早验证最小闭环，也更适合按照关键模块拆分提交。

## 模块设计

### 1. 入口与命令层

- `main.go` 仅负责启动根命令
- `cmd/root.go` 负责：
  - 注册全局参数 `--api-ticket`、`--service-url`
  - 装配运行时依赖
  - 注册各子命令
- 各命令文件只负责参数解析、输入校验、调用 service、格式化输出

### 2. 配置层 `internal/config`

职责：

- 合并配置优先级：命令行参数 > 环境变量 > `~/.config/sfc-cli/config.json`
- 统一校验必需配置项 `serviceUrl`、`apiTicket`
- 缓存运行期 private 资源域所需的用户 `uid`

建议输出模型：

```go
type Config struct {
    ServiceURL string
    APITicket  string
}
```

同时提供带缓存能力的运行时对象，供 service 层共享。

### 3. 客户端层 `internal/client`

职责：

- 统一注入 `Authorization: ApiTicket {ticket}`
- 统一处理 GET/POST/DELETE 请求
- 除 `/api/hello/feature` 外，默认从 JSON 的 `data` 字段解包业务数据
- 统一把 `businessCode` 与 `msg` 转换为错误
- 为下载接口保留二进制流出口

建议拆分：

- 基础请求方法
- JSON 响应解包
- 下载流接口
- multipart 上传接口

### 4. 服务层 `internal/service`

#### `path.go`

统一解析 `[resourceArea:]<path>`：

```go
type ResolvedPath struct {
    Area string
    UID  int64
    Path string
}
```

规则：

- 未显式声明资源域时默认 `private`
- `public` 映射为 `uid=0`
- `private` 通过 profile 接口获取真实用户 ID
- `local` 不触发远端请求

#### `user.go`

- 封装 profile 查询
- 提供 private UID 的懒加载和缓存

#### `diskfile.go`

封装以下能力：

- `ls`
- `get`（单文件与目录递归下载）
- `upload`（单文件与目录递归上传）
- `rm`
- `rename`

#### `copier.go`

封装以下能力：

- remote -> remote 的 `cp` / `mv`
- local -> remote 的退化上传编排
- `mv local -> remote` 上传成功后删除本地源文件

### 5. 本地文件系统层 `internal/localfs`

职责：

- 本地路径规范化
- 目录遍历
- 为上传、下载、移动编排提供文件系统辅助

## 阶段划分与提交策略

### 阶段 A：基础设施 + `ls`

包括：

- Go 模块初始化
- cobra/viper/progressbar 依赖接入
- 根命令与运行时装配
- 配置加载
- HTTP 客户端基础能力
- 路径解析与 private UID 解析
- `ls` 端到端实现

提交建议：

1. `feat: 初始化 CLI 工程骨架与基础配置能力`
2. `feat: 实现列表命令与远端文件查询链路`

### 阶段 B：`get`

包括：

- 单文件下载
- 目录递归下载
- 本地目录创建
- 下载进度反馈

提交建议：

1. `feat: 实现文件下载命令基础链路`
2. `feat: 补充目录递归下载与进度反馈`

### 阶段 C：`upload`

包括：

- 单文件上传
- 目录遍历
- 逐层 `mkdir`
- 上传进度反馈

提交建议：

1. `feat: 实现文件上传命令基础链路`
2. `feat: 补充目录上传编排与进度反馈`

### 阶段 D：`rm`、`rename`

包括路径拆分、文件名提取、用户可读结果输出。

提交建议：

1. `feat: 实现删除与重命名命令`

### 阶段 E：`cp`、`mv`

包括：

- remote -> remote 跨资源域复制与移动
- local -> remote 退化为上传
- `mv local -> remote` 的本地删除逻辑

提交建议：

1. `feat: 实现跨资源域复制与移动命令`

### 阶段 F：`version`、`remoteVersion`

包括：

- 本地版本号注入与输出
- 远端匿名版本查询

提交建议：

1. `feat: 实现版本查询相关命令`

## 统一数据流

1. `cmd` 读取参数并完成基础校验
2. `config` 加载配置并构造运行时上下文
3. `service/path` 解析资源路径
4. `service/user` 在需要 private 资源域时解析 UID
5. `service/diskfile` / `service/copier` 调用 `client`
6. `cmd` 输出表格、进度条或结果文案

## 错误处理策略

- 缺少配置：启动时一次性指出缺失项
- 路径格式错误：在路径解析阶段直接返回带示例的错误
- 网络错误：保留原始错误与目标 URL
- 业务错误：保留 `businessCode` 与 `msg`
- 递归操作部分失败：收集失败列表并在命令结束时汇总

## 测试与验收策略

以单元测试和 `httptest` 模拟后端为主，不要求仓库内依赖真实服务环境。

重点覆盖：

- `internal/config`：配置优先级、缺失配置
- `internal/service/path`：路径解析与边界条件
- `internal/client`：认证头、响应解包、错误映射、特殊接口解析
- `internal/service`：UID 缓存、命令参数编排、递归处理关键分支

阶段验收标准：

- 每个阶段完成后都具备可运行实现
- `gofmt` 与 `go test ./...` 通过
- 用户可见输出符合 README 约定

## 风险与约束

- 目录上传、目录下载、`local -> remote` 的 `cp`/`mv` 都依赖 CLI 自身编排，复杂度高于单接口调用
- `download`、`remoteVersion` 的响应解析规则与普通 JSON 接口不同，需在客户端层集中处理
- 当前仓库从零起步，必须优先稳定共享基础设施，避免后续命令层各自复制逻辑
