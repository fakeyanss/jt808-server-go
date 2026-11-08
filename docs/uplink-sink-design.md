# 终端上行数据业务处理与出口可插拔化设计（Uplink Business Processing & Pluggable Sink）

- 状态：**设计阶段，未修改任何代码**
- 创建日期：2026-11-08
- 代码基线：当前工作区分支 `dev/p2-admin-api`，HEAD `e241511`；上行处理、存储装配与运行时核心与 `dev/p1-1078` 一致，差异仅在 admin API 相关的配置、历史分页查询方法与运行时只读快照
- 说明：核查期间该工作区存在其他并行会话的改动，相关文件随后被提交为 `e241511`；本文未修改任何非 `docs/` 文件
- 关联文档：
  - [storage-abstraction-design.md](storage-abstraction-design.md)（仓储接口与运行时状态的既有分层）
  - [gateway-mode-design.md](gateway-mode-design.md)（接入网关的边界与非目标）
  - [JT808-project-roadmap.md](JT808-project-roadmap.md) 的 P1.4（可插拔事件出口）
- 适用范围：JT/T 808 主链路的终端上行数据处理、业务落库与外部出口；不覆盖苏标附件接收、1078 RTP 媒体落盘、HTTP API 鉴权

> 本文沿用既有命名口径：**耐用（durable）**指进程重启后仍需保留的数据；**易失（volatile）**指绑定活连接或单进程、重启即重建的数据；**投影（projection）**指由业务事实派生、可重建的最新值视图。

---

## 1. 结论摘要

1. 现状是「协议处理 + 业务落库 + 旁路回调」三段黏在同一个函数调用里。
   [msg_processor.go](../internal/protocol/msg_processor.go) 中 `JT808MsgProcessor.Process` 依次完成解码、应答生成、业务处理、钩子分发；
   业务处理函数通过 `storage.Default()` 直接读写仓储，通过 `runtime.Default()` 直接改运行时状态。
2. 注册表实际覆盖 **74 个不同 MsgID**（66 个字面量键 + 两个循环各 4 个键）；
   共 **39 个已注册处理函数**（32 个 `processMsg*` + 7 个通用函数），其中 **34 个已注册处理函数**直接依赖默认单例。
3. 统计口径内 `msg_processor.go` 有 **33 处 `storage.Default()`** 和 **14 处 `runtime.Default()`**，
   分布到 37 个函数（含 3 个辅助函数）。直接依赖的仓储字段为
   `Industry`（13）、`Devices`（7）、`Params`（4）、`Regions`（3）、
   `Geo`（2）、`Alarms`（2）、`Properties`（1）、`Locations`（1）。
4. 最大可靠性问题不是「能不能加 MQ」，而是**业务落库位于协议应答之前**：
   `processFunc` 同步执行完才进入 `encode`/`send`，数据库变慢会直接推迟 `0x8001`；
   业务错误会沿 `Process` 返回到 Pipeline，导致本轮不应答，部分错误还会关闭连接。
5. 目标形态把上行处理拆成三层：**协议内核（同步、无外部 I/O）→ 业务处理器（纯归一化，产出记录）→ 出口 Sink（可替换 I/O）**。
   出口按类别配置为 `local-db` / `noop` / 未来 `mq`，默认 `local-db` 保持现有行为。
6. 首期不引入 outbox，也**不承诺 exactly-once**：高频、可丢类别走有界队列 + at-most-once；
   不可丢类别（报警等）保持同步或后续单独引入 outbox 达到 at-least-once。
7. 分布式演进时，协议内核、注册表、归一化记录和本地适配器不需要改；
   需要新增的是 MQ Sink、按手机号的应答/查询路由、集中式读模型与共享在线态。

---

## 2. 现状核查

### 2.1 从连接接收到落库/回调的完整链路

TCP 与 UDP 均复用同一条 `Pipeline`（`internal/protocol/pipeline.go`）：

```text
TCP Accept / UDP datagram
  -> 每个会话一个 goroutine serveSession（internal/server/tcp_server.go、udp_server.go）
  -> Pipeline.ProcessConnRead
       recv()     : FrameHandler 读取整帧（去转义、校验）
       decode()   : PacketCodec 解析消息头与消息体
       process()  : JT808MsgProcessor.Process
                    a. options[msgID] 查表
                    b. pkt 分包未完成则走 processSegmentPacket 并回 0x8001
                    c. genData() 生成 Incoming/Outgoing 容器
                    d. decodeMessageSafely() 解码消息体
                    e. acknowledgeResponse() 清算会话在途等待项
                    f. Outgoing.GenOutgoing() 生成协议应答
                    g. processFunc(ctx, data) 执行业务处理（存储/运行时/查询投递）
                    h. dispatchHooks() 触发 action.Hooks
       assignSerialNumber() / encode() / send() : 分配流水号、编码、写回连接
```

关键事实：

- `process()` 与 `encode()`/`send()` 在同一个会话 goroutine 内串行执行。
  因此 `processFunc` 中的同步数据库写入会直接增加**应答时延**，而不是仅仅影响后台吞吐。
- `Process` 在 `processFunc` 返回错误时立即返回错误，Pipeline 的 `callWithBlocking` 随之中断，
  本轮不会进入 `encode`/`send`，终端收不到应答。
- `serveSession` 对 `repository.ErrDeviceNotFound`、`io.EOF`、网络超时等错误会直接关闭连接；
  其余错误休眠 1 秒后继续。
- UDP 以一个远端端点对应一个逻辑会话，同样走上述串行链路；分片/重传语义与 TCP 一致。
- 客户端模拟器 `internal/client/tcp_client.go`、`cmd/jt808-client` **复用同一个 `Pipeline` 与同一张 `processOptions`**。
  因此注册表里既有服务端上行处理，也有模拟器侧的下行响应处理，二者不能按 MsgID 数值简单区分。

### 2.2 注册表与处理函数

| 指标 | 数值 | 说明 |
|---|---|---|
| `msg_processor.go` 行数 | 1538 | 含注册表、处理函数、若干辅助函数 |
| 字面量 `options[0x....] = &action` 赋值 | 66 个 | 去重后仍为 66，无重复注册 |
| 循环注册 MsgID 数 | 8 个 | `0x8600/0x8602/0x8604/0x8606`、`0x8601/0x8603/0x8605/0x8607` |
| 实际不同 MsgID | **74 个** | 66 + 8 |
| `processMsg*` 函数 | 32 个 | 以 `func processMsg` 开头 |
| 已注册处理函数（去重） | 39 个 | 32 个 `processMsg*` + `processIndustryReport` 等 7 个通用函数 |
| 带 `process` 字段的注册项 | 59 个 | 其余注册项只声明 `genData`，由 Pipeline 早期分支处理 |
| 直接依赖默认单例的已注册函数 | 34 个 | 另有 `bindPhoneSession`、`recordLocationHistory`、`retransmitCachedSegments` 3 个辅助函数 |

注册表本身还混合了三类语义：

1. **服务端上行处理**（`MsgID < 0x8000`）：如 `0x0200`、`0x0704`、`0x0702`、`0x1205`。
2. **平台下行命令与应答**（`MsgID >= 0x8000`）：如 `0x8100`、`0x8104`、`0x8202`、`0x8802`。
3. **模拟器专属行为**：如 `0x8104` 生成假参数、`0x8700` 生成记录仪应答、`0x8803/0x8804/0x8805` 回放媒体。

