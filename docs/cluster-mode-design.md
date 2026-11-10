# 集群模式（Cluster）架构设计方案

- 状态：**设计阶段，未修改业务代码**
- 代码基线：`origin/main` `b557c8f`（2026-11-10，`Merge pull request #46 from fakeyanss/dev/retention`）
- 长期开发分支：`dev/cluster_mode`（本文档随该分支提交）
- 适用范围：JT/T 808 信令网关、JT/T 1078 媒体角色、苏标附件角色、API 管控角色的集群化拆分
- 不适用范围：多地域容灾、跨数据中心双活、媒体转码与分发、对外 API 网关与负载均衡实现
- 关联文档：
  - [gateway-mode-design.md](gateway-mode-design.md)（接入网关边界，本文档定义其 M3/M4 接口）
  - [uplink-sink-design.md](uplink-sink-design.md)（上行出口语义与可插拔 Sink）
  - [storage-abstraction-design.md](storage-abstraction-design.md)（仓储与运行时状态分层）
  - [operations-runbook.md](operations-runbook.md)（部署与运维）

> 本文只做架构方案与分阶段计划。所有结论以当前 `main` 代码事实和协议事实为依据；涉及外部依赖时同时说明不引入依赖的形态。

---

## 0. 分支与合入策略

### 0.1 长期开发分支

本次集群模式使用一条长期开发分支 `dev/cluster_mode`，不直接在主线上迭代：

1. 从最新 `main` 创建 `dev/cluster_mode`，并推送为上游分支（`git push -u origin dev/cluster_mode`）。
2. 集群模式的**所有阶段**（E0–E4，见第 11 节）都合入 `dev/cluster_mode`。
3. **每个阶段 PR 的目标分支都是 `dev/cluster_mode`**，开发过程中不向 `main` 提 PR。
4. 只有全部阶段开发完成、集群验收通过后，才把 `dev/cluster_mode` 一次性合入 `main`。
5. 开发期间 `main` 保持稳定。`main` 上的缺陷修复合入后，需要把 `main` 反向合并回 `dev/cluster_mode`，避免最后一次性合并时积累冲突。
6. 建议对 `dev/cluster_mode` 开启分支保护：CI 必须全绿、禁止 force push、禁止绕过评审直接推送。

```text
main ──●──────────────────────────────────────────────●──>  最终一次性合入
        \                                            /
         ●──●──●──●──●──●──●──●──●──●──●──●──●──●──●
            E0   E1    E2     E3      E4
            └── 每个阶段 PR 的目标分支都是 dev/cluster_mode ──┘
```

### 0.2 阶段与目标分支

| 阶段 | 内容 | PR 目标分支 | 是否允许单独上线 |
|---|---|---|---|
| E0 | 角色抽象、两个契约的接口与本地实现、配置骨架 | `dev/cluster_mode` | 允许，行为不变 |
| E1 | 上行出口可插拔（内置处理 / HTTP hook / MQ） | `dev/cluster_mode` | 允许，默认内置 |
| E2 | 下行交互契约、outbox 归属上移 | `dev/cluster_mode` | 允许，standalone 同进程 |
| E3 | 物理角色拆分、媒体/附件端点分配与粘性 | `dev/cluster_mode` | 允许，按配置启用 |
| E4 | 集群加固、可观测性、运维手册 | `dev/cluster_mode` | 允许 |

> 实际执行顺序：上行出口可插拔、终端应答 → outbox 回写已先行落地；当前 E1 的剩余部分是**进程角色拆分与两个内部 HTTP 契约**（见 11 节 E1），其 cluster 形态仅用于契约验证，不可上生产。

### 0.3 最终合入 `main` 的条件

- E0–E4 全部合入 `dev/cluster_mode`；
- `standalone` 全量回归通过，行为、端口、外部依赖与当前版本一致；
- 集群端到端场景通过（多网关、管控重启、媒体/附件粘性、故障切换）；
- 文档、配置示例、运维手册同步更新；
- 以单个 PR 合入 `main`，不把阶段 PR 分散提到 `main`。

---

## 1. 结论摘要

| 问题 | 结论 |
|---|---|
| 角色划分 | **808 网关、1078 媒体、苏标附件、API 管控**四个逻辑角色；同一套代码，配置决定进程承担哪些角色。 |
| 808 网关职责 | 只持有贴着连接的状态：会话、编解码、分包重组/重传、ack 配对、有界内存下发状态。**不写业务表、不持有 durable outbox、不分配媒体端点、不做命令编排、不提供管理面。** |
| 上行出口 | 统一出口向上抛：HTTP hook、MQ 投递，或直连内置处理（standalone / 同进程）。出口分 `best-effort` 与 `confirm-before-ack` 两档语义。 |
| 下行契约 | 带幂等键的下发接口 + 同步 ack / 异步回调 / 结果查询 / fire-and-forget；流水号由网关在发送时分配。 |
| outbox 归属 | durable 幂等、租约、重投属于**管控层**；网关只保留连接存活期内的有界重试与同步 ack 结果。standalone 下管控与网关同进程，现有实现在进程内复用，是平移不是删除。 |
| 1078 媒体 | 协议自带目标地址，终端另开连接推流；**网关只发信令字节，不参与数据面，不调用媒体**。端点由媒体角色分配，管控层申请并构造 `0x9101/0x9201`。 |
| 苏标附件 | 协议自带目标地址（`0x9208`），终端另开连接上传；**网关只发信令字节，不参与数据面，不调用附件**。端点由附件角色分配，管控层申请并构造 `0x9208`。 |
| `0x0801` | 只承载图片（JT/T 1078-2016 §5.2）。数据在 808 消息体内、按分包上传、报文体**没有目标地址**，必然经过网关；**定稿方案 A：上行出口 → 管控 → 媒体文件接入**，网关不直连媒体。音频/视频类型记录并告警，不落库、不静默丢弃。 |
| 管控层可替换 | 管控层实现两个契约的调用端；内置管控或客户自有管控系统都可对接，网关不感知业务语义。未来后台管理页面属于管控层。 |
| Standalone | 零外部依赖不变；未配置集群段时不初始化任何新客户端。 |
| 演进 | E0 抽象与契约 → E1 上行出口 → E2 下行契约与 outbox 上移 → E3 角色拆分与端点分配 → E4 集群加固。 |

---

## 2. 角色定义与边界

### 2.1 四个逻辑角色

| 角色 | 数据面 | 负责 | 不负责 |
|---|---|---|---|
| **808 网关** | 808 TCP/UDP 连接 | 拆帧、转义、校验、编解码；注册/鉴权/心跳的连接现场；分包重组与重传；下行命令写入 socket 与 ack 配对；通过上行出口抛事件；执行业务侧提交的下发请求 | 业务落库、设备档案、命令编排、durable outbox、媒体/附件端点分配、媒体/附件数据、管理面 |
| **1078 媒体** | 1078 TCP/UDP 的 RTP/PS | 接收终端推流；PS 解复用；SPS/PPS 缓存；媒体落盘；流注册表；媒体端点分配与粘性 | 808 会话、下行 808 信令构造、业务档案 |
| **苏标附件** | 附件服务器端口 | 接收附件分片；缺口记账；组装完成态；附件落盘；附件端点分配与粘性 | 808 会话、`0x9208` 构造、报警业务编排 |
| **API 管控** | 不监听终端端口 | 业务落库与查询；设备运维；命令编排；下行投递（durable outbox）；媒体/附件端点申请与信令构造；鉴权数据提供；未来的后台管理页面 | 终端连接、协议编解码、分包/重传、ack 配对 |

### 2.2 逻辑角色与物理进程

- 四个角色是**逻辑边界**，不是四个必须独立部署的二进制。
- 同一份代码通过配置决定进程承担哪些角色，例如：
  - `standalone`：一个进程承担全部角色，零外部依赖；
  - `gateway`：只承担 808 网关；
  - `gateway+media`：网关与媒体同进程，减少一跳；
  - `control`：只承担 API 管控；
  - `media` / `attachment`：只承担对应数据面。
- 角色拆分不改变协议内核。协议编解码、分包、ack 配对只有一份实现。

### 2.3 通信方向与“不成环”

允许的调用方向：

