# 存储抽象与多后端持久化设计（Storage Abstraction & Multi-Backend Persistence）

- 状态：**方案已定稿待实施**（关键选型见 §4，评审决策见 §18 D1–D5；下一步从 M1 开始）
- 创建日期：2026-10-31
- 代码基线：分支 `p0-region-route-typed`，HEAD `ac380a5`
- 关联路线图：[JT808-project-roadmap.md](JT808-project-roadmap.md) 的 **P2.1（持久化抽象与耐用存储）**
- 适用范围：JT/T 808 接入网关的数据存储、会话状态、单机与分布式部署形态

> 本文约定：用「耐用（durable）」描述进程/节点重启后仍需保留的数据；用「易失（volatile）」
> 描述绑定在活连接或单进程上、重启即重建的数据。二者是不同的工程目标，不混用「缓存」一词。

---

## 1. 背景与目标

### 1.1 背景

当前 `internal/repository` 下的所有存储单元都是「**包级单例 + 裸 map + mutex**」：
设备、参数、定位环、报警、区域目录、行业上报、会话等全部在内存，重启即丢，也无法替换后端。
业务层（`internal/protocol`、`internal/api`、`internal/server`）直接调用 `storage.GetXxxCache()`，
与内存实现强耦合（全仓约 160 处直接调用）。

这些类型目前统一以 `*Cache` / `GetXxxCache()` 命名，但「cache（缓存）」一词有误导：它们并非
「某份权威数据的可选副本」，而是业务访问数据的**唯一入口**，内存 map 只是其一种后端实现。
接口化时一并正名为 Repository（B 耐用数据）/ Registry（A 连接现场）/ HotView（C 派生最新值），
不再保留 `Cache` 这一层命名（映射见 §6.4）。

随着项目从「协议自测模拟器」走向「单机生产」和「分布式生产」，需要一套可按部署形态切换后端、
且业务代码不返工的存储架构。

### 1.2 目标

1. 业务层只依赖**仓储（Repository）接口**，对存储后端无感知。
2. 一套代码支持三种部署场景（§4）：
   - 开发测试：纯内存；
   - 单机生产：内存 + 本地耐用存储；
   - 分布式生产：内存 + Redis（协调/在线）+ 网络数据库（耐用）。
3. 明确每份数据的生命周期：哪些只能在进程内、哪些必须耐用、哪些是可重建的投影。
4. 协议主流程不被存储故障阻断；内存占用在持续负载下有界；保留期可配置。
5. 分阶段落地，每一阶段都保持 `go build ./... && go test -race ./...` 全绿。

### 1.3 非目标（本期不做）

- 不做 TCP/UDP 连接的跨节点热迁移（fd 无法迁移，统一走「重连重建」，见 §10）。
- 不做 JT/T 1078 流媒体 RTP 层；媒体大 payload 不入库，只存元数据，字节走文件/对象存储。
- 不在第一阶段引入分布式锁、多写者、跨区域多活。
- 不引入重型 ORM。

---

## 2. 现状与问题

| 现状 | 问题 |
|---|---|
| 9 类缓存均为包级单例 + map | 无接口、无法替换后端、无法在测试中注入替身 |
| `model.Device` 同时持有身份字段与 `Conn net.Conn`/`SessionID`/在线态/`LastestComTime` | 可持久化的主数据与不可序列化的运行态混在一个结构里 |
| 定位用 RingBuffer（100/设备）、报警用切片（1000/设备） | 有内存上界但重启全丢，且无时间维度保留 |
| Session 内含 `Conn`、流水号、`pending` 在途重传表、goroutine | 纯执行现场，无法也不应持久化 |
| 参数/属性/区域查询用「注册命令 + chan 等待应答」的内存 broker | 只能在单进程内关联应答，跨节点下行拿不到应答 |
| 160+ 处直接调包函数 | 一次性大改成 DI 风险高，需要门面过渡 |

---

## 3. 数据三分类（A / B / C）

对每份数据依次问三个问题：

1. 是否持有句柄/协程/通道，或只描述「这条活传输」？→ **A 执行现场**
2. 是否为平台侧权威态，无法靠「等设备下次上报」重建，发布/崩溃后必须还在？→ **B 耐用态**
3. 是否仅为上行数据的「最新值」投影，可被下一条上报覆盖重建？→ **C 派生投影**

### 3.1 逐存储单元归类（对照现有文件）

| 现有存储单元（文件） | 内容 | 分类 | 目标承载 |
|---|---|---|---|
| `SessionCache`（session_cache.go）、`Device.Conn/SessionID/在线态` | 连接句柄、流水号、写锁、pending、重传协程 | **A** | 进程内存，永不共享/持久化 |
| `segment_cache.go` / `sent_segment_cache.go`、媒体重传调度器（internal/protocol） | 分片重组、在途重传 | **A** | 进程内存 |
| `*_response.go`（param/property/region/location broker 的 chan） | 在途查询应答 rendezvous | **A**（本地 chan） | 本地 chan + §9 跨节点应答关联 |
| `location_tracking.go` 的 chan/timer | 通知通道、本地定时器 | **A** | 进程内存 |
| `IndustryCache.recording` 录音标志等模拟器运行态 | 模拟器内存开关 | **A** | 进程内存（生产服务端无模拟器） |
| `DeviceCache` 的**身份部分**（id/plate/phone/IMEI/authcode/版本等） | 注册主数据 | **B** | 耐用库（设备表） |
| `DeviceParamsCache`（param_cache.go，0x8103/0104） | 终端参数权威配置 | **B** | 耐用库（write-through） |
| 终端属性（0x0107/8107 取值，非 broker 部分） | 终端上报的属性档案 | **B** | 耐用库 |
| `RegionCatalog`（circle/rectangle/polygon/route） | 平台下发的区域/路线权威目录 | **B** | 耐用库（清空+重设需事务） |
| `IndustryCache` 的下行配置：事件目录(8301)、信息菜单(8303)、电话本(8401)、车辆控制标志、升级命令(8108)、透传下行(8900) | 平台下发配置/最近命令 | **B**（配置部分） | 耐用库；在途命令走命令表/总线 |
| `AlarmCache.events` 报警事件 + `Confirmed` 确认位 | 业务/监管记录，跨节点读与人工确认 | **B** | 耐用库（时序表，幂等去重） |
| `GeoCache` RingBuffer 所代表的定位历史 | 高频时序记录 | **B**（历史） | 耐用库（批量写 + 保留期）；Ring 降级为本地 L1 |
| `IndustryCache`：0x0701 运单、0x0801/0x1205 媒体元数据 | 上报即业务凭证 | **B**（元数据，耐用化列入后续独立任务，见 §18-D3；M1/M2 暂留 HotView 最新值） | 耐用库；媒体字节走文件/对象存储 |
| `AlarmCache.commands`（在途人工确认命令关联） | 下行命令与上行应答的短期关联 | **协调态** | Redis（短 TTL）+ 命令表（耐用/幂等） |
| `location_tracking.go` 的临时跟踪参数（含有效期） | 带有效期的下行参数 | **协调态** | Redis（TTL=有效期） |
| owner 归属、在线 presence、跨节点 latest | 路由与在线视图 | **协调态** | Redis（全 TTL） |
| `IndustryCache` 的上行快照：0702 司机/0705 CAN/0700 记录仪/0800 媒体事件/0302/0303/8304/8400/0108/0900/0901 等 | 只留最新值，可被下次上报覆盖 | **C** | 本地 L1 热缓存；**首版不做跨节点 Redis 镜像**（见 §18-D2，将来需要随时加） |
| 定位 latest（RingBuffer.Latest） | 最新位置热读 | **C** | 本地 L1（+ Redis latest 供跨节点详情） |

要点：

- **A**：永远本地，故障即弃。
- **B**：必须耐用，是数据库里的权威记录；单机进 SQLite，分布式进 MySQL。
- **C**：本地内存即可，只在「非 owner 节点的 API 需要读」时把一份投影放 Redis。
- **协调态**：只存在于分布式形态，用 Redis 且**全部带 TTL**，绝不当长期数据。

### 3.2 Device 结构拆分

将现有 `model.Device` 拆为两个投影：

```
DeviceRecord（B，可持久化）          DeviceRuntime（A，仅内存）
  phone(主键) / plate / id             Session / Conn
  imei / authcode / 版本字段            在线状态 / LastestComTime
  注册的静态属性                         传输协议 / remoteAddr
```

上线时由 owner 把 `DeviceRecord`（库）与 `DeviceRuntime`（本地连接覆盖层）合并成业务使用的视图；
持久化只写 `DeviceRecord` 与一份节流的「最后在线」运行态（§11.3）。

---

## 4. 三种部署场景与组件矩阵（已确认）

| 层 | 场景1 开发测试 | 场景2 单机生产 | 场景3 分布式生产 |
|---|---|---|---|
| 热/易失层（A + C 本地 L1） | 进程内存 | 进程内存 | 各节点进程内存（不共享） |
| 协调/在线层（owner 租约、presence、命令总线、跨节点 latest） | 不需要 | 不需要 | **Redis**（全 TTL，不存长期数据） |
| 耐用持久层（B） | 内存仓储（同一套接口） | **SQLite**（本地文件） | **MySQL**（网络库） |

### 4.1 已确认的选型（Decision: confirmed）

1. **持久层统一关系型**：单机 **SQLite**、分布式 **MySQL**，二者均走标准库 `database/sql`，
   共用同一套表结构与仓储 SQL，方言差异收敛到一个薄 `Dialect`。放弃 bbolt
   （bbolt 仅在「终态也是 KV」时占优；终态是 MySQL 时会导致仓储写两套，详见 §13）。
2. **DAL 手写 SQL + `database/sql`**，不引入 ORM；SQL 集中在仓储层，配小 `Dialect` 与 scan helper。
3. **迁移**：`go:embed` 内嵌 `.sql` + 极简版本化迁移器（DDL 与 Store 解耦，启动只校验版本，
   迁移由独立工具 `jt808-migrate` 执行，见 §7.2）。
