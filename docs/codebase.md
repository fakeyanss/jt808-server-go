# 代码结构与扩展指南

本文说明仓库的分层、目录职责、接口与实现的对应关系与依赖方向，并给出新增仓储、数据库后端、业务模块与协议消息的标准步骤。系统行为、角色边界与部署形态见[架构与集群模式](architecture.md)。

## 1. 分层与依赖方向

代码按“入口、装配、角色、领域、契约、实现”分层，依赖只允许从上往下：

```text
cmd/*            进程入口：只声明角色与配置解析
internal/app     组合根：唯一允许依赖全部包的装配层
角色包           server、control、controlhttp、api、media、alarmserver、attachmentserver、ftpserver、
                 retention、observability、adminapi……
领域包           protocol、business、uplink、downlinkhttp、runtime、internalapi
契约包           repository、contracts、contracts/media、contracts/attachment
实现包           repository/memory、repository/sqlcore、repository/sqlite、repository/mysql、
                 storage、media/playback、mediaupload
生成与工具       tools/registrydoc、cmd/jt808-migrate
```

几条硬约束：

- `internal/repository` 只放契约（接口、读写 DTO、哨兵错误、保留策略），不引入任何后端实现。
- `internal/protocol` 只依赖 `repository` 的接口聚合，不依赖 `repository/database`、`sqlite`、`mysql`；协议内核不直接取全局单例。
- `internal/business` 只依赖 `repository` 契约，不直接写 SQL。
- `internal/repository/database` 只做装配与聚合，不引入数据库驱动；驱动注册集中在 `database/sqlbackend`，仅 control、standalone、migrate 三个入口显式导入。
- `internal/storage` 自己定义 `BlobStore` 并给出实现，不依赖 `contracts`。
- `internal/contracts` 只保留跨进程最小信封（上行、下行、鉴权与终端应答）；媒体与附件契约分别在 `contracts/media`、`contracts/attachment`，播放与录像契约在 `media/playback`。
- 数据面二进制（`jt-signal`、`jt-media`、`jt-attachment`）不链接 sqlite 与 mysql 驱动，也不用 `database` 配置段。

可用以下命令校验依赖方向：

```sh
go list -deps ./cmd/jt-signal ./cmd/jt-media ./cmd/jt-attachment \
  | grep -E 'modernc\.org/sqlite|go-sql-driver/mysql' || echo "no database drivers"
```

## 2. 目录职责