```text
外部调用方 ──HTTP──> API 管控
API 管控 ──下行契约──> 808 网关
API 管控 ──端点申请/读取──> 1078 媒体 / 苏标附件
API 管控 ──落库/查询──> 共享存储
808 网关 ──上行出口──> API 管控（或 MQ）
808 网关 ──鉴权只读查询──> API 管控
1078 媒体 ──元数据/事件──> 共享存储 / 事件出口
苏标附件 ──元数据/事件──> 共享存储 / 事件出口
```

禁止的调用方向：

| 禁止 | 原因 |
|---|---|
| 网关 → 媒体/附件 | 网关不参与数据面，也不应因媒体/附件故障被拖慢或阻塞 |
| 网关 → 网关（跨节点转发会话） | 会话粘在连接上，转发会引入第二跳与状态一致性负担 |
| 媒体/附件 → 网关同步调用 | 反向依赖会让数据面受信令面影响 |
| 媒体/附件 → 管控同步调用并要求管控再调网关 | 形成 `媒体 → 管控 → 网关` 长链，任何一环抖动都会放大 |
| 管控在上行收件处理中同步等待下行结果 | 形成 `网关 → 管控 → 网关` 环，可能自锁 |

**不成环规则**：管控处理上行事件时，只做落库/计算/返回受理结果；需要下发时，走异步编排，不在同一次调用栈里同步等待网关。

### 2.4 管控层可替换

- 网关只认识两个契约：上行出口、下行下发。它不关心上层是内置管控还是客户自有系统。
- 内置管控是默认实现，负责业务落库、命令编排、outbox 和管理面。
- 客户自有管控可以直接实现“上行收件”和“调用网关下发”两端，替换内置管控。
- 因此**业务语义不得下沉到网关**：网关代码中不应出现“报警确认”“媒体文件归属”“命令业务状态”等概念。

---

## 3. 协议事实与数据面规则

这一节是本次方案的核心收敛点：**不要按“消息属于哪个业务”划分归属，而要按“协议是否携带目标地址”划分数据面路径。**

### 3.1 带目标地址：1078 与苏标附件

协议事实：

- `0x9101` 实时音视频传输请求：报文体携带视频服务器 IP、TCP 端口、UDP 端口，见 [msg_9101.go](../internal/protocol/model/msg_9101.go)。
- `0x9201` 远程录像回放请求：同样携带视频服务器 IP、TCP 端口、UDP 端口，见 [msg_9201.go](../internal/protocol/model/msg_9201.go)。
- `0x9208` 报警附件上传指令：携带附件服务器 IP、TCP 端口、UDP 端口，见 [msg_9208.go](../internal/protocol/model/msg_9208.go)。

因此：

1. 终端收到这些信令后，**按报文体中的地址另开连接**推流或上传；
2. 数据面不经过 808 连接；
3. 网关只负责把信令字节写进 808 连接，**不接收也不转发媒体/附件数据**；
4. 网关**不需要、也不允许**调用媒体/附件角色。

端点从哪里来：

- 媒体/附件角色各自维护**端点分配器**，决定“这一路流/这一次上传落到哪个节点”；
- **管控层**在构造信令前向分配器申请端点，拿到 `IP + TCPPort + UDPPort`；
- **管控层**用申请到的端点构造 `0x9101/0x9201/0x9208`；
- 管控层把构造好的消息体交给网关的下行契约；
- **网关只发字节**，不申请端点、不感知端点、不校验端点。

```text
管控层                     分配器(媒体/附件)            网关                终端
  |  申请端点(phone,channel)     |                       |                   |
  |────────────────────────────>|                       |                   |
  |<────{ip,tcpPort,udpPort}────|                       |                   |
  |                                                      |                   |
  |  构造 0x9101/0x9201/0x9208                           |                   |
  |──────────────────下发契约────────────────────────────>|                   |
  |                                                      |──808 信令字节────>|
  |                                                      |                   |
  |                                                      |      终端另开连接  |
  |                                                      |      ┌───────────>| 媒体/附件
  |                                                      |      |  数据面直达  |
  |                                                      |      |  不经过网关  |
```

粘性要求：

- 每一路 1078 流、每一次附件上传都必须绑定到具体节点；
- 不能用普通轮询的 VIP 让分片随机落到不同节点，否则流注册表、缺口记账、文件写入都会失效；
- 如果必须使用统一入口，入口要做**按手机号（或流标识）粘性**的四层代理；
- 节点故障后，流/上传中断，终端重连并重新协商，不做跨节点续传。

### 3.2 不带目标地址：`0x0801`（仅图片）

#### 协议事实

- JT/T 1078-2016 §5.2「继承指令」规定：`0x0800`、`0x0801`、`0x8802`、`0x0802`、`0x8803` 这五条消息中的**多媒体类型字段只应包含图片类型**；音频、视频必须按 §5.4、§5.5 的流程（1078 实时音视频/回放）传输。
- `0x0801` 多媒体数据上传的报文体是：多媒体 ID、类型、格式、事件项、通道号、28 字节位置报警体、**多媒体数据分包**，见 [msg_0801.go](../internal/protocol/model/msg_0801.go)。
- 报文体中**没有任何目标地址字段**；终端没有“另发到某处”的能力。
- 数据按 808 分包机制上传，分包重组在网关完成；重组完成后才是完整图片。
- 因此 `0x0801` 的定位是**图片小对象、低频上传**，不是音视频大文件分片。音频/视频不经过 808 数据面。

结论：**`0x0801` 的数据必然经过网关**，这一点无法通过“让终端改发别处”规避；但它只承载图片，载荷规模与频率都可预期。

#### 定稿：方案 A（上行出口 → 管控 → 媒体文件接入）

**网关 → 上行出口 → 管控 → 媒体文件接入。**

```text
终端 ──808 分包──> 网关(重组) ──上行出口──> 管控 ──文件接入──> 媒体
                      |                      |
                      |<──受理结果────────────|
                      |──0x8800 应答────────>终端
```

- 网关按 `confirm-before-ack` 语义把完整 `0x0801` 交给上行出口；
- 管控收到后转交媒体角色的文件接入能力（写入共享卷 + 元数据落库）；
- 管控返回受理成功后，网关才回 `0x8800`；
- 网关**不直连媒体**，媒体地址、健康状态、重试策略都不进入网关；
- 网关对 `0x0801` 不保留持久状态，失败即不 ack，由终端按协议重传；
- 媒体文件以 `(phone, mediaId)` 为幂等键覆盖写，重传不会产生重复文件。

#### 类型字段分流（实现约定）

网关收到重组完成的 `0x0801` 后，必须按报文体中的多媒体类型字段分流：

| 类型 | 处理 |
|---|---|
| 图片（`0x00`） | 走上行出口的交接通道，交给管控转媒体文件接入 |
| 音频/视频（`0x01`/`0x02`） | 记录日志与指标并告警；按协议回 `0x8800` 终止重传，但**不交接为媒体文件**、不静默当正常数据落库、也不静默丢弃 |
| 其他保留值 | 同音频/视频：记录并告警，不回落到“按图片处理” |

音频/视频分支保留的原因是兼容 2011/2013 版老终端的历史行为：这类终端可能仍通过 `0x0801` 上传音视频。平台既不把它当合法媒体落库，也不无声吞掉，而是通过日志、指标和管控侧的“非法类型”事件让问题可见。

#### 背压与确认语义（按“小对象、低频”确认）

- **载荷上限**：按图片设定，默认 4 MiB（可配）。不需要流式上传、分块续传或大文件分片调度。
- **队列容量**：上行出口的有界队列以**条数**为主控制，字节数作为总闸；不为 `0x0801` 单独建队列。
- **确认语义**：`confirm-before-ack`；超时与普通业务一致（建议 3s），媒体接入慢时不无限等待，超时即不 ack，由终端重传。
- **频率假设**：低频，不做批量合并、管道化或专用线程池；共享出口即可。
- **失败处理**：出口失败即不 ack；终端重传后按 `(phone, mediaId)` 幂等覆盖，平台不需要大文件分片式的断点续传。
- **背压表现**：队列满时拒绝新事件并计数，`0x0801` 不 ack，由终端退避重传；禁止无界堆积。

#### 已否决：方案 B（数据面单向交卸例外）