> 设计要求：Sink 只允许挂在「服务端上行 + 业务事实」这一类入口上。
> 不能按 `MsgID` 是否小于 `0x8000` 自动判断，因为模拟器应答也可能 `MsgID < 0x8000`（如 `0x0001`、`0x0104`、`0x0201`）。

### 2.3 `processFunc` 签名与依赖注入的改造面

当前注册项结构：

```go
type action struct {
    genData func() *model.ProcessData
    process func(context.Context, *model.ProcessData) error
}
```

`processOptions` 由包级函数 `initProcessOption()` 构造，`process` 字段直接保存包级函数地址。
`NewJT808MsgProcessor(hooks)` 只接收 `Hooks`，不接收存储或运行时依赖；`Pipeline` 在每个连接上构造一个新的处理器实例。

把依赖改为处理器字段注入，有三种路径：

| 方案 | 做法 | 改动面 | 评价 |
|---|---|---|---|
| A（推荐） | 处理函数改为 `JT808MsgProcessor` 的方法，处理器持有 `ProcessorDeps{Storage, Runtime, Sinks}`；`initProcessOption` 改为方法，注册项写 `process: mp.processMsg0200` | 39 个已注册处理函数 + 3 个辅助函数、构造函数、Pipeline 选项、TCP/UDP 装配、客户端构造、相关单测构造点 | 注册表结构不变，改动机械、可分批；依赖显式 |
| B | 保持包级函数，注册项改为 `process func(context.Context, *model.ProcessData, *ProcessorDeps) error` | `action` 类型、39 个处理函数、3 个辅助函数、全部注册项、调用点、测试 | 同样显式，但注册表每一项都要改，冲突面更大 |
| C | 通过 `context.Context` 注入依赖 | 仅调用点 | 依赖隐蔽、测试难断言，不推荐 |

方案 A 的示意（仅签名，不含实现）：

```go
type ProcessorDeps struct {
    Storage *storage.Stores
    Runtime *runtime.Stores
    Sinks   uplink.Dispatcher // 首期为 local-db 适配器
}

type JT808MsgProcessor struct {
    hooks   actionpkg.Hooks
    deps    ProcessorDeps
    options processOptions
}

func (mp *JT808MsgProcessor) initProcessOption() processOptions {
    options := make(processOptions)
    options[0x0200] = &action{
        genData: func() *model.ProcessData { return &model.ProcessData{Incoming: &model.Msg0200{}, Outgoing: &model.Msg8001{}} },
        process: mp.processMsg0200, // method value，签名仍满足 action.process
    }
    return options
}
```

装配路径需要同步调整：

| 位置 | 当前 | 改造后 |
|---|---|---|
| `NewJT808MsgProcessor(hooks)` | 仅 `hooks` | `NewJT808MsgProcessor(hooks, opts ...ProcessorOption)`，提供 `WithStorage/WithRuntime/WithSinks`；未注入时回退 `storage.Default()`/`runtime.Default()` |
| `Pipeline.NewPipeline(conn, opts...)` | 仅 `WithHooks` | 增加依赖选项，透传给处理器 |
| `TCPServer`/`UDPServer` | 无存储依赖 | 增加 `SetDeps` 或构造函数注入，创建 Pipeline 时传入 |
| `cmd/jt808-server/main.go` | `storage.Init` 只返回 backend 字符串 | 需要拿到 `*storage.Stores`（返回二元组或改用 `storage.Open`）并下传 |
| `internal/client/tcp_client.go` | `NewPipeline(conn)` | 首期可继续走默认回退；客户端模拟器始终使用本地内存仓储 |

> 兼容策略：第一阶段保留 `storage.Default()`/`runtime.Default()` 回退，先完成签名与注入骨架，
> 再逐步让主服务、客户端显式传入实例。这样可以避免一次性重写大量测试。

历史原因已经写在两个包的注释里：

- [storage.go](../internal/storage/storage.go) 的 `Default()` 注释明确说明：`processFunc` 注册表是包级函数，
  签名固定为 `(ctx, data) error`，无法逐条注入依赖，因此收口为单一 `Default()`。
- [runtime.go](../internal/runtime/runtime.go) 的 `Stores` 注释声明它「作为组件构造函数的运行时依赖」，
  但 `msg_processor` 仍直接调用 `runtime.Default()`。

本设计的 S2 步骤正是解除这一约束；在此之前保留默认入口是为了保证行为不变。

### 2.4 `storage.Default()` 与 `runtime.Default()` 的分布

`msg_processor.go` 内按仓储字段统计：

| 仓储字段 | 引用次数 | 典型处理函数 | 语义 |
|---|---|---|---|
| `Industry` | 13 | `processIndustryReport`、`processMsg9205`、媒体回放（0x8803/0x8804/0x8805） | 行业上报最新值 / 模拟器目录 / 媒体上传缓存 |
| `Devices` | 7 | `processMsg0002/0003/0100/0102/0200/8001/8100` | 设备静态档案与注册校验 |
| `Params` | 4 | `processMsg0104/8103/8104/8106` | 终端参数应答/下发 |
| `Regions` | 3 | `processRegionRouteSet/Delete/Query` | 区域路线目录 |
| `Geo` | 2 | `processMsg0200/8201` | 最新定位投影 |
| `Alarms` | 2 | `processMsg0200/0001` | 报警历史与确认关联 |
| `Properties` | 1 | `processMsg0107` | 终端属性档案 |
| `Locations` | 1 | `recordLocationHistory` | 定位历史（memory 后端为 nil） |

`runtime.Default()` 共 14 处：

| 运行时字段 | 引用次数 | 用途 |
|---|---|---|
| `Retrans` | 3 | 下行分片/多媒体重传缓存回放 |
| `Heartbeat` | 3 | 心跳最近通信时间 |
| `Conn` | 2 | 会话顶替、手机号绑定 |
| `Tracking` | 1 | 0x8202 设置的上报周期/有效期 |
| `ParamResponses` / `PropertyResponses` / `LocationResponses` / `RegionResponses` | 各 1 | 查询应答投递给等待中的 API 请求 |

全仓（不含测试与文档）另有 `storage.Default()` 48 处、`runtime.Default()` 39 处，分布如下：

| 文件 | `storage.Default()` | `runtime.Default()` |
|---|---|---|
| `internal/protocol/msg_processor.go` | 33 | 14 |
| `internal/protocol/keepalive.go` | 2 | 4 |
| `internal/protocol/pipeline.go` / `packet_codec.go` / `segment_retransmission.go` / `media_retransmission.go` | 0 | 各 1 |
| `internal/api/api.go` | 5 | 5 |
| `internal/server/tcp_server.go` | 0 | 4 |
| `internal/server/udp_server.go` | 0 | 2 |
| `internal/client/tcp_client.go` | 0 | 2 |
| `cmd/jt808-server/main.go` | 2 | 3 |
| `cmd/jt808-client/main.go` | 6 | 1 |

### 2.5 仓储接口与 `internal/storage` 装配

`internal/repository` 定义 **8 个仓储接口**：