4. **Go 工具链升级到当前最新稳定版**（Go 官方无 LTS 概念，实施时以当时 latest stable 为准），
   以满足纯 Go SQLite 驱动与 go-redis 的最低版本要求；预期业务代码零改动。

### 4.2 依赖（按场景启用，编译期/配置期隔离）

| 用途 | 组件 | 说明 |
|---|---|---|
| SQLite 驱动 | `modernc.org/sqlite` | 纯 Go、免 CGO，便于交叉编译到车载 ARM |
| MySQL 驱动 | `github.com/go-sql-driver/mysql` | 事实标准 |
| Redis 客户端 | `github.com/redis/go-redis/v9` | 仅场景3 |
| SQL 接口 | 标准库 `database/sql` | 不引 ORM |
| 迁移 | `go:embed` SQL + 内置轻量迁移器 | 幂等、版本化 |

---

## 5. 总体分层架构

```
        msg_processor / simulator / api / server        （业务层，只依赖仓储与协调接口）
                               │
        ┌──────────────────────▼───────────────────────┐
        │  Repository 领域仓储接口（业务契约，只定义一次） │
        │  Device / Params / Region / Location / Alarm  │
        │  Outbox(下行命令) / IndustryConfig / MediaMeta │
        └───────┬───────────────────────────┬───────────┘
                │                           │
   耐用实现（B）│                           │协调接口（仅场景3）
   ┌────────────▼───────────┐    ┌──────────▼──────────────┐
   │ SQL 仓储（共用一套 SQL） │    │ Coordinator             │
   │  + Dialect(SQLite/MySQL)│    │  OwnerLease / Presence  │
   │  + 迁移 / 保留期         │    │  CommandBus / ReplyRoute│
   └───────┬───────┬────────┘    └──────────┬──────────────┘
        SQLite   MySQL                    Redis(TTL)
                │
   内存实现：MemoryRepos（场景1） ；场景2/3 在 SQL 前叠加进程内 L1 热缓存与批量写
   A 执行现场：始终进程内存（Session/Conn/pending/segment/chan）
```

- 业务层面向接口编程；M1 即将现有 `GetXxxCache()`/`*Cache` **正名并收敛为接口**
  （Repository/Registry/HotView，见 §6.4），内部实现可暂时仍是内存 map，调用方改为依赖接口；
  随后再逐步从包级单例过渡到构造函数注入（DI）。
- 三种场景 = 选择不同的仓储实现组合 + 是否装配 Coordinator，业务代码不变。

---

## 6. 核心接口设计

### 6.1 仓储接口（业务契约，示意；方法签名在 M1 细化）

```go
// 设备主数据（B）
type DeviceRepository interface {
    GetByPhone(ctx context.Context, phone string) (*DeviceRecord, error) // ErrNotFound
    GetByPlate(ctx context.Context, plate string) (*DeviceRecord, error)
    Upsert(ctx context.Context, d *DeviceRecord) error
    Delete(ctx context.Context, phone string) error
    List(ctx context.Context, q DeviceQuery) ([]*DeviceRecord, int64, error) // 分页/筛选
}

// 终端参数 / 区域目录（B，配置类 write-through）
type DeviceParamsRepository interface {
    Get(ctx context.Context, phone string) (*model.DeviceParams, error)
    Replace(ctx context.Context, phone string, p *model.DeviceParams) error
    Delete(ctx context.Context, phone string) error
}
type RegionRepository interface {
    ReplaceAll(ctx context.Context, phone string, family RegionFamily, items []RegionItem) error // 单事务：清空+写入
    Upsert(ctx context.Context, phone string, family RegionFamily, id uint32, v RegionItem) error
    Delete(ctx context.Context, phone string, family RegionFamily, ids []uint32) error // 空 ids=删全部
    Query(ctx context.Context, phone string, family RegionFamily, ids []uint32) ([]RegionItem, error)
}

// 时序类（B，批量写 + 保留期）
type LocationRepository interface {
    AppendBatch(ctx context.Context, batch []LocationRecord) error
    Latest(ctx context.Context, phone string) (*LocationRecord, error)
    List(ctx context.Context, phone string, q TimeRangeQuery) ([]LocationRecord, error)
    Trim(ctx context.Context, p RetentionPolicy) error
}
type AlarmRepository interface {
    AppendBatch(ctx context.Context, events []AlarmEvent) error          // 幂等：(phone, serial)
    List(ctx context.Context, phone string, q TimeRangeQuery) ([]AlarmEvent, error)
    MarkConfirmed(ctx context.Context, phone string, serials []uint16) error
    Trim(ctx context.Context, p RetentionPolicy) error
}

// 下行命令（B：崩溃可重投）
type CommandOutboxRepository interface {
    Enqueue(ctx context.Context, cmd *DownlinkCommand) error             // 幂等键 commandId
    MarkState(ctx context.Context, commandId string, state CommandState) error
    ListPending(ctx context.Context, owner string) ([]*DownlinkCommand, error)
}
```

### 6.2 事务与方言

```go
type UnitOfWork interface { // 区域「清空+重设」、报警确认等多步操作必须原子
    Do(ctx context.Context, fn func(tx Tx) error) error
}
type Dialect interface {
    DriverName() string                 // "sqlite" | "mysql"
    Placeholder(n int) string           // 两者当前都用 ?，预留
    Upsert(table string, cols []string, conflictCols []string) string
    NowExpr() string                    // CURRENT_TIMESTAMP / NOW(3)
    ParamLimit() int                    // 单条 SQL 占位符上限（分批）
}
```

### 6.3 协调接口（仅场景3 装配；单机给 no-op 实现）

```go
type Coordinator interface {
    OwnerLease      // Claim/Renew/Release owner；fencing token CAS
    Presence        // 在线 Hash 上报（TTL 续期）/读取
    CommandBus      // 按 owner 发布下行命令、订阅本节点通道
    ReplyRouter     // 按 correlationId 发布/等待设备应答
    LatestCache     // 跨节点 latest 投影（首版仅定位 latest，短 TTL）
}
```

> `LatestCache` 首版只覆盖**定位 latest**（车辆详情跨节点实时位置）。行业上报快照
> （0702/0705/0700/0900 等）首版不做跨节点镜像（§18-D2），将来有跨节点 API 需求时按同一接口增量扩展。

### 6.4 命名规范与现有 `*Cache` 的正名映射

命名约定：

- **`XxxRepository`**：访问 **B 类耐用数据** 的权威入口（有内存/SQL 多实现，可持久化）。
- **`XxxRegistry`**：**A 类执行现场**（连接、分片等），仅进程内存，无耐用实现，明确表达「不可持久化」。
- **`XxxHotView`**：**C 类派生最新值**的进程内热读视图（可叠加 Redis 跨节点投影）。
- 协调能力统一叫 **`Coordinator`**（owner/presence/bus/reply），不叫 cache。
- 即：`cache` 一词只保留给「真正可丢弃的副本/前置缓存」（如 SQL 之前的 L1 内部组件），
  不再作为业务存储类型名。

现有存储单元 → 新接口/类型映射：

| 现有（文件） | 现状方法举例 | 正名后 | 分类 |
|---|---|---|---|
| `DeviceCache`（device_cache.go）身份部分 | `GetDeviceByPhone` / `CacheDevice` / `ListDevice` / `DelDeviceByPhone` | **`DeviceRepository`**：`GetByPhone` / `Upsert` / `List` / `Delete` | B |
| `DeviceCache` 中的 `Conn/SessionID/在线态/LastComTime` | 随设备对象混存 | **`ConnectionRegistry`**（A，内存）+ `device_runtime` 表（节流耐用，见 §11.3） | A/B |
| `DeviceParamsCache`（param_cache.go） | `GetDeviceParamsByPhone` / `CacheDeviceParams` / `DelDeviceParamsByPhone` | **`DeviceParamsRepository`**：`Get` / `Replace` / `Delete` | B |
| `RegionCatalog`（region_catalog.go） | `ApplySet` / 删除 / 查询 | **`RegionRepository`**：`ReplaceAll` / `Upsert` / `Delete` / `Query` | B |
| `AlarmCache.events`（alarm_cache.go） | `Record` / `List` / `ConfirmByCommand` | **`AlarmRepository`**：`AppendBatch` / `List` / `MarkConfirmed`（+`Trim`） | B |
| `AlarmCache.commands`（在途确认关联） | `RegisterConfirmation` / `ConfirmByCommand` | **`Coordinator`（确认命令路由）** + `command_outbox`；本地关联并入 `ReplyRouter` | 协调/A |
| `GeoCache` RingBuffer（geo_cache.go） | `GetGeoRingByPhone` / `GetGeoLatestByPhone` | 历史→**`LocationRepository`**（`AppendBatch/Latest/List/Trim`）；热读→**`LocationHotView`**（RingBuffer 作为 L1） | B+C |
| `SessionCache`（session_cache.go） | `GetSession` / `StoreSession` / `ClearSession` | **`ConnectionRegistry`**：`Get/Bind/Unbind`（仅内存；含 owner 上报钩子） | A |
| `segment_cache.go` / `sent_segment_cache.go` | 分片重组/在途重传 | **`FragmentAssembler` / `RetransmitTracker`**（协议内部组件，非存储入口，仅内存） | A |
| `*_response.go`（param/property/region/location broker） | chan 等待应答 | owner 内保留为 **`PendingReply`（本地 chan）**；跨节点外包 **`Coordinator.ReplyRouter`** | A+协调 |
| `location_tracking.go` | `Set` / `Changed` / `Interval` | 本地定时器/chan→**`TrackingManager`（A）**；带有效期参数→**Redis TTL（协调）** | A+协调 |
| `IndustryCache`（industry_cache.go） | 一个大 struct 持有 20+ map、`Record` 大 switch | **按语义拆分**（见下），不再保留「万能 cache」 | B/C/A |

`IndustryCache` 的拆分去向：