曾考虑“网关直接把图片字节单向交给媒体、元数据另发管控”，以减少一跳。该方案会让网关新增媒体端点依赖、健康探测、失败重试与双写补偿，破坏“网关不调用媒体”的边界，且收益仅限图片级小对象，**已否决**，不再作为备选。理由：`0x0801` 既然只承载图片，多一跳的成本远低于边界例外的长期维护成本。

#### 关于 `0x0802` / `0x8802` / `0x0805`

- `0x0800`、`0x0801`、`0x8802`、`0x0802`、`0x8803` 的多媒体类型字段同样只应包含图片类型；平台构造这些下行消息时不得填入音频/视频类型。
- `0x0805` 是摄像头立即拍摄命令的**应答**，携带媒体 ID 等元数据，不是媒体数据本体，按普通上行业务事件处理。
- `0x0802` 是媒体检索应答，同样是元数据，按普通上行业务事件处理。
- 上述消息都不涉及“媒体数据交接通道”，不要与 `0x0801` 的图片交接混为一谈。

### 3.3 统一规则表

| 消息/通道 | 协议是否带目标地址 | 数据面路径 | 网关职责 | 网关是否调用媒体/附件 |
|---|---|---|---|---|
| 1078 推流（`0x9101` 协商） | 是（IP/TCP/UDP） | 终端 → 媒体节点 | 只发信令字节 | 否 |
| 1078 回放（`0x9201` 协商） | 是（IP/TCP/UDP） | 终端 → 媒体节点 | 只发信令字节 | 否 |
| 苏标附件（`0x9208` 协商） | 是（IP/TCP/UDP） | 终端 → 附件节点 | 只发信令字节 | 否 |
| `0x0801` 多媒体数据（仅图片） | **否** | 终端 → 网关 → 上行出口 → 管控 → 媒体 | 重组后按类型分流；图片交接，音视频记录并告警；不直连媒体 | 否 |
| 普通上行业务（`0x0200/0x0704/0x0802/0x0805` 等） | 否 | 终端 → 网关 → 上行出口 → 管控 | 解码、应答、抛出事件 | 否 |

一句话：**协议里带目标地址的（1078、苏标附件），网关只协商、不碰数据面；数据随 808 消息上来的，必须有明确的交接通道。**

---

## 4. 808 网关

### 4.1 网关持有的状态

| 状态 | 生命周期 | 说明 |
|---|---|---|
| TCP/UDP 连接与 `*model.Session` | 连接存活期 | 会话 ID 仍用远端地址；UDP 以远端端点为会话键 |
| 手机号 → 会话绑定 | 鉴权成功到连接关闭 | 只在本进程内有效 |
| 上行分包重组 | 单条消息 | 见 `runtime.FragmentAssembler` |
| 下行分包/多媒体重传缓存 | 连接存活期 | 见 `runtime.RetransmitTracker` |
| 下行可靠报文等待项 | 连接存活期 | 见 `model.Session.pending`，按 `(MsgID, serial)` 配对 |
| 查询应答 broker | 单次查询 | `0x0104/0x0107/0x0201/0x0608` 的应答配对 |
| 有界下发重试 | 秒级 | 仅连接存活期内，进程重启即丢弃 |
| 幂等窗口 | 分钟级 | 下行请求的 `idempotencyKey -> 最近结果`，有界 |

这些状态都**不需要跨节点共享**：它们要么绑定连接，要么绑定单次请求。网关重启后自然重建。

### 4.2 网关明确不持有

- 设备档案、参数、属性、定位历史、报警、区域路线；
- 下行命令的 durable 状态机（pending/acked/failed、租约、重投）；
- 媒体/附件端点分配结果与节点选择；
- 命令编排、业务规则、报警确认关联；
- 管理面页面与跨节点会话视图。

网关不写业务表，也不把业务表当缓存读。它需要业务数据时，只走“鉴权/注册只读查询”这一条窄接口。

### 4.3 上行出口契约

#### 形态

三种实现，语义一致：

1. **内置处理**（standalone 默认）：进程内直接调用管控逻辑，无网络；
2. **HTTP hook**：`POST /internal/v1/uplink`，适合管控独立部署；
3. **MQ 投递**：适合大扇出、多消费者、跨信任域。

#### 信封

```json
{
  "eventId": "13800138000|0x0200|312|1762742400123",
  "phone": "13800138000",
  "msgId": 512,
  "serial": 312,
  "occurredAtMs": 1762742400123,
  "gatewayId": "gw-a-7f3c",
  "sessionId": "10.20.1.11:53422",
  "transport": "TCP",
  "reliability": "best_effort",
  "payload": { },
  "payloadBase64": "",
  "meta": { "packetCount": 1 }
}
```

- `eventId` 是消费方幂等的唯一依据，必须稳定：手机号、消息 ID、流水号、发生时间组合；
- `payload` 为结构化载荷；无法结构化或体量较大时用 `payloadBase64`；
- `reliability` 由消息类别决定，不由调用方临时指定；
- `meta` 携带分包数、媒体 ID 等辅助信息，消费方不得依赖其完整性做业务判断。

#### 两档语义

| 语义 | 行为 | 适用 |
|---|---|---|
| `best-effort` | 网关先把事件放入有界队列，立即按协议应答；队列满或出口失败时丢弃并计数，不回滚应答 | 高频、可丢（普通定位、心跳类派生事件） |
| `confirm-before-ack` | 网关同步调用出口，拿到受理成功后才应答；失败或超时则不应答，由终端重传 | 不可丢（报警、`0x0801`、注册鉴权、需要业务结果的请求） |

#### 失败与重试边界

- 网关只做**连接存活期内**的重试；出口连续失败达到阈值后，返回错误、不 ack，让终端按协议重传；
- 网关**不**为上行事件建持久队列、不建 outbox、不做跨进程重投；
- 出口必须支持背压：有界队列 + 明确的拒绝语义，禁止无界堆积；
- `confirm-before-ack` 的超时必须小于终端重传窗口，避免“平台还在等、终端已重发”。

#### 需要业务结果的请求

注册 `0x0100` 需要平台生成鉴权码；鉴权 `0x0102` 需要校验设备档案。这类请求走出口的**请求/响应**变体：

```json
// 请求
{ "kind": "registration", "phone": "...", "payload": { } }

// 响应
{ "action": "ack", "replyBase64": "...", "reason": "" }
```

规则：

- 只有“协议应答内容依赖业务状态”的消息才允许使用请求/响应变体；
- 响应必须是纯查询/登记结果，不得在响应处理中再触发下行；
- 超时视为失败，网关不应答，由终端重传。

### 4.4 下行交互契约

#### 接口

```text
POST /internal/v1/downlink
Authorization: Bearer <token>

{
  "idempotencyKey": "ctl-cmd-9f2c1a",
  "phone": "13800138000",
  "msgId": 33024,
  "bodyBase64": "...",
  "mode": "sync_ack | async_callback | fire_and_forget",
  "callbackUrl": "http://control/internal/v1/downlink/result",
  "ackTimeoutMs": 8000,
  "ttlMs": 60000
}
```

响应：

```json
{ "downlinkId": "dl-...", "status": "accepted | offline | rejected", "serial": 312 }
```

异步回调：

```json
{
  "downlinkId": "dl-...",
  "status": "acked | failed | timeout",
  "ackCode": 0,
  "responseBase64": "",
  "attempts": 1,
  "gatewayId": "gw-a-7f3c",
  "finishedAtMs": 1762742408456
}
```

结果查询：`GET /internal/v1/downlink/{downlinkId}`，语义与回调一致，用于回调丢失后的补偿。

#### 规则

| 项 | 约定 |
|---|---|
| 幂等键 | 由调用方生成并保证稳定；同一幂等键在 TTL 内返回同一结果，不重复执行 |
| 流水号 | **网关在发送时分配**，调用方不分配；结果中回传实际流水号 |
| 连接归属 | 网关本地有会话才执行；没有会话返回 `offline`，不猜测、不转发 |
| 同步 ack | `mode=sync_ack` 时等待协议 ack 或超时，返回 `acked/failed/timeout` |
| 异步回调 | `mode=async_callback` 时先返回 `accepted`，完成后回调；回调失败可查询补偿 |
| fire-and-forget | 只保证写出 socket，不等待 ack；仅用于可丢通知 |
| 查询类 | `0x8104/0x8107/0x8201/0x8608` 的应答载荷通过结果返回，网关复用本地 broker |
| 有界重试 | 网关只在连接存活期内重试；连接关闭或进程重启即返回失败，由管控层 outbox 重新提交 |
| 拒绝语义 | 参数非法、消息体过大、并发超限、鉴权失败都明确返回 `rejected` 与原因 |

