# JT808 项目进展与 TODO 路线图

> **本轮基于生态调研重排（2026-11-03）。**
> 重排依据：[jt808-ecosystem-research.md](jt808-ecosystem-research.md) 第 7～10 节
> （四个同类项目的源码剖析、横向对比、借鉴清单与反面教材）。
> 代码基线：分支 `dev/p2-persistence`，提交 `f9ca3bc`（`docs: add JT808/JT1078 ecosystem research`）。
> 上一版路线图的基线是 `main@6a79797`（2026-11-01），其"最大缺口是持久化"的结论已经过期，
> 本轮按当前代码逐项复核后重写。

最近对齐时间：2026-11-03  
逐条协议消息矩阵见 [JT808-2019-coverage.md](JT808-2019-coverage.md)。  
行业 HTTP API 说明见 [industry-api.md](industry-api.md)。  
存储分层与多后端设计见 [storage-abstraction-design.md](storage-abstraction-design.md)。  
网关拆分（当前仅设计、无代码）见 [gateway-mode-design.md](gateway-mode-design.md)。

本文件跟踪项目级工作、实现深度、依赖关系与验收标准。覆盖矩阵仍是
附录 B 的逐消息索引。"已完成"表示代码中存在并在标注层级上验证过；
仅有编解码或应答并不代表终端侧业务流程完整。

**状态定义**

- **已完成**：代码存在，且有单测、模拟器路径或集成测试覆盖主流程。
- **部分完成**：主流程可用，但存在明确的边界、缺口或仅内存实现。
- **未实现**：没有代码路径，或只有设计文档。

---

## 一、现状总结（以当前 HEAD 代码为准）

### 1.1 一句话结论

项目已经越过"能收发报文"的阶段：**编解码、分包重组与补传、下行应答重试、
内存运行态、SQLite/MySQL 持久化与版本化迁移、五类业务回调**都有可执行代码和测试；
它现在缺的不是"协议正确性"，而是**商用闭环**——苏标附件、1078 媒体、下行投递语义、
多实例会话归属、可观测性与运维配套。

当前最大风险不是功能少，而是**两个正确性缺口**：

1. `Session.AcknowledgeResponse` 只按"手机号 + 应答消息 ID"清算，不校验应答流水号，
   同一终端连发两条下行命令时，第一条 `0x0001` 会把两条待确认全部清掉；
2. 连接注册表只有 `sessionID` 索引，同号重连后旧连接不会关闭，
   旧连接继续上报会覆盖 `device_runtime` 里的最新状态，API 也可能拿到旧 `sessionID`。

这两项都比"再加一个协议消息"更值得先修。

### 1.2 分领域清单

#### 1.2.1 协议消息覆盖

| 能力 | 状态 | 代码证据与说明 |
|---|---|---|
| JT808-2019 消息注册与分发 | 已完成 | `internal/protocol/msg_processor.go` 用 `processOptions map[uint16]*action` 表驱动，当前注册 **71 个消息 ID**，`genData` 负责实例化、`process` 负责业务。 |
| 消息编解码 | 已完成 | 按消息逐条手写 `Decode/Encode`（`internal/protocol/model/msg_*.go`），位运算读消息头（`header.go`），无反射、无代码生成。 |
| 转义/校验/长度校验 | 已完成 | `internal/protocol/packet_codec.go` 处理 `7D 02/7D 01`、BCC、`Attr.BodyLength` 一致性、单帧 ≤1023；`frame_handler.go` 的 `MaxFrameLen` 同时兜住 2019 消息头与双倍转义。 |
| 版本识别 | 部分完成 | 消息头只区分 `Version2019`（版本标识位=1）与 `Version2013`（=0）；**2011 只能在 `Msg0100.Decode` 里用消息体长度反推**（`msg_0100.go:41-47`），不是消息头级识别。 |
| 下行分包（0x0005） | 已完成 | 平台超长消息拆帧并缓存，终端可用 `0x0005` 指定帧请求重放。 |
| 上行分包（0x8003） | 已完成 | `segment_retransmission.go`：空闲 300ms 后请求缺片，最多 3 次，成功后清理。 |
| 0x0200 附加信息 | 部分完成 | `model/msg_0200.go` 只解析 `LocationExtItem{ID,Length,Raw}`，**没有类型化解析**；`OnAlarm` 只能拿到 `alarmSign != 0`，说不出 ADAS/DSM/TP 与报警类型。 |
| 0x0801 多媒体扩展定位项 | 部分完成 | 基础 28 字节定位体与媒体字节已解码；扩展定位项仍未实现（覆盖矩阵已标注）。 |
| RSA 加密（0x8A00/0x0A00） | 未实现 | `packet_codec.go` 对非 0 加密位直接返回 `ErrEncryptionUnsupported`。 |
| 保留/厂商自定义消息 | 未实现 | 无通用扩展分发；不认识的 ID 返回 `ErrMsgIDNotSupportted`。 |
| 标准消息覆盖完整性 | 部分完成 | 详见 `JT808-2019-coverage.md`；第 1 号修改单尚未评审，文档已声明不宣称符合性。 |

#### 1.2.2 会话与连接

| 能力 | 状态 | 代码证据与说明 |
|---|---|---|
| TCP/UDP 双栈接入 | 已完成 | `internal/server/tcp_server.go`、`udp_server.go`；TCP 默认 8080、UDP 8081。 |
| 连接注册表 | 部分完成 | `internal/runtime/connection_registry.go` **只有 `cacheByID map[string]*Session`**，`Get/Bind/Unbind/Count`，10s 打印连接数。 |
| 手机号 ↔ 会话索引 | 未实现 | `model.Session` 没有 `Phone` 字段；API 只能先 `devices.GetByPhone` 取 `SessionID` 再 `connRegistry.Get`（见 `internal/api/api.go:238-248`）。 |
| 同号重连顶替（踢旧连接） | 未实现 | `Bind` 是直接覆盖 map，不关闭旧连接；旧连接存活到保活超时。 |
| 鉴权流程 | 已完成 | `processMsg0100` 判重并下发 `0x8100`；`processMsg0102` 真实查设备并返回错误，不是空实现。 |
| 心跳与离线判定 | 已完成 | `keepalive.go` 用 `gron` 按设备 `Keepalive` 周期注册 `CheckDeviceJob`；在线失效置离线、离线超 3 倍保活清理缓存并关连接；`runtime.Heartbeat` 提供进程内更实时的心跳时间。 |
| 连接生命周期清理 | 部分完成 | 优雅停机 `storage.ResetRuntime` 清 `session_id/status`；异常重启靠读路径 `last_com_time_ms + keepalive_ms` 的 TTL 收敛，但**没有显式的"旧节点残留租约"处理**。 |

#### 1.2.3 分包与重传

| 能力 | 状态 | 代码证据与说明 |
|---|---|---|
| 上行分包重组 | 已完成 | `fragment_assembler.go`：键为 `phone/消息ID/首片原流水号`，乱序到达可归槽；`FirstFragmentSerial` 处理 1..65535 回绕。 |
| 分包缓存上限 | 已完成 | `segmentCacheTTL=2m`、`maxSegmentCacheEntries=1024`、`maxSegmentCacheBytes=16MB`、`maxSegmentCount=4096`，超限按最旧淘汰。**上一版路线图的"无容量上限"已不成立。** |
| 上行缺片补传 | 已完成 | `segment_retransmission.go`：300ms 空闲触发、最多 3 次、下发 `0x8003`。 |
| 媒体缺片补传 | 已完成 | `media_retransmission.go` 检测 `0x0801` 缺片并用 `0x8800` 请求；首片丢失降级为 `0x8003`。 |
| 下行应答与重试 | 部分完成 | `model/session.go` 的 `TrackReliable` 维护 `pending map[msgID<<16|serial]`：默认 5s 超时、最多 3 次重发、超时递增，重试耗尽后关闭连接。**但 `AcknowledgeResponse` 只匹配 `phone + responseID`，不校验 `0x0001.AnswerSerialNumber`，会误清同号多条待确认。** |
| 下行结果回传 | 未实现 | API 返回 `202 Accepted` 即结束；`Send` 是 fire-and-forget，调用方拿不到"终端是否应答"。 |
| 下行幂等 | 未实现 | 无 `command_outbox`、无幂等键；前端重试会重复下发。 |