- 下行配置类：事件目录 / 信息菜单 / 电话本 → **`TerminalCatalogRepository`**（B，对应表 `terminal_catalogs`）；
- 业务凭证：运单（0701）、媒体元数据（0801/1205）→ **`WaybillRepository` / `MediaMetadataRepository`**（B，**后续独立任务**，M1/M2 暂归入 HotView 只留最新值）；
- 上行最新值快照（0702/0705/0700/0800/0302/0303/8304/8400/0108/0900/0901 等）→ **`IndustrySnapshotHotView`**（C，本地 L1；**首版不镜像 Redis**，需要跨节点 API 时再增量添加）；
- 录音标志等模拟器开关 → **模拟器内存态**（A，不进服务端存储抽象）；
- 车辆控制标志/升级命令/透传下行等「最近一次下行」→ 命令走 `command_outbox`；结果快照归 HotView。

方法命名通用规则：`CacheXxx/GetXxxByPhone` → `Upsert/GetByPhone`；`Del` → `Delete`；
`Record(追加)` → `Append`/`AppendBatch`；查询统一接收 `ctx context.Context` 与 query/option 参数；
返回值不暴露内部可变指针（保持现有深拷贝语义，防止调用方改脏库内状态）。

装配与访问方式：

- 启动时由 `repository.NewStore(cfg)`（Registry）构建各 Repository/Registry/HotView/Coordinator；
- 业务层通过**构造函数注入**接口；迁移期可提供一个返回接口（而非具体 `*Cache`）的访问器过渡，
  但名称同步改为接口名，`GetXxxCache` 与 `*Cache` 类型在 M1 完成后不再保留。

> 实施状态（M2 与包结构分层已完成，见 §19.3 / §19.4）：`GetXxxCache` 与 `*Cache` 旧名已全部删除，
> 调用点统一迁移到接口方法命名。落地分层为 **`repository`（持久化契约 + 内存实现）/
> `runtime`（A 类执行现场与本地 rendezvous）/ `storage`（仅装配与后端选择）** 三包：
> `repository.go` 只保留持久化接口与错误值，A 类接口在 `runtime/runtime.go`，
> `Stores` 聚合、`NewStores()`/`Default()` 在 `internal/storage`。上表"现有（文件）"列保留为历史对照。

---

## 7. 关系模型与迁移

### 7.1 表（首版范围）

| 表 | 用途 | 关键约束/索引 |
|---|---|---|
| `devices` | 设备静态主数据（B，来源 0x0100/0x0102 主动上报） | `phone` PK；`plate` 仅普通索引（不加唯一，见 §7.4）；拆分后 10 列，不含运行时投影 |
| `device_runtime` | 设备运行时投影（B，与设备写入同事务更新） | `phone` PK，`session_id`,`trans_proto`,`last_com_time_ms`,`status`,`updated_at`；**不在启动时批量重置**：单机由优雅停机清空，异常重启的残留由读路径 TTL 判定收敛为离线（见 §7.4） |
| `device_params` | 终端参数（B） | `phone` PK，参数集合**整体 JSON 单字段**（已定，见 §18-D1；将来需要按参数 ID 服务端查询时再演进为 EAV） |
| `terminal_properties` | 终端属性档案（B，来源 0x0107 查询应答） | `phone` PK，常用属性列化 + `extra_json` 长尾兜底；与 `devices` 不合并（见 §7.4） |
| `region_items` | 区域/路线（B） | 唯一键 `(phone, family, region_id)`，payload JSON，`updated_at`；**本期非交付项** |
| `terminal_catalogs` | 事件/信息菜单/电话本等下发目录（B） | 唯一键 `(phone, kind, item_id)`，payload JSON；**本期非交付项** |
| `locations` | 定位历史（B，时序） | 索引 `(phone, ts)`；批量插入；按 `ts` 保留期删除；**本期明确延后（§7.5）** |
| `alarms` | 报警历史与确认（B，时序） | 唯一键 `(phone, message_serial)` 去重；`confirmed`；索引 `(phone, ts)`；**本期明确延后（§7.5）** |
| `command_outbox` | 下行命令（B，幂等可重投） | `command_id` PK，`phone`,`owner`,`state`,`idempotency_key`,`created_at`；**本期非交付项** |
| `media_metadata` / `waybills` | 媒体/运单元数据（B，**后续独立任务**，不在 M1/M2；见 §18-D3） | 媒体字节外置；元数据按 phone/时间索引 |

大 payload（0x0801 媒体、0x8900/0901 透传压缩字节）**不入关系库**，落文件/对象存储，库里存 URI 与元数据。

### 7.2 迁移

- 目录（`go:embed *.sql` 编译进二进制）：
  - `bootstrap_schema_migrations.sql`：迁移元数据表建表脚本（引导脚本，不参与版本编号）；
  - `init_20261101.sql`：当前唯一版本。开发阶段不保留增量迁移，直接给出最终 schema。
- **版本号取自文件名末尾日期**：`<描述>_<YYYYMMDD>.sql`，迁移按日期升序执行；
  后续变更追加新文件（如 `add_xxx_20261201.sql`），无需人工维护递增序号。
- **`schema_migrations` 的 DDL 也在 SQL 文件里**，不在 Go 常量中。它必须先于版本存在
  （否则无处记录已应用版本），因此单独作为引导脚本；迁移工具在执行任何版本前先执行它，
  失败会作为普通迁移错误返回。MySQL 与 SQLite 都需要该表，二者共用同一份脚本。
- **独立迁移工具**：`jt808-migrate -c <config>`，源码位于 `cmd/jt808-migrate/`，与主服务
  **分开编译、分开发布**（`make compile-migrate` / `make release-migrate`）。迁移工具只做连接、
  执行迁移、打印 `applied` 数量，不启动 TCP/UDP/API，也不初始化主服务的运行时。
  MySQL 场景 DBA 可直接执行：先 `bootstrap_schema_migrations.sql`，再按文件名顺序执行版本文件。
  除索引创建外语句均为幂等形式（`CREATE TABLE IF NOT EXISTS`）；`init_20261101.sql` 末尾的
  `CREATE INDEX` 在 MySQL 上没有 `IF NOT EXISTS`，重复手工执行会报"索引已存在"，属预期，
  执行前请先备份并核对 `schema_migrations`。
- **所有后端一致，启动只校验不执行**：SQLite 与 MySQL 都不在服务启动时执行任何 DDL。
  读取 `MAX(version)` 与期望版本比较：低于期望 → 报错退出并提示执行 `jt808-migrate`；
  高于期望 → 报错（库版本高于程序版本），拒绝启动。入口代码为 `storage.Migrate` /
  `sqlstore.Migrate`，仅被迁移工具调用。
- 执行健壮性：逐版本执行，全部语句成功后才写入 `schema_migrations`；失败时错误信息带版本号与
  失败语句，已成功版本不会重复应用。
- **客观限制（MySQL）**：DDL 会隐式提交，无法与"插入版本记录"同事务。若一个版本内多条 DDL
  中途失败，可能留下"部分表已建、版本未记录"的中间态；应对为重新执行迁移（幂等 DDL 会补齐）
  或由 DBA 人工核对。SQLite 的 DDL 本可事务化，但为共用同一执行路径，本轮同样按"先 DDL、后记录"。

### 7.3 保留期与容量（有界）

> 以下保留期为设计草案；`locations` / `alarms` 本期**明确延后**（§7.5），未执行任何 Trim。

```
locations: 按时间保留（默认 168h）且每设备上限（默认 10000 条）
alarms:    按时间保留（默认 720h）且每设备上限（默认 5000 条）
```

- 定时（如每 10 分钟）+ 写入时惰性触发 `Trim`；删除走时间索引，分批 `DELETE ... LIMIT`，避免大事务。
- 内存 L1 维持现有 RingBuffer/切片上界，作为热读与抗抖动层，不再是唯一副本。

### 7.4 本期实现与关系模型的差异（2026-11-01）

本轮 SQL 后端覆盖范围扩展为 `DeviceRepository`（含 `device_runtime`）、`DeviceParamsRepository`、
`TerminalPropertiesRepository`；其余 B 类仓储仍为内存实现。据此与 §7.1 的差异如下：