#### 为什么流水号必须由网关分配

- `Session.GetNextSerialNum()` 是每会话唯一的分配器；
- 平台主动下发的 `0x9208`、查询命令、控制命令都共享同一流水号空间；
- 如果调用方各自分配，多个管控实例可能给同一终端分配相同 `(MsgID, serial)`，导致 ack 误配；
- 因此 outbox 只保存“消息体 + 消息 ID”，实际流水号在网关首次发送时绑定并回传。

### 4.5 outbox 归属与重试边界

| 能力 | 归属 | standalone | cluster |
|---|---|---|---|
| durable 状态机（pending/acked/failed） | 管控层 | 同进程，沿用 `repository.CommandRepository` | 管控层共享库 |
| 幂等、租约、重投、过期 | 管控层 | 同进程，沿用现有 outbox replay | 管控层多副本协调 |
| 有界内存重试 | 网关 | 现有连接内重传 | 同左 |
| 同步 ack 结果 | 网关 | 现有 `Session.pending` | 同左 |
| 结果回填 | 管控层 | 进程内回调 | 回调 / 结果查询 |

**单机模式是平移，不是删除**：

- standalone 下管控与网关同进程，outbox 仍写同一张表、仍由同一套 replay 逻辑驱动；
- 区别只是“投递”从直接调用本地发送，改为调用下行契约的本地实现；
- 行为、重试节奏、状态机、API 查询结果保持不变。

### 4.6 网关不做的四件事（再次强调）

1. 不写业务表；
2. 不持有 durable outbox；
3. 不分配媒体/附件端点；
4. 不构造业务信令内容（包括 `0x9101/0x9201/0x9208`）。

---

## 5. 1078 媒体角色

### 5.1 边界

- 拥有 1078 的 TCP/UDP 监听、RTP 接收、PS 解复用、SPS/PPS 缓存、流注册表、媒体落盘；
- 拥有**媒体端点分配器**：按节点容量、流数量、粘性键选择端点；
- 不处理 808 会话，不构造 808 信令，不直接给终端发 808 消息。

### 5.2 信令路径

```text
调用方 -> 管控：请求某手机号某通道开流
管控 -> 媒体分配器：申请端点（phone, channel, source）
媒体分配器 -> 管控：{ip, tcpPort, udpPort, leaseId}
管控 -> 构造 Msg9101/Msg9201
管控 -> 网关下行契约：发送字节
网关 -> 终端：808 信令
终端 -> 媒体节点：另开连接推流
媒体 -> 落盘 + 元数据
管控 -> 媒体分配器：释放端点（流结束/超时）
```

### 5.3 粘性与故障

- 每路流绑定具体节点，不跨节点迁移；
- SPS/PPS 缓存与流状态保持节点本地，不共享；
- 节点故障后流中断，终端重新协商、重新建流、重新落文件；
- **文件必须落共享存储**（共享卷或对象存储），元数据落共享库，其他节点可读，但不接管活跃流；具体契约见 5.5；
- 端点租约必须有 TTL，节点崩溃后租约自动失效，避免“分配了但没人接”。

### 5.4 808 UDP 的特殊约束

- 808 UDP 以远端端点为会话键；多网关部署时，L4 负载均衡必须按源端点做一致性哈希，否则同一终端的不同分片可能落到不同网关，分包重组会失败；
- 不满足该条件时，集群只保证 TCP 接入，UDP 需单独评估或关闭；
- 1078 UDP 的目标端点由终端按信令选择，天然指向被分配的媒体节点，不经过 808 的 LB。

---

## 6. 苏标附件角色

- 拥有独立附件端口、分片接收、缺口记账、完成态 TTL、附件落盘；
- 拥有**附件端点分配器**，按手机号/报警标识做粘性；
- `0x9208` 由**管控层**申请端点并构造，网关只发字节；
- 网关不接收附件数据，不感知附件端口，不调用附件角色；
- 附件组装状态在节点本地，节点故障后本次上传失败，终端重新上传；
- **附件文件必须落共享存储**（共享卷或对象存储），元数据落共享库，其他节点可读，不做跨节点续传；契约与媒体角色共用 5.5。

### 5.5 文件存储契约（BlobStore）

E1 只定义契约与启动校验，**不引入对象存储 SDK**；本地 FS 实现承载 standalone 与过渡期共享卷，对象存储实现放到拆媒体/附件阶段。

```go
type BlobStore interface {
    // Put 只写暂存区（本地临时文件 / 对象存储 pending/<txid>/<key>），不使最终 key 可见。
    Put(ctx context.Context, key string, r io.Reader, opts BlobOptions) (BlobInfo, error)
    // Promote 把暂存对象提升为最终 key，幂等；本地 FS 是同目录原子 rename，
    // 对象存储是 multipart complete 或 copy 到最终 key 后删除 pending。
    Promote(ctx context.Context, info BlobInfo) (BlobInfo, error)
    // MarkCommitted 在业务元数据写库成功后调用，标记对象可被引用；
    // standalone 本地 FS 为 no-op，sharedVolume 更新 sidecar，对象存储更新对象标签/元数据。
    MarkCommitted(ctx context.Context, key string) error
    Open(ctx context.Context, key string) (io.ReadCloser, error)
    Delete(ctx context.Context, key string) error
    Stat(ctx context.Context, key string) (BlobInfo, error)
}
```

- **standalone**：现有本地 FS 就是实现，key 等于当前 `RelativePath`，文件布局与“临时文件 + 原子 rename”行为不变；`MarkCommitted` 为 no-op。
- **cluster**：只允许 `sharedVolume`（过渡期，把本地 FS 指向共享挂载）或 `objectStore`（后续实现）。配置为普通本机目录、或缺少共享标记时启动失败；共享卷必须显式 `sharedVolume: true` 并在根目录存在部署创建的 `.jt808-shared-volume` 标记文件，避免把本机独占路径当成共享存储。
- **提级与状态分离**：`Put` 返回时最终 key 不可见，只有 `Promote` 成功后才可见；`MarkCommitted` 只表达业务引用状态，绝不参与提级，失败可重试且不影响对象可见性。读取方永远看不到半个对象：本地靠 rename，对象存储靠 pending 前缀 + completion/copy。
- **校验和**：客户端计算 MD5，`MediaFile.MD5` 是权威值；对象存储 ETag 不能等同于内容 MD5（分片上传是 `md5-of-md5s-N`），只能作为后端标识记录。完整性依赖显式校验和元数据/`Content-MD5`，不靠 ETag 猜测。
- **key 结构**：仍由 `(phone, 日期, mediaId.ext)` 决定；即使对象存储没有目录，key 也必须做路径穿越与字符校验（段内 `..`、绝对路径、分隔符、长度），因为它同时被本地 FS、数据库和下载地址使用。
- **流式文件**：1078 流与附件分片先写节点本地有界 scratch（`<key>.part`），完成后再 `Put` + `Promote`；不对远端对象做追加写。
- **状态落点按后端区分**：
  - `objectStore`：状态放在对象标签/用户元数据（`jt808-phone`、`jt808-media-id`、`jt808-checksum`、`jt808-state`）。
  - `sharedVolume`：普通文件系统没有对象标签，状态放同目录 sidecar `<key>.meta.json`；`Promote` 后立刻写 sidecar（`state=pending`），`MarkCommitted` 原子改写为 `committed`。sidecar 只由 control 读取/更新，媒体/附件不解析。
  - `standalone`：无状态机，不写 sidecar。