#### 1.2.4 持久化与迁移

| 能力 | 状态 | 代码证据与说明 |
|---|---|---|
| 存储抽象 | 已完成 | `internal/storage/storage.go` 统一装配 `memory | sqlite | mysql`，`internal/repository` 定义契约。 |
| SQLite/MySQL 后端 | 已完成 | `internal/repository/sqlstore/store.go`，`Open/Migrate/ResetRuntime`；SQLite 使用 WAL。 |
| 覆盖的仓储 | 已完成 | `Devices`（`devices` + `device_runtime`）、`Params`、`Properties`、`Industry`、`Regions`、`Alarms`、`Locations` 共 7 类。 |
| 表结构 | 已完成 | 两份迁移：`20261101_init.sql`（`devices`/`device_params`/`terminal_properties`/`device_runtime`）、`20261102_persistence_tables.sql`（`terminal_catalogs`/`region_items`/`locations`/`alarms`）。 |
| 版本化迁移 | 已完成 | 文件名即版本（`<YYYYMMDD>_<描述>.sql`），`go:embed` 打包，`schema_migrations` 记录；**服务启动只校验版本，迁移由独立 `cmd/jt808-migrate` 执行**。 |
| 设备静态/运行时拆分 | 已完成 | `devices` 存身份与静态字段，`device_runtime` 存 `session_id/trans_proto/last_com_time_ms/status`；读用 `LEFT JOIN` 合并，写同事务按归属分写。 |
| 心跳落库节流 | 部分完成 | `server.heartbeatPersistIntervalMs` 已支持（0=逐条），但默认逐条写库；高频心跳场景仍需开启。 |
| 定位/报警历史 | 已完成 | `locations` 支持 `0x0200` 单条同步写与 `0x0704` 200 行分块批量写；`alarms` 有去重窗口、每设备上限、`0x8203` 确认。 |
| 区域/目录持久化 | 部分完成 | `region_items`、`terminal_catalogs`（`event/info_menu/phonebook/upgrade/transparent_down`）已落库；**媒体上传目录（`0x0801`/`0x0800`/`0x0802`/`0x1205`）仍只在内存**。 |
| 命令 outbox | 未实现 | `command_outbox` 只在设计文档中，代码与迁移均无。 |
| MySQL 方言约束 | 已完成 | 迁移只使用 SQLite/MySQL 共同语法；MySQL DDL 隐式提交限制已在设计文档说明。 |

#### 1.2.5 缓存与运行态

| 能力 | 状态 | 代码证据与说明 |
|---|---|---|
| 设备主数据缓存 | 已完成 | `repository` 内存实现；SQL 后端为 write-through（先 L1 再库），读未命中回源。 |
| 心跳运行态 | 已完成 | `internal/runtime/heartbeat_registry.go` 进程内最新心跳，读路径优先于库值。 |
| 定位/参数/属性/区域应答关联 | 已完成 | `location_response_broker.go`、`param_response_broker.go`、`property_response_broker.go`、`region_response_broker.go`，按流水号等待并投递。 |
| 临时定位跟踪 | 已完成 | `location_tracking.go` 维护临时上报间隔。 |
| 进程级单例 | 部分完成 | `storage.Default()`、`runtime.Default()`、`NewKeepaliveTimer()` 均为进程单例；**多实例共享同一 MySQL 时会彼此覆盖 `device_runtime`**。 |
| Redis/共享缓存 | 未实现 | 无 Redis 依赖；多节点无共享会话、无分布式锁、无 presence。 |

#### 1.2.6 管理 API

| 能力 | 状态 | 代码证据与说明 |
|---|---|---|
| HTTP 管理接口 | 已完成 | `internal/api/api.go` 使用 Gin，注册 **29 条路由**：设备查询/上报读取/定位/参数/属性、区域增删查、驾驶员查询、记录仪命令、媒体拍摄/检索/资源/命令、文本下发、事件设置、车辆控制、问题下发、信息菜单/服务、回拨、电话簿、终端升级、透传下发、报警查询/确认、参数设置。 |
| 统一下发入口 | 部分完成 | 每条路由各自构造 `model.Msg*` 并 `serv.Send(session.ID, msg)`；没有通用 `POST /send`，也不支持"只给手机号、由服务端解析会话"。 |
| 会话管理 API | 未实现 | 无 `/sessions`、无强制断开、无在线列表。 |
| 黑名单 | 未实现 | 无配置、无热更新、无拦截点。 |
| API 鉴权 | 未实现 | Gin 未挂任何 Token/签名中间件；默认监听 `httpPort: 8008`。 |
| 下发幂等/审计 | 未实现 | 无幂等键、无操作人、无审计日志。 |

#### 1.2.7 Hooks（业务事件出口）

| 能力 | 状态 | 代码证据与说明 |
|---|---|---|
| Hooks 接口 | 已完成 | `internal/action/hooks.go` 定义 `OnRegister/OnAuth/OnLocation/OnAlarm/OnDisconnect` 五类事件，`Noop` 兜底。 |
| HTTP 回调实现 | 已完成（示例级） | `internal/action/http.go`：队列 256、**单 goroutine 串行 POST**、默认 2s 超时、队列满丢弃、失败只记日志、不重试。 |
| 多 Sink / MQ | 未实现 | 只有 HTTP 一种实现，无 Kafka/RocketMQ/Redis Stream 适配。 |
| 事件可靠投递 | 未实现 | 无本地落盘兜底、无重放、无 at-least-once 语义。 |
| 网关事件契约 | 仅设计 | `gateway-mode-design.md` 定义了 5 类事件字段与 HTTP 协议；代码中没有 gateway 进程。 |

#### 1.2.8 JT1078 音视频

| 能力 | 状态 | 代码证据与说明 |
|---|---|---|
| 1078 信令 | 部分完成 | 只有 `msg_9205.go`（终端上传音视频资源列表）与 `msg_1205.go`。 |
| 实时音视频请求 0x9101/0x9102 | 未实现 | 无消息体、无 HTTP 下发。 |
| 历史回放 0x9201/0x9202 | 未实现 | 无消息体。 |
| 文件上传/完成 0x9206/0x9207/0x1206 | 未实现 | 无消息体、无处理。 |
| 音视频属性 0x1003/0x1005 | 未实现 | 无消息体、无处理。 |
| RTP 接入 | 未实现 | 无 UDP 监听、无 `JT1078Package` 解析、无帧重组。 |
| PS/H264 分流 | 未实现 | 无 NALU 状态机、无 PES 解封装。 |
| 媒体转推（FLV/HLS/fMP4） | 未实现 | 无相关包。 |

#### 1.2.9 苏标主动安全

| 能力 | 状态 | 代码证据与说明 |
|---|---|---|
| 0x1210/0x1211/0x1212 | 未实现 | 无消息体、无附件服务器。 |
| 0x9208/0x9212 | 未实现 | 无下发、无缺口回报。 |
| 0x0200 报警扩展 0x64~0x67 | 未实现 | 附加信息只有 raw，未按苏标字段解析。 |
| 附件文件服务 | 未实现 | 无独立端口、无 offset 随机写、无完整性校验。 |