| 接口 | 类别 | 主要方法 | 当前 SQL 后端 |
|---|---|---|---|
| `DeviceRepository` | B 耐用 | `GetByPhone/GetByPlate/List/Upsert/DeleteByPhone/DeleteByPlate` | `devices` 静态列 |
| `GeoRepository` | C 投影 | `Append/Latest/Delete` | 无，始终内存 |
| `DeviceParamsRepository` | B 耐用 | `Get/Replace/Delete` | 有 |
| `TerminalPropertiesRepository` | B 耐用 | `Get/Replace/Delete` | 有 |
| `AlarmRepository` | B 耐用 | `Record/List/ListPage/RegisterConfirmation/ConfirmByCommand` | 有；确认关联为进程内协调态 |
| `RegionRepository` | B 耐用 | `ApplySet/Delete/Query` | 有；读路径仍为内存目录 |
| `IndustryRepository` | B/C 混合 | `Record` + 30 余个按设备最新值读取方法 | 仅终端配置目录/升级/透传快照落库，绝大多数上报仍为内存 |
| `LocationRepository` | B 耐用 | `Record/List/ListPage` | 有；memory 后端为 `nil` |

> 媒体元数据当前没有独立仓储：`0x0801` 上传片段与 `0x1205` 资源列表都保存在
> `IndustryRepository` 的内存字段中（`MediaUploads` 按设备最多保留 100 条）。
> 这意味着「媒体元数据」如果要成为独立 Sink 类别，需要先定义其落库目标。

`internal/storage` 只做装配，不承载实现：

- `Open(Config)` 按 `memory|sqlite|mysql` 选择后端，返回 `*Stores`（8 个仓储字段 + `Backend` + `close`）。
- `Init(Config)` 把实例写入包级默认存储；`Default()` 未初始化时懒加载 memory。
- `FromConfig(cfg)` 将 `storage` 段与 `retention` 段映射为 `storage.Config`，主服务与迁移工具共用。
- memory 后端下 `Locations` 为 `nil`，`recordLocationHistory` 直接跳过。

> 现状评价：仓储接口化已经完成，但**业务层仍通过包级单例取仓储**。
> 出口可插拔的第一道结构性障碍不是仓储接口本身，而是 `processFunc` 无法接收实例依赖。

### 2.6 `internal/runtime` 的定位

`runtime.Stores` 持有 9 类状态：

| 字段 | 是否天然节点本地 | 说明 |
|---|---|---|
| `Conn` | 是 | 持有 `*model.Session` 与活连接，无法跨节点迁移 |
| `Fragment` | 是 | 上行分包重组缓存，按手机号组织 |
| `Retrans` | 是 | 下行分片与多媒体重传缓存 |
| `Heartbeat` | 是（但保活判定需要跨节点视图） | 进程内最近通信时间；SQL 后端不持久化 |
| `ParamResponses` / `PropertyResponses` / `LocationResponses` / `RegionResponses` | 是 | 等待应答的 API 请求，绑定发起查询的进程 |
| `Tracking` | 是 | 0x8202 设置的上报周期/有效期，仅服务于本进程模拟器逻辑 |

结论：`runtime` 的所有状态都**不应**进入 Sink，也不应进入 MQ。
分布式下只能通过「会话归属节点路由 + 共享在线态/应答转发」解决，而不是把这些状态序列化出去。

### 2.7 `internal/action` 钩子机制

接口与事件：

| 项 | 现状 |
|---|---|
| 事件类型 | `OnRegister`、`OnAuth`、`OnLocation`、`OnAlarm`、`OnDisconnect` |
| 返回值 | 无。注释明确「任何失败都不得影响协议主流程」 |
| 触发点 | 注册/鉴权在 `processFunc` 之后；位置/报警来自 `dispatchHooks` 中的 `0x0200` 与 `0x0704` 逐条；断连来自 `serveSession` 的 defer |
| 与业务处理顺序 | `dispatchHooks` 在 `processFunc` 成功之后执行；`processFunc` 失败则钩子也不触发 |
| 默认实现 | `action.Noop{}` |
| 示例实现 | `action.HTTPCallback`：容量 256 的有界 channel、单 worker 顺序发送、队列满丢弃并告警 |
| HTTP 失败 | 网络错误或状态码 >= 400 只记日志，**不重试** |
| 投递语义 | at-most-once；无事件 ID；多协程入队时跨事件不保证全局有序 |
| 装饰器 | `alarmserver.UploadTrigger` 装饰 `Hooks`，在报警事件后自动下发 `0x9208` |

现有问题：

1. 位置与报警同时写仓储、又通过钩子外发，存在两套口径；`0x0704` 的报警只走钩子，不写 `Alarms`。
2. 钩子与业务落库没有共同的事件标识，下游无法把「HTTP 回调」与「数据库记录」对齐或去重。
3. 队列满直接丢弃，报警这类不可丢事件没有保底通道。
4. 业务处理失败会连带吞掉钩子，外部消费者无法感知「终端确实上报了、只是本地落库失败」。

### 2.8 配置结构

`Config` 当前顶层字段：`Log`、`Server`、`Client`、`Storage`、`Retention`、`AlarmServer`、`Admin`。
`Server` 下有 `Action.HTTP`（唯一的外部出口配置）、`AckTimeoutMS`、`MaxRetransmissions`、`Port`、`Banner`。

`configs/default.yaml` 是嵌入默认配置，`storage`、`retention`、`alarmServer`、`admin` 均为顶层段。
因此新增的上行出口配置应作为**顶层 `uplink` 段**，与 `storage`/`retention` 平级，而不是塞进 `server.action`：
`sink` 是业务数据出口策略，与监听端口无关。

### 2.9 每一类上行数据当前的处理归属

| 类别 | 上行消息 | 当前处理函数 | 实际依赖 | 是否落库 | 是否走钩子 |
|---|---|---|---|---|---|
| 会话与设备档案 | `0x0002` 心跳、`0x0003` 注销、`0x0100` 注册、`0x0102` 鉴权 | `processMsg0002/0003/0100/0102` | `Devices`、`Conn`、`Heartbeat`、`Fragment`、`Retrans` | SQL 后端仅静态档案；运行时字段不落库 | 注册/鉴权走 `OnRegister/OnAuth` |
| 位置 | `0x0200`、`0x0704` | `processMsg0200`、`processMsg0704` | `Devices`（存在性/ACC）、`Geo`、`Locations`、`Alarms`、`Industry`(0704) | `Geo` 内存投影、`Locations` SQL、`Alarms` SQL | `OnLocation`、`OnAlarm` |
| 报警 | `0x0200` 中 `alarmSign != 0`；`0x0704` 仅钩子 | 同上 | `Alarms` | `0x0200` 落 `alarms`；`0x0704` 不落 | `OnAlarm` |
| 参数应答 | `0x0104` | `processMsg0104` | `Params`、`ParamResponses` | 是 | 否 |
| 属性应答 | `0x0107` | `processMsg0107` | `Properties`、`PropertyResponses` | 是 | 否 |
| 查询应答 | `0x0201`、`0x0608` | `processMsg0201`、`processRegionResponse` | `LocationResponses`、`RegionResponses` | 否 | 否 |
| 行业上报 | `0x0301/0302/0303`、`0x0500`、`0x0700/0701/0702/0705`、`0x0800/0801/0802/0805`、`0x0900/0901`、`0x0108` | `processIndustryReport` | `Industry` | 绝大多数仅内存最新值；目录/升级/透传按快照落库 | 否 |
| 多媒体元数据 | `0x1205` 资源列表、`0x0801` 上传片段元数据 | `processIndustryReport`（与行业上报共用） | `Industry` | 内存；无独立媒体元数据仓储 | 否 |
| 附件信令 | `0x1210/1211/1212` | `processAttachmentSignaling` | 无（仅日志） | 否 | 否 |
| 传输可靠性 | `0x0001`、`0x0005`、`0x8003`、`0x8800` | `processMsg0001/0005/8003/8800` | `Retrans`、`Session`、`Alarms`(确认) | 否 | 否 |
| 下行命令/模拟器 | `0x8104/8106/8107`、`0x8202`、`0x8301/8303/8401`、`0x8600` 系列、`0x8700`、`0x8802/8803/8804/8805`、`0x9205` 等 | 多个 `processMsg*` | `Params`、`Industry`、`Regions` | 部分是终端配置目录 | 否 |