- **对账与失败处理**：对账键是确定性 blob key + checksum，数据库唯一键 `(phone, media_id)`。管控写库成功后 `MarkCommitted`；`MarkCommitted` 失败时数据库行保持 `pending` 并由管控重试。超宽限期**不删行**：把行标为 `missing` 保留引用依据；仅当对象也确认不存在（或显式删除成功）后，才由保留策略在同一操作里同时删对象与行。删行不先于删对象，避免“对象仍在、引用先没”。
- **孤儿清理归属改变**：对象存储形态下应用不做远端遍历，`retention.orphanFiles` 必须关闭并由存储桶生命周期承担：`pending/` 前缀 24h 过期、`trash/` 前缀 1 天过期；被保留策略删除的正式对象由应用调用 `BlobStore.Delete`，删除失败转 `trash/` 交给生命周期。
  - `sharedVolume` 过渡形态由 **control** 做单实例 sidecar 巡检：只处理 `state=pending` 且超过宽限期的对象，删除前必须查共享库确认该 key 没有行（含 `pending/missing`）；有行则修复 sidecar 为 `committed` 而不是删除。因此“文件已落、写库失败”只会留下无行的 pending 对象，最终由巡检删除，不会误删仍被引用的文件。
- **下载**：API 通过 `BlobStore.Open` 读取；对象存储实现落地前，cluster 下文件下载端点明确返回 501，不伪装可用。
- **为什么 E1 不验证真实对象存储**：E1 不改文件写入路径，只冻结契约与启动校验；此时引入 SDK 会把角色拆分绑死在某个厂商实现上，而流式分段、multipart、对象标签等细节要等媒体/附件真正拆出去才能定。真实对象存储验证是 E3 的退出条件，不是 E1 的。

**已否决方案：把节点地址写进业务数据**

有人提出“cluster 下媒体仍写媒体节点本地盘，库中记录 `file://媒体ip:port/path`，读取时访问该节点”。结论：**否决**。

- 节点地址固化进业务数据后，Pod 重建、扩缩容、迁移都会让 `ip:port` 变成静默死链，只有真正读取时才发现。
- 多副本下请求必须命中持有文件的节点，需要维护“文件 → 节点”路由表并处理节点下线不可达，复杂度接近自建半个对象存储。
- k8s 下 Pod 本地盘不持久，仍要挂 PVC；RWO 通常不能多 Pod 共享，能共享的 RWX 基本是 NFS/CephFS，运维成本不低于对象存储。

**保留的结论**：数据里只放 `blob key`，不放节点地址。后端三个枚举：`local`（standalone 现状，零依赖）、`sharedVolume`（单实例验证，共享卷 + 标记文件）、`objectStore`（cluster 正式形态，E3 落地）。“媒体服务提供本地文件读取能力”由 `BlobStore.Open(key)` 承担：单机读本地文件，集群读对象存储，上层代码不变。

---

## 7. API 管控角色

### 7.1 职责

- 业务落库与查询：设备、参数、属性、定位、报警、区域路线、媒体元数据；
- 设备运维：注册受理、鉴权数据、在线状态视图（基于会话目录）；
- 命令编排：构造消息体、决定重试与过期、维护 durable outbox；
- 媒体/附件协调：申请端点、构造 `0x9101/0x9201/0x9208`、释放端点；
- 对外 HTTP API；
- 未来的后台管理页面。

### 7.2 会话目录

管控需要知道“某个手机号当前在哪个网关”，才能把下行送到正确节点。这属于管控层的**路由索引**，不是会话本体：

```text
phone -> { gatewayId, gatewayAddr, sessionId, generation, updatedAt, expiresAt }
```

- 网关在鉴权成功、心跳刷新、连接关闭时上报（走上行出口的会话类别，异步）；
- 索引有 TTL，网关崩溃后自动失效；
- 索引只是路由提示：真正“有没有会话”以网关本地注册表为准；
- 索引丢失时下行返回 `offline`，由 outbox 重试，不跨网关猜测；
- 索引可以落在管控自己的存储（MySQL 或 Redis）中，**不要求网关依赖 Redis**。

### 7.3 可替换性

- 内置管控实现两个契约的调用端，并提供会话目录；
- 客户自有管控可以实现同样的两端，网关无需改动；
- 管控层替换不得要求网关增加业务概念；
- 管理面只读/写管控存储，不直接访问网关内存。

---

## 8. 状态归属总表

| 状态 | 归属角色 | 存储位置 | 跨节点共享 |
|---|---|---|---|
| 连接、会话、手机号绑定 | 网关 | 进程内存 | 否 |
| 上行分包重组 | 网关 | 进程内存 | 否 |
| 下行重传缓存 | 网关 | 进程内存 | 否 |
| `Session.pending` | 网关 | 进程内存 | 否 |
| 查询 broker | 网关 | 进程内存 | 否 |
| 幂等窗口 | 网关 | 进程内存（有界 TTL） | 否 |
| 设备档案、参数、属性 | 管控 | 共享库 | 是 |
| 定位、报警、行业载荷 | 管控 | 共享库 | 是 |
| durable outbox | 管控 | 共享库 | 是 |
| 会话目录 | 管控 | 管控存储 | 是 |
| 1078 流、SPS/PPS、文件句柄 | 媒体 | 进程内存 | 否 |
| 媒体端点租约 | 媒体 | 内存/共享存储（TTL） | 是（仅分配需要） |
| 附件组装、缺口记账 | 附件 | 进程内存 | 否 |
| 附件端点租约 | 附件 | 内存/共享存储（TTL） | 是（仅分配需要） |
| 媒体/附件文件 | 媒体/附件 | 共享卷/对象存储 | 是（读） |

判断标准：**绑定连接或单次请求的留网关；需要重启后仍在、需要被其他角色查询的归管控/共享存储。**

---

## 9. 部署形态与配置

### 9.1 形态

**Standalone（默认）**

```text
一个进程：808 接入 + 协议 + 内置管控 + 媒体 + 附件
依赖：memory / sqlite / mysql 三选一；无 Redis、无 MQ、无内部 RPC
上行出口：内置处理
下行契约：本地实现
```

**Cluster / gateway + control**

```text
gateway 节点：808 接入、协议、上行出口客户端、下行契约服务端
control 节点：业务 API、落库、命令编排、outbox、会话目录
依赖：共享 MySQL；可选 Redis/MQ
```

**Cluster / gateway + control + media + attachment**

```text
gateway：只做信令
control：业务与编排
media：1078 数据面与端点分配
attachment：附件数据面与端点分配
依赖：共享 MySQL + 共享卷/对象存储；可选 Redis/MQ
```

**Cluster / 合并部署**

```text
gateway+media 同进程、control 独立，或 gateway+control 同进程、media/attachment 独立
按接入量与运维边界选择，不强制四进程
```

### 9.2 配置骨架

```yaml
server:
  # standalone：当前默认，单进程零外部依赖
  # cluster：启用角色拆分、上行出口与下行契约
  mode: "standalone"

cluster:
  nodeId: ""

  # 本进程承担的角色，可多选
  roles:
    - gateway        # 808 接入
    # - control      # 业务与编排
    # - media        # 1078 数据面
    # - attachment   # 苏标附件数据面

  gateway:
    uplink:
      # builtin | http | mq
      mode: "builtin"
      http:
        endpoint: ""
        token: ""
        maxBodyBytes: 4194304
        timeoutMs: 3000
      mq:
        topic: ""
    downlink:
      # 下行契约监听地址，只绑内网
      listen: "0.0.0.0:9100"
      token: ""
      maxConcurrent: 256
      idempotencyTTL: "5m"
    sessionDirectory:
      # 上报会话目录的接口；为空时由 control 主动探测（不推荐）
      endpoint: ""

  control:
    downlinkEndpoint: "http://gateway:9100"
    uplinkListen: "0.0.0.0:9101"
    callbackBaseURL: "http://control:9000"

  media:
    advertiseIP: ""
    allocator:
      listen: "0.0.0.0:9200"

  attachment:
    advertiseIP: ""
    allocator:
      listen: "0.0.0.0:9201"
```

### 9.3 启动校验