#### 1.2.10 部署与可观测性

| 能力 | 状态 | 代码证据与说明 |
|---|---|---|
| 构建与发布脚本 | 已完成 | `Makefile`（`test/compile/release/smoke/dockerbuild`）、`scripts/build.sh`；`build/Dockerfile` 两阶段构建，暴露 8080/8081/8008。 |
| CI | 部分完成 | `.github/workflows/ci.yml` 只在 `main` 的 push/PR 上跑 `make all`；**`dev/*` 分支没有 CI**。 |
| 优雅停机 | 已完成 | `cmd/jt808-server/main.go` 监听 SIGINT/SIGTERM，调用 `storage.ResetRuntime` 清运行时投影。 |
| 日志 | 已完成 | `zerolog` + 滚动文件（`configs/default.yaml` 的 `log` 段）。 |
| 指标 | 未实现 | 无 Prometheus/expvar；只有 10s 一条连接数 Debug 日志。 |
| 健康检查 | 未实现 | 无 `/healthz`、`/readyz`。 |
| 链路追踪/pprof | 未实现 | 无 pprof 端口。 |
| Docker Compose / 部署清单 | 未实现 | `deploy/` 只有 `.keep`；无 compose、无 systemd/K8s 清单。 |
| 数据备份/迁移回滚 | 未实现 | 迁移只向前；无备份脚本、无回滚演练。 |

#### 1.2.11 测试覆盖

| 能力 | 状态 | 代码证据与说明 |
|---|---|---|
| 单元测试 | 已完成 | 65 个 `_test.go`、**198 个 `Test` 函数**，覆盖编解码、分包、重传、持久化、API、客户端/服务端模拟器。 |
| 双端集成测试 | 已完成 | `internal/server/tcp_server_test.go`、`udp_server_test.go` 起真实 TCP/UDP，模拟器走注册/鉴权/上报/下行。 |
| 持久化一致性测试 | 已完成 | `storage_test.go` 验证迁移可重复执行、内存/SQLite 行为对齐；`repository/*_test.go` 验证报警去重与确认过期。 |
| 协议模糊/异常帧测试 | 部分完成 | `packet_codec_test.go` 覆盖坏帧与长度不一致；无长期 fuzz。 |
| 性能/压测 | 未实现 | 无 benchmark 门限、无 N 终端并发脚本、无内存长稳测试。 |
| 苏标/1078 测试 | 未实现 | 对应功能尚未实现。 |

### 1.3 与上一版路线图的差异（已过期的旧结论）

| 上一版结论（2026-11-01） | 当前事实（2026-11-03） |
|---|---|
| "最大的现存缺口是持久化，所有缓存均仅存内存" | 已落 SQLite/MySQL，7 类仓储 + 4 张新表；内存后端仍保留为默认，用于开发/测试。 |
| "分包缓存无容量上限，是线上最容易被触发的内存问题" | 已加 `maxSegmentCacheEntries=1024`、`16MB`、`4096` 分片上限与 TTL 淘汰。 |
| "`internal/repository/sqlstore` 缺 Industry/Region/Alarm/Location 实现，可能不可编译" | 已补齐（`catalog_repository.go`、`region_repository.go`、`alarm_repository.go`、`location_repository.go`）。 |
| "gateway 模式代码已删除，仅留设计" | 仍成立；本路线图不把 gateway 作为近期交付。 |
| "Hooks 仅 5 个事件 + HTTP 示例" | 仍成立。 |

---

## 二、重排后的路线图

### 2.1 阶段总览

| 阶段 | 主题 | 完成标志 | 估算 |
|---|---|---|---|
| **P0** | 正确性与可验证基线 | 两个已知正确性缺口关闭；0x0200 可类型化；苏标附件可收完整文件；核心回归可自动化 | 约 16 人日 |
| **P1** | 商用必需能力 | 1078 信令与 RTP 可用；下行可靠投递闭环；媒体元数据落库；API 有鉴权与运维面 | 约 33 人日 |
| **P2** | 架构与分布式 | 多实例会话归属明确；下行定向投递不丢；有容量基准与扩展结论 | 约 34 人日（条件触发） |
| **P3** | 运维与长尾 | 可观测、可部署、可压测、可扩展协议域 | 约 13 人日 |

> 估算口径：熟悉本仓库的 1 名开发者，含单测与联调，不含产品等待与第三方联调排期。
> P2 不是"排期到了就做"，而是"达到触发条件才做"，触发条件见 2.4。

---

### 2.2 P0 —— 正确性与可验证基线

#### P0.1 会话双索引（`手机号 → Session`）与同号顶替

- **状态**：已实现（`dev/p0-correctness`）。注册表同时维护 `sessionID → Session` 与
  `phone → sessionID`，断开时两索引一并清理；接受连接时只建会话索引，手机号在
  0x0100 注册成功（手机号空闲）或 0x0102 鉴权成功后才绑定。同号已有活连接时不在
  0x0100 顶替，等新连接 0x0102 用库内鉴权码校验通过后才关闭旧连接、切换绑定并清空
  旧会话等待项与该手机号的分包/重传缓存；校验失败只拒绝新连接，保留旧连接与设备档案。
  被顶替会话在与绑定同一把锁内从两套索引摘除，避免「已被顶替的会话再次绑定成功」与顶替者
  互相关闭、形成无活会话的环；绑定失败（会话已被顶替）时不写设备档案。
  TCP 与 UDP 走同一套逻辑（手机号索引不区分 transport，未采用"禁止跨 transport 顶替"的旧设想）。
  未实现：`OnDisconnect` 的 `reason` 字段、`Session.Phone/VersionDesc` 可写字段；API 仍以
  `device.SessionID` 定位会话，尚未切换为按手机号查询。

- **目标**：任何时刻同一手机号最多一条"有效连接"；设备重连后旧连接立即关闭，
  `device_runtime` 不再被旧连接覆盖；业务侧可以只凭手机号定位会话。
- **涉及模块**：`internal/runtime/connection_registry.go`（新增 `cacheByPhone`）、
  `internal/protocol/model/session.go`（`Session` 增加 `Phone`/`VersionDesc` 的可写字段）、
  `internal/protocol/pipeline.go`（注册/鉴权成功后回填手机号）、
  `internal/protocol/msg_processor.go`（`processMsg0100/0102` 触发绑定）、
  `internal/api/api.go`（后续可新增 `GetByPhone` 快捷路径）。
- **技术方案要点**：
  1. `Bind` 接收 phone 后，若 `cacheByPhone[phone]` 存在且 `old.ID != new.ID`，
     先在锁外记录"待顶替"对象，再 `old.Conn.Close()`，避免持锁回调业务；
  2. 关闭旧连接后触发 `OnDisconnect`，事件增加 `Reason` 字段（`superseded` / `closed` / `keepalive_timeout`）；
  3. `Unbind` 必须校验"删的是不是当前索引里的同一条 session"，防止旧连接的清理把新连接删掉；
  4. UDP 会话没有稳定 `Conn` 身份，`cacheByPhone` 只用于 TCP；UDP 以 `phone` 为键但**不允许跨 transport 顶替**。
- **验收标准**：
  1. 单测：同一 phone 先 `Bind(A)` 再 `Bind(B)`，`A.Conn` 被关闭、`cacheByPhone[phone]==B`、`cacheByID[A.ID]` 不存在；
  2. 单测：`Unbind(A)` 不会删除 `B`；
  3. 集成：模拟器断链后立即重连，旧连接收到关闭，`device_runtime` 最终只反映新连接；
  4. `OnDisconnect` 事件带 `reason=superseded`，可被 HTTP 回调观察到。