### 2.10 现状缺口清单

| 编号 | 缺口 | 严重度 | 依据 |
|---|---|---|---|
| G1 | 业务写入与协议应答串行，DB 慢直接推迟 `0x8001`，终端会重传 | 高 | `processFunc` 在 `encode/send` 之前 |
| G2 | 业务错误让本轮不应答，部分错误关闭连接 | 高 | `Process` 返回错误，`serveSession` 对 `ErrDeviceNotFound` 直接返回 |
| G3 | 业务落库失败会连带跳过 `dispatchHooks` | 中 | `dispatchHooks` 位于 `processFunc` 成功之后 |
| G4 | 同一报警在 `0x0200` 落库、`0x0704` 只走钩子，口径不一致 | 中 | `processMsg0704` 注释与 `dispatchHooks` 行为 |
| G5 | 钩子只有 at-most-once，队列满丢弃，无重试/顺序/事件 ID | 中 | `internal/action/http.go` |
| G6 | 仓储写入无单次超时、无全局并发上限，多会话同时写 DB 会拖垮连接池；部分处理器显式使用 `context.Background()` | 高 | `processMsg0104/0107` 等直接调用仓储 |
| G7 | 设备状态更新使用整行 `Upsert` 读改写；SQL 后端只写静态列，运行时字段实际丢失 | 中 | `processMsg0200/8001`、`sqlstore/device_repository.go` |
| G8 | 查询应答依赖进程内 broker，分布式下响应可能落到错误节点 | 高（演进时） | `ParamResponses`、`LocationResponses` 等 |
| G9 | 注册表混合上行、下行、模拟器三种语义，无法安全地统一挂 Sink | 中 | `initProcessOption` |

---

## 3. 目标形态

### 3.1 分层原则

```text
+-----------------------------------------------------------------------+
| 协议内核（必须同步、无外部 I/O）                                      |
| 拆帧/转义/校验、解码/编码、分包重组、应答生成、会话与运行时状态       |
+-------------------------------+---------------------------------------+
                                | decoded message + protocol context
                                v
+-----------------------------------------------------------------------+
| 业务处理器（纯逻辑，无仓储/网络调用）                                 |
| 校验业务字段、构造归一化记录、计算 EventID、决定记录类别               |
+-------------------------------+---------------------------------------+
                                | []uplink.Record
                                v
+-----------------------------------------------------------------------+
| 出口 Dispatcher / Router（可替换）                                     |
| sync 或 async；按类别选择 local-db / noop / mq；失败策略与背压         |
+-------------------------------+---------------------------------------+
                                v
+-----------------------------------------------------------------------+
| Sink 适配器                                                           |
| local-db：映射到 repository；noop：丢弃；mq：未来 Kafka/Redis Stream   |
+-----------------------------------------------------------------------+
```

三条硬规则：

1. **协议内核不得调用可替换 Sink。** 解码、应答、会话顶替、分包重传、查询应答投递必须保持同步与确定性。
2. **业务处理器不直接调用仓储或网络。** 它只产出 `Record`；是否落库、是否外发由 Router 决定。
3. **应答不等待 Sink。** 协议应答只依赖协议内核结论；业务失败不得阻断应答（设备存在性等协议前置校验除外）。

### 3.2 处理器内部逻辑与可替换出口的边界

| 内容 | 归属 | 理由 |
|---|---|---|
| 消息解码、字段校验、协议版本适配 | 处理器内部逻辑 | 无 I/O，结果决定应答 |
| 注册/鉴权判定、鉴权码生成、会话绑定/顶替 | 处理器内部逻辑 + 必需的 `DeviceRepository` 依赖 | 判定结果直接影响 `0x8100/0x8001` 与连接是否关闭；不能异步 |
| 心跳刷新、保活判定、分包/重传缓存 | 处理器内部逻辑 + `runtime` | 节点本地状态，跨节点无法序列化 |
| 查询应答投递（`0x0201/0x0608/0x0104/0x0107` 的 broker） | 处理器内部逻辑 | 等待方是发起查询的进程；必须本地投递 |
| 位置历史、报警历史、行业上报、参数档案、属性档案、媒体元数据 | **可替换 Sink** | 业务事实，可落本地库、可外发 MQ |
| 最新定位投影 `Geo`、行业最新值 `Industry` | **本地投影**（始终本地，不选 Sink） | 服务于本节点 API 与下行查询；分布式需另建共享读模型 |
| 报警确认关联（`0x0001` 应答 `0x8203`） | 处理器内部逻辑 + 本地协调态 | 关联发起节点的命令流水号；分布式需应答路由 |
| 苏标附件接收、1078 RTP/PS 落盘 | 独立服务，不在本设计内 | 已有独立端口与独立生命周期 |
| `0x9208` 自动下发等下行动作 | `Hooks` 装饰器，不在 Sink 内 | 与业务数据出口无关 |

> 重要结论：不是所有 I/O 都能变成 Sink。
> 注册/鉴权所需的设备档案读取、查询应答 broker、运行时状态属于「协议正确性依赖」，必须保留为处理器必需依赖；
> 只有「终端上报的业务事实」才进入可替换出口。
>
> 投影与 Sink 解耦：`Geo` 与 `Industry` 最新值由处理器内的 projection writer 始终写入，与 Sink 选择无关；
> `local-db` 只承担耐用事实（定位历史、报警历史、参数/属性档案、目录快照），不重复写投影。

### 3.3 归一化记录模型

```go
package uplink

// Category 是业务出口的配置与路由单元。
type Category string

const (
    CategoryLocation   Category = "location"
    CategoryAlarm      Category = "alarm"
    CategoryIndustry   Category = "industry"
    CategoryParams     Category = "params"
    CategoryProperties Category = "properties"
    CategoryMediaMeta  Category = "media-meta"
)

// EventID 是跨进程稳定的记录标识，用于下游幂等、outbox 去重与问题追踪。
// 建议组成：phone|MsgID|serial|itemIndex|occurredAt。
// 同一报文的终端重传必须得到相同 EventID；receivedAt 不参与计算。
type EventID string

// Record 是所有类别的统一信封。生产者只构造它，不知道最终出口。
type Record struct {
    SchemaVersion uint16
    EventID       EventID
    Category      Category
    Phone         string
    MessageID     uint16
    Serial        uint16
    OccurredAt    string    // 协议时间（BCD 字符串），无则为空
    ReceivedAt    time.Time // 平台接收时间
    Payload       any       // 类别专属结构，见 3.4
}
```

各类别 Payload（字段级草案，不含实现）：

