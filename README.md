# jt808-server-go

[![Go Version](https://img.shields.io/github/go-mod/go-version/fakeyanss/jt808-server-go)](https://github.com/fakeyanss/jt808-server-go/blob/main/go.mod)
[![License](https://img.shields.io/github/license/fakeyanss/jt808-server-go)](https://github.com/fakeyanss/jt808-server-go/blob/main/LICENSE)
[![CI](https://img.shields.io/github/actions/workflow/status/fakeyanss/jt808-server-go/ci.yml?branch=main&label=CI)](https://github.com/fakeyanss/jt808-server-go/actions/workflows/ci.yml)

> **JT/T 808 终端接入服务。一套协议内核，支持单机 Standalone 和 Cluster 模式运行。**

## 1. 项目简介与功能特性

jt808-server-go 是一个用 Go 编写的 JT/T 808 终端接入服务。它把车载终端的注册鉴权、位置与报警上报、主动安全附件、音视频信令和平台下行控制收敛到一套协议内核里，业务数据可按部署规模选择内存、SQLite 或 MySQL。它面向自建车辆监管平台、设备接入网关和协议联调工具的使用者：启动一个进程就能接终端，接入量增长后再按配置拆分接入层与业务层。

**功能特性**

- **JT/T 808 三版本接入**：支持 2011/2013/2019 的注册鉴权、心跳、定位与报警、参数与属性、文本/事件/提问、电话本、车辆控制、区域与路线、记录仪、司机身份/CAN/运单、多媒体、透传与压缩上报等消息族。
- **苏标主动安全（T/JSATL 12）**：接收 `0x1210` 报警附件信令与 `0x30/0x31/0x63/0x64` 文件码流帧，提供附件服务器与手动/自动 `0x9208` 上传指令。
- **JT/T 1078 信令与媒体**：808 的多媒体信令只承载图片，实时音视频与回放拆到 1078，避免长时间占用主连接。服务实现 9 条 1078 信令，并可用独立 UDP 端口接收 RTP、还原 H.264 裸流落盘。
- **多存储后端**：`memory` 开箱即用；SQLite 适合单机持久化；MySQL 适合多实例共享业务数据。仓储接口统一，协议层不感知具体数据库。
- **下行 outbox 重投**：`off` / `memory` / `durable` 三档，支持幂等键、命令状态查询、租约、退避与过期，语义为 at-least-once。
- **上行可靠性语义**：按消息分类选择 `best_effort`（先应答）或 `at_least_once`（先落库再应答），用应答时机换取吞吐或可靠性。
- **管理面与可观测性**：独立管理面提供存活/就绪探针、Prometheus 文本指标、会话运维与历史分页查询；业务 API 具备鉴权、限流与审计。
- **可验证的接入链路**：自带终端模拟器，支持分包乱序、丢片、异常帧注入与 UDP 接入；`make smoke` 用真实二进制跑端到端场景。

### 1.1 协议版本划分

消息头用消息体属性 bit14（`VersionSign`）区分 2019 与非 2019：2019 消息头额外带 1 字节 `ProtocolVersion`，手机号为 10 位 BCD；非 2019 手机号为 6 位 BCD。2011 与 2013 在消息头层面无法区分，当前仅在 `0x0100` 终端注册消息中按消息体剩余长度做启发式判别，其余消息按 2013 处理。

注册/鉴权/终端属性等消息体再按版本使用不同字段宽度：

| 字段 | 2011 | 2013 | 2019 |
|---|---|---|---|
| 终端制造商编码 | 5 字节 | 5 字节 | 11 字节 |
| 终端型号编码 | 8 字节 | 20 字节 | 30 字节 |
| 终端 ID 编码 | 7 字节 | 7 字节 | 30 字节 |
| 从业资格证编码 | 40 字节 | 20 字节 | 20 字节 |

版本判别集中在消息头解码；消息体编码按 `Header.Attr.VersionDesc` 分支，例如 `0x8108` 的制造商 ID 在 2019 下为 11 字节、2011/2013 下为 5 字节。注册表当前覆盖 **81 个不同 MsgID、42 个处理函数**（以 `internal/protocol/msg_processor.go` 为准），其中同时包含服务端上行处理、平台下行命令与应答、模拟器侧行为，不能按 MsgID 数值简单区分方向。

### 1.2 已知边界与未实现项

以下为当前代码的真实边界，不要按“计划能力”理解：

| 领域 | 边界 |
|---|---|
| 808 版本 | 消息头无法区分 2011/2013，2011 仅在 `0x0100` 注册时按长度启发式识别；其余消息会按 2013 处理 |
| 808 消息 | 81 个 MsgID 包含服务端上行、平台下行与模拟器行为，并非全部已完整实现；部分消息只有模型与应答，业务未闭环 |
| 808 扩展 | 保留字段/厂商自定义扩展没有通用分发；2019 第 1 号修改单未做符合性评估 |
| 1078 | 仅 UDP、仅 H.264；TCP 推送、H.265、音频、转码、分发、播放器索引与回放服务均未实现；平台侧无发起实时传输/回放的 API |
| 苏标 | 附件可按手机号列出、按文件名查询元数据，没有文件内容下载接口；外设/透传的厂商扩展按原始或结构化模型处理，未做扩展分发；未实现粤标 `0x1FC4` |
| `memory` 后端 | 重启丢失全部业务数据；不保存定位历史（管理面 `/locations` 返回 503）；outbox 默认 `memory`，重启丢失 pending 命令；`retention` 配置不生效 |
| 定位投影 | 最新定位 `Geo` 始终在进程内，SQL 后端重启后需等下一次上报才恢复；SQL 只持久化定位历史 |
| 行业媒体快照 | `0x0801` 已落盘并落库，分包与非分包都覆盖；`0x0805` 拍摄记录落库，按 mediaId 关联文件是否到达；`0x0800/0x0802` 等其余快照仍只在内存 |
| 媒体取证关联 | `0x0801` 位置体只有报警标志位、没有报警流水号，`alarmSign` 只表达报警类别，无法与某一条具体报警精确关联；本批不做保留期清理，磁盘占用需通过 `jt808_media_files` / `jt808_media_file_bytes` 指标监控 |
| 集群与分布式 | 会话、心跳、分包缓存与查询应答协调均为节点本地状态；媒体文件按本地目录落盘，Cluster 形态要求 `mediaUpload.dir` 位于所有节点可见的共享卷；未实现共享会话路由、跨节点下行投递、节点路由、对象存储、MQ Sink、Redis/Kafka 等分布式能力 |
| 升级工作流 | 只有 `0x8108` 下发与 `0x0108` 结果记录；没有升级包托管/下载 URL、版本与车型管理、灰度发布、失败重试与回滚 |
| 投递保证 | 下行 outbox 为 at-least-once，可能重复执行；上行 `best_effort` 可能丢一条，`at_least_once` 依赖终端重传，不是 exactly-once |
| 安全 | 业务 API 鉴权可关闭；TCP/UDP 接入与所有 HTTP 接口均未提供 TLS；管理面默认关闭，开启后要求 token |
| 形态 | 仓库不含前端控制台；可插拔上行 Sink（`local-db` / `noop` / 未来 `mq`）仍为设计概念，代码中只有按分类选择应答时机的 `uplink.Policy`，尚未落地出口配置段 |
| Action Hook | 回调是尽力而为：异步队列满即丢弃，HTTP 失败只记日志，不重试、不保证送达 |

## 2. 系统架构

架构按“接入、协议、业务、存储、投递、运维”分界，而不是按代码目录分界。这样切的原因是三件事的变化频率不同：终端连接与并发模型会随部署规模变化；消息编解码与版本差异随协议标准变化；业务落库与查询随产品需求变化。把三者隔开，才能在增加接入量时不改协议，在换存储时不改业务处理，在补版本差异时不碰网络层。

### 2.1 一条消息的流转

<img src="docs/.asset/pipeline_component.png" alt="消息处理组件" width="760">

TCP 与 UDP 复用同一条 `Pipeline`，每个会话一个协程串行处理：

1. **接入层**从 socket 读取字节流，去转义、校验并得到完整帧；
2. **协议层**把帧解码为消息头与消息体，按 MsgID 查注册表，分包未完成时先走分包缓存；
3. **业务处理**执行解码、落库/缓存/查询投递，并生成应答消息；
4. **协议层**把待发送消息编码回帧；
5. **接入层**写回连接，完成一次请求/应答。

业务处理与写回应答在同一个会话协程内执行，因此落库耗时会直接体现在应答时延上——这正是按消息分类选择“先应答”还是“先落库”的原因。接入层不关心业务字段，协议层不直接访问数据库，存储层也不理解协议语义。

### 2.2 典型交互时序

设备管理：注册 → 注册应答 → 鉴权 → 心跳保活 → 参数查询/设置。

<img src="docs/.asset/sequence_device_manage.png" alt="设备管理时序" width="420">

位置与报警：终端主动上报定位/报警，平台回 `0x8001`；平台可主动查询位置或设置临时跟踪。

<img src="docs/.asset/sequence_location_alarm.png" alt="位置与报警时序" width="420">

### 2.3 Standalone 与 Cluster 部署形态

<img src="docs/.asset/system-context-2.png" alt="单机形态系统上下文" width="720">

**Standalone** 是当前默认形态：接入、协议、业务、存储、附件服务和媒体服务都在一个进程内协作，启动即用，不需要外部中间件。

**Cluster** 是接入量增长后的拆分形态：接入层与业务层可按配置拆成独立服务，协议内核不变；业务数据外置到 MySQL，附件与媒体目录需要换成共享存储。拆分的关键不在进程数量，而在状态归属——节点本地状态必须通过会话粘滞或共享会话路由解决，跨节点下行投递才能成立：

| 状态 | 当前位置 | Cluster 下的处理 |
|---|---|---|
| 连接与会话、心跳、最近通信时间 | 节点内存 | 节点本地；需要会话粘滞或共享会话注册表 |
| 分包缓存 | 节点内存 | 节点本地；同一终端的分片应落到同一节点 |
| 参数/属性/位置查询的应答协调 | 节点内存 | 节点本地；下行请求需路由到持有会话的节点 |
| 设备档案、参数、属性、报警、定位历史 | `memory` 或 SQL | 外置到 MySQL 后可在实例间共享 |
| 下行命令 outbox | `memory` 或 SQL | 用 `durable` 落 SQL，靠租约避免重复占用；跨节点投递仍依赖会话路由 |
| 附件文件、H.264 文件 | 本地磁盘 | 需要对象存储或共享文件系统 |
| 1078 RTP 码流 | 节点 UDP 端口 | 终端与节点需保持对应关系，或在上层做流路由 |

当前仓库默认以 Standalone 形态运行；Cluster 所需的外部化存储与 outbox 租约已具备，共享会话路由与跨节点下行投递尚未实现，属于已知边界。

### 2.4 模块职责

| 模块 | 职责 | 不负责什么 |
|---|---|---|
| 终端接入 | TCP/UDP 监听、会话生命周期、保活判定、下行写回与超时重传 | 不解析业务字段，不决定落库 |
| 协议内核 | 消息头/消息体编解码、版本判别、分包重组、MsgID 注册与分发 | 不持有连接，不直接写数据库 |
| 业务处理 | 消息语义、行业数据缓存、查询应答协调、生成应答内容 | 不关心 socket 与帧格式 |
| 下行投递 | outbox 落库、幂等、租约、退避、过期与重投 | 不建立终端连接，不编码协议帧 |
| 存储 | 仓储接口与 `memory`/SQLite/MySQL 实现、迁移版本校验 | 不理解协议消息与应答语义 |
| 附件服务 | 苏标附件 TCP/UDP 组装、落盘、保留期清理 | 不参与 808 主连接的会话管理 |
| 媒体服务 | 1078 RTP 接收、PS 解复用、H.264 落盘与清理 | 不做转码、分发与回放 |
| 业务 API | 设备/历史查询、下行命令入口、附件元数据查询 | 不直接操作数据库，不持有协议会话 |
| 管理面 | 探针、指标、会话运维、历史分页 | 不参与终端协议处理链路 |
| 生命周期 Hook | 把注册、鉴权、定位、报警、断连事件异步通知第三方 | 不保证送达，不阻塞协议处理 |

### 2.5 连接建立、保活与断连

协议口径：

- **建立**：终端复位后尽快与平台建立 TCP 或 UDP 连接，连接建立后立即发送 `0x0102` 鉴权。
- **维持**：鉴权成功后，在没有正常数据包传输时，终端按参数指定周期发送 `0x0002` 心跳，平台回 `0x8001`。
- **断开**：平台和终端都可主动断开。平台侧的判定依据是 TCP 断开、相同身份的新连接顶替旧连接、或超过保活时间未收到任何消息；终端侧依据是链路断开、平台主动断开，或达到重传次数仍未收到应答。

实现上，每个会话注册一个保活检查任务，按设备保活间隔检查最近通信时间：超时未收到消息则标记离线；离线后再次到期则关闭连接并清理运行时缓存。SQL 后端只用进程内连接与最近心跳判定在线，**不会删除设备静态档案**。同一手机号建立新连接时，旧会话被顶替。

### 2.6 上行与下行可靠性语义

**上行：应答时机决定投递语义。** JT/T 808 的上行重传由终端负责：终端收到对应应答（通常为 `0x8001`）才停止重传，收不到就继续重传。因此平台发送应答的时机直接决定投递语义。配置段为 `uplink.reliability`：

| 分类 | 配置键 | 默认 | 行为 |
|---|---|---|---|
| `default` | `uplink.reliability.default` | `best_effort` | 其余消息 |
| `location` | `uplink.reliability.location` | `best_effort` | `0x0200` 且报警标志位为 0 |
| `batch_location` | `uplink.reliability.batchLocation` | `at_least_once` | `0x0704` 批量定位 |
| `alarm` | `uplink.reliability.alarm` | `at_least_once` | `0x0200` 且报警标志位非 0 |

- `best_effort`：先应答后落库；落库失败只记日志，不等终端重传。语义为 **at-most-once**，可能丢一条。
- `at_least_once`：先完成落库，成功后才应答；落库失败则本次不应答，终端按协议重传，从而获得 **at-least-once**。可能重复落库。

上述策略只作用于“应答内容固定、业务结果不参与应答”的上行消息（如 `0x0200`、`0x0704`、`0x0801`）。注册、鉴权、查询应答等需要业务结果决定应答内容的消息始终先执行业务再应答。`0x0801` 的应答是 `0x8800`，`0x1210/0x1211` 固定为 `0x0001`，其余通用应答为 `0x8001`。实现不引入本地文件队列、spool 或后台补偿线程；落库动作由存储层承担（`memory` 后端只在进程内，重启仍会丢失）。

**下行：会话重传 + outbox 重投。**

- 单条命令下发后按 `(手机号, MsgID, 流水号)` 等待终端 `0x0001/0x8001`；超时后按 `server.ackTimeoutMs` 重传，最多 `server.maxRetransmissions` 次，耗尽后计入 `jt808_ack_timeouts_total`。
- `commandOutbox.mode` 控制下行命令是否落 outbox：`off` 直接下发、不落库、不可查、不可重投；`memory` 保留幂等与状态查询、重启即丢；`durable` 写入 SQL 表、重启后仍可重投。未显式配置时按存储后端归一化：`memory` 后端默认 `memory`，SQL 后端默认 `durable`。
- 重投复用原始流水号，语义为 **at-least-once**：终端可能收到并执行重复命令，这是为保证应答能回填原 outbox 记录而接受的代价。
- 设备离线时 pending 命令保持等待，由租约与过期时间兜底；`maxAttempts` 达到上限或超过 `expireAfter` 后转 `failed`。
- 租约避免同一命令被多个实例同时占用，但会话本身是节点本地状态，多实例下仍不具备跨节点投递语义。

## 3. 技术栈

| 类别 | 选型 | 用途 |
|---|---|---|
| 语言 | Go 1.26 | 并发模型、静态编译、低运行时依赖 |
| HTTP | `gin-gonic/gin` | 业务 API 与管理面路由 |
| 配置 | `spf13/viper` + YAML | 文件配置、环境变量覆盖、内嵌默认配置 |
| 日志 | `rs/zerolog` + `lumberjack` | 结构化日志、按大小滚动 |
| SQLite | `modernc.org/sqlite` | 纯 Go 驱动，无需 cgo，单机持久化 |
| MySQL | `go-sql-driver/mysql` | 共享业务数据与多实例部署 |
| 编解码 | `golang.org/x/text`、`cn/GB2260.go`、`mix-go/xfmt` | GBK、行政区划、十六进制与终端数据生成 |
| 测试 | `stretchr/testify` | 断言与端到端测试辅助 |

**刻意不引入**：

- **ORM**：仓储接口与 SQL 都显式书写，迁移文件可审计；ORM 的隐式查询与自动迁移不适合协议字段固定、版本受控的场景。
- **cgo SQLite**：交叉编译与容器镜像更简单，代价是部分扩展能力不可用。
- **消息队列 / Redis / Kafka**：当前单机形态不需要；集群所需的共享状态优先由 SQL 与会话路由解决，避免为未落地能力预埋中间件。
- **前端控制台**：服务只提供 JSON API，界面由使用者自建。
- **TLS 终结**：协议接入与 HTTP 均未内建 TLS，生产环境由反向代理或网关承担。
- **本地 spool / 补偿线程**：上行可靠性用应答时机表达，不引入额外的本地队列。

## 4. 安装与部署

### 4.1 5 分钟跑通

前置条件：Go 1.26+、`make`、`bash`。默认配置不需要数据库。

```sh
# 终端 1：编译并启动服务（内嵌默认配置，memory 后端）
make compile && make run

# 终端 2：启动自带模拟终端，完成注册、鉴权、心跳与位置上报
make run-client

# 终端 2：查出一条已注册设备
curl -s http://localhost:8008/api/device
```

看到设备列表中出现模拟终端的手机号，说明接入、协议处理与查询链路已经打通。默认 `storage.type: memory`，重启后数据丢失；附件服务器默认开启，媒体服务与管理面默认关闭，业务 API 鉴权默认关闭。

### 4.2 编译

```sh
# 本地调试二进制：target/debug/jt808-server-go，并复制 configs/
make compile

# 交叉编译 darwin/linux amd64+arm64：target/release/jt808-server-go 与 jt808-migrate
make release

# 模拟器与管理工具
make compile-client      # target/debug/jt808-client-go
make release-client      # target/release/jt808-client-go（四个平台产物）
make compile-migrate     # target/debug/jt808-migrate
```

### 4.3 运行

不传 `-c` 时使用编译进二进制的内嵌默认配置；`-c <file>` 才读取 YAML 文件：

```sh
./target/debug/jt808-server-go                        # 内嵌默认配置
./target/debug/jt808-server-go -c configs/default.yaml
```

启动横幅可通过 `server.banner.bannerPath` 指向的文件自定义，默认 `configs/banner.txt`；`server.banner.enable: false` 可关闭。

### 4.4 Docker Compose

```sh
make dockerbuild

docker compose -f deploy/docker-compose.yml up --build
```

镜像入口为 `/app/output/jt808-server-go -c /app/output/configs/default.yaml`，同时包含 `/app/output/jt808-migrate`。compose 映射 `8080`、`8081/udp`、`8008`、`7200`、`7201/udp`，挂载只读配置与 `data`/`logs` 卷。管理面默认只监听容器内 `127.0.0.1:9000`，不发布到宿主机；compose 的 healthcheck 依赖 `/healthz`，因此需要先在配置中开启 `admin` 并配置非空 token，否则容器会一直处于不健康状态。

### 4.5 数据库迁移

SQLite 与 MySQL 都不在服务启动时自动执行迁移。首次部署或升级后先运行独立工具：

```sh
# 先在 configs/default.yaml 中设置 storage.type 与连接参数
make compile-migrate
./target/debug/jt808-migrate -c configs/default.yaml

# 然后再启动主服务
make run
```

迁移文件位于 `internal/repository/migration/`，按 `<日期>_<描述>.sql` 命名。主服务启动只校验 `schema_migrations` 版本：版本落后会提示先执行 `jt808-migrate`，版本超前会拒绝启动。MySQL 场景也可以由 DBA 按文件名顺序直接执行这些 SQL。

**时区语义变更（`20261113_bcd_time_gmt8.sql`）**：旧版本把终端 BCD 时间当作 UTC 解析，属于“伪 UTC”。该迁移会把 `locations.report_time_ms`、`alarms.occurred_at_ms`、`media_files.location_time` 统一减去 8 小时；`media_resource_lists.items_json` 内嵌的 `startTime`/`endTime` 原地从 `Z` 标签改为显式 `+08:00`（墙钟不变、瞬时值同样减 8 小时），不清空数据。`received_at*` 等服务端时间不在迁移范围内。执行前必须备份数据库；回滚方式见迁移文件头部注释，且回滚必须在启动新版本前完成。

## 5. 配置说明

配置文件默认为 `configs/default.yaml`，完整注释见该文件。按用途分组如下。

### 5.1 存储与保留

| 配置键 | 默认 | 说明 |
|---|---|---|
| `storage.type` | `memory` | `memory` / `sqlite` / `mysql`；空值等价 `memory` |
| `storage.sqlite.path` | `./data/jt808.db` | `sqlite` 后端库文件 |
| `storage.mysql.dsn` | 空 | `mysql` 后端 DSN，如 `user:pass@tcp(host:port)/db` |
| `retention.locations.retentionDays` / `maxPerDevice` | `7` / `10000` | 定位历史保留；仅 SQL 后端生效 |
| `retention.alarms.dedupWindowMs` / `confirmExpireMs` / `maxPerDevice` | `1800000` / `600000` / `1000` | 报警去重、确认关联与上限 |

定位 7 天/10000 条为工程默认值（JT/T 808 未规定平台侧保存期限）；报警 30 分钟去重、10 分钟确认关联、1000 条上限沿用既有内存实现。显式写 `0` 表示关闭对应策略。`memory` 后端下 `retention` 不生效。

### 5.2 接入与协议

| 配置键 | 默认 | 说明 |
|---|---|---|
| `server.port.tcpPort` | `8080` | 终端 TCP 接入；置空可关闭 |
| `server.port.udpPort` | `8081` | 终端 UDP 接入；置空可关闭 |
| `server.port.httpPort` | `8008` | 业务 HTTP API |
| `server.ackTimeoutMs` | `5000` | 下行命令等待应答的超时 |
| `server.maxRetransmissions` | `3` | 下行命令最多重传次数 |
| `server.action.http.url` | 空 | 生命周期 Hook 回调地址；空则不启用 |
| `server.action.http.timeoutMs` | `2000` | Hook 回调超时 |
| `server.banner.enable` / `bannerPath` | `true` / `configs/banner.txt` | 启动横幅 |

### 5.3 业务 API 与管理面

| 配置键 | 默认 | 说明 |
|---|---|---|
| `api.auth.token` | 空 | 业务 API Bearer token；空表示关闭鉴权，生产必须配置 |
| `api.rateLimit.ratePerSecond` / `burst` | `50` / `100` | 业务 API 按来源 IP 的令牌桶 |
| `admin.enable` | `false` | 是否启动独立管理面 |
| `admin.addr` / `token` | `127.0.0.1:9000` / 空 | 管理面监听地址；开启时 token 必填 |
| `admin.rateLimit.ratePerSecond` / `burst` / `keyBy` | `10` / `20` / `credential` | 管理面令牌桶；`keyBy` 可为 `credential` 或 `source` |

### 5.4 附件与媒体

| 配置键 | 默认 | 说明 |
|---|---|---|
| `alarmServer.port` / `udpPort` | `7200` / `7201` | 苏标附件 TCP / UDP 监听；两者都为空时整段不启动 |
| `alarmServer.dir` | `./data/alarms` | 附件落盘目录 |
| `alarmServer.maxFileSize` / `maxConcurrent` | `104857600` / `16` | 单附件上限与并发组装数 |
| `alarmServer.completedTTL` | `20m` | 已完成附件的内存状态保留时长 |
| `alarmServer.retentionDays` | `7` | 已完成附件文件保留天数；`0` 表示不清理 |
| `alarmServer.autoUpload` / `uploadIP` | `false` / 空 | 报警首次上报时是否自动下发 `0x9208` |
| `mediaServer.port` | 空 | 1078 RTP UDP 监听；空则不启动 |
| `mediaServer.dir` / `retentionDays` | `./data/media` / `7` | `.h264` 落盘目录与保留天数；`0` 表示不清理 |
| `mediaUpload.dir` | `./data/media-uploads` | `0x0801` 多媒体文件落盘目录，始终启用；Cluster 形态需指向共享卷 |

### 5.5 下行 outbox 与上行可靠性

| 配置键 | 默认 | 说明 |
|---|---|---|
| `commandOutbox.mode` | 按后端归一化 | `off` / `memory` / `durable`；见 2.6 |
| `commandOutbox.pollInterval` / `batchSize` | `10s` / `50` | 重投轮询周期与批次 |
| `commandOutbox.maxAttempts` / `expireAfter` | `5` / `24h` | 总下发次数与过期兜底 |
| `commandOutbox.leaseDuration` / `initialWait` | `1m` / `1m` | 投递租约与首次等待窗口 |
| `commandOutbox.retryBase` / `retryMax` | `5s` / `5m` | 退避区间 |
| `uplink.reliability.default` | `best_effort` | 其余上行消息 |
| `uplink.reliability.location` | `best_effort` | 普通定位 `0x0200` |
| `uplink.reliability.batchLocation` | `at_least_once` | 批量定位 `0x0704` |
| `uplink.reliability.alarm` | `at_least_once` | 报警定位 `0x0200` |

### 5.6 日志

| 配置键 | 默认 | 说明 |
|---|---|---|
| `log.level` | `info` | 日志级别 |
| `log.console` / `log.file` | 见默认配置 | 控制台与文件输出开关 |
| `log.maxSize` / `maxBackups` / `maxAge` | 见默认配置 | 滚动大小、保留份数与天数 |

### 5.7 配置示例

切换到 SQLite、开启鉴权与管理面、使用 durable outbox：

```yaml
storage:
  type: "sqlite"
  sqlite:
    path: "./data/jt808.db"

api:
  auth:
    token: "change-me"

admin:
  enable: true
  addr: "127.0.0.1:9000"
  token: "change-me"

uplink:
  reliability:
    default: "best_effort"
    location: "best_effort"
    batchLocation: "at_least_once"
    alarm: "at_least_once"

commandOutbox:
  mode: "durable"
```

## 6. 使用方法与 API

### 6.1 监听端口

| 入口 | 默认地址 | 配置 | 说明 |
|---|---|---|---|
| 终端接入 TCP | `:8080` | `server.port.tcpPort` | 808/1078 信令主连接 |
| 终端接入 UDP | `:8081` | `server.port.udpPort` | 置空即关闭 |
| 业务 HTTP API | `:8008` | `server.port.httpPort` | `/api`，Token 鉴权 + 按 IP 限流 + 审计 |
| 管理面 | `127.0.0.1:9000` | `admin.addr` | 默认不启动；开启后要求 Bearer token |
| 苏标附件 TCP/UDP | `:7200` / `:7201/udp` | `alarmServer.port` / `udpPort` | 两者都为空时不启动 |
| 1078 RTP | 配置值 | `mediaServer.port` | 独立 UDP 端口，空则不启动 |

### 6.2 业务 API

公共前缀 `/api`。除查询类接口外，变更类请求会做鉴权与审计；下行接口在 outbox 开启时支持 `Idempotency-Key` 请求头，重复键返回已有命令记录而非二次下发。

| 方法 | 路径 | 用途 |
|---|---|---|
| GET | `/api/commands/:commandId` | 查询下行命令状态；outbox 关闭时返回 404 |
| GET | `/api/device` | 列出已注册设备 |
| GET | `/api/device/:phone/reports/:kind` | 查询行业上报最新快照；`kind` 为 `driver`、`can`、`recorder`、`media-event`、`media-capture`、`media-search`、`media-uploads`、`transparent`、`peripheral` |
| GET | `/api/device/:phone/geo` | 设备档案 + 最新定位 |
| GET | `/api/device/:phone/params` | 查询本地保存的终端参数 |
| GET | `/api/device/:phone/properties` | 查询本地保存的终端属性 |
| GET | `/api/device/:phone/location` | 实时查询位置：下发 `0x8201`，等待 `0x0201`，10 秒超时 |
| POST | `/api/device/:phone/control` | 终端控制 `0x8105` |
| POST | `/api/device/:phone/regions/delete` | 删除区域或路线 |
| POST | `/api/device/:phone/regions/set` | 设置区域或路线 |
| POST | `/api/device/:phone/regions/query` | 查询区域或路线 |
| POST | `/api/device/:phone/driver/query` | 司机身份查询 `0x8702` |
| POST | `/api/device/:phone/recorder/command` | 记录仪命令 `0x8700` / `0x8701` |
| POST | `/api/device/:phone/media/capture` | 摄像头立即拍摄 `0x8801` |
| POST | `/api/device/:phone/media/search` | 存储多媒体数据检索 `0x8802` |
| POST | `/api/device/:phone/media/resources` | 查询音视频资源列表 `0x9205` |
| POST | `/api/device/:phone/media/command` | 多媒体上传/录音控制 `0x8803` / `0x8804` / `0x8805` |
| POST | `/api/device/:phone/text/issue` | 文本信息下发 `0x8300` |
| POST | `/api/device/:phone/events/set` | 事件设置 `0x8301` |
| POST | `/api/device/:phone/vehicle/control` | 车辆控制 `0x8500` |
| POST | `/api/device/:phone/question/issue` | 提问下发 `0x8302` |
| POST | `/api/device/:phone/info/menu/set` | 信息菜单设置 `0x8303` |
| POST | `/api/device/:phone/info/service` | 信息服务 `0x8304` |
| POST | `/api/device/:phone/call/callback` | 电话回拨 `0x8400` |
| POST | `/api/device/:phone/phonebook/set` | 电话本设置 `0x8401` |
| POST | `/api/device/:phone/terminal/upgrade` | 终端升级 `0x8108` |
| POST | `/api/device/:phone/transparent/down` | 数据下行透传 `0x8900` |
| POST | `/api/device/:phone/alarms/confirm` | 人工确认报警 `0x8203` |
| GET | `/api/device/:phone/alarms` | 查询报警记录 |
| PUT | `/api/device/:phone/params` | 设置终端参数 `0x8103` |
| POST | `/api/device/:phone/attachments/upload` | 下发 `0x9208` 报警附件上传指令 |
| GET | `/api/device/:phone/attachments` | 列出已接收附件 |
| GET | `/api/device/:phone/attachments/:fileName` | 查询单个附件的元数据（不返回文件内容） |
| GET | `/api/device/:phone/media/files` | 查询 `0x0801` 媒体文件；参数 `channel`、`type`、`start`、`end`、`alarmOnly`、`limit` |
| GET | `/api/device/:phone/media/files/:mediaId` | 查询单个媒体文件的元数据 |
| GET | `/api/device/:phone/media/files/:mediaId/content` | 下载媒体文件内容，支持 HTTP Range |
| GET | `/api/device/:phone/media/captures` | 查询 `0x0805` 拍摄记录及各 mediaId 的文件到达状态 |

请求体字段直接复用协议模型，字段级定义见 `internal/protocol/model/`；路由与参数校验见 `internal/api/api.go`。

**时间口径**：所有由终端上报 BCD 时间派生的时间（定位、报警、多媒体位置时间、1078 资源列表等）统一按固定 GMT+8 解析为真实瞬时值，不随服务器时区变化；存储为毫秒时间戳。业务 API 中不带时区的时间参数（`YYYY-MM-DD HH:MM:SS`，如 1078 资源查询的 `startTime`/`endTime`、媒体文件查询的 `start`/`end`）按同一口径解释，保证存储与查询一致。管理面 `/locations`、`/alarms` 的 `from`/`to` 使用带时区的 RFC3339，不受此规则影响。**已知限制：`0x0801` 位置体只有报警标志位、没有报警流水号，返回的 `alarmSign` 只能表达报警类别，`alarmOnly=true` 也只能按类别筛选，不能与某一条具体报警精确关联。**

### 6.3 调用示例

下发一条文本信息，返回 `202` 表示已受理并开始投递：

```sh
curl -s -X POST http://localhost:8008/api/device/13000000000/text/issue \
  -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: demo-1' \
  -d '{"flag":0,"textType":1,"text":"hello"}'
```

```json
{
  "commandId": "0f8c2f6d9a1e4b7c8d3e5f60718293a4",
  "duplicate": false,
  "messageId": "0x8300"
}
```

查询命令最终状态（`pending` / `acked` / `failed`）：

```sh
curl -s http://localhost:8008/api/commands/0f8c2f6d9a1e4b7c8d3e5f60718293a4
```

```json
{
  "commandId": "0f8c2f6d9a1e4b7c8d3e5f60718293a4",
  "phone": "13000000000",
  "messageId": "0x8300",
  "serial": 12,
  "status": "acked",
  "attempts": 1
}
```

### 6.4 管理面与可观测性

需 `admin.enable: true` 与 `admin.token`，`/healthz`、`/readyz` 本身不校验 token：

| 方法 | 路径 | 鉴权 | 说明 |
|---|---|---|---|
| GET | `/healthz` | 否 | 存活探针，恒返回 200 |
| GET | `/readyz` | 否 | 就绪探针，返回各检查项明细；失败返回 503 |
| GET | `/ping` | 是 | 管理面连通性检查 |
| GET | `/metrics` | 是 | Prometheus 文本格式指标 |
| GET | `/sessions` | 是 | 当前实例会话列表 |
| GET | `/sessions/:phone` | 是 | 当前实例会话详情 |
| DELETE | `/sessions/:phone` | 是 | 强制断开当前实例上的会话 |
| GET | `/locations` | 是 | 历史分页查询，必须带 `phone`、`from`、`to` |
| GET | `/alarms` | 是 | 历史分页查询，必须带 `phone`、`from`、`to` |

会话类接口只作用于当前进程，跨实例无效；`/locations`、`/alarms` 读取持久化历史（`memory` 后端下 `/locations` 不可用）。

`/readyz` 依次检查 `storage`（SQL Ping）、`outbox_backlog`（过期 pending 命令数，默认阈值 100）、`media_disk`、`attachment_disk`、`media_upload_disk`；未启用的组件不参与判定，避免把“功能关闭”误报成“服务异常”。`/metrics` 当前导出：

- `jt808_sessions_connected`
- `jt808_ack_pending`
- `jt808_ack_timeouts_total`
- `jt808_outbox_pending`
- `jt808_outbox_expired`
- `jt808_media_streams`
- `jt808_attachments_active`
- `jt808_attachments_completed_total`
- `jt808_media_files`（`0x0801` 落盘文件数量）
- `jt808_media_file_bytes`（`0x0801` 落盘文件总字节数）

业务 API 的变更类请求（POST/PUT/PATCH/DELETE）与鉴权失败、限流拒绝会写入审计日志（zerolog），只记录方法、路径、来源 IP、状态码、手机号与耗时，不记录请求体与 token。服务收到 `SIGINT`/`SIGTERM` 后按顺序优雅关机：停业务 API → 停终端接入并关闭会话 → 停 outbox 重投 → 刷盘媒体/附件 → 最后停管理面并关闭存储。

### 6.5 附件服务器与媒体服务

**苏标附件服务器**同时监听 TCP 与 UDP，共用同一套组装逻辑，处理 `0x30/0x31/0x63/0x64` 文件码流帧，落盘目录为 `<alarmServer.dir>/<手机号>/<YYYYMMDD>/<报警编号>/<文件名>`。`alarmServer.autoUpload` 为 `false` 时仅通过 `POST /api/device/:phone/attachments/upload` 手动下发 `0x9208`；开启后需配置 `alarmServer.uploadIP`，`0x9208` 通告的端口取自 `alarmServer.port` 与 `alarmServer.udpPort` 的实际监听值，未配置的一侧通告为 `0`。

**1078 媒体服务**在配置 `mediaServer.port` 后启用独立 UDP 端口接收 RTP 码流：仅支持 UDP 与 H.264，把 PS/ES 还原为 Annex B 裸流并落成 `.h264` 文件，路径为 `<mediaServer.dir>/<手机号>/channel_<通道号>/<时间戳>.h264`。不做转码、分发、播放器索引或回放。已实现的 1078 信令为 `0x9101`、`0x9102`、`0x9105`、`0x9201`、`0x9202`、`0x9205`、`0x9206`、`0x1205`、`0x1206`；`0x1205` 资源列表与 `0x1206/0x9206` 媒体元数据支持持久化。

**0x0801 多媒体落盘**始终启用：完整消息（分包由运行时重组，非分包单包）写入 `<mediaUpload.dir>/<手机号>/<YYYYMMDD>/<媒体ID>.<扩展名>`，日期取位置体 BCD 时间，缺失时用接收日期；扩展名按格式映射为 `jpg/tif/mp3/wav/wmv`，未知格式回退 `bin`。写文件采用临时文件 + 原子 rename，同一 mediaId 重复上报幂等覆盖。元数据（含 MD5、字节数、位置与报警标志位）写入 `media_files` 表；`0x0805` 的 mediaId 列表写入 `media_captures` 与 `media_capture_items`，查询时关联 `media_files` 判断文件是否已到达。文件内容通过 `GET /api/device/:phone/media/files/:mediaId/content` 下载，支持 Range。本批不做保留期清理，请用 `jt808_media_files` 与 `jt808_media_file_bytes` 监控磁盘占用。

### 6.6 Action Hook

`server.action.http.url` 用于把消息处理生命周期事件回调给第三方业务平台：注册、鉴权、定位、报警、断连五类事件以 JSON 异步 POST 到该地址。设计意图是让工单、风控、通知等业务逻辑留在平台侧，不必耦合进协议进程。回调队列容量 256，队列满时丢弃事件；HTTP 失败只记日志，不重试，也不影响应答。因此它适合通知型场景，不适合作为可靠事件总线。关闭时留空即可。

### 6.7 运维要点

数据备份按后端处理：SQLite 直接备份 `storage.sqlite.path` 指向的库文件；MySQL 使用常规逻辑或物理备份。附件与媒体目录独立于数据库，需要一并备份 `alarmServer.dir`、`mediaServer.dir` 与 `mediaUpload.dir`。压测可用 `bash scripts/loadtest.sh [持续秒数]`（默认 60 秒、200 并发模拟终端、memory 后端），结束时从 `/metrics` 采集连接数、待应答数、应答超时与 outbox 指标。

## 7. 开发指导

### 7.1 目录结构

| 目录 | 内容 |
|---|---|
| `cmd/jt808-server` | 主服务启动、组件装配、信号处理与优雅关机 |
| `cmd/jt808-client` | JT808 终端模拟器入口 |
| `cmd/jt808-migrate` | 独立数据库迁移工具 |
| `internal/protocol` | 帧处理、编解码、Pipeline、消息注册表与业务处理 |
| `internal/protocol/model` | 各 MsgID 的消息结构、终端与消息头定义、会话与分包 |
| `internal/codec` | hex、GBK、哈希、行政区划等编解码 |
| `internal/server` | TCP/UDP 监听、会话生命周期、下行应答与重传 |
| `internal/runtime` | 进程内运行时状态：连接注册表、心跳、跟踪、查询应答协调 |
| `internal/repository` | 持久化仓储接口；`sqlstore` 为 SQLite/MySQL 实现 |
| `internal/repository/migration` | 版本化 SQL 迁移文件 |
| `internal/storage` | 存储后端装配，聚合为 `Stores` |
| `internal/uplink` | 上行可靠性分类与 `best_effort` / `at_least_once` 策略 |
| `internal/command` | 下行命令 outbox 重投循环 |
| `internal/alarmserver` | 苏标附件服务器（TCP/UDP、文件组装、保留期清理） |
| `internal/media` | 1078 RTP UDP 接收、PS 解复用、H.264 落盘 |
| `internal/api` | 业务 HTTP API（`/api`） |
| `internal/adminapi` | 独立管理面、探针、指标与会话运维 |
| `internal/observability` | 健康检查、就绪检查、Prometheus 文本指标 |
| `internal/httpguard` | 业务 API 鉴权、按来源 IP 限流、操作审计 |
| `internal/action` | 消息处理生命周期 Hook 与 HTTP 回调实现 |
| `internal/config` | YAML 配置加载、内嵌默认值与校验 |
| `pkg/` | 通用小工具：日志、容器/依赖装配、安全协程封装 |
| `build/` | Dockerfile 与镜像构建上下文 |
| `deploy/` | compose 与部署产物 |
| `scripts/` | 构建、冒烟与负载脚本 |
| `client/` | 模拟器配置与数据生成 |

### 7.2 新增一条消息处理

1. 在 `internal/protocol/model/` 新建 `msg_XXXX.go`，实现 `Decode` / `Encode`，按 `Header.Attr.VersionDesc` 处理版本差异；
2. 在 `internal/protocol/msg_processor.go` 的注册表登记 MsgID，绑定解码模型与处理函数；
3. 在处理函数中完成业务逻辑与应答生成；需要落库时只调用 `internal/repository` 的接口，不直接写 SQL；
4. 在 `internal/protocol/model/` 与 `internal/protocol/` 补单元测试，必要时在 `internal/server/` 补端到端用例。

### 7.3 迁移文件命名规范

- 文件名格式为 `<日期>_<描述>.sql`，例如 `20261111_upgrade_results.sql`；
- 同一天只能有一个迁移文件，当日追加的变更合并进同一个文件；
- SQL 必须同时兼容 SQLite 与 MySQL：不使用方言专属子句，不使用 `TEXT`/`BLOB` 默认值；
- 迁移由独立工具按文件名顺序执行，主服务只校验 `schema_migrations` 版本。

### 7.4 代码与文档约定

- 日志与 `error` 信息使用英文，便于检索与对接；
- 注释、文档、测试业务数据使用中文；
- 描述上行语义时统一用“应答时机”，不混用“时序”与“时机”；
- 协议字段以标准为准，版本差异通过消息头判别后分支处理，不做无依据的兼容猜测。

### 7.5 测试

```sh
# 单元测试 + race + 覆盖率，产出 target/test/
make test

# 构建真实 server/client 二进制并跑 5 个端到端场景
make smoke
```

`make smoke` 覆盖：注册→鉴权→注销、全量上行消息、常驻终端心跳/定位、分包乱序与 `0x8003` 补传、异常帧拒绝后继续处理正常帧。可用 `SMOKE_TCP_PORT` / `SMOKE_UDP_PORT` / `SMOKE_HTTP_PORT` 覆盖端口，`--keep-logs` 保留日志。

模拟器还可用于故障注入与分包测试：

```sh
# 注册消息按 16 字节分包、逆序发送、丢弃第 2 片
./target/debug/jt808-client-go -c client/configs/default.yaml \
  -fragment-size 16 -reverse-fragments -drop-fragment 2

# 注册前先发异常帧：bad-checksum / bad-escape / oversize
./target/debug/jt808-client-go -c client/configs/default.yaml \
  -fault-frame bad-escape
```

模拟器默认走 TCP。验证 UDP 接入时，修改 `client/configs/default.yaml`：

```yaml
device:
  transProto: "UDP"
```

提交前建议至少执行：

```sh
gofmt -l .
go build ./...
go vet ./...
go test -race -count=1 ./...
```

## 8. 贡献指南

- 从最新 `main` 新建 `dev/<主题>` 分支，不直接向 `main` 提交；
- 一个提交只做一件事，提交信息说明“为什么改”，不只描述“改了什么”；
- 新增或修改协议消息时，同步补模型测试与处理函数测试；
- 提交前跑完 7.5 的检查项；不提交构建产物、数据库文件、日志与本地配置；
- 通过 Pull Request 合并，描述中写清行为变化、兼容性与验证方式。

## 9. 许可证

[MIT](LICENSE)