| 项 | §7.1 设计 | 本轮实现 | 原因/建议 |
|---|---|---|---|
| 设备运行时投影 | 拆 `devices` + `device_runtime` 两表，`last_com_time` 节流写 | 拆为：`devices` 保留 10 列静态/身份字段，`device_runtime` 承载 `session_id`/`trans_proto`/`last_com_time_ms`/`status`/`updated_at`。读用 `LEFT JOIN` 合并回完整 `model.Device`，写在同一事务内按归属分写两表 | `keepalive_ms` 是代码常量（`model.NewDevice` 固定 1 分钟，`device.go:50`），不属运行时观测值，留在 `devices`。**重启语义**：不做启动批量重置——单机由优雅停机（SIGINT/SIGTERM）调用 `ResetRuntime` 清空；异常重启残留的 `session_id`/`status` 在读路径按 `last_com_time_ms + keepalive_ms` 的 TTL 判定收敛为 offline 并清空 `session_id`，`last_com_time_ms` 保留作"上次在线时间"。分布式形态以 Redis 运行时缓存的全 TTL + 过期扫描回写替代单机重置。**心跳落库频率可配**：`server.heartbeatPersistIntervalMs`（缺省 `0`，逐条写库；若开启，应小于保活周期，当前保活 1 分钟故建议 ≤30000），被节流时保活判定取进程内 `runtime.Heartbeat` 的实时值（§11.3） |
| `terminal_properties` | `phone` PK，属性 JSON | 新增：`phone` PK + 常用属性列（终端类型/制造商/型号/终端 ID/ICCID/硬件版本/固件版本/GNSS 与通信能力）+ `extra_json` 长尾 | 只由 0x0107 查询应答写入，与 `devices` **不合并**：`devices` 静态字段来自 0x0100/0x0102，属性应答若回写 `devices` 会触发其非主键列全量重写。0x0107 处理为"持久化 + 内存最新值 + broker 投递"三者同时 |
| `plate` 唯一性 | `plate` UNIQUE | 未加唯一约束，仅加**非唯一**普通索引 `idx_devices_plate`（见 `init_20261101.sql`） | 内存实现按 `plate` 覆盖映射、允许空车牌重复，加 UNIQUE 会改变既有行为（多个空车牌或同牌设备会写入失败）。设备身份以协议里的终端手机号（`phone`）为准，`plate` 仅用于注册时"车辆已注册"校验；保持非唯一 |
| `devices` 索引 | 未明确 | `phone` 主键 + `idx_devices_plate`（非唯一，见 `init_20261101.sql`） | `GetByPlate` 由全表扫描改为索引查找；不加唯一约束，理由见上一行的 `plate` 唯一性 |
| `device_params` | 参数集合整体 JSON 单字段 | 一致（`params_json` TEXT） | 遵循 D1；`ParamValue` 的 JSON 数字会以 `float64` 回读，既有 `any2uintX` 编码逻辑已兼容 |
| `locations` / `alarms` | B 类持久化 | 未落地（**明确延后**） | 理由见 §7.5；不是"不需要持久化" |
| `region_items` / `terminal_catalogs` / `command_outbox` | B 类持久化 | 未落地（本阶段非交付项） | 区域需要事务化 `ReplaceAll`，目录与 outbox 需要幂等/重投语义，均未在本轮定义；媒体/运单按 D3 后置 |
| `schema_migrations` | 版本化迁移器 | DDL 位于 `internal/repository/migration/*.sql`（`go:embed`），启动只校验版本，迁移由独立工具 `jt808-migrate` 执行 | 详见 §7.2；MySQL 的 DDL 隐式提交限制已在该节说明 |
| 主键类型 | 未指定 | `VARCHAR` 主键 + `BIGINT` 毫秒时间戳 | 同时兼容 SQLite/MySQL；时间用毫秒整数避免时区与解析差异 |

### 7.5 明确延后的持久化决策（2026-11-01 确认）

- **`locations`（定位历史）延后。** ①写量与保留期策略未定（§7.3 的 168h / 每设备 10000 条仍为草案）；
  ②按 §8 需批量写通道（有界 buffer + 1s/500 条刷库）与优雅退出 flush，尚未实现，直接落库会变成
  每条 0x0200 同步写；③最新定位查询已由内存 C 类 `GeoRepository.Latest` 满足，历史查询无调用方。
  **结论：值得持久化，本轮不做，不写成"不需要"。**
- **`alarms`（报警历史与确认）延后。** 现有 `AlarmRepository` 无 `ctx`，且含 30 分钟去重窗口、
  每设备 1000 条上限、10 分钟确认命令过期等行为；这些语义与关系库的"唯一键去重 + 过期清理归属"
  不匹配，需先定口径（去重键、确认状态机、TTL 清理由谁执行），再落库。
  **结论：值得持久化，本轮不做，不写成"不需要"。**
- `region_items` / `terminal_catalogs` / `command_outbox` 为本阶段非交付项；
  `media_metadata` / `waybills` 按 D3 留待独立任务。

---

## 8. 内存层与写策略

| 数据类别 | 写策略 | 读路径 |
|---|---|---|
| 配置/主数据（设备、参数、属性、区域、目录） | **write-through**：先改内存 L1，再同步写库；库失败按 §12 降级 | 先读 L1，未命中回源库并回填 |
| 定位/报警历史（高频追加） | **有界 buffer + 定时/定量批量刷库**（默认 1s 或 500 条），非每条同步写 | latest/热窗口读内存 L1；历史查询读库 |
| 下行命令 | 先入 `command_outbox`（耐用、幂等），再经 owner/总线投递 | owner 重连或启动时扫描 pending 重投 |
| C 派生快照 | 仅写本地 L1；定位 latest 首版可镜像 Redis，行业快照首版不镜像（§18-D2） | 本地直读；关 L1 时 latest 回源库/Redis |

- 历史批量写：独立 goroutine 消费有界 channel；优雅退出时 flush（§12）。
- 背压：channel 满时按「丢弃最旧 + 计数埋点」处理，**绝不反压定位上报主流程**。
- 现有 RingBuffer（100）/报警切片（1000）保留为 L1；耐用副本在库，容量上界双保险。
- **L1 可关闭（§18-D4）**：`frontCache.enabled=false` 时不装配进程内 L1，配置类直读/直写库，
  历史 latest 读库（或 Redis latest）。单机与分布式均支持该开关，适配超低资源部署；默认开启。

---

## 9. 分布式协调（仅场景3：Redis）

> Redis 是「协调与在线」组件，**全部 key 带 TTL**，不作为长期数据源；长期数据只在 MySQL。

### 9.1 owner 租约（连接归属 + fencing）

```
owner:{phone} -> { node, proto, epoch(fencing token), remoteAddr, connectedAt }   TTL≈3×keepalive
```

- owner 每收到报文（含心跳 0x0002）续期；Lua 做 **CAS 占用**：epoch 单调递增，旧 owner 无法覆盖新 owner。
- 「占用语义」（谁能发）与「健康语义」（谁在续租）分离。
- 下行前 fencing 校验「自己仍是 owner」；租约失效后旧 owner **主动停发**，杜绝脑裂双发。
- TCP：连接期 owner 稳定；UDP：NAT 重绑定会漂移，更依赖报文活动续期与 fencing。

### 9.2 presence（在线状态投影）

```
presence:{phone} = Hash { status, node, proto, remoteAddr, sessionId,
                          connectTime, lastComTime, lastHeartbeatTime, keepaliveSec }  TTL
online:set = { phone... }   # 在线集合/基数
```

API 只读，owner 单写；key 存在即在线，TTL 过期即离线（崩溃自动收敛，无需扫描）。

### 9.3 命令总线与应答关联（替代单进程 broker 的跨节点版）

```
API 节点(任意) --查 owner--> 自己即 owner：本地直发
                          \--非 owner：
                              1) 命令先入 command_outbox（幂等，崩溃不丢）
                              2) PUBLISH node:{ownerNode}  命令(带 commandId)
owner 节点  SUB 本节点通道 -> fencing 校验 -> session.WriteFrame 本地 Conn
设备应答(0x0001/0x8001/业务应答) 到达 owner
owner -> PUBLISH reply:{commandId} ; API 侧按 correlationId 等待(带超时)
```

- 现有 `param/property/region/location_response.go` 的**本地 chan 保留**（owner 内 device↔session 关联），
  外层包一层跨节点 `ReplyRouter`。
- 命令以 `commandId` 幂等；设备/服务端重投不产生重复副作用。

---

## 10. 会话与连接的维护（Session / Conn 不持久化）

### 10.1 结论

- **Conn** 是内核 fd，绑定单机网络栈，无法序列化/迁移；
- **Session**（`Conn`、`WriteMu`、`serialNumber`、`pending`、重传 goroutine、`closed`）是活传输附属状态，
  崩溃后无意义。两者**均不持久化、不做快照恢复**。
- 持久化的是与连接无关的 **B 类数据**，以及带 TTL 的**协调投影**；下行命令入 **outbox** 保证崩溃可重投。

### 10.2 生命周期

- **稳态**：LB 一致性哈希使设备落在 owner；owner 本地持 Conn/Session，处理上行并续 owner/presence 租约。
- **优雅发布/drain**：节点先摘 LB → 停接新连接 → 等已有连接自然超时/主动关闭 → 设备被重哈希到新节点，
  重新 **0x0100 注册 → 0x0102 鉴权**，新节点据库里的设备主数据接受注册并成为新 owner。
- **节点崩溃**：fd/Session 立即消失；Redis owner/presence 在 TTL 后过期；期间下行命令留在 outbox，
  设备重连、owner 更新后重投。无需连接热迁移，正确性靠「**重连重建 + 租约过期 + 命令幂等重投**」。

### 10.3 为什么 LB 粘性不能替代共享状态

粘性解决「报文通常回到同一台」，不解决「重启不丢、别的节点能读、API 落点随机」：
发布/崩溃/扩缩容会打破粘性；UDP 经 NAT 重绑定会漂移；HTTP API 是无亲和短连接，可落任意节点。

---

## 11. 后台查询与一致性模型

### 11.1 目标语义

连接状态**不追求强一致（linearizable）**，而追求「**有界最终一致 + 显式陈旧度 + 崩溃必收敛 + 不脑裂**」。

### 11.2 车辆详情读路径（任意 API 节点）

```
GET /api/devices/{phone}
  1) MySQL 取 DeviceRecord + 参数 + 耐用 last-seen + 最后位置   （身份/历史权威）
  2) Redis 读 presence:{phone}                                  （实时连接投影）
       命中 -> 整块采用连接态（online/node/proto/addr/lastComTime...）
       未命中 -> status=offline，连接字段置空，lastComTime 取库中耐用值
  3) 分区块合并返回，每区块带各自时间戳与 staleness
```

```json
{
  "phone": "...", "plate": "...", "imei": "...",
  "connection": {
    "online": true, "status": "online", "transport": "TCP", "node": "gw-2",
    "remoteAddr": "10.0.0.9:51234", "sessionId": "10.0.0.9:51234",
    "connectTime": "2026-10-31T09:00:00Z",
    "lastComTime": "2026-10-31T09:11:52Z",
    "lastHeartbeat": "2026-10-31T09:11:52Z",
    "stalenessMs": 1200
  },
  "lastLocation": { "time": "...", "location": {}, "drive": {} }
}
```

### 11.3 耐用「最后在线」：心跳两路写

- 高频心跳：**只刷 Redis**（不打库）；
- **上线 / 离线状态翻转强制写库**；运行中每 30~60s 节流刷 `device_runtime.last_com_time`。

由此：在线读 Redis 秒级实时，离线后页面仍能显示库里的「最后在线于 …」。