```go
type LocationPayload struct {
    MsgID      uint16
    AlarmSign  uint32
    StatusSign uint32
    Latitude   uint32
    Longitude  uint32
    Altitude   uint16
    Speed      uint16
    Direction  uint16
    Time       string
    ExtItems   []model.LocationExtItem
}

type AlarmPayload struct {
    Serial    uint16
    AlarmSign uint32
    Latitude  uint32
    Longitude uint32
    Time      string
    ExtItems  []model.LocationExtItem
    ExtRaw    []byte
    AlarmID   []byte // 苏标 16 字节报警标识号
}

type IndustryPayload struct {
    Message model.JT808Msg // 由 MQ 适配器按 MsgID 选择显式编码器
}

type ParamsPayload struct {
    Params *model.DeviceParams // 深拷贝快照
}

type PropertiesPayload struct {
    Properties *model.TerminalProperties // 深拷贝快照
}

type MediaMetaPayload struct {
    Kind              string // resource-list | upload-metadata
    ResourceList      *model.Msg1205
    MediaID           uint32
    MediaType         uint8
    MediaContainer    uint8
    MediaEventID      uint8
    LogicChannelID    uint8
    FragmentBytes     int
    // 0x0801 的媒体字节不属于元数据出口，由附件/1078 链路单独处理。
}
```

约束：

- Payload 必须是可序列化的稳定契约，不能携带 `*model.Session`、连接、函数或带锁对象。
- 行业上报不要依赖默认反射 JSON；MQ 适配器应为每个受支持 MsgID 注册显式编码器，避免内部字段泄漏与版本漂移。
- `EventID` 的组成必须由统一的构造函数生成，禁止各处理函数自行拼字符串。
- `EventID` 只要求在去重/重放窗口内唯一；终端流水号回绕后允许产生重复，消费方仍需结合 `category` 与时间窗口判断。

### 3.4 Sink 接口草案

```go
// Sink 是业务数据的出口。实现必须并发安全。
type Sink interface {
    // Name 返回配置中使用的稳定名称，例如 "local-db"、"noop"、"mq"。
    Name() string
    // Deliver 批量投递一组同一类别的记录。
    // 返回 error 表示本批整体失败；部分成功语义必须由实现显式声明。
    Deliver(ctx context.Context, records []Record) error
}

// Router 按类别选择 Sink，并把同一批记录分组分发。
type Router struct {
    sinks map[Category][]Sink
    mode  DeliveryMode // sync | async
}

func (r *Router) Deliver(ctx context.Context, records []Record) error

// Dispatcher 是业务处理器看到的唯一出口。
// sync 实现直接调用 Router；async 实现先入有界队列，再由 worker 调用 Router。
type Dispatcher interface {
    Emit(ctx context.Context, records ...Record) error
    Close(ctx context.Context) error
}
```

配套实现形态（仅命名与职责）：

| 组件 | 职责 |
|---|---|
| `localsink` | 把 `Record.Payload` 映射到现有 `repository.Stores` 方法；1:1 复刻当前行为 |
| `noopsink` | 显式丢弃，用于纯接入网关或排障 |
| `mqsink`（未来） | 按类别/手机号分区投递到 Kafka 或 Redis Stream |
| `projection` | 始终本地写 `Geo`/`Industry` 最新值；不经 Router 选择 |
| `syncDispatcher` | 同步调用；保留当前语义，默认模式 |
| `asyncDispatcher` | 每类别一个有界 channel + 单 worker；满时按策略丢弃或阻塞 |

### 3.5 与 `action.Hooks` 的关系

不合并，二者语义不同：

| 维度 | `action.Hooks` | `uplink.Sink` |
|---|---|---|
| 定位 | 生命周期旁路通知 | 业务事实投递 |
| 事件 | 注册/鉴权/位置/报警/断连 | 六类业务记录 |
| 失败语义 | 无返回值，失败不影响协议 | 返回 error，由 Dispatcher 决定记录/丢弃/重试 |
| 投递 | 单 worker、队列满丢弃、无重试 | 按类别配置，报警可走同步或 outbox |
| 标识 | 无事件 ID | 统一 `EventID` |

过渡期建议：

1. `Hooks` 继续保留，`HTTPCallback` 继续作为示例出口。
2. 位置/报警会同时进入本地库与钩子，这是现有行为；文档必须明确「下游可能收到两份语义相近的数据」。
3. 后续可让 `Hooks` 的事件携带 `EventID`，但不把 `HTTPCallback` 改写成 Sink，避免一次性扩大改动面。

### 3.6 明确不进入 Sink 的硬边界

- 注册、鉴权、心跳、注销、会话顶替、鉴权失败清理。
- 上行分包重组、下行分片补传、`0x8003/0x0005/0x8800` 重传。
- 协议应答生成与发送。
- `0x0201`、`0x0608`、`0x0104`、`0x0107` 的 broker 投递。
- `0x0001` 对 `0x8203` 的报警确认关联。
- 苏标附件服务器的缺口记账、`0x9212` 回报、完成态 TTL。
- 1078 RTP/PS 接收与 `.h264` 落盘。
- 模拟器/下行命令对 `Industry`、`Regions`、`Params`、`Media` 的本地目录读写。

> 模拟器与下行处理必须始终使用本地仓储，不受 `uplink` 配置影响；
> 否则客户端模拟器在 `noop`/`mq` 配置下会失去参数、区域、媒体回放能力。

---

## 4. 配置驱动

### 4.1 配置位置

在 `internal/config/config.go` 的 `Config` 中新增：

```go
type Config struct {
    // ... 现有字段
    Uplink *uplinkConf `yaml:"uplink"`
}

type uplinkConf struct {
    Delivery   *uplinkDeliveryConf            `yaml:"delivery"`
    Categories map[string][]string            `yaml:"categories"`
    MQ         *uplinkMQConf                  `yaml:"mq"`
}
```

新增 `(c *Config) ParseUplink() (UplinkOptions, bool, error)`，风格对齐现有 `ParseAlarmServer()`/`ParseAdmin()`：

- 返回归一化后的 `UplinkOptions` 与 `enabled`；
- 非法枚举、未知类别、未知 Sink 名称、缺失 MQ 必需字段时返回错误；
- 主服务启动时校验失败直接 `os.Exit(1)`。

`configs/default.yaml` 增加顶层 `uplink` 段，并保持「整段缺省 = 现有行为」。

### 4.2 默认配置草案

```yaml
# 终端上行业务数据的出口配置。
# 整段缺省时等价于 sync + 全部类别 local-db，行为与改造前一致。
uplink:
  delivery:
    mode: "sync"        # sync | async
    queueSize: 4096     # async 模式下每个类别的队列容量
    workers: 1          # async 模式下每个类别的 worker 数；首期固定 1 保证顺序
    overflow: "drop"    # drop | block；drop 记录指标并丢弃新记录
  categories:
    location: ["local-db"]
    alarm: ["local-db"]
    industry: ["local-db"]
    params: ["local-db"]
    properties: ["local-db"]
    media-meta: ["local-db"]
  # 未来接入 MQ 时启用；当前实现不应接受未实现的 sink 名称
  mq:
    backend: ""         # kafka | redis-stream
    brokers: []
    topicPrefix: "jt808.uplink"
```

### 4.3 校验规则