| 路径 | 职责 | 约束 |
|---|---|---|
| `cmd/` | 进程入口：`jt-server-standalone`、`jt-signal`、`jt-control`、`jt-media`、`jt-attachment`、`jt808-migrate`、`jt808-client` | 只声明角色、解析配置、调用 `internal/app` |
| `internal/app/` | 组合根：按角色装配依赖、启动顺序、优雅关停 | 唯一允许依赖全部包的层；装配保持显式 |
| `internal/protocol/` | 帧处理、编解码、版本判别、分包重组、MsgID 注册与网关半边 | 不持有连接、不直接写库、不承载管控业务落库 |
| `internal/protocol/model/` | 808/1078 报文结构与设备、参数、属性等领域实体 | 只放数据结构与编解码，不放业务编排 |
| `internal/business/` | 管控业务半边：注册/鉴权落库、定位报警、行业上报、多媒体与附件元数据 | 只依赖 `repository` 接口；由 control 调用或单机注入 |
| `internal/server/` | TCP/UDP 接入、会话表、可靠重传、下行 `Send` | 通过 `protocol.ProcessorDeps` 注入依赖 |
| `internal/control/` | 集群控制面、上行事件消费、outbox、媒体与附件调度、节点与会话目录、FTP 任务 | 唯一连业务库的角色 |
| `internal/controlhttp/` | control 侧内部 HTTP 入口：上行事件、设备档案查询、节点注册/心跳、FTP 登录、进度与切片上报 | 只由 control 装配；路径常量取自 `internalapi` |
| `internal/uplink/` | 网关侧上行出口：HTTP 投递、可靠性分类与重投 | 不承载 control 侧入口 |
| `internal/downlinkhttp/` | control → 网关下行 HTTP 的 client、router 与 server | 与 `uplink` 对称，不混入上行逻辑 |
| `internal/internalapi/` | 角色间内部 HTTP 的路径常量与上行事件信封 | 叶子包；网关出口与 control 入口共用 |
| `internal/api/` | 对外管理面 HTTP：设备、位置、下行、媒体/附件/播放接口 | 仓储由装配层注入，不取全局单例 |
| `internal/adminapi/` | 管理面 HTTP：健康/就绪/指标/会话/历史 | `Dependencies` 显式注入 |
| `internal/contracts/` | 跨进程最小信封：`UplinkSink`、`Downlink`、`AuthProvider`、终端应答 | 不再承载媒体、附件与对象存储类型 |
| `internal/contracts/media/` | 媒体端点、租约、`MediaAllocator`、`MediaReader` 与调度错误码 | 只放媒体领域契约 |
| `internal/contracts/attachment/` | 附件端点、租约、`AttachmentAllocator`、进度与 FTP 登录错误 | 只放附件领域契约 |
| `internal/repository/` | 持久化契约：按领域拆分的 `XxxRepository` 接口、读写 DTO、哨兵错误、保留策略 | 接口唯一来源；不引入任何后端实现 |
| `internal/repository/memory/` | 内存实现，默认后端与测试基座 | 与 SQL 实现保持同一语义 |
| `internal/repository/sqlcore/` | 方言无关的 SQL 实现、`Store`（读写池/方言语句/时钟/迁移计划） | 控制流与扫描只写一份 |
| `internal/repository/sqlite/`、`mysql/` | 方言语句、DDL、连接池与迁移计划；`Backend` 嵌入 `sqlcore.Store` | 只放本方言差异 |
| `internal/repository/database/` | 后端装配、`Repositories` 聚合、`Init/Default/Close/Check` | 不引入数据库驱动 |
| `internal/repository/database/sqlbackend/` | 把 sqlite/mysql 的打开与迁移注册进 `database` | 仅 control、standalone、migrate 导入 |
| `internal/storage/` | `BlobStore` 契约与 `sharedVolume`、`objectStore` 两种实现 | 不依赖 `contracts`；不感知业务语义 |
| `internal/media/` | 1078 RTP/PS/TS、帧环、录制、HTTP-FLV | 通过 `media/playback` 输出 |
| `internal/media/playback/` | 播放输出（进程内 FLV / 转推 RTMP）与播放、录像契约 | 接口中不出现 `http.ResponseWriter` |
| `internal/mediaupload/` | `0x0801` 多媒体本地落盘与路径安全 | 与 `storage.BlobStore` 分工明确 |
| `internal/alarmserver/` | 苏标附件 `0x1210/0x1211/0x1212` 接入、组装、落盘/切片 | 附件接收核心 |
| `internal/attachmentserver/` | 附件节点的 blob 模式装配层 | 复用 `alarmserver` |
| `internal/ftpserver/` | JT/T 1078 文件上传的 FTP 服务端 | 只接受 PASV/EPSV |
| `internal/retention/` | 保留期分批清理与孤儿文件回收 | 清理按对象 key 前缀隔离 |
| `internal/runtime/` | 连接注册表、分包、重传与各类等待 broker | 显式注入，业务层不取全局 |
| `internal/coordination/` | 多副本租约与共享 TTL 去重 | 不参与协议处理 |
| `internal/observability/`、`internal/httpguard/` | 指标、就绪探针；鉴权、限流、审计中间件 | 中间件与 handler 分离 |
| `internal/action/` | 终端接入生命周期钩子、事件过滤、HTTP 回调出口 | 只依赖 `pkg/routines` |
| `internal/config/` | 全量配置结构、解析与校验 | 字段与 `configs/default.yaml`、`docs/configuration.md` 同步 |
| `internal/codec/` | GBK、哈希、十六进制、行政区划查询 | 通用编解码工具 |
| `internal/client/`、`client/` | 模拟终端 TCP 客户端 | 不引入服务端装配 |
| `pkg/` | 可复用小工具：环形缓冲、日志装配、panic 恢复 | 只放与业务无关的通用代码 |
| `tools/registrydoc/` | 生成并校验协议覆盖矩阵 | 生成段禁止手改 |