> **实施现状（2026-11-01）**：
> - **默认仍逐条写库**（`server.heartbeatPersistIntervalMs: 0`），与拆分前行为一致；
> - 需要时配置若干毫秒（如 30000）即可开启节流，但必须小于保活周期（当前 1 分钟），
>   否则库内 `last_com_time` 落后会让读路径 TTL 在下次落库前短暂判离线。被节流跳过的心跳仍逐条
>   记入进程内 `runtime.Heartbeat`，保活判定（`keepalive`）取"库内 `last_com_time` 与进程内心跳的较新值"，
>   因此节流不会把在线设备误判为离线；进程重启后进程内记录清空，回落为以库值判定。
> - 上线 / 离线状态翻转仍强制写库；设备注销（0x0003）与鉴权失败清理会同时移除进程内心跳记录。
> - "心跳只刷 Redis"属分布式形态，单机不适用；分布式方案见 §11.2 与 §19.6 的 Redis 运行时缓存规划。

### 11.4 列表与详情分离

- 列表/分页/多条件筛选：走 MySQL（`devices` + 准实时 `device_runtime`），不用 `SCAN presence:*` 凑数；
- 「仅在线」：用 Redis `online:set` 交库里的主键；在线总数用集合基数；
- 详情：才读 Redis 拿秒级连接字段（列表读库、详情补 Redis）。

### 11.5 五类不一致与对策

| 异常 | 对策 |
|---|---|
| 幽灵在线（owner 崩溃未主动下线） | owner 单写 + TTL 租约自动过期；正确性不依赖主动 DEL |
| 假在线窗口（刚断 TTL 未到） | 接受 ≤TTL 的有界误差；响应给 `staleness`，并可用 `now-lastCom>阈值` 辅助判离线 |
| 抖动翻转 flapping | 离线确认宽限期（grace，默认 10~15s）+ TTL 吸收瞬断，避免刷库刷页 |
| 撕裂读（Redis 新、库旧） | 分区块固定优先级合并（连接态整块以 Redis 为准、身份以库为准），每块带时间戳，不做跨存储事务 |
| 脑裂双 owner | fencing epoch CAS：旧 owner 写不进、不能发下行 |

### 11.6 展示与动作分离

- **展示**用 presence 投影（秒级、有界陈旧）；
- **动作**（下发前确认在线/可送达）不信任投影，走 **owner 路由 + fencing**，由真正持连接的节点权威判定。

---

## 12. 故障隔离与降级

- **库写失败（配置类 write-through）**：内存生效 + 错误日志/指标，协议主流程不中断；
  可配置 `strict` 让对应的**管理 API** 返回错误（设备协议路径永不阻断）。
- **历史批量写失败**：重试有界，超限丢弃最旧并计数，不反压上报；保留 L1 近期数据。
- **Redis 故障（场景3）**：API 读不到 presence → 以库中 `device_runtime` 返回「未知/可能离线（数据降级）」标记，
  不报错、不编造在线；下行命令仍入 outbox，待协调恢复/重连后投递。
- **优雅退出**：捕获信号 → 停止接收 → flush 历史 buffer → 关闭 SQL/Redis。
- **存储初始化失败**：按配置决定 fail-fast（生产）或降级内存（开发，需显式开关与启动告警）。
- **时钟**：`lastComTime` 等以 owner 节点时间为准，依赖 NTP；staleness 在 API 节点计算，容忍秒级偏差。

---

## 13. 关键取舍（为什么不用 bbolt / KV 基座）

早期曾设计「统一 KVStore 基座，memory/file/bbolt/redis 各实现一遍」。终态确认分布式用 MySQL 后调整为
「**统一关系型仓储 + Dialect**」：

- 若单机 bbolt、分布式 MySQL：仓储要写两套（KV 复合键 vs SQL 表/时间索引），保留期与查询在迁移时重写，
  bbolt 沦为一次性本地技术栈。
- 若单机 SQLite、分布式 MySQL：同一套 `database/sql` 仓储与表结构，仅方言差异，单机→分布式零业务改动。
- 访问模式虽以点查为主，但定位/报警确有按时间保留、按设备/时间段查询、后台统计需求，关系模型更贴切。
- 代价：需要表结构与迁移（一次性成本）；纯 Go SQLite 驱动体积较大（可接受）。

内存后端直接实现仓储接口（map 语义），并非「在内存里模拟 SQL」。

---

## 14. 配置设计（在现有 `configs/*.yaml` 上新增 `storage` 段）

```yaml
storage:
  type: memory            # memory | sqlite | mysql（缺省 memory）
  sqlite:
    path: "./data/jt808.db"   # type=sqlite 时必填
  mysql:
    dsn: ""                    # type=mysql 时必填，如 user:pass@tcp(host:port)/db
```

`config.Config` 增加 `Storage *storageConf`，沿用现有 viper Unmarshal 流程。
启动时由 `storage.Init(Config)` 选择后端并完成建表；非法 `type`、缺失 `path`/`dsn` 直接报错退出，
启动日志打印 `backend=<实际后端>`。`coordinator` / `frontCache` / `write` / `retention` / `strict`
等字段属 M3 及后续范围，本期未实现，避免配置出现但无效果的误导。

---

## 15. 目录结构（已落地）

```
internal/repository/              # 持久化契约 + 内存实现；只依赖 model
  repository.go                   # DeviceRepository / GeoRepository / DeviceParamsRepository + 错误值
  device_repository_memory.go     # 内存实现（非导出类型，包外只能经接口使用）
  geo_repository_memory.go
  params_repository.go
  alarm_repository.go             # AlarmRepository（报警历史 + 确认关联）
  region_repository.go            # RegionRepository（圆/矩形/多边形/路线目录）
  industry_repository.go          # IndustryRepository（行业上报/下发目录快照）
  sqlstore/                       # SQL 实现（SQLite / MySQL 共用，已实现 device/params）
    store.go                      # 连接池、DSN、schema_migrations 迁移执行
    schema.go                     # 建表 DDL 与版本列表
    device_repository.go          # DeviceRepository（含最小 upsert 方言差异）
    params_repository.go          # DeviceParamsRepository（参数 JSON 单字段）
    store_test.go                 # 临时文件真实读写、并发、重开验证

internal/runtime/                 # A 类执行现场：生命周期绑定连接/进程，多实例不可共享
  runtime.go                      # ConnectionRegistry / FragmentAssembler / RetransmitTracker
                                  # + Stores 聚合 + Default()
  connection_registry.go          # 连接现场（session + 在线态）
  fragment_assembler.go           # 分包重组（TTL 2min / 容量淘汰）
  retransmit_tracker.go           # 下行重传（0x8003/0x8800 帧缓存）
  param_response_broker.go        # 本地 chan rendezvous
  property_response_broker.go
  location_response_broker.go
  region_response_broker.go
  location_tracking.go            # 临时跟踪定时器
  *_test.go

internal/storage/                 # 仅装配：后端选择与初始化
  storage.go                      # Stores 聚合 + NewMemory() + Default()
```

依赖方向（编译期约束）：

- `repository`、`runtime` 只依赖 `internal/protocol/model`，互不依赖；
- `repository/sqlstore` → `repository`（实现其接口与错误值），不反向；
- `storage` → `repository` / `repository/sqlstore`（后端选择与组装），不反向；
- `protocol` / `server` / `api` / `client` / `cmd/jt808-client` → `storage.Default()`（持久化仓储）
  与 `runtime.Default()`（A 类执行现场），二者入口分离；
- 包外只能通过接口使用内存实现，`*Cache` 等旧名与旧全局入口已全部删除。


---

## 16. 分阶段里程碑

> 每阶段交付标准：`go build ./...`、`go vet ./...`、`gofmt`、`go test -race ./...` 全绿；
> 行为不回退（现有 E2E 不变）。

### M1 — 接口化、正名 + 内存实现（行为不变）
- 定义仓储接口、Registry、配置项；按 §6.4 把现有 `*Cache` / `GetXxxCache()` **正名**为
  Repository/Registry/HotView，并把现有单例 map/ring 逻辑收敛为这些接口的内存实现。
- 拆分 `model.Device`（DeviceRecord / 连接覆盖层）与过宽的 `IndustryCache`（配置/凭证/快照/模拟器态分流）。
- 调用方改为依赖接口（可先用返回接口的访问器过渡，再渐进 DI）；提供测试替身注入点；删除 `*Cache` 旧名。
- 验收：切换 `driver: memory` 与今天行为一致；全部既有测试通过；无新增外部依赖；代码中不再出现业务层 `GetXxxCache`。

### M2 — 单机耐用（SQLite + 内存 L1）
- 升级 Go；引入 `modernc.org/sqlite`；实现 SQL 仓储 + Dialect + 迁移器。
- 本期实际落地（见 §19.5 / §19.6）：`devices`（静态字段）+ `device_runtime`（运行时投影）
  + `device_params` + `terminal_properties`；DDL 文件化与启动版本校验。
- **定位（`locations`）与报警（`alarms`）历史明确延后**，不属本期交付（§7.5）；
  区域/目录/命令 outbox 亦不在本期范围。
- **不含**媒体/运单元数据落库（后续独立任务，§18-D3），该阶段它们仍只留 HotView 最新值。
- `frontCache.enabled=false` 直读直写库模式（§18-D4）**尚未实施**，留待后续里程碑。
- 验收：重启不丢配置（设备/参数/属性）；运行时投影按 §7.4 语义重置；存储失败只告警不阻断协议；内存有界。

### M3 — 分布式（MySQL + Redis 协调）
- MySQL 方言复用 SQL 仓储；实现 Coordinator（owner 租约/fencing/presence/命令总线/应答关联/latest）。
- API 合并读（§11）；drain/崩溃/重连/重投；TCP/UDP owner 语义。
- 验收：非 owner API 可下行并收到应答；杀节点后 TTL 内转离线、设备重连后命令重投成功；无双发。

---

## 17. 风险与对策