| 配置项 | 规则 | 失败处理 |
|---|---|---|
| `delivery.mode` | 仅 `sync`/`async`；缺省 `sync` | 启动报错 |
| `delivery.queueSize` | `async` 时必须 > 0；`sync` 时忽略 | 启动报错 |
| `delivery.workers` | 必须 > 0；首期只允许 1 | 启动报错 |
| `delivery.overflow` | `drop`/`block`；`sync` 时忽略 | 启动报错 |
| `categories.*` | 键必须与 `uplink.Category` 常量值完全一致（`location`/`alarm`/`industry`/`params`/`properties`/`media-meta`）；值必须是非空且去重的 Sink 名称列表 | 启动报错 |
| Sink 名称 | 首期仅 `local-db`、`noop`；`mq` 在实现落地前拒绝 | 启动报错并提示未实现 |
| 类别缺省 | 未配置的类别取 `["local-db"]` | 不报错，保持兼容 |
| `mq` 段 | 仅当某类别引用 `mq` 时校验 | 启动报错 |

启动时应打印一张有效路由表（类别 → Sink 列表 → 模式），便于运维核对。

### 4.4 按类别分别配置的可行性

机械上完全可行：`Categories` 就是显式的类别到 Sink 列表映射，Router 按 `Record.Category` 分组。
但有七个语义约束必须写进设计，否则配置能启动、行为却是错的：

| 约束 | 影响 | 处理 |
|---|---|---|
| `location = noop` | 最新定位投影仍写入；定位历史不再落库 | 允许；`0x8201` 与 API 最新定位不受影响 |
| `params/properties = noop` | 档案不落库，但 broker 投递仍来自解码结果 | 允许；查询应答不受影响 |
| `industry = noop` | 行业最新值投影仍写入；耐用目录/历史不再落库 | 允许；模拟器/下行处理仍走本地仓储 |
| `alarm = noop` | 报警历史与确认关联失效 | 建议启动时告警；生产默认禁止 |
| `media-meta = local-db` | 当前没有独立媒体元数据仓储，只能写 `Industry` 内存缓存 | 首期在文档中明确该类别不会产生独立持久化 |
| `local-db + mq` 双写 | 两份数据可能不一致；MQ 失败不影响本地 | 允许作为过渡，但必须声明 at-most-once 与不一致窗口 |
| 所有类别都改用 `mq` | 最新值投影仍在本节点；依赖耐用历史的 API 失去数据来源 | 需要独立读模型；本期不做 |

### 4.5 首期推荐默认

| 类别 | 默认 Sink | 投递模式 | 理由 |
|---|---|---|---|
| `location` | `local-db` | sync | 当前行为；高频但单机可接受，后续可切 async |
| `alarm` | `local-db` | sync | 不可丢；确认关联依赖本地仓储 |
| `industry` | `local-db` | sync | 当前行为；SQL 后端多数只写内存，后续按需求外发 |
| `params` | `local-db` | sync | 应答时延已在当前行为内 |
| `properties` | `local-db` | sync | 同上 |
| `media-meta` | `local-db` | sync | 元数据量小，保留可查询性 |

---

## 5. 可靠性缺口与投递语义

### 5.1 同步写入对协议路径的反压

现状时序：

```text
终端报文 -> 解码 -> 生成 0x8001 -> processFunc（DB 写） -> encode 0x8001 -> send
                                   ^^^^^^^^^^^^^^^
                                   这里的耗时全部计入终端等待应答的时间
```

后果：

1. DB 延迟超过 `server.ackTimeoutMs`（默认 5000 ms）时，终端会重传。
2. 重传会再次走完整处理：`0x0200` 报警可按 `(phone, serial)` 在 `DedupWindow` 内去重，
   但定位历史会重复追加；行业最新值幂等，媒体元数据可能重复覆盖。
3. 每会话一个 goroutine，没有全局并发上限；大量会话同时写 DB 会打满连接池，
   延迟进一步放大，形成正反馈。
4. 仓储调用没有独立的单次超时（部分处理器显式使用 `context.Background()`），SQL 故障时可能长时间挂起。
5. `Industry.Record`、`Alarms.Record` 等大多只记日志；`0x0200` 的设备查找与地理信息解析错误会向上返回，
   直接表现为「不应答」。

### 5.2 错误语义需要拆成两类

| 错误类型 | 例子 | 应该的行为 |
|---|---|---|
| 协议前置错误 | 解码失败、消息体畸形、鉴权不通过、`ErrDeviceNotFound` | 影响本轮应答或连接生命周期 |
| 业务出口错误 | 定位历史写入失败、行业上报写失败、MQ 不可用 | 只记录指标与日志；**不得阻断应答**，不得关闭连接 |

当前实现没有区分这两类：`processFunc` 返回的任何错误都会中断 Pipeline。

设计要求：

```go
// 处理器返回的 error 只表示协议结论；业务出口错误由 Dispatcher 自行消费。
// 设备存在性等协议前置校验仍在处理器内返回 error。
```

这意味着改造后 `processMsg0200` 不应再因 `Alarms.Record`/`Locations.Record` 失败返回 error；
这些错误由 Sink 记录并计数。

### 5.3 投递模式与语义矩阵

| 模式 | 应答时延 | 丢失 | 重复 | 顺序 | 复杂度 | 适用类别 |
|---|---|---|---|---|---|---|
| `sync + local-db`（现状） | 包含写库耗时 | 写失败可能表现为不应答 | 终端重传可能重复 | 会话内天然有序 | 低 | 默认、报警、参数、属性 |
| `async + local-db` | 仅入队耗时 | 队列满丢弃 | 基本不重复（除非重传） | 单 worker 保持到达顺序 | 中 | 定位历史、行业上报 |
| `async + mq` | 仅入队耗时 | 队列满或进程崩溃 | 可能重复，取决于实现 | 按手机号分区有序 | 高 | 跨系统集成 |
| `async + mq + outbox` | 仅入队/写 outbox | 不丢（除非磁盘故障） | 必然可能重复，需要幂等 | 按 EventID 重放 | 高 | 报警、运单等不可丢数据 |

语义口径必须写死：

- `sync + local-db` 不宣称 at-least-once：写失败时终端可能重传，但服务端没有重试保证。
- `async` 首期实现 at-most-once：队列满丢弃，进程崩溃丢队列。
- `outbox` 只承诺 at-least-once；**不承诺 exactly-once**。
- 任何模式下，下游都必须按 `EventID` 幂等。

### 5.4 是否需要 outbox，边界在哪

判断标准：

| 条件 | 结论 |
|---|---|
| 数据可丢（定位历史、普通行业上报） | 不需要 outbox；有界队列 + drop 即可 |
| 数据不可丢（报警、运单、监管类上报） | 需要 outbox 或保持同步本地写 |
| 外部系统要求可重放 | 需要 outbox |
| 只是本地 SQL 落库 | 不需要 outbox；数据库事务本身就是持久化 |

边界：

1. outbox 属于**投递可靠性机制**，不属于业务处理器；处理器仍然只产出 `Record`。
2. outbox 只有在「本地库 + 外部 MQ」同时存在时才有意义：
   业务记录与 outbox 行在同一事务内写入，后台 relay 再发布并标记。
3. 纯 MQ 部署（不落本地库）时，outbox 退化为本地 WAL；仍然需要磁盘与清理策略。
4. outbox 表不进入本轮实现；本轮只需保证 `EventID` 稳定、Payload 可序列化，避免以后返工。

未来表结构草案（仅设计，本轮不建表）：

```sql
CREATE TABLE uplink_outbox (
    event_id      TEXT    NOT NULL,
    category      TEXT    NOT NULL,
    phone         TEXT    NOT NULL,
    payload_json  TEXT    NOT NULL,
    status        TEXT    NOT NULL,
    attempts      INTEGER NOT NULL,
    created_at    INTEGER NOT NULL,
    dispatched_at INTEGER,
    PRIMARY KEY (event_id)
);
```