- **优先级理由**：修复"旧连接覆盖最新状态"的数据正确性问题；为 API 按手机号下发、
  为后续 owner 租约（P2.1）提供同一套索引语义；改动面小、可独立上线。
- **工作量**：2 人日。
- **风险**：踢旧连接会触发业务 `OnDisconnect`，下游若把"重连"当"下线"会误告警；
  必须在事件里区分 `reason`，并在文档/示例里写清楚。
- **依赖**：无。

#### P0.2 下行应答关联精确化（`0x0001/0x8001` 按应答流水号配对）

- **状态**：已实现（`dev/p0-correctness`）。`Session.AcknowledgeSerial(answerSerial, phone)`
  在会话内按被应答流水号 + 手机号精确清算一条等待项，`Acknowledge(msgID, serial)` 保留为
  按 `msgID<<16|serial` 精确删除的实现；0x0001/0x8001 以及 0x0104/0x0201/0x1205/0x0700/
  0x0802/0x0805 等携带流水号的应答统一走精确匹配。0x0608/0x0702/0x8800 的协议体不含
  流水号，改为「同类等待项唯一才清算，多条并存时不清并记 warn」的保守策略，未采用
  「按 responseID 兜底」的旧设想（它正是误清多条等待项的来源）。不匹配、重复、错会话、
  找不到等待项的应答只记 warn，不清除任何等待项。未实现：待确认数量指标与重试耗尽的
  `msgID/serial/phone/attempt` 日志（已提供 `Session.PendingCount()` 供观测与断言）。

- **目标**：下行命令的"待确认"只被它自己的应答清算，杜绝"一条应答清空多条待确认"。
- **涉及模块**：`internal/protocol/model/session.go`（`AcknowledgeResponse` 改为接收
  `answerSerial`、`answerMsgID`、`phone`）、`internal/protocol/msg_processor.go:391-393`
  （从 `Msg0001/Msg8001` 消息体取 `AnswerSerialNumber/AnswerMessageID`）。
- **技术方案要点**：
  1. 通用应答按 `msgID<<16|serial` 精确删除；`0x0104/0x1205/0x0608` 等专用应答同样走流水号；
  2. 保留"按 responseID 兜底"的兼容分支，但只在找不到精确键时使用，并打 `Warn`；
  3. 增加"待确认数量"指标/日志，重试耗尽时输出 `msgID/serial/phone/attempt`。
- **验收标准**：
  1. 单测：同一 phone 连续下发两条 `0x8103`（不同流水号），只回第一条的 `0x0001`，第二条仍在 pending；
  2. 单测：回错误 `Result=1` 时仍视为"已应答"，但记录失败结果（或按产品口径决定是否重试）；
  3. 集成：模拟器选择性丢一条应答，服务端只重发对应命令，另一条不重发。
- **优先级理由**：当前实现会在多命令并发时静默失去重试，属于协议可靠性缺陷；
  修复成本 1 人日，收益是"重试真的按命令生效"。
- **工作量**：1 人日。
- **风险**：若历史终端对 `0x0001.AnswerSerialNumber` 填错（少数厂商 bug），
  精确匹配会导致永远等不到应答；需要保留兜底分支并观察日志比例。
- **依赖**：无。

#### P0.3 0x0200 附加信息"类型注册表 + raw 兜底"

- **状态**：已实现（`dev/p0-position-ext`）。按消息头协议版本位选域：2019 版本使用
  `base2019`（粤标 30 字节终端ID 布局），2011/2013 使用 `subiao`（7 字节终端ID 布局）；
  `0x64~0x67` 已结构化解析，未注册 ID 与长度不符项退化为 raw 兜底并保留原始字节；
  `OnAlarm` 回调新增 `extItems`/`extRaw`。苏标文档未定义 `VehicleNo/VehicleType`，
  车辆身份由报警标识号中的终端ID（`alarmIdNumber.deviceId`）承载。
- **目标**：位置与报警事件里能拿到结构化的 ADAS/DSM/TP 报警字段，同时不认识的
  附加信息 ID 仍然保留原始字节，不丢包。
- **涉及模块**：`internal/protocol/model/msg_0200.go`（新增
  `map[uint8]func([]byte) (any, error)` 与 `ParseExtItems()`）、
  `internal/protocol/model/msg_0200_ext.go`（新增，放 `0x64~0x67` 字段定义）、
  `internal/action/hooks.go`（`AlarmEvent` 增加 `ExtItems []any` 与 `ExtRaw`）。
- **技术方案要点**：
  1. 按协议域注册：`ext/subiao` 与 `ext/base2019` 分表，**不要**用一个全局 map，
     因为 `0x64` 在苏标与粤标含义不同；
  2. 未注册 ID 返回 raw 结构，`ParseExtItems` 永不因未知 ID 失败；
  3. 字段映射以苏标文档为准：`AlarmID/FlagState/AlarmOrEventType/Speed/Altitude/
     Latitude/Longitude/DateTime/VehicleNo/VehicleType`（`0x64/0x65/0x66/0x67`）；
  4. 坐标与时间保持原始整数与 BCD，不在协议层转 WGS84/时区。
- **验收标准**：
  1. 单测：一条含 `0x64/0x65/0x66/0x67` 的 `0x0200` 能解出各字段；未知 `0xE0` 仍保留 raw；
  2. 单测：`0x64` 在 base 与苏标域下解析结果不同且不串；
  3. 集成：`OnAlarm` HTTP 回调 JSON 含 `extItems`，业务能区分 ADAS 与 DSM。
- **优先级理由**：苏标附件服务器（P0.4）需要报警元数据；没有它，
  附件只能"收到文件但不知道是哪条报警"。同时它是 1078/苏标扩展的共同地基。
- **工作量**：3 人日。
- **风险**：附加信息存在厂商私有 ID 与标准重号；必须以"域 + ID"注册并保留 raw，
  否则会把不同标准的字段串读。
- **依赖**：无（可与 P0.1 并行）。

#### P0.4 苏标主动安全附件服务器

- **状态**：已实现（`dev/p0-subiao`）。阶段 1 完成
  `0x1210/0x1211/0x1212/0x9208/0x9212` 编解码；阶段 2 完成独立 TCP 附件服务器，
  字节区间记账与随机写位于 `assembler.go`（未单独拆 `index.go`），持久化
  `*.part.state.json` 并支持启动对账；阶段 3 完成主链路注册、`alarmServer` 配置、`0x9208` 下发与
  附件元数据查询。已知偏差：`0x1211` 在文件已有数据时同时触发 `0x9212` 缺口回报，
  标准仅要求 `0x1212` 触发；标准时序下（`0x1211` 先到）仍只回通用应答。
- **目标**：终端能按苏标流程把 ADAS/DSM/TP 报警附件完整传到平台；
  断点续传、缺口检测、完成通知（`0x9212`）全链路可用。
- **涉及模块**：
  - 新增 `internal/alarmserver/`：`server.go`（独立 TCP 端口监听与帧切分）、
    `assembler.go`（按 `fileName` 维护 `fileState`）、`file_store.go`（随机写与完整性校验）、
    `index.go`（块索引/位图，替代单一日志文件）；
  - `internal/protocol/model/`：新增 `msg_1210.go`、`msg_1211.go`、`msg_1212.go`、
    `msg_9208.go`、`msg_9212.go`；
  - `internal/protocol/msg_processor.go`：注册上行 `0x1210/0x1211/0x1212`；
  - `internal/api/api.go`：新增报警附件下发（`0x9208`）与附件查询；
  - `internal/config/config.go`：新增 `alarmServer.{port,dir,maxFileSize,maxConcurrent}`。