| 风险 | 对策 |
|---|---|
| 160+ 调用点一次性 DI 改造爆炸 | M1 先「接口化 + 正名」（内存实现不变、行为不变），DI 按文件渐进迁移 |
| Go 升级带来的兼容问题 | 先单独提一个「工具链升级」提交，跑全量测试后再做存储 |
| SQLite 并发写（单写者） | WAL + 批量合并写；配置类低频；历史走批量队列 |
| MySQL/SQLite 方言差异 | 全部收敛到 `Dialect`；CI 至少对两种方言跑仓储集成测试 |
| 历史高频写拖垮库/主流程 | 批量、有界、可丢弃、埋点；保留期定时清理 |
| 分布式陈旧/脑裂 | TTL + fencing + grace + 展示/动作分离 |
| 关系库存 JSON 牺牲查询力 | 定位/报警用专用列与时间索引；仅目录/参数等用 JSON 负载 |

---

## 18. 评审决策记录（2026-10-31 已确认）

| 编号 | 议题 | 决策 | 落地位置 |
|---|---|---|---|
| **D1** | 终端参数落库形态：整体 JSON 单字段 vs EAV 键值表 | **先用整体 JSON 单字段**（简单、与现有模型直接对应）；将来确有「按参数 ID 服务端查询」需求时，再通过迁移演进为 EAV，不影响仓储接口 | §7.1 `device_params` |
| **D2** | 行业上报（0702/0705/0700/0900/0901 等）最新值是否跨节点镜像 Redis | **首版不需要**。仅留本地 L1（HotView）；`Coordinator.LatestCache` 首版只覆盖定位 latest。将来需要跨节点 API 时按同一接口随时增量扩展 | §3.1、§6.3、§8 |
| **D3** | 媒体（0x0801/0x1205）/运单（0x0701）等 B 类元数据何时落库 | **后续独立任务开发，不放入 M1/M2**。这两阶段只在 HotView 留最新值；`WaybillRepository`/`MediaMetadataRepository` 与对应建表在独立里程碑实现 | §3.1、§6.4、§7.1、M2 |
| **D4** | 单机是否支持「关闭内存 L1 / 直读直写库」模式（超低资源设备） | **支持**。提供 `storage.frontCache.enabled` 开关（单机/分布式均生效，默认开）；关闭后配置类直读直写库、latest 回源库或 Redis | §8、§14 |
| **D5** | API 鉴权 / 多租户 / 存储隔离 | **需求尚不明确，后续专题讨论**（与路线图 P2.2 运维就绪一并考虑）；本期存储设计不做多租户假设，表结构预留 `phone` 维度即可 | P2.2 |

---

## 19. 实施记录

### 19.1 M1（2026-11-01，分支 `dev/p0-baseline`，基线 `84b888a`）

**已落地**

- 新增 `internal/repository/repository.go`：定义 `DeviceRepository`、`GeoRepository`、`ConnectionRegistry`、
  `FragmentAssembler`、`RetransmitTracker`、`DeviceParamsRepository` 六个接口，并提供
  `NewDeviceRepository` / `NewGeoRepository` / `NewConnectionRegistry` / `NewFragmentAssembler` /
  `NewRetransmitTracker` / `NewDeviceParamsRepository` 构造独立内存实例，供测试与新代码注入。
- 现有 `*_cache.go` 的具体类型即内存实现（map + mutex 逻辑未搬家，避免破坏既有测试对包内状态的访问），
  通过编译期断言确认其满足接口；包级函数与 `GetXxxCache()` 保留为薄 wrapper，转发默认实例，
  既有调用点零改动。
- 每个仓储新增业务语义方法（`GetByPhone`/`Upsert`/`DeleteByPhone`/`Append`/`Bind`/`Unbind`/`CacheSegment` 等），
  原方法（`GetDeviceByPhone`/`CacheDevice`/`StoreSession`/`MissingSegmentIndexes` 等）改为一行转发。
- 新增单元测试：`device_repository_test.go`、`session_registry_test.go`、`segment_assembler_test.go`、
  `geo_repository_test.go`，覆盖错误值、快照语义、并发写入/读取。

**与 §6.4 的偏差（需在 M2 收口）**

§6.4 要求 M1 完成后删除 `GetXxxCache()` 与 `*Cache` 旧名。本轮按任务范围先保留旧入口作为过渡 wrapper，
原因：① 本轮明确不做全量调用点迁移；② 既有测试直接访问 `getSegmentCache()` 的包内字段。
因此"删除旧名"顺延到 M2，与调用点 DI 迁移一起完成。

**本轮未接口化（按优先级顺延）**

- `AlarmCache`：现有 `Record`/`List`/`RegisterConfirmation`/`ConfirmByCommand` 与目标接口方法名冲突，
  需要先决定接口命名或拆分"事件存储 / 确认命令路由"（后者属协调态），留待 M2。
- `RegionCatalog`：`Delete`/`Query` 与目标接口语义需按 family 重新设计（`ReplaceAll` 事务化），M2 处理。
- `IndustryCache`：单个结构承载 20+ 类数据，必须先按 §6.4 拆分（配置/凭证/快照/模拟器态）再定义接口。
- `*_response.go` 与 `LocationTrackingCache`：属 A 类 rendezvous/本地定时器，接口化价值低于上面几类，
  且 `LocationTrackingCache` 在 `internal/repository` 之外也有直接读，放到 M2/M3 与 Coordinator 一并设计。

### 19.2 发现但未修改的既有实现问题（仅记录，待单独评审）

1. **`sentSegmentCache.CacheFrame` 容量拒绝路径破坏了字节计数**（`internal/repository/sent_segment_cache.go`）：
   重写同一分片时先执行 `state.bytes -= len(old)` / `cache.bytes -= len(old)`，随后若
   `cache.bytes+len(frame) > maxSegmentCacheBytes` 直接 `return`，但 `state.frames[idx]` 仍是旧帧。
   结果是旧帧还在、计数却已扣减，后续淘汰会多扣一次，容量上界失真。属既有逻辑，本轮原样保留。
2. **`container.RingBuffer` 的并发读写未同步**（`pkg/container/ring_buffer.go`）：
   `Write` 直接写 `r.Container[current]`，`Latest` 直接读 `r.Container[(w-1)%r.Size]`，
   仅位置变量使用 atomic；定位上报与 API 详情并发访问时存在数据竞争的理论风险。
   `go test -race ./...` 当前未触发，因为单测未构造该并发组合。本轮未改动。
3. **`RingBuffer.seekReader` 的负数取模**：`(current+delta)%r.Size` 在 delta 为负时可能得到负索引；
   `Read` 路径潜在越界。定位流程只用 `Write`/`Latest`，暂未触发。本轮未改动。
4. `GeoRepository.Latest` 与既有 `GetGeoLatestByPhone` 一样返回缓冲区内的对象指针（非快照），
   调用方若修改会影响后续读取；这是既有语义，M1 为保持行为一致未改为深拷贝。

### 19.3 M2（2026-11-01，分支 `dev/p0-baseline`，基线 `a170c1a`）

**已落地**

- 旧名全部删除：`GetDeviceCache` / `GetGeoCache` / `GetDeviceParamsCache` 及 `DeviceCache` /
  `GeoCache` / `SessionCache` / `SegmentCache` / `sentSegmentCache` / `DeviceParamsCache` 类型；
  包级函数 `GetSession` / `StoreSession` / `ClearSession` / `CacheSegment` / `MissingSegmentIndexes` /
  `CacheMediaSegment` / `GetMediaSegmentSerial` / `CacheSentSegmentFrame` / `GetSentSegmentFrames`；
  过渡方法 `GeoRepository.Ring` 与便捷方法 `HasPlate` / `HasPhone`。
- 内存实现类型全部非导出：`memoryDeviceRepository` / `memoryGeoRepository` /
  `memoryConnectionRegistry` / `memoryFragmentAssembler` / `memoryRetransmitTracker` /
  `memoryDeviceParamsRepository`，包外只能通过接口使用。
- 对外只保留 `repository.go`：六个接口、`NewXxxRepository` 构造器、`Stores` 聚合结构、
  `NewStores()` 独立实例入口与 `Default()` 进程默认入口。
- `Default()` 是唯一保留的全局入口。理由：msg_processor 注册表（`initProcessOption`）是包级函数，
  其 `processFunc` 签名固定为 `(ctx, data) error`，无法逐条注入依赖；改造注册表属更大结构变更，
  M2 先显式收口为单一 `Default()`，调用点与默认值来源可追溯（见 `repository.go` 注释）。
- 迁移按五批提交，每批 `go build ./... && go vet ./... && go test -race ./...` 均通过：
  ① repository 内部与测试（新增临时 `legacy.go` 兼容层承载旧 API，保证中间批次可编译）→
  ② keepalive / packet_codec / 两个 retransmission / pipeline →
  ③ api → ④ tcp/udp server、server 测试、client、cmd/jt808-client 模拟器 →
  ⑤ msg_processor、msg_processor_test，并删除 `legacy.go`。
- `HasPlate` / `HasPhone` 的调用点改为 `GetByPlate` / `GetByPhone` 返回 `ErrDeviceNotFound`
  的等价判断；geo 写入统一走 `GeoRepository.Append`，`RingBuffer` 不再暴露到接口。
- api `/api/device` 列表因 `List` 接口返回 error 增加 500 分支（内存实现实际不可达）。
- 包目录由 `internal/storage` 更名为 `internal/repository`，包名同步改为 `repository`；
  对外符号（接口、构造器、`Stores`、`Default()`）不变，仅导入路径与包限定符变化。

**完成后不再有全局访问的位置**

- `internal/server`（tcp/udp server 及测试）、`internal/api`、`internal/client`、`cmd/jt808-client`、
  `internal/protocol`（keepalive / codec / retransmission / pipeline / msg_processor 及测试）
  对六类仓储只经 `repository.Default()` 显式访问；`internal/repository` 包内测试使用
  `NewStores()` / `NewXxxRepository()` 独立实例，不触碰默认单例。