约束：SQLite/MySQL 双兼容；TEXT/BLOB 不使用字面量默认值；主键显式 `NOT NULL`；
迁移文件按 `<描述>_<YYYYMMDD>.sql` 命名；主服务启动只校验版本，不执行 DDL。

### 5.5 顺序与幂等

- 分区键统一为 `phone`；只保证**同一手机号内**的到达顺序，不保证全局顺序。
- 定位类记录按 `occurredAt` 业务排序，允许网络乱序；消费方不得假设到达顺序等于时间顺序。
- 报警去重继续使用现有 `(phone, serial)` + `DedupWindow` 语义；MQ 下游额外用 `EventID` 去重。
- `0x0704` 一条报文产生多条位置记录，`itemIndex` 必须进入 `EventID`。

### 5.6 应答路径的硬规则

无论 Sink 选择什么，必须满足：

1. `0x8001` 的生成与发送只依赖协议内核结论。
2. 业务 Sink 失败不改变应答结果码，不关闭连接。
3. 设备不存在、鉴权失败等协议前置错误仍按现有语义处理。
4. 异步模式下，进程退出前 `Dispatcher.Close(ctx)` 必须尽量排空队列；排空失败只记日志，
   不得阻塞优雅停机超过一个明确上限。

---

## 6. 改造步骤

每一步都要求可独立提交、可独立回归；提交前执行
`gofmt -l .`、`go build ./...`、`go vet ./...`、`go test -race -count=1 ./...`。

| 步骤 | 内容 | 涉及文件 | 影响面 | 触及主路径 | 风险 | 回滚方式 |
|---|---|---|---|---|---|---|
| S0 | 本文档与基线核查 | `docs/uplink-sink-design.md` | 无 | 否 | 无 | 删除文档 |
| S1 | 注册表拆分与元数据标注：按「服务端上行 / 平台下行 / 模拟器」分组；为每个上行入口标注类别 | `internal/protocol/msg_processor.go`、相关测试 | 表结构可读性 | 是（仅注册表构造） | 低 | 还原注册表 |
| S2 | 依赖注入骨架：`ProcessorDeps`、处理函数改方法、`initProcessOption` 改方法、Pipeline/Server/Client 传递依赖；保留 `Default()` 回退 | `msg_processor.go`、`pipeline.go`、`tcp_server.go`、`udp_server.go`、`internal/client/tcp_client.go`、`cmd/jt808-server/main.go`、测试 | 39 个处理函数 + 3 个辅助函数、74 个注册项 | 是（构造与签名） | 中（机械冲突多） | 保留旧构造函数薄封装 |
| S3 | 归一化记录与 `local-db` 适配器：按类别产出 `Record`，`localsink` 1:1 映射现有耐用仓储，投影保持直写；行为不变 | 新增 `internal/uplink/`；`msg_processor.go`；`storage` 装配 | 六类业务写入 | 是（处理函数内部） | 中 | 每个类别独立开关，可逐类回退 |
| S3a | 位置与报警类别 | `processMsg0200/0704`、`recordLocationHistory` | `Geo` 投影直写 + `Locations`/`Alarms` 耐用事实 | 是 | 中 | 保留旧分支 |
| S3b | 行业上报类别 | `processIndustryReport` 的 16 个上行注册项（该函数共注册 19 个 MsgID，其中 `0x8108/0x8400/0x8900` 为下行语义；`0x0801/0x1205` 的媒体元数据归 S3d） | `Industry` 投影直写 + 目录快照走 Sink | 是 | 中 | 同上 |
| S3c | 参数与属性类别 | `processMsg0104/0107` | `Params`、`Properties`、broker | 是 | 中 | 同上 |
| S3d | 多媒体元数据类别 | `0x1205`、`0x0801`（`processIndustryReport`） | `Industry` 投影；无独立耐用目标 | 是 | 中 | 同上 |
| S4 | 错误语义分离：业务 Sink 错误不再中断 Pipeline；仅协议前置错误影响应答/连接；增加按类别失败计数 | `msg_processor.go`、`pipeline.go`、`server`、测试 | 应答与连接行为 | 是（语义变化） | 中高 | 配置项临时恢复旧语义 |
| S5 | 配置化 Sink 与 `noop`：`config.Uplink`、`ParseUplink`、Router、启动装配、有效路由表日志；默认 `local-db` | `config.go`、`default.yaml`、`cmd/jt808-server/main.go`、新增 `internal/uplink/router.go` | 启动装配 | 是（仅启动） | 低 | 删除配置段即回退 |
| S6 | 异步投递（可选，默认关闭）：有界队列、单 worker/类别、drop 策略、队列深度与丢弃指标 | `internal/uplink/async.go`、`config.go`、`main.go`、测试 | 应答时延与顺序 | 是（开启后时序） | 高 | 将 `mode` 改回 `sync` |
| S7 | MQ Sink 与 outbox（未来）：独立设计、独立迁移、独立提交 | `internal/uplink/mqsink`、迁移 SQL、relay | 跨系统投递 | 否（不改变现有路径，新增出口） | 高 | 关闭类别 MQ 配置 |

步骤间的依赖与建议节奏：

1. **S1、S2 必须先做。** 没有显式依赖与角色拆分，后续 Sink 无法安全落地。
2. **S3 按类别拆成 4 个提交。** 每类都能单独回归，避免一个提交同时改定位、报警、行业、媒体。
3. **S4 建议在 S3 完成后立即做。** 它是语义变化点，越晚做，后续异步化的风险越大。
4. **S5 只改启动装配。** 默认值不变时，回归范围可控。
5. **S6 必须作为独立实验特性。** 不建议默认开启；需要补充「慢 DB」「队列满」「进程退出」三类测试。
6. **S7 不在本轮范围。** 本轮只需把 `EventID`、Payload 契约、配置形状定好。

### 6.1 每一步的验收口径

| 步骤 | 验收 |
|---|---|
| S1 | 注册项数量与行为快照一致（74 个 MsgID / 39 个处理函数）；按角色分组的表可枚举 |
| S2 | 不注入依赖时行为与改造前一致；注入独立 `storage.NewMemory()` 后测试互不污染；`Default()` 回退仅剩装配层 |
| S3 | 同一组报文在改造前后产生相同的仓储状态（逐字段断言）；API 查询结果不变 |
| S4 | 模拟仓储错误时仍返回 `0x8001`；设备不存在时仍关闭连接；错误计数可观测 |
| S5 | 缺省配置行为不变；`noop` 下协议应答正常、业务落库为空；非法配置启动失败 |
| S6 | 队列满不阻塞协议读循环；同手机号记录顺序稳定；关闭时按上限排空 |

---

## 7. 分布式演进覆盖度

### 7.1 接入 MQ 时，业务侧需要改什么、不需要改什么