| 配置 | standalone | cluster |
|---|---|---|
| `server.mode` | 缺省/`standalone` | 必须为 `cluster` |
| `cluster` 段 | 忽略；非空时打印 warning | 必须存在 |
| `cluster.roles` | 不校验 | 至少一个；角色组合合法 |
| `cluster.nodeId` | 不校验 | 非空 |
| 上行出口 | 固定 `builtin` | `builtin/http/mq` 三选一；`http/mq` 必填 endpoint/token |
| 下行契约监听 | 不启动 | `gateway` 角色必须配置且只绑内网 |
| 媒体/附件端点分配器 | 不启动 | 承担对应角色时必须配置 |
| `storage.type` | `memory/sqlite/mysql` | `control` 角色必须为 `mysql` |
| `commandOutbox.mode` | 现有归一化规则 | `control` 角色必须为 `durable` |
| `api` | 现有规则 | `control` 角色沿用现有 `api.token` 规则 |
| 共享卷 | 现有规则 | `media/attachment` 目录必须可写，启动时检查并告警 |

### 9.4 负载均衡要求

| 端口 | 要求 |
|---|---|
| 808 TCP | L4 轮询即可；会话由网关本地维护，重连会重新绑定 |
| 808 UDP | **必须按源端点一致性哈希**，否则分包跨网关 |
| 管控 HTTP | 普通轮询；控制面请求由会话目录定位网关 |
| 下行契约 | 只在内网可达，不对公网暴露 |
| 1078 TCP/UDP | 终端按信令直连媒体节点，不经过 808 LB |
| 附件端口 | 终端按 `0x9208` 直连附件节点，不经过 808 LB |

### 9.5 Standalone 的依赖保证

- `standalone` 不解析集群网络地址、不创建 HTTP/MQ/Redis 客户端、不启动内部监听；
- `cluster` 段存在但 `mode=standalone` 时打印 warning 并忽略；
- 单机端口、启动顺序、优雅停机顺序不变；
- 单机上行出口为内置处理，下行契约为本地调用，不产生网络往返。

---

## 10. 故障语义

| 故障 | 表现 | 系统行为 | 数据后果 |
|---|---|---|---|
| 网关宕机 | 终端连接断开 | 终端重连到其他网关；会话目录 TTL 过期 | 网关内 pending 与幂等窗口丢失；管控 outbox 未决命令重新投递 |
| 管控宕机 | 上行出口失败、下行不可用 | `best-effort` 丢弃并计数；`confirm-before-ack` 不应答，终端重传；outbox 停止推进 | 可能丢失可丢类别；不可丢类别由终端重传补齐 |
| 媒体宕机 | 推流中断、`0x0801` 文件接入失败 | 端点租约过期；管控重新分配；`0x0801` 不 ack | 当前流/当前文件失败，终端重新协商/重传 |
| 附件宕机 | 附件上传中断 | 端点租约过期；终端重新上传 | 当前上传失败，已收分片丢弃 |
| 上行出口超时 | 网关等待受理 | 不应答，终端重传；计入背压指标 | at-least-once |
| 下行契约超时 | 管控调用网关失败 | 返回失败；outbox 保持 pending 并退避 | 可能重复下发，业务需幂等 |
| 网关无该会话 | 设备已离线或目录过期 | 返回 `offline`，不转发 | outbox 等待设备上线或过期 |
| 会话目录过期 | 管控以为在线、网关无会话 | 返回 `offline`；网关心跳/绑定事件刷新目录 | 短暂下行失败 |
| 重复注册 | 新连接同手机号 | 注册阶段不踢旧连接；鉴权成功后顶替旧会话 | 旧连接 pending 转失败，由管控重投 |
| 共享库不可用 | 管控无法落库 | `best-effort` 继续应答但可能丢；`confirm-before-ack` 不应答 | 与当前单机语义一致 |
| 端点租约泄漏 | 节点崩溃未释放 | TTL 到期自动回收 | 短暂端点浪费 |
| 时钟偏差 | 各节点时间不一致 | 租约与幂等窗口使用统一时间源或相对 TTL | 不影响协议正确性 |

明确不承诺：

- 不承诺 exactly-once；
- 不承诺网关重启后保留连接期内的 pending；
- 不承诺媒体/附件跨节点续传；
- 不承诺管控宕机期间下行可用。

---

## 11. 演进顺序

### E0：角色抽象与契约骨架（零行为变化）

- 目标：把“谁持有会话、谁落库、谁下发”从现有实现中抽象出来，为拆分留接口。
- 改动面：
  - 配置新增 `server.mode` 与 `cluster.roles` 解析、校验；
  - 抽取并落地四类契约的 Go 接口与本地实现：**上行出口、下行下发接口、鉴权提供者、媒体/附件分配与读取**；
  - 把 durable outbox 从协议层平移到内置管控模块（幂等、租约、退避、过期能力全部保留）；
  - `internal/api`、`internal/command` 改为依赖抽象，不再直接持有 `*server.TCPServer`；
  - `cmd/jt808-server/main.go` 按角色装配（standalone 仍装全部）。
- 验收：standalone 行为、应答时机、重投语义完全不变；端口与外部依赖不变；全量测试通过。

### E1：角色拆分与两个内部 HTTP 契约（仅契约验证）

> **阶段定位（硬约束）**：E1 的 `cluster` 形态只用于验证配置、启动校验与两个契约本身，**不承接生产业务，不得表述为可上生产**。业务可用性从 E2/E3 起算；E1 不提供“跳过 consumer 校验”的开关。

**角色配置**

- `server.mode`：`standalone`（缺省）| `cluster`。`standalone` 隐式等于四个角色全开、全部本地，不初始化任何集群监听或客户端；显式配置 `cluster.roles` 视为矛盾配置，启动失败。
- `cluster.roles`：`gateway` / `control` / `media` / `attachment` 的非空、去重子集。每个角色只启动自己拥有的监听：gateway → TCP/UDP + 下行入口；control → 业务 API + 上行入口；media/attachment → 各自端口。
- 密钥：**上下行各一把独立共享密钥，不跨方向复用**。`cluster.gateway.token` 由 control 发送、gateway 校验；`cluster.control.token` 由 gateway 发送、control 校验。同一方向上“客户端凭据”与“服务端校验值”配置成同一密钥是设计选择，不是巧合：gateway 的 `uplink.http.token` 必须等于 control 的 `cluster.control.token`。cluster 下任一所需 token 为空即启动失败。
- 文件存储：`cluster.blob.backend`（`sharedVolume` | `objectStore`）与 `cluster.blob.sharedVolume`、`cluster.blob.marker`（默认 `.jt808-shared-volume`）。cluster+media/attachment 必填；共享卷必须显式 `sharedVolume: true` 且根目录存在标记文件，否则启动失败；`objectStore` 在 E1 只做字段校验，启动时明确报“E1 未实现”，不静默降级。

**启动校验（fail-fast，一次性列出全部问题）**

- `cluster`：roles 非空、取值合法、无重复；各角色所需监听/地址/token 齐全；media/attachment 端口齐全。
- 存储：`gateway` 与 `control` 都要求 `storage.type=mysql`（gateway 只读设备档案与鉴权码，control 读写业务与 outbox）；`memory`/`sqlite` 一律拒绝。`media`/`attachment` 不连业务库（见下）。
- outbox：`control` 要求 `commandOutbox.mode=durable`；`off`/`memory` 拒绝。gateway-only 不构造 outbox，配置忽略并 Warn。
- 出口：gateway-only 必须 `uplink.sink=http`；gateway+control 同进程允许 `local`；同进程同时配置 `sink=http` 指向自身视为回环，拒绝。
- **confirm-before-ack 覆盖校验**：出口策略中所有 `at_least_once` 类别，以及被强制为 confirm-before-ack 的 `0x0001`、`0x0801`，启动时必须已经注册能处理它们的 consumer；缺失则启动失败并列出缺失类别/消息。E1 不内置业务 consumer，因此默认配置下 `cluster+control` 无法启动——这是预期行为，契约验证通过测试注入 fake consumer 完成。运行期若 confirm-before-ack 事件仍找不到 consumer，回 503（绝不以 501/200 让终端持续重传或静默丢弃）。
- best-effort 事件未注册 consumer 时回 202 `{outcome:"unhandled"}`，同时记日志与指标；终端已被网关应答，不产生重传风暴，但丢弃必须可见。

启动矩阵：