- 仍为全局入口的只剩 §19.1 顺延的未接口化缓存：`GetAlarmCache` / `GetIndustryCache` /
  `GetRegionCatalog` / 各 `GetXxxResponseBroker` / `GetLocationTrackingCache`，
  待后续里程碑接口化时一并收口。

### 19.4 包结构分层（2026-11-01，分支 `dev/p0-baseline`，基线 `cf3e3b3`）

**决策：A 类执行现场与持久化仓储不共用同一套存储接口（成立，已按此重构）**

代码证据：

- `model.Session` 持有 `Conn net.Conn`（活连接，不可序列化）；`Bind`/`Unbind` 在 TCP/UDP server
  的连接建立/断开路径调用，api 取到 session 后直接写 `session.Conn` 下发。
- `FragmentAssembler` 以 `(phone, msgID, firstSerial)` 为键、TTL 2min + 容量淘汰，语义是
  「同一次上报的分片在短时间内凑齐」；`RetransmitTracker` 保存已编码帧供 0x8003/0x8800 重传，
  TTL 2min / 16MB 上界。两者都无跨进程共享需求，进程重启后丢弃即正确。
- 4 个 `*ResponseBroker` 是本地 `chan` 配对（请求方等待、应答方投递），超时/取消即释放；
  `LocationTracking` 是本地临时定时器。均无法持久化，也无法跨节点配对。
- 拆分未产生循环依赖：`runtime` 与 `repository` 都只依赖 `internal/protocol/model`。

结论：A 类接口归 `internal/runtime`；Alarm/Region/Industry 归 `internal/repository`（业务数据，
对应 §7 的 `alarms` / `region_items` / `terminal_catalogs`）；Geo 历史仍在 repository，
其内存 RingBuffer 作为 C 类热读 L1 的拆分留待后续（见 §19.2-4）。

**新包结构**

- `internal/repository`：仅持久化契约与内存实现。`repository.go` 保留
  `DeviceRepository` / `GeoRepository` / `DeviceParamsRepository`；新增
  `AlarmRepository` / `RegionRepository` / `IndustryRepository`（接口 + 非导出内存实现 +
  `NewXxxRepository()`）。不再包含 `Stores` / `Default()`。
- `internal/runtime`：`ConnectionRegistry` / `FragmentAssembler` / `RetransmitTracker`
  （`runtime.go`），以及 4 个 ResponseBroker 与 `LocationTracker`；`Stores` 与
  `runtime.Default()` 只聚合 A 类，不含任何持久化字段。
- `internal/storage`：只做装配与后端选择，当前为 `NewMemory()` / `Default()`，阶段二在此加入
  `memory|sqlite|mysql` 后端选择；持久化实现仍位于 `internal/repository`。

**分批提交与验证**（每批 `gofmt` + `go build ./...` + `go vet ./...` + `go test -race ./...` 全绿）

| 批次 | commit | 内容 |
|---|---|---|
| B1 | `9047ae3` | `internal/storage` → `internal/repository` 重命名，行为不变 |
| B2 | `dad95d5` | A 类连接现场/分包重组/下行重传拆到 `internal/runtime`，删除 `repository.Stores` 中的 A 类字段 |
| B3 | `b71c432` | 报警/行业/区域仓储接口化，4 个 ResponseBroker 与 LocationTracking 归入 runtime，57 处旧全局访问迁移 |
| B4 | `a5f3213` | 新增 `internal/storage` 装配层，调用点改为 `storage.Default()` / `runtime.Default()` |

**行为保持**：协议输出、错误值、TTL/容量、深拷贝与并发语义均未改变；仅包路径、类型可见性与
访问入口变化。

### 19.5 SQL 后端（SQLite / MySQL，2026-11-01，基线 `183a33c`）

**范围**：只实现 `DeviceRepository` 与 `DeviceParamsRepository`；其余 B 类仓储仍为内存实现，
不新建业务语义（与 §7.4 一致）。后续的属性档案、运行时拆分与迁移改造见 §19.6。

**新增依赖**

- `modernc.org/sqlite v1.58.0`：纯 Go，无 cgo，`CGO_ENABLED=0 go build ./...` 已验证通过。
  未选 `mattn/go-sqlite3` 以避免 Docker/CI 交叉编译依赖 C 工具链。
- `github.com/go-sql-driver/mysql v1.10.1`：官方生态常用 MySQL 驱动。
- 为兼容本机 `goenv`（仅安装 `1.26`），将 `modernc.org/libc` 固定为 `v1.75.6`、
  `golang.org/x/sys` 固定为 `v0.47.0`（其上游版本声明 `go 1.26.0`，会导致 `go.mod` 被抬到
  未安装的补丁工具链）。

**实现位置**：`internal/repository/sqlstore`（持久化实现下沉 repository，
`internal/storage` 只负责后端选择与装配）。

**建表与迁移**

- `schema_migrations(version INTEGER PRIMARY KEY, applied_at BIGINT NOT NULL)`：
  DDL 位于内嵌的 `bootstrap_schema_migrations.sql`，由 `jt808-migrate` 在执行版本前创建；
  服务启动只读该表，缺失即拒绝启动。不引入迁移框架。
- `devices`：`phone` 主键；字段与 `model.Device` 可序列化部分一一对应，`Conn` 不入库；
  时间统一存毫秒整数；未加 `plate` 唯一约束（原因见 §7.4）。
- `device_params`：`phone` 主键，`param_cnt` + `params_json`，遵循 D1。

**连接池与事务边界**

- SQLite：`SetMaxOpenConns(1)`，DSN 带 `busy_timeout(5000)` 与 `journal_mode(WAL)`，
  单写者模型下避免 `SQLITE_BUSY`。
- MySQL：`MaxOpenConns=10`、`MaxIdleConns=5`、`ConnMaxLifetime=30m`。
- 本期 Device/Params 的写操作均为单语句，使用隐式事务；区域「清空+重设」等后续多步操作
  再引入显式 `UnitOfWork`。

**验证**

- SQLite：临时文件真实读写；关闭后重新打开同一 db 可读到设备与参数；8 并发写 + 并发读
  `go test -race` 通过；不存在数据返回既有错误值 `ErrDeviceNotFound` / `ErrDeviceParamsNotFound`。
- 启动冒烟：`type=sqlite` 能建库并打印 `backend=sqlite`；`type=redis` 启动报错退出（exit=1）。
- MySQL：仓库内测试通过 `JT808_TEST_MYSQL_DSN` 环境变量启用，本地无实例时跳过；
  未验证假设见下。
- 默认 `storage.type=memory` 时既有测试全部不变。

**MySQL 未验证假设**

1. `ON DUPLICATE KEY UPDATE ... VALUES(col)` 在 MySQL 8.0.20+ 会产生弃用告警但语义正确；
   若目标版本更高，可迁移到 `INSERT ... AS new ON DUPLICATE KEY UPDATE`。
2. `VARCHAR(32)` 主键在 `utf8mb4` 下索引长度符合默认 `innodb_large_prefix`（3072 字节）限制。
3. DSN 未强制 `parseTime`/`loc`，因为时间统一以毫秒整数读写，不依赖驱动时区解析。
4. 并发写入依赖 InnoDB 行锁与主键冲突语义；未在真实 MySQL 上跑 8 并发用例。

### 19.6 属性档案 / 运行时拆分 / 迁移机制（2026-11-01，分支 `dev/p0-baseline`）

**决策**

1. `locations` / `alarms` 明确延后（理由见 §7.5），不再用"本期不新建业务语义"笼统带过。
2. 新增 `terminal_properties`：只承载 0x0107 属性应答；与 `devices` 不合并，避免属性应答触发
   `devices` 静态列全量重写。0x0107 处理为"持久化 + 内存最新值 + broker 投递"。
3. 拆分 `device_runtime`：`session_id`/`trans_proto`/`last_com_time_ms`/`status` 移出 `devices`；
   `keepalive_ms` 是代码常量，留在 `devices`。读时合并两表，写时同事务按归属分写；
   `DeviceRepository` 契约仍返回完整 `model.Device`。
4. 迁移文件化 + 显式入口 + 启动只校验（§7.2）。

**分批记录**

| 批次 | commit | 内容 |
|---|---|---|
| 1 | `387b9fd` | 文档决策与缺口修正（本节、§7.1–§7.5、§11.3、§16） |
| 2 | `82aa13d` | `terminal_properties` 落库 + 0x0107 三者同时 |
| 3 | `8d49dfc` | `device_runtime` 拆分与重启语义 |
| 4 | 本记录提交 | 迁移机制改造（SQL 文件 + 启动校验；迁移入口后续独立为 `jt808-migrate`，见 §19.8） |

**迁移机制实现细节**

- 目录：`internal/repository/migration/` 下 `migrate.go` + `0001_init.sql` /
  `0002_terminal_properties.sql` / `0003_device_runtime.sql`；SQL 经 `//go:embed *.sql` 编译进二进制，
  迁移器只依赖 `database/sql`，未引入框架。（这些增量文件已在 §19.10 合并为
  `bootstrap_schema_migrations.sql` + `init_20261101.sql`。）
- 迁移入口：本批最初复用主服务的 `migrate` 位置参数；**§19.8 起改为独立工具 `jt808-migrate`**，
  SQLite 的启动自动迁移同时取消。DBA 也可按文件名顺序直接执行这些 SQL 文件。
- 启动校验（当前行为）：所有后端都只比对版本。低于期望报
  `... is behind expected ... run \`jt808-migrate -c <config>\``；高于期望报
  `... is ahead of this program's expected ...`；全新库缺 `schema_migrations` 同样报错并提示迁移。
  原 `storage.sqlite.autoMigrate` 配置项已删除（§19.8）。
- 失败处理：逐版本执行，错误带版本号、文件名与语句序号；仅当该版本全部语句成功后才写入
  `schema_migrations`，已成功版本不会重复应用。