- **技术方案要点**：
  1. **独立端口**（参考 yezhihao 的 7200），不复用 8080；帧头魔数 `0x30 0x31 0x63 0x64`，
     帧内为 `offset(uint32) + length(uint32) + data`；
  2. **首包 `0x1210`**：登记 `fileName/报警ID/报警编号/附件类型`，创建目录与空文件，
     文件名 `filepath.Base` + 白名单字符，拒绝 `../` 与绝对路径；
  3. **随机写**：`WriteAt(data, offset)`；同一文件用 `sync.Map[fileName]*fileState` +
     单文件互斥，禁止两个 goroutine 同时改位图；
  4. **完整性**：块位图（每块 1 bit）字节数与期望大小对比；`0x1211` 到达后
     排序找缺口，回 `0x9212`（缺失偏移列表）；全部到齐后 `rename` 为正式文件并生成元数据；
  5. **索引**：位图存 SQLite/内存，按 `fileName` 可恢复；启动时扫描目录做对账；
  6. **超时**：`fileState` 30 分钟无进展则清理并记 `Warn`，防目录膨胀。
- **验收标准**：
  1. 单测：乱序 offset 写入 100 块，位图正确、缺口列表正确；
  2. 单测：重复块不重复计数；越界 offset 被拒绝；路径穿越文件名被拒绝；
  3. 集成：模拟器按 `0x1210 → 随机块 → 0x1211` 上传，平台回 `0x9212`；
     人为丢 3 块，平台准确报 3 个偏移，补传后文件 MD5 与源一致；
  4. 并发：同一终端两个文件并行上传互不干扰；
  5. 文件落盘目录结构可按 `phone/日期/报警ID/文件名` 检索。
- **优先级理由**：调研认定的两个最高 ROI 之一；这是苏标落地的唯一正确姿势，
  "按顺序收、错了整包重来"在弱网下永远传不完。且纯 IO，与现有 Go 架构无冲突。
- **工作量**：8 人日（`0x1210/1211/1212` 消息体 1 人日、服务器与组装 4 人日、
  位图/对账 2 人日、联调与单测 1 人日）。
- **风险**：
  1. 终端厂商对 `0x1210` 字段与文件名规则实现不一，需要准备 2~3 家终端联调；
  2. 大文件（几十 MB）并发上传时磁盘 IO 与内存位图要压测；
  3. `0x9212` 的偏移语义（字节偏移 vs 块序号）必须按标准与终端实测确认，
     写错会导致终端无限补传。
- **依赖**：P0.3（报警元数据）；`internal/runtime` 的会话查询（P0.1 后可按手机号取）。

#### P0.5 可验证基线：回归脚本与 CI 覆盖 dev 分支

- **目标**：把"重连顶替、应答配对、苏标断点续传、分包补传"变成可重复执行的回归，
  避免后续改动悄悄回退。
- **涉及模块**：`.github/workflows/ci.yml`、`scripts/`（新增 `smoke-reconnect.sh`、
  `smoke-subiao.sh`）、`internal/server/*_test.go`、`cmd/jt808-client`。
- **工作内容**：
  1. CI 增加 `dev/*` 分支触发与 `go test -race ./...`；
  2. 用 `cmd/jt808-client` 起 2 个同号终端验证顶替；
  3. 用模拟器构造"丢 3 个分包 + 丢 2 个附件块"的确定性场景；
  4. 输出每次运行的关键指标（重传次数、补传次数、文件 MD5）。
- **验收标准**：CI 在 10 分钟内跑完；上述 4 个场景全绿；失败时能定位到具体消息 ID/流水号。
- **优先级理由**：P0 的三项修复都涉及并发与状态机，没有自动化回归就无法证明修好了。
- **工作量**：2 人日。
- **风险**：CI 里跑真实端口可能撞端口；用随机端口或 `t.Parallel` 隔离。
- **依赖**：P0.1/P0.2/P0.4 完成后补齐用例（可先搭框架）。

---

### 2.3 P1 —— 商用必需能力

#### P1.1 JT1078 信令补全（先信令、后媒体）

- **目标**：平台能发起实时预览/历史回放/文件上传，并接收终端的 1078 上行信令。
- **涉及模块**：`internal/protocol/model/`（新增 15~20 个消息体）、
  `internal/protocol/msg_processor.go`（注册 `0x1003/0x1005/0x1205/0x1206`）、
  `internal/api/api.go`（新增 `9101/9102/9201/9202/9206/9207` 六条下发路由）。
- **技术方案要点**：
  1. `0x9101`：`ip(lengthUnit=1)/tcpPort/udpPort/channelNo/mediaType/streamType`；
  2. `0x9201`：在 `0x9101` 基础上加 `storageType/playbackMode/playbackSpeed/startTime/endTime`（BCD6）；
  3. `0x9206` 先下发 `0x9208`（文件上传指令）再下发 `0x9206`，顺序不可颠倒；
  4. `0x1205` 已是现有模型，补"按通道/类型/时间过滤"的查询参数即可；
  5. 所有新消息保留 `Raw []byte` 兜底，厂商差异字段不硬解析。
- **验收标准**：模拟器能收 `0x9101/0x9201/0x9206` 并按 1078 回 `0x1003/0x1205/0x1206`；
  HTTP 下发返回 202 且报文可被模拟器解码；单测覆盖字段边界。
- **优先级理由**：信令是媒体的前置条件，没有 `0x9101/0x9201`，终端不会推流。
- **工作量**：5 人日。
- **风险**：`0x1005` 字段多且可变，各厂商差异大；先按标准实现 + raw 兜底。
- **依赖**：P0.5 的模拟器增强。

#### P1.2 JT1078 RTP 接入与 PS/H264 分流

- **目标**：能从 UDP 收 RTP、按 `SIM + 通道 + 码流类型` 重组完整帧，并输出可供
  播放/落盘的最小媒体单元。
- **涉及模块**：新增 `internal/media/rtp.go`（UDP 监听与包解析）、
  `internal/media/frame.go`（三维索引 + TTL + 容量上限）、
  `internal/media/nalu.go`（H264 状态机）、`internal/media/ps.go`（PS/PES 解封装）。
- **技术方案要点**：
  1. 解析 `JT1078Package` 字段顺序：`FH 0x30316364 / Label1 / Label2 / SN /
     SIM(BCD 12) / LogicChannel / Label3 / Timestamp / LastIFrameInterval /
     LastFrameInterval / BodyLength / Bodies`；
  2. **先判 payload 类型**：`0x000001BA` 为 PS pack start，必须走 PES 解封装；
     只有确认 Annex-B 才扫 `00 00 01`；
  3. 帧缓存按 `SIM+channel+streamType` 三维键（**SmallChi 的 key 缺 streamType，
     主/子码流会串，不要照抄**）；TTL 30s、总容量 64MB、单帧上限 8MB；
  4. SN 不连续只记指标，不做协议级重传（1078 没有重传），由上层重开流；
  5. 每帧只分配一次缓冲，禁止 `copy` 链式追加。
- **验收标准**：
  1. 单测：给定 20 个 RTP 包（含 I 帧与 P 帧），能切出正确 NALU 边界；
  2. 单测：PS 封装样本能解出 H264 与音频 ES；
  3. 压测：单进程 2000 路（4 通道 × 2 码流 × 250 车）持续 10 分钟，内存不涨、丢包率可统计；
  4. 指标：`rtp_packets_total`、`frame_gap_total`、`frame_bytes`。
- **优先级理由**：从"信令平台"走到"视频平台"的分水岭；但必须在 P1.1 之后做。
- **工作量**：10 人日（解析 3、PS/PES 3、帧缓存 2、压测与指标 2）。
- **风险**：
  1. 不同厂商的 PS 封装差异（PES 头、填充字节）；
  2. UDP 丢包与乱序在弱网下必然发生，验收要允许"帧不完整"；
  3. 端口与 NAT：终端主动推流到平台 UDP 端口，云上安全组要放行。