| mode | roles | storage | outbox | uplink.sink | 结论 |
|---|---|---|---|---|---|
| standalone/缺省 | 任意（忽略） | memory/sqlite/mysql | off/memory/durable | local/http | 允许，行为不变 |
| standalone | 非空 | — | — | — | 拒绝（矛盾配置） |
| cluster | gateway | mysql（只读设备档案） | 忽略并 Warn | http | 允许 |
| cluster | control | mysql | durable | local/http | 允许，需 consumer 覆盖校验通过 |
| cluster | gateway+control | mysql | durable | local | 允许 |
| cluster | gateway+media / media+attachment / 三数据面角色 | mysql（gateway）/ media、attachment 不连业务库 | 按角色 | http | 允许，但 media/attachment 必须配 `cluster.blob.backend=sharedVolume`（带共享标记）或 `objectStore`（E1 未实现，启动拒绝） |
| cluster | media/attachment 配置普通本机目录、或 sharedVolume 缺标记 | — | — | — | 拒绝（文件不在共享存储） |
| cluster | objectStore + `retention.orphanFiles.enabled=true` | — | — | — | 拒绝（对象存储禁用应用侧孤儿扫描） |
| cluster | 任意含 gateway/control | memory/sqlite | — | — | 拒绝（共享状态不可用） |
| cluster | control | mysql | off/memory | — | 拒绝 |
| cluster | gateway-only | mysql | — | local | 拒绝（事件无人消费） |
| cluster | gateway+control | mysql | durable | http 指向自身 | 拒绝（同进程回环） |
| cluster | roles 为空/未知/重复 | — | — | — | 拒绝 |
| cluster | 任一角色缺端口/地址/token | — | — | — | 拒绝 |
| cluster | control | mysql | durable | — | 若 confirm-before-ack 类别无 consumer：拒绝并列出缺失项 |

**两个内部 HTTP 契约**

| 方向 | 端点 | 鉴权 | 语义 |
|---|---|---|---|
| control → gateway | `POST /internal/v1/downlink`、`GET /internal/v1/downlink/{id}` | `Bearer cluster.gateway.token` | 4xx/5xx=立即失败；202=已受理；200+status=已有终态。`offline`/`rejected` 走 200，便于控制侧保留 pending |
| gateway → control | `POST /internal/v1/uplink` | `Bearer cluster.control.token` | 2xx=处理完成/幂等命中；503=处理失败，confirm-before-ack 不 ack；413=超限 |

- 下行幂等：`(idempotencyKey)` 命中返回原 `downlinkId/serial`；网关侧为有界内存表（默认 2m），持久幂等仍由管控 outbox 负责。
- 下行超时：`cluster.downlink.timeoutMs` 缺省 8s，上限 `server.ackTimeoutMs × (maxRetransmissions+1)`，越界 400；`sync_ack` 超时回 200 `status=timeout`，不伪造 5xx。
- `async_callback` 在 E1 返回 400 `unsupported_mode`，不做半成品。
- 上行去重：按 `eventId` 有界 TTL 去重（默认 10m / 10 万条），命中直接 2xx；TTL 必须覆盖网关超时×重试窗口。请求体上限默认 4 MiB。
- 下行查询窗口：网关只保留连接存活期内的在途记录，未知 id 404，TTL 淘汰返回 410，明确区分“已失效”与“从未下发”。权威查询面始终是管控 outbox。

**gateway-only 语义**

- 网关不构造 outbox；`contracts.Downlink.Enabled()` 属于管控侧：control 为 `true` 且 API 暴露命令查询；gateway-only 若包一层 `Downlink` 适配器，`Enabled()` 恒为 `false`（能发、不可持久查），`Query` 返回明确未找到/已失效。
- 控制侧拿 ack 的主路径是 `0x0001` → 上行 `reply` → `HandleTerminalReply`；网关 `GET` 仅作连接期辅助。

**媒体/附件文件与元数据归属（决策 B：不连业务库，文件走 BlobStore）**

- 媒体/附件角色只写文件，不装配任何 repository、不连 MySQL；文件统一通过 5.5 的 `BlobStore` 写入，cluster 下必须配置共享存储（共享卷过渡或对象存储），普通本机目录启动失败。
- `media_files` / `media_captures` / `media_capture_items` 等业务表**只由 control 写入**，control 对文件只读。`0x0801` 与附件完成态走同步交接：control 调 media/attachment 的 intake（E3 落地），拿到 `{key,size,md5}` 后写库；**写库成功才向上游 ack**，写库失败则删除刚提交的对象并回 503，由终端重传。
- 1078 流等异步文件：媒体 `Put` + `Promote` 后，对象状态初始为 `pending`（objectStore 用对象标签，sharedVolume 用同目录 sidecar）；通过 at-least-once 事件交给 control，control 写库成功后调用 `MarkCommitted` 置为 `committed`。对账键是确定性 blob key + checksum，数据库唯一键 `(phone, media_id)`。
- 失败处理：DB 写失败时对象保持 `pending`，objectStore 由 bucket 生命周期在 24h 后清掉，sharedVolume 由 control 单实例巡检删除（删前查库确认无行）；**不存在“半个文件被当成有效文件”**。`MarkCommitted` 失败由 control 重试，超过宽限期把 DB 行标 `missing` **保留行**，仅当对象也确认不存在或删除成功后才同时删对象与行，删行不先于删对象。sidecar `.meta.json` 只用于 sharedVolume 过渡形态，standalone 不写、objectStore 不用。
- 孤儿清理：objectStore 形态下 `retention.orphanFiles` 必须关闭，改由 `pending/`、`trash/` 前缀的 bucket 生命周期承担；sharedVolume 过渡形态由 control 依据 sidecar 做单实例 pending 巡检，不再做无差别的 mtime 全量扫描。E1 的 cluster 下文件下载端点返回 501，不伪装可用。
- **E1 交付后 `cluster` 仍不承接业务**：业务处理（`act.process` 落库路径）仍在网关进程内，媒体/附件文件写入也尚未切到 `BlobStore`；本阶段只交付契约与启动校验，可用性从后续阶段起算。

**回归保证**

- `mode` 缺省 → standalone，装配顺序与参数不变；`uplink.sink=local` 时 `deps.UplinkSink` 仍为 nil；`commandOutbox.mode=off` 仍不构造命令仓储、`Enabled()==false`、API 不暴露查询。


### E2：下行契约与 outbox 上移

- 目标：管控通过下行契约操作网关；durable outbox 归管控。
- 改动面：
  - 下行契约服务端（网关）与客户端（管控）；
  - 幂等键、同步 ack、异步回调、结果查询；
  - 流水号由网关分配并回传；
  - outbox 投递改为调用契约；standalone 用本地实现；
  - 查询类应答经结果返回。
- 验收：standalone 行为不变；管控/网关分进程时命令、查询、回调、重试全部通过。

#### E2 补充：终端应答 → outbox 状态回写（同进程先行）