- MySQL 客观限制：DDL 隐式提交，无法与版本记录同事务；中途失败可能留下"部分表已建、版本未记录"，
  应对为重跑迁移（幂等 DDL 补齐）或 DBA 人工核对。SQLite 本轮共用同一路径（先 DDL、后记录）。
- 重启语义（本轮修正，见 §19.7）：**移除**了启动时单条
  `UPDATE device_runtime SET session_id='', status=0` 的批量重置——多节点共享同一 MySQL 库时，
  任一节点启动都会覆盖其它节点的在线状态。替代方案为：
  1. **单机**：`main` 监听 SIGINT/SIGTERM，退出前调用 `storage.ResetRuntime` 再 `storage.Close`；
  2. **异常重启**：读路径按 `last_com_time_ms + keepalive_ms` 的 TTL 收敛为 offline 并清空
     `session_id`，`last_com_time_ms` 保留作"上次在线时间"；
  3. **分布式（规划）**：运行时投影另存一份带全 TTL 的 Redis 缓存，各实例定期兜底扫描过期条目
     并回写持久化状态，不再依赖任何节点启动时的批量操作。

**验证**

- `gofmt` / `go build ./...` / `go vet ./...` / `go test -race ./...` 全绿。
- 新增用例：迁移幂等（第二次 `applied=0`、版本行不重复）；`devices` 不再含运行时列；
  版本落后 / 超前拒绝启动；版本一致正常启动；显式迁移可重复；内存后端迁移报错。
  （SQLite 自动迁移路径的用例在 §19.8 随该行为取消而删除。）

### 19.7 心跳节流 / 车牌索引 / 重启语义修正（2026-11-01，分支 `dev/p0-baseline`，基线 `af0dff4`，提交 `cbde5aa`）

**决策**

1. **心跳落库可配节流，默认逐条**：新增 `server.heartbeatPersistIntervalMs`（缺省 `0`）。
   取值大于 0 时，距上次落库不足该间隔的心跳跳过写库；缺省行为与之前完全一致。
2. **`plate` 加非唯一索引入库**：迁移 `0004_devices_plate_index.sql` 创建 `idx_devices_plate`；
   不加唯一约束，理由见 §7.4。
3. **移除启动批量重置运行时投影**：单机改为优雅停机重置，异常重启残留由读路径 TTL 收敛；
   分布式改由 Redis 运行时缓存的全 TTL + 过期扫描回写（规划项，§11.2 / §11.5）。

**实现要点**

- `runtime.HeartbeatRegistry`（A 类，仅进程内存）：`processMsg0002` 逐条 `Touch`，
  被节流跳过时保活判定仍能拿到实时心跳；0x0003 注销与鉴权失败清理同时 `Remove`。
- `keepalive` 判定前把 `Heartbeat.Last` 与库内 `last_com_time` 取较新值，避免节流把在线设备判离线。
- `sqlstore.Open` 不再执行任何重置；新增 `Store.ResetRuntime`（`UPDATE device_runtime SET session_id='', status=0`）
  由 `main` 在收到 SIGINT/SIGTERM 后调用（5s 超时），随后 `storage.Close`。
- `deviceRow.device()` 读取时按 TTL 收敛：`status != offline` 且 `now > last_com_time_ms + keepalive_ms`
  → 置 offline 并清空 `session_id`，`last_com_time_ms` 与 `trans_proto` 保留。
- 迁移数量变为 4（`ExpectedVersion=4`），SQLite/MySQL 共用同一 `.sql`（普通 `CREATE INDEX`，
  未使用 MySQL 不支持的 `IF NOT EXISTS`）。

**验证**

- `gofmt` / `go build ./...` / `go vet ./...` / `go test -race ./...` 全绿。
- 新增用例：心跳节流跳过写库但保留进程内心跳、超过间隔后恢复落库；保活使用进程内心跳
  不误判离线；`ResetRuntime` 清空 session/status 且保留静态字段与时间；
  TTL 收敛陈旧读为 offline 且保留 `last_com_time`；迁移 4 个文件且 `idx_devices_plate` 存在。
- 已知限制：MySQL 下 `0004` 为普通 `CREATE INDEX`，重复手工执行会因索引已存在而失败，
  迁移器按 `schema_migrations` 版本号避免重复执行。（迁移文件已在 §19.10 合并为
  `init_20261101.sql`。）

### 19.8 独立迁移工具与取消 SQLite 自动迁移（2026-11-01，分支 `dev/p0-baseline`，基线 `bc439b9`，提交 `629c29d`）

**决策**

1. **迁移不再复用主服务二进制**：新增 `cmd/jt808-migrate`，单独编译、单独发布为 `jt808-migrate`。
2. **取消 SQLite 启动自动迁移**（原 `storage.sqlite.autoMigrate`，缺省 `true`）。所有后端一致：
   启动只校验版本，任何 DDL 只能由迁移工具或 DBA 执行。
3. **主服务删除 `migrate` 位置参数**，避免一个二进制同时承担在线服务与运维动作。

**实现要点**

- `cmd/jt808-migrate/main.go`：仅接受 `-c <config>`，调用 `storage.Migrate(storage.FromConfig(cfg))`，
  打印 `applied` 后退出；不启动 TCP/UDP/API。
- 新增 `storage.FromConfig`，主服务与迁移工具共用配置映射，避免两处漂移（主服务原 `storageConfig` 删除）。
- `sqlstore.Open` 只保留版本校验；`sqlstore.Config`、`storage.Config`、`config.sqliteConf`
  删除 `AutoMigrate` 字段。
- 构建：`scripts/build.sh` 增加 `jt808-migrate` 目标（`cmd/jt808-migrate/main.go`）；
  `make compile-migrate` / `make release-migrate`；Dockerfile 同时构建并拷贝迁移工具。

**验证**

- `gofmt` / `go build ./...` / `go vet ./...` / `go test -race ./...` 全绿。
- 用例调整：所有 SQLite 测试先显式 `Migrate` 再 `Open`；保留“全新库未迁移则拒绝启动”；
  内存后端 / 未知后端迁移报错；删除 `autoMigrate` 相关断言。

### 19.9 入口统一收拢到 `cmd/`（2026-11-01，分支 `dev/p0-baseline`，基线 `b20b6b3`，提交 `621f18e`）

**布局**

| 二进制 | 入口 | 说明 |
|---|---|---|
| `jt808-server-go` | `cmd/jt808-server/main.go` | 原仓库根 `main.go` |
| `jt808-client-go` | `cmd/jt808-client/main.go` | 原 `test/client/main.go`；`test/client/configs` 保留（被 `internal/config` 嵌入为客户端默认配置） |
| `jt808-migrate` | `cmd/jt808-migrate/main.go` | 独立迁移工具（§19.8） |

**配套更新**

- `scripts/build.sh`：默认 `build_file=cmd/jt808-server/main.go`；客户端改为
  `cmd/jt808-client/main.go`；迁移工具不变。
- `scripts/smoke.sh`：冒烟构建改为 `go build ./cmd/jt808-server` 与 `./cmd/jt808-client`。
- `Makefile`：`run` / `run-client` 的 `go run` 路径同步。
- `.vscode/launch.json`：调试入口同步。
- 根目录不再有 Go 入口，`go build .` 不再可用；统一使用 `go build ./cmd/...`、
  `make compile` / `make compile-client` / `make compile-migrate`。

**验证**

- `gofmt` / `go build ./...` / `go vet ./...` / `go test -race ./...` 全绿；
  `scripts/smoke.sh` 端到端通过（五个场景全部 PASS）。

### 19.10 合并迁移版本 + 迁移元数据表 DDL 移入 SQL（2026-11-01，分支 `dev/p0-baseline`，基线 `621f18e`）

**决策**

1. **开发阶段只保留一个合并版本**：删除 `0001`–`0004` 四个增量文件，合并为
   `init_20261101.sql`（最终 schema 形态，`devices` 不再含运行时列）。
2. **文件名改为 `<描述>_<YYYYMMDD>.sql`**，版本号取文件名末尾日期（如 `20261101`），
   迁移按日期升序执行；后续变更追加新文件即可，无需人工维护序号。
3. **`schema_migrations` 的建表 DDL 从 Go 常量移入 SQL**：新增
   `bootstrap_schema_migrations.sql`，由迁移工具在执行任何版本前先执行。
   该表是迁移机制自身的元数据，必须先于版本存在，所以作为不参与版本编号的引导脚本；
   MySQL 与 SQLite 同样需要它，二者共用该脚本。

**原因**

- 开发阶段没有存量库，保留四个增量版本只会增加阅读与维护成本；合并后 schema 一眼可见。
  已有的本地开发库直接删除重建即可（不承诺从旧 4 个增量版本平滑升级）。
- 版本按日期而非递增序号，新增迁移不会因为旧序号冲突而需要重排文件。
- Go 代码中的 DDL 无法与其它 DDL 一起被审阅、被 DBA 直接执行；移入 `bootstrap_*.sql` 后
  迁移工具与手工执行路径使用同一份语句，失败也会以普通迁移错误返回。

**实现要点**

- `migration.Apply`：先执行 `bootstrapStatements`，再读 `CurrentVersion`，再按日期应用未执行版本；
  引导脚本失败返回 `bootstrap <file> statement N: ...`。
- `mustLoadMigrations` 跳过引导脚本；`parseVersion` 改为校验 8 位日期后缀（含
  `time.Parse("20060102")` 合法性检查），并对同日期文件 panic。
- 服务启动行为不变：只读 `schema_migrations`，缺失/落后/超前都拒绝启动。

**验证**

- `gofmt` / `go build ./...` / `go vet ./...` / `go test -race ./...` 全绿。
- 用例更新：`ExpectedVersion=20261101`、仅 1 个迁移文件；日期后缀解析合法/非法；
  重复执行迁移仍幂等；版本落后（清空记录）与超前（`ExpectedVersion()+1`）均拒绝启动；
  迁移后四张表与 `idx_devices_plate` 均存在。