| 项目 | 是否需要改 | 说明 |
|---|---|---|
| 协议解码/编码、分包、应答 | 不需要 | 与出口无关 |
| `processOptions` 注册表与处理函数 | 不需要 | 只产出 `Record` |
| `Record` / `EventID` / Payload 契约 | 不需要（前提是本轮定好） | 这是本轮最重要的前瞻性约束 |
| `local-db` 适配器 | 不需要 | 继续服务单机与过渡期双写 |
| MQ Sink 适配器 | 需要新增 | 编码、分区、生产者、重试、指标 |
| `uplink` 配置 | 需要扩展 | 增加 `mq` 段与类别路由 |
| API 读模型 | 需要 | 若关闭 `local-db`，依赖耐用历史的 API 不能再读本地仓储；最新值投影仍可用 |
| `0x0104/0x0107/0x0201/0x0608` 应答路由 | 需要 | broker 是进程内的，必须回到发起查询的节点 |
| 报警确认关联 | 需要 | `0x8203` 由节点 A 下发、`0x0001` 可能在节点 B 到达 |
| 设备注册/鉴权 | 需要（若尚未共享） | 设备档案必须共享耐用存储；会话仍由归属节点持有 |
| 在线态/保活 | 需要 | 需要共享 presence 或按手机号粘性路由 |
| `runtime` 内部状态 | 不需要 | 保持节点本地 |
| `action.Hooks` 契约 | 不需要 | 可另行增加 MQ Hook，不影响本设计 |
| 苏标附件服务、1078 媒体服务 | 不需要（本轮） | 已是独立端口；跨节点文件共享另行设计 |

### 7.2 必须现在写进设计的假设

| 编号 | 假设 | 不写会怎样 |
|---|---|---|
| A1 | 分区键是 `phone`；只保证同手机号内顺序 | 下游按全局顺序消费会得到错误结论 |
| A2 | `EventID` 由 `phone|MsgID|serial|itemIndex|occurredAt` 稳定生成；`receivedAt` 不参与 | 重传会生成新 ID，下游无法去重 |
| A3 | at-least-once 按类别显式选择，永不宣称 exactly-once | 运维会误以为不会重复 |
| A4 | 每类记录明确「快照」还是「事件」语义；`Industry` 最新值属于投影 | 下游会把覆盖写当追加事件 |
| A5 | 不保证跨类别事务；报警与位置可能各到各的 | 下游假设原子性会出错 |
| A6 | 应答不依赖 Sink 成功 | 异步化后应答会被业务故障拖死 |
| A7 | 关闭 `local-db` 后依赖耐用历史的 API/查询不可用，除非提供读模型；最新值投影仍在本节点 | 配置能启动但历史查询静默变空 |
| A8 | 模拟器/下行处理永远使用本地仓储，不受 `uplink` 配置影响 | 客户端模拟器功能被配置误伤 |
| A9 | `Record` 带 `SchemaVersion`；MQ 编码按 MsgID 显式选择 | 结构升级后旧消费者解析失败 |
| A10 | 敏感字段（`0x0702` 驾驶员身份等）在 MQ 编码前可脱敏 | 个人信息随消息队列扩散 |

### 7.3 分布式下的数据归属

| 数据 | 归属 | 演进方式 |
|---|---|---|
| 会话、手机号绑定、分包、重传 | 单节点 | 按手机号粘性路由或会话归属注册表 |
| 心跳最近时间、保活判定 | 单节点 + 共享 presence | Redis TTL 副本或独立在线态服务 |
| 设备静态档案 | 共享耐用存储 | 已有 `DeviceRepository`；写入需字段级更新，避免整行读改写 |
| 位置最新值（`Geo`） | 本地投影或共享缓存 | 0x8201/API 需要时读共享缓存，或路由到归属节点 |
| 位置历史、报警、行业上报、参数、属性、媒体元数据 | 共享耐用存储或 MQ + 读模型 | Sink 适配器决定 |
| 查询等待项（4 个 broker） | 单节点 | 应答按原命令归属节点路由 |
| 报警确认关联 | 单节点 | 命令与应答必须路由到同一节点，或改为共享关联表 |

### 7.4 失败模式与降级

| 失败 | 期望行为 |
|---|---|
| 本地 DB 慢 | sync 模式下应答延迟；async 模式下队列积压；超过容量按策略丢弃并计数 |
| 本地 DB 不可用 | 协议应答继续；业务记录丢失（at-most-once）；日志与指标告警 |
| MQ 不可用 | 协议应答不受影响；有 outbox 时落盘重试，无 outbox 时丢弃并计数 |
| 进程崩溃（async 模式） | 队列内未投递记录丢失；有 outbox 时重启可重放 |
| 队列满 | 默认丢弃新记录；`block` 策略只允许在明确评估后启用，且必须有超时 |
| 重复投递 | 下游按 `EventID` 幂等；服务端不承诺去重 |
| 查询应答落到错误节点 | 当前会超时；分布式前必须补齐归属路由 |

---

## 8. 未决问题（需要产品/架构决策）

| 编号 | 问题 | 影响 |
|---|---|---|
| D1 | 哪些类别必须 at-least-once（报警、运单、监管上报？） | 决定是否以及何时引入 outbox |
| D2 | API 是否接受最终一致性读模型 | 决定 MQ 阶段是否需要独立读服务 |
| D3 | 是否保留纯网关形态（全部 `noop`）作为正式部署选项 | 决定 noop 的文档与告警策略 |
| D4 | 位置/报警是否继续同时走 Sink 与 Hooks | 决定下游是否收到两份数据 |
| D5 | 设备在线态是否迁出进程内注册表 | 决定分布式保活方案 |
| D6 | 行业上报的 SQL 落库范围（当前仅目录/升级/透传） | 决定 industry Sink 的默认实现 |
| D7 | MQ 选型（Kafka / Redis Stream） | 不在本轮范围，但影响分区与顺序实现 |
| D8 | 敏感字段脱敏责任在生产者还是 MQ Sink | 决定 Payload 是否保留原始字段 |

---

## 9. 附录：核查口径

本文数值来自以下命令（在代码基线 `e241511` 上执行）：

```bash
grep -c "storage.Default()" internal/protocol/msg_processor.go   # 33
grep -c "runtime.Default()" internal/protocol/msg_processor.go   # 14
grep -c "^func processMsg" internal/protocol/msg_processor.go      # 32
grep -o "options\[0x[0-9A-Fa-f]*\] = &action" internal/protocol/msg_processor.go | sort -u | wc -l  # 66
grep -o "process: process[A-Za-z0-9_]*" internal/protocol/msg_processor.go | sort -u | wc -l       # 39
```

- 66 个字面量 MsgID 加上两个循环各 4 个 MsgID，得到 74 个不同 MsgID；
  其中 59 个注册项声明了 `process`，其余只声明 `genData`。
- 33 处 `storage.Default()` 按字段拆分：`Industry` 13、`Devices` 7、`Params` 4、`Regions` 3、
  `Geo` 2、`Alarms` 2、`Properties` 1、`Locations` 1。
- 14 处 `runtime.Default()` 按字段拆分：`Retrans` 3、`Heartbeat` 3、`Conn` 2、`Tracking` 1、
  四个 broker 各 1。
- 37 个函数直接引用默认单例（含 `bindPhoneSession`、`recordLocationHistory`、
  `retransmitCachedSegments` 三个辅助函数），其中 34 个是已注册处理函数。
- 全仓库（不含测试与文档）`storage.Default()` 共 48 处、`runtime.Default()` 共 39 处。

---

## 10. 一句话总结

**先把「协议正确性」和「业务事实投递」拆开，再把业务事实收敛成带 `EventID` 的归一化记录，
最后用配置选择这些记录的去向；协议内核不等待 Sink，Sink 失败不影响应答，
这样接入 MQ 时只需要新增出口和路由，而不需要重写任何上行处理函数。**