## 3. 接口与实现的对应关系

仓储契约全部定义在 `internal/repository/*.go`，装配聚合在 `internal/repository/repositories.go`：

| 契约 | memory 实现 | SQL 实现 | 说明 |
|---|---|---|---|
| `DeviceRepository`、`DeviceParamsRepository`、`TerminalPropertiesRepository` | `memory.NewDeviceRepository` 等 | `sqlcore.Store.DeviceRepository()` 等 | 设备档案与参数 |
| `GeoRepository`、`LocationRepository`、`AlarmRepository` | `memory.NewGeoRepository` 等 | `sqlcore.Store` 对应 getter | 定位、报警与历史 |
| `CommandRepository` | `memory.NewCommandRepository` | `sqlcore.Store.CommandRepository()` | outbox 命令 |
| `SessionDirectoryRepository`、`GatewayNodeRepository` | `memory.NewSessionDirectoryRepository` 等 | `sqlcore.Store` 对应 getter | 集群会话与网关注册 |
| `MediaNodeRepository`、`MediaFileRepository`、`MediaMetadataRepository`、`MediaSearchRepository` | `memory.New...` | `sqlcore.Store` 对应 getter | 媒体节点与文件 |
| `RecordingSegmentRepository` | `memory.NewRecordingSegmentRepository` | `sqlcore.Store.RecordingSegmentRepository()` | 录像切片索引 |
| `AttachmentRepository`、`AttachmentNodeRepository`、`AttachmentFTPTaskRepository` | `memory.New...` | `sqlcore.Store` 对应 getter | 附件任务、节点与 FTP |
| `IndustryRepository`、`RegionRepository`、`UpgradeResultRepository` | `memory.New...` | `sqlcore.Store` 对应 getter | 行业扩展、行政区划与升级结果 |

`memory` 与 `sqlcore` 是两套平行实现；`sqlite`、`mysql` 只提供方言语句、DDL、连接池与迁移计划，通过嵌入 `sqlcore.Store` 复用控制流。

跨角色契约与实现的对应关系：

| 契约 | 实现 | 使用方 |
|---|---|---|
| `contracts.UplinkSink` | `uplink.HTTPSink`（集群）、`control.LocalUplinkSink`（单机） | 协议内核投递上行事件 |
| `contracts.Downlink` | `control` 的 outbox（`durable`/`memory`）或直连实现 | 业务 API |
| `contracts.AuthProvider` | `control` 的本地/远程鉴权代理 | 网关注册鉴权 |
| `contracts/media.MediaAllocator` | `control.RemoteMedia`、`control.LocalMedia` | 业务 API、自动上传 |
| `contracts/attachment.AttachmentAllocator` | `control.RemoteAttachment`、`control.LocalAttachment` | 业务 API、自动上传 |
| `playback.PlaybackLibrary` | `media.Library` | 业务 API、control 停机释放 |
| `storage.BlobStore` | `storage.SharedVolume`、`storage.ObjectStore` | 媒体、附件、保留期清理 |

## 4. 元数据存储与迁移