- **依赖**：P1.1。

#### P1.3 下行可靠投递闭环：`command_outbox` + 幂等 + 结果回传

- **目标**：API 下发命令后，调用方可以查询"是否已应答/是否重试耗尽"；
  进程重启后未完成命令可重投；重复请求不重复下发。
- **涉及模块**：`internal/repository/`（新增 `CommandRepository` 契约）、
  `internal/repository/sqlstore/`（`command_outbox` 表与实现）、
  `internal/protocol/model/session.go`（`TrackReliable` 输出结果事件）、
  `internal/api/api.go`（下发接受幂等键、新增 `GET /commands/:id`）、
  `internal/repository/migration/2026xxxx_command_outbox.sql`。
- **技术方案要点**：
  1. 表字段：`command_id`（业务幂等键）、`phone`、`msg_id`、`serial`、`payload`、
     `status(pending/acked/failed)`、`attempts`、`created_at/updated_at`；
  2. 状态机：`pending → acked | failed`；重试耗尽置 `failed` 并触发事件；
  3. 重投：启动时扫描 `pending` 且 `created_at` 在窗口内的命令，仅单实例场景直接重发；
     多实例场景等 P2.1 的 owner 路由，避免"谁都能发"；
  4. 幂等：`Idempotency-Key` 重复提交返回同一条 `command_id`，不产生第二条报文。
- **验收标准**：模拟器不回应答时命令 3 次重试后置 `failed`；重启后 `pending` 可继续；
  重复 POST 同 key 只产生一条 `command_outbox`；API 可查状态与尝试次数。
- **优先级理由**：现有重试只在内存里，进程重启即丢；商用系统必须能回答"这条命令到底发没发出去"。
- **工作量**：5 人日。
- **风险**：重投可能导致同一命令执行两次，终端侧要有幂等或业务侧接受 at-least-once；
  必须在文档里明确语义，不做"假 exactly-once"。
- **依赖**：P0.1（按手机号找会话）、P0.2（精确应答）。

#### P1.4 可插拔事件出口（多 Sink + 一类 MQ 实现）

- **目标**：在 HTTP 之外增加至少一种 MQ/流式出口，且不改变协议主流程。
- **涉及模块**：`internal/action/`（新增 `Sink` 接口、`MultiSink`、`kafka.go` 或
  `redis_stream.go`）、`internal/config/config.go`（`action.sinks[]`）。
- **技术方案要点**：
  1. `Sink` 与 `Hooks` 解耦：`Hooks` 负责采集，`Sink` 负责投递；
  2. 有界队列 + 背压 + 失败落本地文件（参考调研中对 `async void` 的批评）；
  3. 语义明确为 at-most-once（HTTP 示例现状）或 at-least-once（落盘重放），
     两者不能混着宣传；
  4. Kafka key 用 `phone`，但**下游必须按 key 幂等**，不假设 exactly-once。
- **验收标准**：同时配置 HTTP + MQ，两个 Sink 独立计数；MQ 断连时主流程不阻塞；
  重启后可选重放本地落盘事件。
- **优先级理由**：现在只有一种出口，任何真实业务接入都要改代码；抽象成本低。
- **工作量**：4 人日。
- **风险**：多 Sink 的失败语义容易含糊；先用配置项显式选择语义，不自动重试 HTTP。
- **依赖**：无（可在 P0 期间并行设计）。

#### P1.5 媒体元数据持久化（`0x0801/0x0802/0x1205`）

- **目标**：媒体上传记录、检索结果、资源列表在重启后仍可查询，而不是只活在内存。
- **涉及模块**：`internal/repository/industry_repository.go`（拆出 `MediaRepository`）、
  `internal/repository/sqlstore/media_repository.go`（新增）、
  迁移 `2026xxxx_media_metadata.sql`。
- **技术方案要点**：
  1. 表 `media_uploads(phone, media_id, channel, media_type, event, upload_time, payload_json)`；
  2. `0x0801` 分片重组完成后写一条，不要在分片过程中写；
  3. `0x0802` 检索结果只缓存最近 N 条，不落库（终端可重查）；
  4. `0x1205` 资源列表按 `(phone, channel, start, end)` 覆盖，保留 7 天。
- **验收标准**：上传 3 个媒体后重启，`GET /device/:phone/reports/media` 仍返回 3 条；
  检索结果不重复膨胀；旧记录按保留期清理。
- **优先级理由**：媒体是 808 商用场景的高频功能，内存实现在重启后全丢。
- **工作量**：3 人日。
- **风险**：媒体字节本身不落库（只落元数据），要避免把 `payload_json` 写成大对象。
- **依赖**：无。

#### P1.6 API 鉴权、会话运维面与基础审计

- **目标**：管理 API 不再裸奔；能查在线会话、强制断开、按手机号统一下发。
- **涉及模块**：`internal/api/api.go`（中间件 + 新路由）、
  `internal/config/config.go`（`api.token` / `api.enable`）、
  `internal/runtime/connection_registry.go`（列表与强制关闭）。
- **工作内容**：
  1. Token 中间件（`Authorization: Bearer`），启动时未配置 token 则默认只监听 `127.0.0.1`；
  2. `GET /sessions`（手机号、会话 ID、传输协议、最后心跳、版本）；
  3. `DELETE /sessions/:phone`（强制断开，事件 `reason=manual`）；
  4. `POST /send`（按手机号 + 消息 ID + JSON body，内部复用现有构造器）；
  5. 审计日志：操作人、来源 IP、幂等键、命令 ID。
- **验收标准**：无 token 访问返回 401；`/sessions` 与内存注册表一致；
  强制断开后设备可重连；`/send` 与现有 29 条路由行为一致。
- **优先级理由**：当前 API 可以直接控制车辆（`0x8500` 车辆控制等），无鉴权是部署阻断项。
- **工作量**：4 人日。
- **风险**：`/send` 通用化会削弱现有路由的类型校验；先只允许白名单 MsgID。
- **依赖**：P0.1、P1.3（幂等键）。

#### P1.7 报警与定位历史查询闭环

- **目标**：历史数据不仅能落库，还能分页查询、按时间/车牌过滤，并暴露清理结果。
- **涉及模块**：`internal/repository/location_repository.go`、`alarm_repository.go`、
  `internal/api/api.go`（新增 `GET /locations`、`GET /alarms` 分页参数）、
  `internal/storage/config.go`（保留期配置校验）。
- **工作内容**：
  1. 统一分页契约（`from/to/limit/cursor`），禁止一次拉全量；
  2. 查询按 `(phone, report_time_ms)` 与 `alarm_sign` 走现有索引；
  3. 清理任务输出"删除条数/耗时"，启动时校验 `retentionDays` 非负；
  4. 明确 `0x0704` 只记定位不记报警（沿用设计文档口径）。
- **验收标准**：10 万条定位下分页查询 P95 < 200ms（SQLite 本机）；清理后总量符合上限；
  时间参数缺失时返回 400 而不是全表扫描。
- **优先级理由**：落库之后没有查询闭环，等于只做了一半。
- **工作量**：3 人日。
- **风险**：SQLite 大表无 ANALYZE 时查询计划可能退化；先加 `EXPLAIN QUERY PLAN` 测试。
- **依赖**：无（持久化已就绪）。

---

### 2.4 P2 —— 架构与分布式（条件触发，不默认启动）