- 统一入口：`control.Outbox.HandleTerminalReply`。本地会话回调 `HandleResult` 是传输层适配器，HTTP 事件消费方从信封 `reply` 字段还原后调用同一入口；状态推进、幂等、迟到与无匹配分类只有一份实现。
- 匹配键：`(phone, answerMsgId, answerSerial)`。`answerMsgId/answerSerial` 来自 `0x0001` 报文体，`phone` 来自报文头；重投复用原流水号，迟到应答仍能命中同一命令。
- 状态机：`pending` 收到 `Result=0` → `acked`；收到 `Result=1/2/3` → 默认 `failed`；`Result=1` 在 `retryOnTerminalFailure=true` 且未达 `maxAttempts` 时可重投，`Result=2/3` 始终终结。终态命令不会被迟到成功应答改回 `acked`。
- 幂等与分类：终态命令再次收到同结果应答归为 `duplicate`；结果矛盾或已过期归为 `late`；找不到记录归为 `unmatched`，并输出 Warn 日志与 `jt808_terminal_reply_total{outcome=...}`。`acked/failed/retry_scheduled` 只在真正推进状态时计数。
- `sink=http` 双来源：网关侧传输层 ack 仍更新本地 outbox（用于停止重投），HTTP 消费方若共用同一 outbox，第二次处理因“终态 + 同结果”归为 `duplicate`，不会污染 `late` 或重复推进；若消费方独立进程，则各自持有 outbox，不存在共享记录。
- 租约收尾：`acked/failed` 清空 `LeaseOwner/LeaseExpiresAt`，后续 `ClaimPending` 不会再取到；安排重投时清租约并设置 `NextAttemptAt`。`BeginAttempt` 之后、写 socket 之前到达的应答仍可能多写一帧，这是有界的一次 at-least-once 窗口。
- **终态保留期结论**：本轮不纳入 retention 清理，列为后续项。原因：SQL 后端的 `command_outbox` 在本轮之前已经无界；routing 索引只是 memory 实现的额外映射。纳入清理需要跨后端新增 `PurgeTerminal`、配置与迁移，属于独立改动。后续建议参数：只清终态（`status IN ('acked','failed')`），清理键 `updated_at <= now - terminalRetention`，默认 `24h`，批量按 `batchSize`；pending 仍由 `expireAfter` 兜底。本轮用 `jt808_terminal_reply_total{outcome="late|unmatched"}` 观测误匹配，表体积由数据库侧巡检。
- **结果查询窗口必须小于等于终态保留期（硬约束）**：调用方按 `commandId`/幂等键查询命令结果的窗口，不能长于终态清理的 `terminalRetention`。否则记录被清理后调用方必然查不到结果；更严重的是，清理不得让查询表现为“从未下发过”——`Query` 必须能区分“已过保留期被清理”与“幂等键从未出现过”，前者返回明确的已清理/过期语义（如 `410 Gone` 或 `status=expired`），后者才返回 `404`。实现清理时需保留覆盖查询窗口的最小 tombstone（幂等键、终态、终结时间），或在清理前把该语义固化到查询契约里；启动校验需联动两者，`terminalRetention` 小于对外承诺的查询窗口时视为非法配置并拒绝启动。本节清理仍未实现，约束先固化，后续实现 `PurgeTerminal` 时必须同时满足。

### E3：物理角色拆分与端点分配

- 目标：media、attachment 可独立部署；`0x9101/0x9201/0x9208` 由管控构造。
- 改动面：
  - 媒体/附件端点分配器与租约；
  - 管控申请端点、构造信令、下发、释放；
  - 媒体/附件粘性与共享卷；
  - 网关角色不加载媒体/附件代码路径；
  - `0x0801` 按方案 A 接入上行出口与媒体文件接入。
- 验收：信令经网关、数据面直达媒体/附件；网关无媒体依赖；端点粘性与故障回收正确；`0x0801` 可在媒体落盘并可查询。

### E4：集群加固与运维

- 目标：可观测、可运维、可回归。
- 改动面：
  - 指标：上行出口成功率/丢弃、下行契约延迟/超时、会话目录命中、端点租约、`0x0801` 交接时延；
  - 运行手册：扩缩容、角色排水、故障切换、共享卷检查；
  - 混沌测试：杀网关、杀管控、杀媒体、网络分区；
  - 性能压测：`0x0801` 图片交接的端到端时延与出口背压表现。
- 验收：故障场景可复现；指标可定位；standalone 回归全绿。

依赖关系：

```text
E0 -> E1 -> E2 -> E3 -> E4
```

每个阶段都合入 `dev/cluster_mode`，不单独向 `main` 提 PR。

---

## 12. 测试与验收

### 12.1 通用门槛

每个阶段提交前：

```bash
gofmt -l .
go build ./...
go vet ./...
go test -race -count=1 ./...
```

并确认：

- standalone 全量测试通过；
- 未配置集群段时，启动日志不出现任何新客户端连接；
- 现有 API、outbox、媒体、附件行为无变化。

### 12.2 集群专项

| 场景 | 方法 | 验收 |
|---|---|---|
| 上行出口内置 vs HTTP | 同一批消息分别走两种出口 | 事件内容一致；幂等键稳定 |
| `confirm-before-ack` 失败 | 出口返回失败 | 终端收不到应答并重传；网关不落持久状态 |
| 下行幂等 | 同一 `idempotencyKey` 重复提交 | 只执行一次；结果一致 |
| 流水号归属 | 管控并发提交多条命令 | 流水号由网关分配且不冲突 |
| 跨网关下行 | 终端在 A，管控向 B 提交 | B 返回 `offline` 或按目录路由到 A；不跨网关猜测 |
| 1078 信令 | 管控申请端点并构造 `0x9101` | 网关只发字节；终端连到媒体节点；网关无媒体连接 |
| 苏标附件 | 管控申请端点并构造 `0x9208` | 网关只发字节；终端连到附件节点 |
| `0x0801` 方案 A | 终端上传分包图片 | 网关重组；经管控转交媒体落盘；`0x8800` 在受理后发送 |
| `0x0801` 媒体故障 | 媒体文件接入不可用 | 管控不受理；网关不 ack；终端重传 |
| 端点租约 | 媒体节点崩溃 | 租约过期；重新分配；旧端点不再被通告 |
| 共享卷 | 媒体写、另一节点读 | 可读；元数据一致 |
| UDP 分片 | 分片经 LB 到不同网关 | 测试应暴露失败；部署要求源端点哈希 |

### 12.3 单机回归

- `internal/server`、`internal/api`、`internal/command`、`internal/media`、`internal/alarmserver` 测试全部保留；
- 新增配置测试：standalone 忽略集群段、非法集群配置拒绝启动；
- 新增依赖测试：standalone 启动不创建 HTTP/MQ/Redis 客户端。

---

## 13. 明确不做

1. **不做分布式会话共享**：不把连接、`Session.pending`、分包缓存、SPS/PPS 序列化到共享存储。
2. **不做网关跨节点转发**：会话粘连接，网关之间不互转。
3. **不做上行 outbox**：上行可靠性由出口语义 + 管控落库/终端重传解决。
4. **不做 exactly-once**：下行重投、终端重传、节点故障都可能重复，业务按幂等处理。
5. **不做网关直连媒体/附件**：`0x0801` 一律走管控转交；方案 B 已否决，不再评审。
6. **不做媒体/附件跨节点续传**：节点故障即重建。
7. **不做对外 API 网关、TLS 终结、LB 实现**：由部署方负责。
8. **不做多地域双活**：假设同地域、低延迟内网。
9. **不做业务语义下沉**：网关不出现报警、媒体归属、命令业务状态等概念。

---

## 14. 风险与应对

| 风险 | 影响 | 应对 |
|---|---|---|
| 上行出口成为关键路径 | `confirm-before-ack` 时延上升 | 出口短超时、连接池、并发上限；可丢类别走 `best-effort` |
| `0x0801` 载荷复制 | 图片级内存与网络开销 | 按图片设定 4 MiB 上限；有界队列与背压；不按大文件分片设计 |
| 管控与网关双向依赖成环 | 自锁、雪崩 | 上行收件不同步等下行；管控异步编排；禁止回调链 |
| 端点分配器故障 | 无法建立新流/上传 | 租约 TTL、重试、降级为“暂不可用”而非错误端点 |
| 端点不粘性 | 流/附件重组失败 | 分配器按粘性键选节点；统一入口必须四层粘性 |
| 会话目录过期 | 下行失败 | 网关绑定/心跳上报；TTL；返回 `offline` 由 outbox 重试 |
| 共享卷性能 | 落盘抖动 | 单写多读；按手机号分目录；监控；可替换对象存储 |
| UDP LB 配置错误 | 分包跨网关 | 部署强约束；启动/巡检提示；不支持时只启用 TCP |
| 角色组合配置漂移 | 行为不一致 | 启动校验、有效配置摘要、最小示例 |
| 网关重启丢 pending | 命令结果丢失 | 管控 outbox 重新投递；业务幂等 |

---

## 15. 待确认事项

1. **会话目录存储**：管控用 MySQL 还是 Redis；默认 MySQL，避免新增中间件。
2. **上行出口默认形态**：cluster 下默认 `http` 还是 `mq`；建议按部署规模选择，不设全局默认。
3. **媒体/附件端点分配器归属**：跟随媒体/附件角色，还是独立协调服务；默认为前者。

---

## 16. 一句话总结

**808 网关只做连接、协议、分包与 ack，业务和耐用状态全部上移管控；带目标地址的 1078 与苏标附件，网关只发信令、不碰数据面；`0x0801` 只承载图片、数据必然经过网关，定稿经上行出口交管控再转媒体，网关不直连媒体。** 所有阶段在 `dev/cluster_mode` 上迭代，全部完成后再一次性合入 `main`。