- 分层约定：`repository` 只放契约；`memory`、`sqlcore` 是两套实现；`sqlite`/`mysql` 只提供方言语句、DDL 与连接；`database` 负责装配与聚合；`sqlbackend` 负责驱动注册。
- 迁移文件名格式为 `v<MAJOR>.<MINOR>.<PATCH>.sql`，两套目录版本号必须同步推进。
- 每个迁移文件只写本后端的方言：SQLite 使用 `TEXT`/`BLOB` 与 `ON CONFLICT`，MySQL 使用显式 `ENGINE=InnoDB`/`utf8mb4` 与方言类型。
- 版本按三段数值比较（`1.10.0` > `1.9.0`），不要用字符串排序。
- 新增表或字段时，同步检查仓储接口、memory 实现、sqlcore 实现与 `Repositories` 装配，避免只改一端。
- 主服务启动只校验 schema 版本，不执行 DDL；迁移由 `cmd/jt808-migrate` 独立执行。改迁移后请本地跑一次升级路径。
- `sqlbackend` 只被 control、standalone、migrate 导入；gateway、media、attachment 不链接数据库驱动，也不应新增数据库依赖。

## 5. 新增仓储

显式装配是有意为之：新增一个仓储必须让编译器提示所有装配点，不要用反射或自动注册消除这些步骤。

1. `internal/repository/<domain>_repository.go`：定义接口与读写 DTO/错误；接口方法要说明读写所有权与快照语义。
2. `internal/repository/memory/<domain>_repository.go`：实现内存版本，作为测试与 `memory` 后端。
3. `internal/repository/sqlcore/<domain>_repository.go`：实现方言无关 SQL，并在 `sqlcore.Store` 上暴露 getter。
4. 需要新 SQL/表时：补 `sqlite/statements.go`、`mysql/statements.go` 与两端 `migrations/v<MAJOR>.<MINOR>.<PATCH>.sql`。
5. `internal/repository/repositories.go`、`memory/repositories.go`、`database.NewSQLRepositories`：补字段与装配。
6. 消费方注入：`business.Options.Repositories`、`api.Run` 的 `repository.Repositories` 或 `protocol.ProcessorDeps.Repos`。
7. 测试至少覆盖 memory 与一条 SQL 后端（`database_test.go` 或 `sqlite`/`mysql` 包内）。

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

## 6. 新增数据库后端

1. 在 `internal/repository/sqlcore/` 复用现有 `Statements` 接口；方言差异放到新后端包的 `statements.go`。
2. 新建 `internal/repository/<backend>/`：实现 `Open`、`Migrate`、连接池与 `migrations/`，返回嵌入 `sqlcore.Store` 的 `Backend`。
3. 在 `internal/repository/database/sqlbackend/` 注册 `open` 与 `migrate` 两个函数。
4. 需要该后端的入口二进制显式 blank import `sqlbackend`；数据面二进制不要导入。
5. 同步 `configs/default.yaml` 的后端枚举、`docs/configuration.md` 与 `docs/installation.md` 的部署说明，并补迁移语句。
6. 用 `go list -deps ./cmd/jt-signal | grep -E 'sqlite|mysql'` 确认数据面没有被带进驱动。

## 7. 新增业务模块（管控侧）

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

## 8. 新增协议消息

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

## 9. 集群模式开发

- 五个角色二进制分别是 `jt-server-standalone`、`jt-signal`、`jt-media`、`jt-control`、`jt-attachment`，固定角色与样例配置见 `configs/cluster/`。
- 集群改动至少验证两类形态：
  - Standalone：不配置 `cluster` 段时行为、端口与外部依赖不变；
  - Cluster：按 `configs/cluster/` 的样例启动相关角色，确认启动校验、探针与关键接口。
- 集群内部 HTTP 契约、会话目录、节点注册与下行路由的语义以[docs/architecture.md](architecture.md) 为准；改契约时同步更新文档与配置示例。
- 集群元数据面只允许 control 连数据库；gateway、media、attachment 不应新增数据库依赖（构建上也不会链接驱动，见第 1 节）。