> **触发条件（满足任意两条才启动 P2）**：
> 1. 单实例稳定承载 > 5000 条长连接或 > 2000 条上行报文/秒；
> 2. 业务要求滚动发布/重启不中断在线设备；
> 3. 出现跨机房/多活部署需求；
> 4. 单库写入成为瓶颈（定位/报警写入 P95 > 50ms 且无法靠批量/分区缓解）。
>
> 未达到上述条件时，优先做单机优化（批量写、索引、连接参数），
> **不要为了"看起来像分布式"引入 Redis/Kafka**。

#### P2.1 会话归属路由表与定向投递（替代"Kafka 单 group 消费"）

- **目标**：多实例时，下行命令只投递到持有该手机号连接的节点；
  节点宕机后租约可回收，不会出现"两个节点都以为自己持有"。
- **涉及模块**：新增 `internal/coordination/`（`LeaseStore`、`SessionRouter`、`Presence`）、
  `internal/runtime/connection_registry.go`（Bind/Unbind 时上报归属）、
  `internal/api/api.go`（下发前查路由）、`docs/gateway-mode-design.md`（同步更新）。
- **技术方案要点**：
  1. `phone → nodeID` 存 Redis（或 MySQL 表），带 TTL 租约与 fencing token；
  2. 下行：API 节点按 `phone` 查路由 → 定向 HTTP/gRPC 投递到目标节点 →
     目标节点用本地 `sessionID` 发送；
  3. 节点启动时注册自身 ID；宕机后租约 TTL 到期，其他节点可接管；
  4. **不要**让所有节点订阅同一个 Kafka topic + 同一 consumer group：
     消息只会被一个实例消费，不保证是持有连接的那个（SmallChi Gateway 的架构级缺陷）；
  5. 若一定要用 MQ，必须"每实例独立 group + 按 phone 分区 + 消费者只处理自己分区"，
     或改成"命令总线 + 定向投递"。
- **验收标准**：两节点各持一半设备，任意 API 节点下发都成功；
  杀掉持有节点后 30s 内租约回收、设备重连到另一节点并恢复下发；
  重复投递由 P1.3 的幂等键吸收。
- **优先级理由**：多实例的正确性基础；没有它，gateway 拆分与 MQ 都是错的。
- **工作量**：8 人日。
- **风险**：租约与 GC 停顿/网络分区会导致"脑裂双活"，必须用 fencing token
  并在连接建立时校验"当前节点是否仍持有租约"。
- **依赖**：P0.1、P1.3。

#### P2.2 Redis 共享运行态与 presence

- **目标**：在线状态、最后心跳、临时参数在节点间一致；API 节点不依赖"恰好连到持有节点"。
- **涉及模块**：新增 `internal/runtime/redis_*`（`Heartbeat`、`Presence` 实现）、
  `internal/config/config.go`（`redis.dsn/ttl`）。
- **工作内容**：
  1. `presence:{phone}` 存 `nodeID/lastSeen/version`，TTL = 3 倍保活；
  2. 心跳两路写：本地 L1 实时 + Redis 投影（节流）；
  3. 读路径：本地未命中读 Redis，再回源库；
  4. Redis 故障时降级为"仅本地"，并明确"可能误判离线"。
- **验收标准**：两节点共享在线状态；Redis 宕机后服务不崩、只降级；TTL 到期自动清理。
- **优先级理由**：P2.1 的配套；没有共享 presence，API 查询仍会漂移。
- **工作量**：5 人日。
- **风险**：Redis 成为新的单点；必须设计降级路径与超时（≤50ms）。
- **依赖**：P2.1。

#### P2.3 Gateway 模式落地（协议接入与业务分离）

- **目标**：按 `gateway-mode-design.md` 的矩阵，把"接入/会话/编解码"与"业务/存储"拆成两类进程。
- **涉及模块**：新增 `cmd/jt808-gateway`、`internal/gateway/`（29 个上行 MsgID 路由、
  42 个下行忽略矩阵、5 类事件契约）、`internal/action`（事件出口）、
  `internal/protocol`（剥离存储依赖）。
- **工作内容**：
  1. 先做"进程内可切换"：`mode: standalone|gateway`，gateway 只做编解码 + 事件；
  2. 按设计文档实现上下行矩阵与"网关必须自己保存的最小子集"；
  3. 业务侧消费事件并回写命令；下行走 P2.1 的定向投递；
  4. 保留 standalone 作为默认，gateway 作为可选构建。
- **验收标准**：gateway 进程不连业务库；设备注册/定位/报警事件可被业务进程消费；
  下行命令经网关到达终端；两类进程可独立重启。
- **优先级理由**：只有在 P2 触发条件满足、且单机已稳定时才值得做；
  否则会提前引入两套部署与两套排障。
- **工作量**：12 人日。
- **风险**：拆分后"事件丢失/顺序错乱/重复"会被放大；必须先有 P1.4 的可靠 Sink。
- **依赖**：P2.1、P2.2、P1.4。

#### P2.4 容量基准与扩展瓶颈测定

- **目标**：用数据回答"单机能扛多少、瓶颈在哪、下一步该优化什么"。
- **涉及模块**：`cmd/jt808-client`（并发终端与报表）、`scripts/loadtest/`、`docs/`（基准报告）。
- **工作内容**：
  1. 场景：1k/5k/10k 长连接，1k/2k msg/s，心跳 60s，定位 10s；
  2. 指标：CPU、RSS、GC、连接建立耗时、P95 处理延迟、丢帧/重传率；
  3. 分别测 memory/sqlite/mysql 后端，明确写入瓶颈；
  4. 给出"单机上限 + 扩容建议"结论。
- **验收标准**：报告可复现（脚本 + 参数 + 机器规格）；瓶颈点有火焰图或 pprof 证据。
- **优先级理由**：没有基准就无法判断 P2 是否值得做，也无法给容量承诺。
- **工作量**：4 人日。
- **风险**：压测客户端与服务同机会抢 CPU；必须分机或限核。
- **依赖**：P3.1 的可观测性（至少 pprof）。

#### P2.5 存储演进评估（时序库/读写分离/分表）

- **目标**：只做评估与 PoC，不直接上生产。
- **涉及模块**：`docs/storage-abstraction-design.md` 增补、`internal/repository`（PoC 适配层）。
- **工作内容**：对比"SQLite/MySQL 分区表 vs ClickHouse/TimescaleDB"在
  定位历史 10 万~1 亿行的写入/查询成本；评估读写分离与按 `phone` 哈希分表。
- **验收标准**：给出"何时切换、切换成本、回滚方案"的决策文档，不写生产代码。
- **优先级理由**：当前数据量远未到瓶颈，提前分库分表只会增加复杂度。
- **工作量**：5 人日（评估 + PoC）。
- **风险**：PoC 结论受样本数据影响；必须用真实量级的合成数据。
- **依赖**：P2.4 的基准数据。

---

### 2.5 P3 —— 运维与长尾

| 项 | 目标与验收 | 涉及模块 | 工作量 |
|---|---|---|---|
| P3.1 Compose 与初始化 | 一条命令起 server + migrate + volume；SQLite 场景 `journal_mode=WAL`、数据目录持久化；容器健康检查可用 | `deploy/docker-compose.yml`、`build/Dockerfile` | 1 人日 |
| P3.2 Prometheus/health/pprof | `/healthz`、`/readyz`、`/metrics`；指标只打"总数/按版本/按 MsgID"，禁止手机号高基数标签 | `internal/server`、`internal/api` | 3 人日 |
| P3.3 模拟器 N 终端并发 | `cmd/jt808-client` 支持 N 个终端并发注册/上报/重连，输出 CSV 报表 | `cmd/jt808-client`、`client/` | 2 人日 |
| P3.4 2011 消息头识别 + 协议扩展包 | 消息头级区分 2011/2013；`internal/protocol/ext/{subiao,yuebiao}` 按域注册，build tag 可选编译，重复注册 panic | `internal/protocol/model/header.go`、`internal/protocol/registry.go` | 4 人日 |
| P3.5 覆盖矩阵与新标准评审 | 更新 `JT808-2019-coverage.md`；评审第 1 号修改单；标注未验证项 | `docs/` | 2 人日 |
| P3.6 备份与升级演练 | 备份脚本、迁移失败回滚手册、演练记录 | `scripts/`、`docs/` | 1 人日 |

