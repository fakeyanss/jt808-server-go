# 贡献指南

感谢你愿意改进 jt808-server-go。本项目是一个用 Go 编写的 JT/T 808 终端接入服务，同时覆盖苏标主动安全附件、JT/T 1078 音视频与集群部署。提交前请先阅读本指南，避免把时间花在返工上。

## 开始之前

- 先搜索已有 [Issue](https://github.com/fakeyanss/jt808-server-go/issues) 与 Pull Request，避免重复工作。
- 较大的功能、协议行为变更或集群接口调整，请先开 Issue 说明场景、边界与验证方式，达成一致后再写代码。
- 安全漏洞不要走公开 Issue，请按 [SECURITY.md](SECURITY.md) 的私密渠道报告。

## 环境要求

- Go 1.26+
- `make`、`bash`
- 可选：`sqlite3`（检查本地库）、Docker（验证镜像与 compose）

默认配置使用 `memory` 后端，不需要数据库即可跑通接入链路。

## 分支与提交

1. 从最新 `main` 拉出主题分支，命名建议 `dev/<主题>`，例如 `dev/protocol-0x8105`、`dev/media-playback`。
2. 不要直接向 `main` 提交；通过 Pull Request 合并。
3. 一个提交只做一件事。提交信息说明“为什么改”，而不只是“改了什么”，格式建议：

   ```text
   feat(media): 支持按通道查询录像覆盖区间
   fix(gateway): control 不可达时按有界宽限放行缓存档案
   docs(coverage): 补记 0x9207 并刷新注册表自动生成段
   refactor(attachment): 附件节点不连库，进度与恢复点交由 control
   ```

   常用类型：`feat`、`fix`、`refactor`、`docs`、`test`、`ci`、`chore`。作用域可用模块名，如 `protocol`、`server`、`storage`、`cluster`、`media`、`attachment`、`api`。

4. 不提交构建产物、数据库文件、日志、真实配置或密钥。提交前检查 `git status`，只暂存本次相关的文件。

## 提交前检查

在仓库根目录执行：

```sh
gofmt -l .                      # 应无输出
go build ./...
go vet ./...
go test -race -count=1 ./...
./scripts/check-coverage.sh     # 协议注册表与覆盖矩阵一致
```

涉及端到端链路时再执行：

```sh
make smoke                      # 构建真实 server/client 二进制并跑 5 个场景
```

`make smoke` 会占用默认端口，可用 `SMOKE_TCP_PORT`、`SMOKE_UDP_PORT`、`SMOKE_HTTP_PORT` 覆盖；需要保留日志时加 `--keep-logs`。

CI 会在 `main`、`dev/**` 的 push 与 Pull Request 上执行 `make all`、覆盖矩阵校验和 outbox 回归测试。PR 合并前必须全绿。

## 协议消息开发

新增或修改一条 JT/T 808 / 苏标 / 1078 消息时，按以下顺序处理：

1. 在 `internal/protocol/model/` 新建或修改 `msg_XXXX.go`，实现 `Decode` / `Encode`，按 `Header.Attr.VersionDesc` 处理 2011/2013/2019 的字段差异；
2. 在 `internal/protocol/msg_processor.go` 的注册表登记 MsgID，绑定请求/应答模型与网关半边处理函数；控制面业务处理登记在 `internal/business/registry.go`；
3. 如果消息有归属差异，同步更新 `internal/protocol/ownership.go`：网关项、管控项、双方项必须与两份注册表一致；
4. `OwnerGateway` 的应答与连接行为留在 `internal/protocol`；`OwnerControl` 的业务与落库放 `internal/business`，只调用 `internal/repository` 的接口，不直接写 SQL；`OwnerBoth` 两边都要实现；
5. 补单元测试。模型测试覆盖编解码往返与版本差异；网关半边测试放 `internal/protocol`，业务处理测试放 `internal/business`，涉及连接、分包或应答配对时在 `internal/server/` 或端到端测试中补场景；
6. 刷新覆盖矩阵：

   ```sh
   go run ./tools/registrydoc -write
   ./scripts/check-coverage.sh
   ```

   自动生成段位于 `docs/protocol-coverage.md` 的 `BEGIN/END GENERATED REGISTRY` 标记之间，**不要手工编辑**。手工状态表需要按代码实际行为更新；状态定义见该文档。

## 目录结构与扩展方式

### 目录职责

| 路径 | 职责 | 约束 |
|---|---|---|
| `internal/protocol/` | 帧处理、编解码、版本判别、分包重组、MsgID 注册与网关半边 | 不持有连接、不直接写库、不承载管控业务落库 |
| `internal/business/` | 管控业务半边：注册/鉴权落库、定位报警、行业上报、多媒体与附件元数据 | 只依赖 `repository` 接口；由 control 调用或单机注入 |
| `internal/repository/` | 持久化契约：按领域拆分的 `XxxRepository` 接口、读写 DTO、哨兵错误、保留策略 | 接口唯一来源；不引入任何后端实现 |
| `internal/repository/memory/` | 内存实现，默认后端与测试基座 | 与 SQL 实现保持同一语义 |
| `internal/repository/sqlcore/` | 方言无关的 SQL 实现、`Store`（读写池/方言语句/时钟/迁移计划） | 控制流与扫描只写一份 |
| `internal/repository/sqlite/`、`mysql/` | 方言语句、DDL、连接池与迁移计划；`Backend` 嵌入 `sqlcore.Store` | 只放本方言差异 |
| `internal/repository/database/` | 后端装配、`Repositories` 聚合、`Init/Default/Close/Check` | 不引入数据库驱动 |
| `internal/repository/database/sqlbackend/` | 把 sqlite/mysql 的打开与迁移注册进 `database` | 仅 control、standalone、migrate 导入 |
| `internal/server/` | 连接生命周期、pipeline、会话调度 | 通过 `protocol.ProcessorDeps` 注入依赖 |
| `internal/control/` | 集群控制面、上行事件消费、节点与会话调度 | 唯一连业务库的角色 |
| `internal/controlhttp/` | control 侧内部 HTTP 入口：上行事件、设备档案查询、节点注册/心跳、FTP 登录、进度与切片上报 | 只由 control 装配；路径取自 `internalapi` |
| `internal/uplink/` | 网关侧上行出口：HTTP 投递、可靠性分类与 409 重投 | 不承载 control 侧入口 |
| `internal/internalapi/` | 角色间内部 HTTP 的路径常量与上行事件信封 | 叶子包；网关出口与 control 入口共用 |
| `internal/api/` | 业务 HTTP API | 仓储由装配层注入，不取全局单例 |
| `tools/registrydoc/` | 生成并校验协议覆盖矩阵 | 生成段禁止手改 |

### 新增仓储

显式装配是有意为之：新增一个仓储必须让编译器提示所有装配点，不要用反射或自动注册消除这些步骤。

1. `internal/repository/<domain>_repository.go`：定义接口与读写 DTO/错误；接口方法要说明读写所有权与快照语义。
2. `internal/repository/memory/<domain>_repository.go`：实现内存版本，作为测试与 `memory` 后端。
3. `internal/repository/sqlcore/<domain>_repository.go`：实现方言无关 SQL，并在 `sqlcore.Store` 上暴露 getter。
4. 需要新 SQL/表时：补 `sqlite/statements.go`、`mysql/statements.go` 与两端 `migrations/v<MAJOR>.<MINOR>.<PATCH>.sql`。
5. `internal/repository/repositories.go`、`memory/repositories.go`、`database.NewSQLRepositories`：补字段与装配。
6. 消费方注入：`business.Options.Repositories`、`api.Run` 的 `repository.Repositories` 或 `protocol.ProcessorDeps.Repos`。
7. 测试至少覆盖 memory 与一条 SQL 后端（`database_test.go` 或 `sqlite`/`mysql` 包内）。

### 新增数据库后端

1. 在 `internal/repository/sqlcore/` 复用现有 `Statements` 接口；方言差异放到新后端包的 `statements.go`。
2. 新建 `internal/repository/<backend>/`：实现 `Open`、`Migrate`、连接池与 `migrations/`，返回嵌入 `sqlcore.Store` 的 `Backend`。
3. 在 `internal/repository/database/sqlbackend/` 注册 `open` 与 `migrate` 两个函数。
4. 需要该后端的入口二进制显式 blank import `sqlbackend`；数据面二进制不要导入。
5. 同步 `configs/default.yaml` 的后端枚举、`docs/configuration.md` 与 `docs/installation.md` 的部署说明，并补迁移语句。
6. 用 `go list -deps ./cmd/jt-signal | grep -E 'sqlite|mysql'` 确认数据面没有被带进驱动。

### 新增业务模块（管控侧）

1. 先判断归属：只做协议编解码与网关应答的留在 `internal/protocol`；需要落库的控制面业务放 `internal/business`；跨节点编排与调度放 `internal/control`。
2. 在 `internal/protocol/msg_processor.go` 注册 `genData`（解码与默认应答），在 `internal/business/registry.go` 注册 `process`（业务处理）。
3. 在 `internal/protocol/ownership.go` 声明归属；`OwnerControl` 必须有业务处理函数，`OwnerBoth` 必须同时有协议半边与业务半边。
4. 仓储通过 `business.Options.Repositories` 注入，不在业务代码里调用 `database.Default()`。
5. 单机链路由 `ProcessorDeps.Business` 注入；集群网关不注入，业务只在 control 执行。
6. 刷新生成物并跑检查：

   ```sh
   go run ./tools/registrydoc -write
   ./scripts/check-coverage.sh
   ```

### 最小示例

新增一个 `FooRepository` 时，契约与两套实现的最小骨架如下，装配点按上一节补全：

```go
// internal/repository/foo_repository.go
type FooRepository interface {
	Get(ctx context.Context, phone string) (*FooRecord, error)
	Save(ctx context.Context, record *FooRecord) error
}

// internal/repository/memory/foo_repository.go
type fooRepository struct { mu sync.RWMutex; items map[string]*repository.FooRecord }
func NewFooRepository() repository.FooRepository { return &fooRepository{items: map[string]*repository.FooRecord{}} }

// internal/repository/sqlcore/foo_repository.go
func (s *Store) FooRepository() repository.FooRepository { return &fooRepository{store: s} }
```

```go
// internal/repository/repositories.go
type Repositories struct {
	// ...
	Foo FooRepository
}

// internal/repository/database/database.go（NewSQLRepositories）
repos.Foo = handle.FooRepository()

// internal/repository/memory/repositories.go
Foo: NewFooRepository(),
```

## 元数据存储与迁移

- 分层约定：`repository` 只放契约；`memory`、`sqlcore` 是两套实现；`sqlite`/`mysql` 只提供方言语句、DDL 与连接；`database` 负责装配与聚合；`sqlbackend` 负责驱动注册。
- 仓储接口定义在 `internal/repository/`，方言实现由 `sqlcore` 复用，`internal/repository/sqlite/` 与 `internal/repository/mysql/` 只放差异部分。
- 迁移文件名格式为 `v<MAJOR>.<MINOR>.<PATCH>.sql`，两套目录版本号必须同步推进。
- 每个迁移文件只写本后端的方言：SQLite 使用 `TEXT`/`BLOB` 与 `ON CONFLICT`，MySQL 使用显式 `ENGINE=InnoDB`/`utf8mb4` 与方言类型。
- 版本按三段数值比较（`1.10.0` > `1.9.0`），不要用字符串排序。
- 新增表或字段时，同步检查仓储接口、memory 实现、sqlcore 实现与 `Repositories` 装配，避免只改一端。
- 主服务启动只校验 schema 版本，不执行 DDL；迁移由 `cmd/jt808-migrate` 独立执行。改迁移后请本地跑一次升级路径。
- `sqlbackend` 只被 control、standalone、migrate 导入；gateway、media、attachment 不链接数据库驱动，也不应新增数据库依赖。

## 集群模式开发

- 五个角色二进制分别是 `jt-server-standalone`、`jt-signal`、`jt-media`、`jt-control`、`jt-attachment`，固定角色与样例配置见 `configs/cluster/`。
- 集群改动至少验证两类形态：
  - Standalone：不配置 `cluster` 段时行为、端口与外部依赖不变；
  - Cluster：按 `configs/cluster/` 的样例启动相关角色，确认启动校验、探针与关键接口。
- 集群内部 HTTP 契约、会话目录、节点注册与下行路由的语义以 `docs/architecture.md` 为准；改契约时同步更新文档与配置示例。
- 集群元数据面只允许 control 连数据库；gateway、media、attachment 不应新增数据库依赖（构建上也不会链接驱动，见上文 `sqlbackend`）。

## 代码与文档约定

- 日志与 `error` 信息使用英文，便于检索与对接；注释、文档、测试业务数据使用中文。
- 描述上行可靠性时统一用“应答时机”，不混用“时序”与“时机”。
- 协议字段以标准为准，版本差异通过消息头判别后分支处理，不做无依据的兼容猜测。
- 面向使用者的文档放在 `docs/`：
  - `installation.md`：安装与部署
  - `configuration.md`：配置说明
  - `architecture.md`：架构与集群模式
  - `protocol-coverage.md`：协议覆盖（含自动生成段）
  - `api.md`：业务 API
  - `operations.md`：运维手册

  `docs/archive/` 保存历史设计与研究文档，不再维护，除修正事实性错误外不要改写。目录职责与新增仓储/后端/业务模块的步骤见本文的[目录结构与扩展方式](#目录结构与扩展方式)。

## Pull Request

- PR 描述按 [.github/PULL_REQUEST_TEMPLATE.md](.github/PULL_REQUEST_TEMPLATE.md) 填写，写清行为变化、兼容性与验证方式。
- 关联相关 Issue；如果 PR 依赖其他未合入的分支，请在描述中明确依赖关系与合并顺序。
- 只包含本次主题的改动，不混入无关格式化或重构。
- UI/接口/配置变更请在描述中给出调用示例或配置片段；集群行为变更请说明 Standalone 是否受影响。
- 评审意见通过新增提交处理，不要用 force push 覆盖评审历史，除非维护者明确要求。

## 许可证

提交贡献即表示你同意以本仓库的 [MIT License](LICENSE) 授权你的贡献。