---

## 三、明确不建议照搬的做法及理由

1. **不要把 yezhihao 的"注解 + 反射"编解码搬到 Go。**
   Go 没有注解，struct tag + 反射的分配与 `time` 开销比手写 `Decode` 更差，
   还会削弱"解码时做业务校验"的灵活性。可借鉴的是 protostar 的字段 DSL
   （`length/lengthUnit/lengthExpression/version`），不是它的运行机制。

2. **不要照搬 yezhihao 开源业务层。**
   它的 `0x0102` 不校验、`0x0200` 批量入库方法体为空、MyBatis 依赖但零 Mapper，
   是演示骨架。本项目 `processMsg0102` 已真实查设备，不能为了"对齐"而退化。

3. **不要照搬 SmallChi Gateway 的手工切帧。**
   它不还原转义（`7D 02` 会被误判帧尾）、无长度上限、`AcceptAsync` 异常即 `break`。
   本项目的 `bufio.Reader + MaxFrameLen + 长度一致性校验` 更稳，
   只可借鉴"按 `0x7E` 双标记切段"的兼容思路。

4. **不要照搬 SmallChi Gateway 的 Kafka 下行。**
   单 consumer group + 无实例路由 = 多实例下行必丢。
   正确顺序是"先会话归属路由表（P2.1），再定向投递；MQ 只做事件或按 phone 分区"。

5. **不要照搬 SmallChi/JT1078 的静态 `Merge`。**
   无超时、无上限、O(n²) 拷贝、key 缺 `streamType`、静态字段多实例不可用。
   本项目应重写为"三维键 + TTL + 容量上限 + 每帧只分配一次"。

6. **不要把 RTP `Bodies` 一律当 H264 Annex-B。**
   设备可能推 PS 封装，直接扫 `00 00 01` 会切出垃圾 NALU；必须先判 `0x000001BA`。

7. **不要照搬"把业务写进网关进程"的形态。**
   SmallChi README 自己说"改业务要重启网关"。本项目应保持 `Hooks + 存储装配层`，
   网关只做接入与事件。

8. **不要照搬 `async void ProduceAsync` 式事件投递。**
   无背压、无落盘、失败静默；必须"有界队列 + 背压 + 失败落盘 + 指标"。

9. **不要照搬 yezhihao 的单个 `.log` 块日志。**
   文件多时读放大明显；本项目改用位图或 SQLite 块索引（P0.4 已定）。

10. **不要把 `0x0200` 附加信息做成全局注册表。**
    苏标 `0x64` 与粤标 `0x64` 含义不同，必须按协议域注册，否则串读。

---

## 四、现在不做，以及为什么

| 不做项 | 为什么现在不做 | 何时重启 |
|---|---|---|
| Kafka/RocketMQ 作为核心下行通道 | 单机未到 5k 连接/2k msg/s；引入后要处理分区、重平衡、顺序、幂等，收益为负；P1.4 只做"事件出口" | P2 触发条件满足，且 P2.1 已完成 |
| Gateway 进程拆分 | 当前单进程足够，拆分会增加两套部署/排障；设计文档已保留 | P2 触发条件满足，且 P1.4/P2.1 完成 |
| 1078 转封装（FLV/HLS/fMP4） | 没有 RTP 接入与帧重组，转封装是空中楼阁；先做 P1.1/P1.2 | P1.2 稳定后再评估 |
| 1078 音频转码（G711/ADPCM/MP3/AAC） | 需求未确认；转码是 CPU 大头，且终端通常已给目标编码 | 有明确播放端需求 |
| MongoDB/Elasticsearch | 定位/报警用现有关系库 + 索引可满足；多引入一个存储就多一套备份/权限/监控 | 查询形态需要全文检索/聚合时 |
| 分库分表 | 当前数据量远未到瓶颈；分表会破坏事务与迁移简单性 | P2.4/P2.5 给出数据结论后 |
| 反射式编解码框架 | 会牺牲性能与解码期校验；现有 71 个消息手写成本可控 | 不重启（只借鉴 DSL） |
| 黑名单文件热更新 | `FileSystemWatcher` 在容器/多实例下不可靠；应走管理接口下发 | P3.2 有配置中心后 |
| JTT 2011 完整支持 | 只在 `0x0100` 用长度反推，尚无真实 2011 终端需求 | P3.4 有终端实测样本 |
| 苏标 ADAS/DSM 算法判定 | 平台只负责收附件与元数据，算法在终端/第三方 | 不重启（明确边界） |
| 全量 RSA 加密消息体 | 现网几乎不启用；实现与密钥管理成本高 | 有明确安全合规要求 |

---

## 五、依赖关系与阶段门槛

```
P0.1 会话双索引 ──┬──> P1.3 下行闭环 ──┬──> P2.1 归属路由 ──> P2.2 Redis presence ──> P2.3 Gateway
                  │                    │
P0.2 应答精确 ────┘                    └──> P2.4 容量基准 ──> P2.5 存储评估
P0.3 0x0200 类型化 ──> P0.4 苏标附件
P0.5 回归基线 ──> P1.1 1078 信令 ──> P1.2 RTP/PS
P1.5 媒体元数据 ─┐
P1.6 API 鉴权  ──┴──> P3.x 运维
```

**进入下一阶段的门槛**

- P0 → P1：两个正确性缺口关闭；4 条回归在 CI 常绿；苏标附件至少 1 家终端实测通过。
- P1 → P2：单机基准报告完成；下行命令可查询状态；API 有鉴权。
- P2 → P3：多实例演练通过（杀节点、租约回收、设备重连）；无"双活写同一设备"事故。

---

## 六、结论：参考谁、先做什么

**最值得参考的项目是 `yezhihao/jt808-server`**，参考三段：

1. **苏标附件服务器的完整闭环**（独立端口 + 魔数帧 + offset 随机写 + 块日志/位图 + `0x9212` 缺口回报）——
   这是四个项目里唯一接近生产级的实现，直接决定 P0.4 的技术方案；
2. **`Mono` 式"请求-应答"关联与超时清理**——对应本项目的 P0.2/P1.3；
3. **`T9101/T9201` 等 1078 字段定义**——可直接对照实现 P1.1，但不要参考它的业务层（演示骨架）。

`SmallChi/JT808` 只借"按域注册的扩展包"与 `0x0200` 二级工厂思路；
`JT808Gateway` 只借"双索引 + 踢旧连接"与黑名单/会话通知的形态；
`SmallChi/JT1078` 只当字段手册，分包合并与静态单例一律不用。

**下一步最该先做：P0.1 会话双索引 + 同号顶替（2 人日）。**

理由：
- 它是两个最高 ROI 中成本最低的一项，且直接修复"旧连接覆盖最新状态"的数据正确性问题；
- 它同时为 API 按手机号下发、P1.3 命令闭环、P2.1 归属路由提供同一套索引语义，
  是后续所有会话相关工作的地基；
- 改动集中在 `connection_registry.go` 与注册/鉴权路径，可独立上线、可回滚。

建议把 **P0.2（1 人日）作为同一批"正确性补丁"一起做**，两项合计 3 人日，
既关闭两个已知缺陷，又能在 P0.5 的回归里固化为用例。
