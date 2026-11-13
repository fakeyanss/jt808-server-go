# 运维手册（部署 / 迁移 / 备份恢复 / 监控 / 压测基线）

- 状态：**初版，覆盖 R4/R5 交付内容**
- 适用版本：`main` 上包含管理面探针、业务 API 鉴权限流、保留期清理、compose 产物的版本
- 相关文档：
  - [uplink-sink-design.md](uplink-sink-design.md)（上行语义与出口设计）
  - [storage-abstraction-design.md](storage-abstraction-design.md)（存储后端与运行时状态）
  - [JT808-project-roadmap.md](JT808-project-roadmap.md)（阶段与优先级）

> 本文只描述运维动作，不重复协议与设计细节。所有示例命令默认在仓库根目录执行。

---

## 1. 部署

### 1.1 前置检查

1. 配置文件中的 `database.type` 决定持久化后端：`memory | sqlite | mysql`。
   - `memory` 只用于演示/压测，重启即丢，生产不得使用。
   - `sqlite` 需要可写的库文件路径（默认 `./data/jt808.db`）。内部使用读写双池：写池固定 1 连接（写写串行），
     只读池默认按 CPU 数、可通过 `database.sqlite.readConns` 调整；DSN 已开启 WAL、busy_timeout、只读模式与 `BEGIN IMMEDIATE`。
   - `mysql` 需要可用的 DSN。
2. 生产必须设置：
   - `api.auth.token`：业务 API 的 Bearer token；为空时鉴权关闭，仅建议可信内网临时使用。
   - `admin.enable: true` 且 `admin.token` 非空：管理面与 `/metrics` 需要鉴权。
   - `admin.addr`：默认 `127.0.0.1:9000`，只监听本机；需要远端访问时改为 `0.0.0.0:9000` 并配合防火墙。
3. `report`/`banner` 等相对路径按进程工作目录解析。容器内工作目录固定为 `/app/output`。

### 1.2 二进制部署

```bash
# 编译（Linux 静态链接）
bash scripts/build.sh release jt808-server-go linux amd64
bash scripts/build.sh release jt808-migrate linux amd64

# 产物目录含二进制与 configs/；按需修改 configs/default.yaml
./target/release/jt808-server-go_linux_amd64/jt808-server-go \
  -c ./target/release/jt808-server-go_linux_amd64/configs/default.yaml
```

按角色部署时五个二进制共用同一套装配层，直接编译对应 cmd：

```bash
go build -o bin/jt-server-standalone ./cmd/jt-server-standalone
go build -o bin/jt-signal           ./cmd/jt-signal
go build -o bin/jt-media            ./cmd/jt-media
go build -o bin/jt-control          ./cmd/jt-control
go build -o bin/jt-attachment       ./cmd/jt-attachment
```

`jt808-server` 保留为**通用入口**：完全按配置文件里的 `server.mode` / `cluster.roles`
运行，行为与拆分前一致，零破坏。它的 `probes.addr` 可选项也保持原行为（未配置就不启动探针）。
按角色拆进程时请使用下面四个固定角色的二进制。

进程收到 `SIGTERM`/`SIGINT` 后按以下顺序优雅退出：
业务 API → 终端 TCP 接入与存量会话 → UDP 接入 → 下行 outbox 当前轮次 → 媒体与附件落盘 → 管理面 → 探针 → 存储。
给编排系统预留至少 10 秒的 `stop_grace_period`。

### 1.3 Docker Compose 部署

```bash
# 首次构建与启动
docker compose -f deploy/docker-compose.yml build
docker compose -f deploy/docker-compose.yml up -d

# 查看状态与日志
docker compose -f deploy/docker-compose.yml ps
docker compose -f deploy/docker-compose.yml logs -f jt808
```

说明：

- `../configs` 以只读方式挂载到容器 `/app/output/configs`，部署方直接维护宿主机配置。
- `jt808-data`、`jt808-logs` 是命名卷，分别承载 `data/`（SQLite、媒体、附件）与 `logs/`。
- 端口：`8080/tcp` 终端接入、`8081/udp` 终端接入、`8008` 业务 API、`7200/tcp` 与 `7201/udp` 苏标附件、媒体 UDP 按配置。
- 管理面默认只监听容器内 `127.0.0.1:9000`，**不发布到宿主机**：

```bash
docker exec jt808 wget -qO- http://127.0.0.1:9000/healthz
docker exec jt808 wget -qO- http://127.0.0.1:9000/readyz
docker exec jt808 sh -c 'wget -qO- --header="Authorization: Bearer $ADMIN_TOKEN" http://127.0.0.1:9000/metrics'
```

如需从宿主机访问管理面，把配置改为 `admin.addr: "0.0.0.0:9000"`、设置强 token，再在 compose 中发布端口并限制来源。

### 1.4 1078 媒体播放输出（可选）

媒体角色把收到的 1078 流按 `mediaServer.playback.output` 转成播放输出，播放地址由媒体节点在创建播放会话时返回，调用方不要自行拼接。

```yaml
mediaServer:
  port: 7000            # 同一端口同时监听 UDP 与 TCP，0x9101/0x9201 两个端口字段填同一值
  httpPort: 7001        # 会话管理 + HTTP-FLV 播放；未配置时取 port + 1
  advertiseIp: 10.0.0.12
  record:
    mode: off           # off | legacy | segmented
    segmentMinutes: 10
  playback:
    output: flv         # flv | rtmp | both
    rtmp:
      url: rtmp://10.0.0.20/live      # 只写到 app，不含流名
      streamPrefix: jt                 # 推流名前缀，最终流名 <prefix>_<phone>_<channel>
      playUrlPrefix: http://10.0.0.20:8080/live   # 可选，观众播放地址前缀
      dialTimeoutSeconds: 5
      writeTimeoutSeconds: 10
      queueFrames: 120
```

- `flv`（默认）：平台内置 HTTP-FLV，观众直接向媒体节点 `GET /media/<sessionId>.flv`；平台按关键帧对齐，新观众从最近的 GOP 头开始。
- `rtmp`：平台把流推给 SRS 等流媒体服务器，观众从流媒体服务器播放；播放地址取 `rtmp.playUrlPrefix`，未配置时只返回推流地址。
- `both`：两路并联，单路异常不影响另一路。
- `rtmp`/`both` 必须配置 `rtmp.url`，且协议前缀必须是 `rtmp://`；协议不支持、缺少 URL 或超时/队列为负数都会导致**启动失败**，不是运行期降级。
- 播放会话按 `(手机号, 逻辑通道号)` 唯一。同一通道已有会话时新请求返回 `409 session_conflict`，并在响应体里带现有会话 ID、类型与过期时间；不会顶替也不会排队。`DELETE /api/playback/:sessionId` 释放后可重新创建。
- `DELETE /api/playback/:sessionId` 会尽力下发停止指令：实时会话发 `0x9102`（关闭音视频传输），终端回放会话发 `0x9202`（结束回放）。无论下发是否成功，本地会话都会释放；响应里的 `stopCommand` 区分三种结果：`notified=true` 表示直发模式已写入终端连接，`queued=true` 表示 outbox 已受理（异步投递，凭 `commandId` 在 `GET /api/commands/<commandId>` 回查 `0x0001` 结果），`notified/queued` 都为 false 时 `reason` 给出未下发原因。重复 `DELETE` 同一会话返回 `alreadyReleased=true`，不会重复下发。
- 会话因 30 秒无媒体包被空闲回收时**不下发**停止指令：该回收只发生在媒体侧确实收不到包时，且媒体角色不反向调用 808 信令。

> **部署注意（务必遵守）：`rtmp` / `both` 模式下必须保持 SRS 的 `gop_cache` 开启（默认值），不要关闭。**
>
> 桥接模式下观众直连 SRS，平台不参与观众接入，无法替观众做 GOP 对齐。SRS 的 `gop_cache` 正是缓存 GOP 头、给新观众补发 SPS/PPS 与关键帧的机制；关掉后新观众会黑屏或长时间等待下一个关键帧。
>
> 平台侧的帧环对齐（从最近 GOP 头起播）**只对自研 `flv` 模式生效**，不能替代 SRS 的 `gop_cache`。若历史上按“由平台做对齐”的建议关闭过，请恢复为默认值：

```conf
# srs.conf
vhost __defaultVhost__ {
    gop_cache       on;      # 保持开启
    gop_cache_max_frames 2500;
}
```

- `record.mode` 控制是否落盘：`off`（集群默认，只保留内存帧环）、`legacy`（持续写 `.h264/.g711`）、`segmented`（按周期切 MPEG-TS 片并上传对象存储）。终端回放流（`kind=terminal_playback`）默认**不录制**，需要留档时显式配置 `segmented`。
- `0x9201` 拉的是**终端本地存储**上的录像，不是平台录像。平台录像回放走业务 API：先查覆盖区间，再按 `Range` 读取对象存储中的片段，不经过 `0x9201`。覆盖查询不完整时返回 `200` 与 `{requested, available, missing, playable, segments}`，**不是 409**；只有参数错误才返回 4xx。

### 1.5 集群角色二进制部署

五个二进制各自固定 `server.mode` 与 `cluster.roles`。配置文件写了不匹配的角色时**启动失败**，
错误信息直接给出应该使用的二进制，例如：

```text
jt-control requires cluster.roles to contain "control"; current roles=[media]. use jt-media instead
```

可直接使用的配置样例在 `configs/cluster/`：

| 二进制 | 固定角色 | 样例配置 | 探针端口 |
|---|---|---|---|
| `jt-server-standalone` | `mode=standalone` | `configs/cluster/jt-server-standalone.yaml` | `:9105` |
| `jt-signal` | `cluster.roles=[gateway]` | `configs/cluster/jt-signal.yaml` | `:9106` |
| `jt-media` | `cluster.roles=[media]` | `configs/cluster/jt-media.yaml` | `:9107` |
| `jt-control` | `cluster.roles=[control]` | `configs/cluster/jt-control.yaml` | `:9108` |
| `jt-attachment` | `cluster.roles=[attachment]` | `configs/cluster/jt-attachment.yaml` | `:9109` |

`jt808-server` 是保留的通用入口：完全按配置文件的 `server.mode` 与 `cluster.roles` 运行，
适合同机调试任意角色组合；生产部署应使用固定角色的二进制，避免配置与二进制职责错配。

最小启动方式（五个终端并行，先按上表创建共享卷标记文件）：

```bash
# 0) 一次性：初始化共享库并迁移到最新 schema
./bin/jt808-migrate -c configs/cluster/jt-control.yaml

# 1) 独立形态：一个进程承担全部角色
./bin/jt-server-standalone -c configs/cluster/jt-server-standalone.yaml

# 2) 网关：只做 808 接入与信令
./bin/jt-signal -c configs/cluster/jt-signal.yaml

# 3) 媒体：只做 1078 数据面与播放会话
./bin/jt-media -c configs/cluster/jt-media.yaml

# 4) 管控：业务 API、outbox 与媒体/附件调度
./bin/jt-control -c configs/cluster/jt-control.yaml

# 5) 附件：独立监听附件 TCP/UDP，切片写入共享对象存储
./bin/jt-attachment -c configs/cluster/jt-attachment.yaml
```

`jt-signal` / `jt-control` 依赖共享 MySQL；`jt-media` / `jt-attachment` 是数据面节点，不连数据库。
所有集群角色必须指向**同一套** `cluster.storage`（同一个 `root`，或同一个 `objectStore` 桶）。

> `cluster.allowEmbeddedStorage` 是 **dev-only** 开关，只允许本地联调时让多个角色进程共用一个 SQLite 文件。
> 开启后启动打印 WARN，`/readyz` 返回 `degraded`（HTTP 200，但 `status` 不是 `ready`）。
> **生产环境禁止开启**，集群部署必须使用共享 MySQL。

**时间同步（强制）**：集群所有副本的节点判死、会话路由、outbox 租约与任务租约
都使用**进程时钟**，不再依赖数据库时钟。部署前必须：

1. 所有主机启用 NTP/chrony（如 `chronyd` 或 `systemd-timesyncd`），并与同一时间源同步；
2. 核对 `chronyc tracking` 的 `System time` 偏差在 100ms 内，`Leap status` 为 `Normal`；
3. 跨副本抢租约使用保守容差，判定式为 `expires_at + tolerance < now`。容差默认
   2 秒，可通过 `cluster.leaseTolerance` 调整；调小会放大时钟偏差导致双持有的风险，
   调大会延后故障接管，通常无需修改。

进程每分钟读取一次数据库时间与本机时钟比对，结果只用于观测与告警
（`jt808_clock_skew_ms` / `jt808_clock_skew_alerts_total`）；绝对值超过 2 秒时
打印 WARN 并累加告警计数，不会改变任何业务判定。memory 后端没有数据库时钟，
该探针自动跳过。


**端口规划**（同机部署全部角色时逐项核对，避免撞车）：

| 用途 | 默认端口 | 角色 / 配置项 |
|---|---|---|
| 808 主连接 TCP | 8080 | signal，`server.port.tcpPort` |
| 808 主连接 UDP | 8081 | signal，`server.port.udpPort` |
| 业务 HTTP API | 8008 | control，`server.port.httpPort` |
| admin | 9000 | control，`admin.addr` |
| gateway 下行契约 | 9101 | signal，`cluster.gateway.addr` |
| control 上行入口 | 9102 | control，`cluster.control.addr` |
| 1078 媒体 TCP/UDP | 7300 | media，`mediaServer.port` |
| 1078 媒体 HTTP / FLV | 7301 | media，`mediaServer.httpPort` |
| 附件 TCP | 7400 | attachment，`alarmServer.port` |
| 附件 UDP | 7401 | attachment，`alarmServer.udpPort` |
| 附件内部 HTTP | 7402 | attachment，`alarmServer.httpPort` |
| 未来 1078 FTP 控制 | 7500 | 预留，不实现 |
| 未来 1078 FTP 数据 | 7501 | 预留，不实现 |
| probes | 9105–9109 | 每角色独立，`probes.addr` |

**探针**：五个二进制都必须配置 `probes.addr`，只暴露两个接口：

| 接口 | 语义 |
|---|---|
| `GET /healthz` | 进程存活，恒 `200 {"status":"ok"}` |
| `GET /readyz` | 逐项执行真实依赖检查；任一失败返回 `503` 与 `status: not ready` |

`/readyz` 的检查项按角色装配：

- `database`：数据库连通（gateway / control / attachment）。
- `outbox_backlog`：过期 pending 命令未超过阈值（control，且 outbox 开启）。
- `control_endpoint`：`uplink.http.url` 的 host 可解析（gateway-only）。
- `media_nodes`：`cluster.media.nodes` 非空、至少一个节点可达且仍有并发额度（control 且没有本地媒体）。
- `attachment_nodes`：`cluster.attachment.nodes` 非空、至少一个附件节点可达且仍有并发额度（control）。
- `attachment_object_store`：附件节点已接入共享 objectStore 与共享元数据库（attachment）。
- `media_disk` / `attachment_disk` / `media_upload_disk`：对应目录存在且可写。

**control 调度 media**：

- `cluster.media.nodes` 是管控可选的媒体节点清单，地址为 `jt-media` 的 `mediaServer.httpPort` 基址。
- 管控通过 `GET /internal/media/status` 获取节点会话与剩余额度，选择“剩余额度多、当前负载低”的节点；
  同一 `(phone, channel)` 在会话生命周期内固定在同一节点，管控重启后会从节点状态恢复粘性。
- 创建/释放在媒体节点上真实执行：`POST /internal/media/sessions`、`DELETE /internal/media/sessions/{id}`。
- 没有可用节点时，播放接口返回 `503` 与稳定错误码：
  - `no_media_node_configured`：`cluster.media.nodes` 为空；
  - `no_media_node_available`：节点全部不可达、全部满载或创建全部失败，`message` 带逐个节点的原因。
- 平台录像覆盖查询会汇总所有媒体节点的切片；切片内容按 `Range` 从持有该切片的节点流式读取。

**control 调度 attachment**：

- `cluster.attachment.nodes` 的地址是 `jt-attachment` 的 `alarmServer.httpPort`（默认 `7402`）。
- 管控调用 `GET /internal/attachment/status` 获取 `active / maxConcurrent / available / advertiseIp / tcpPort / udpPort`。
- 手工 `POST /api/device/:phone/attachments/upload` 自动填入节点地址；同一 `(phone, alarmNo)` 粘性。
- 没有可用节点时返回 `503` 与稳定错误码：
  - `no_attachment_node_configured`：`cluster.attachment.nodes` 为空；
  - `no_attachment_node_available`：节点全部不可达或全部满载，`err` 带逐个节点的原因。
- 自动 `0x9208` 在 control 内识别报警后经下行契约下发，附件节点地址来自节点状态，
  不再要求 gateway 与 control 同进程；下发失败会打 `WARN` 并保留报警记录。

**集群下行路由与会话目录**：

- `cluster.gateway.routing` 必须显式选择：
  - `fixed`：control 固定发往 `cluster.gateway.url`，只适用于单网关部署；
  - `directory`：control 按 phone 查询 `session_directory`，联表 `gateway_nodes`
    定位归属网关，`cluster.gateway.url` 必须留空。
  - `fixed` 缺少 `url`、`directory` 仍配置 `url`、control 未配置 `routing` 都直接拒绝启动。
- 启动日志打印实际下行模式：`downlink_mode=local|fixed|directory`
  （`local` 表示 gateway 与 control 同进程，走进程内直连）。
- 网关注册时会在同一事务内删除同 `gateway_id` 下旧 `instance_id` 的
  `session_directory` 行；网关重启换实例后，旧会话不会继续被路由。
- `session_directory` **没有 per-phone TTL**。会话是否有效由所属节点是否
  `status=online` 且未过期推出；网关本地 JT808 心跳超时/断链后上报 release。
- 预置会话访问自己的网关所在节点 = 一次 `INSERT IGNORE` 或 `DELETE`，因此终端重连必须
  重新 `0x0102` 才能建立新 generation。
- 下行找不到目录、节点离线或过期、`sessionId` 不匹配时一律返回 `offline`，
  由 outbox 按既有重试策略重投；control 不跨节点轮询，也不猜测兜底。

**集群混沌场景**：

| 场景 | 预期行为 | 观测线索 |
|---|---|---|
| 网关进程重启（同 `gatewayId` 新实例） | 注册即清理旧实例目录行；旧 generation 上行被 `fenced`，旧实例心跳返回 `instance_stale` | `jt808_gateway_node_fenced_total` 增长；`jt808_uplink_fenced_total` 增长；`jt808_gateway_nodes_online` 不抖动 |
| 同 `gatewayId` 两个进程同时在线 | 后注册者顶替前者的节点行并清理旧目录；部署侧应避免，否则归属会周期性抖动 | `jt808_gateway_node_fenced_total` 持续增长；注册日志中同一 `gateway_id` 频繁换 `instance_id` |
| 网关宕机/网络分区，心跳停止 | 30 秒后节点过期；该节点下所有会话不可路由，下行返回 `offline` 并由 outbox 重试 | `jt808_gateway_nodes_expired` > 0；`jt808_downlink_offline_total` 增长 |
| 网关下线但目录残留（bug / release 丢失） | 下行继续路由到该网关，直到 phone 重新 claim 或节点换实例；平台侧不兜底 | `jt808_downlink_directory_hit_total` 正常但终端无响应；节点仍为 online |
| 终端跨网关重连 | 新 `0x0102` 立即改写目录并递增 generation；旧网关在下一次上行收到 `fenced` 后关闭会话 | `jt808_uplink_fenced_total` 短时增长；目录 `generation` 递增 |
| control 重启 | 不丢目录与节点状态；启动后重新按 `directory` 模式路由 | 启动日志 `downlink_mode=directory`；`jt808_gateway_nodes_*` 与重启前一致 |
| 主机时钟漂移 | 节点判死、租约接管时间偏移；偏差在 2 秒容差内时接管延后但不会双持有 | `jt808_clock_skew_ms` 绝对值上升；`jt808_clock_skew_alerts_total` 增长时检查 NTP |

**附件存储与进度**：

- 附件节点只保留有界本地暂存（`alarmServer.dir/staging`），收到数据即写入，按
  `alarmServer.segmentSizeBytes`（默认 8 MiB）切片上传到共享 objectStore。
- 元数据由 control 落库 `attachment_files`：`received_bytes / watermark / segments / status`，
  控制面通过 `GET /api/device/:phone/attachments` 查询进度，附件节点重启不丢。
- 附件节点不连数据库，通过 `alarmServer.progressReport` 出站上报进度（幂等 upsert），
  重连时向 control 查询恢复点；control 不可达时用本地暂存与对象存储扫描继续接收。
- 暂存区总量 `alarmServer.stagingMaxBytes` 到顶后拒绝新的文件登记（终端收到失败应答），不排队、不写满磁盘。
- 崩溃残留按 `.part` 标记 + 宽限期扫描清理；文件全部切片提交后才转为 `completed`，并把对象 `MarkCommitted`。
- 过期标记、pending 回收与保留期统一由 control 执行，范围限定在 `attachment/` 前缀与 `attachment_files` 表；
  control 不得删除或引用边缘节点的本地暂存路径。
- 对象 key 自带元数据，扫描即可重建索引：
  `attachment/<source>/<phone>/<alarmNo>/<fileName>/<offset>_<size>_<total>_<index>`。
- 集群的媒体与附件共用一套 objectStore，key 前缀按来源分区：
  `media/segments/`（1078 切片）、`media/uploads/`（0x0801 图片）、`attachment/subia/`（苏标附件）、
  `attachment/ftp1078/`（预留）。pending 回收与保留期清理都按前缀隔离。

---

## 2. 数据库迁移

迁移是独立工具，主服务启动时**只校验 schema 版本，不执行 DDL**；版本不符会拒绝启动。

```bash
# 二进制部署
./target/release/jt808-migrate_linux_amd64/jt808-migrate -c <config file>

# 容器部署
docker exec jt808 /app/output/jt808-migrate -c /app/output/configs/default.yaml
```

- 首次部署、升级后、恢复备份到新版本前都必须先执行。
- 迁移 SQL 分两套目录：`internal/repository/sqlite/migrations/` 与 `internal/repository/mysql/migrations/`；
  当前基线为 `v1.0.0.sql`，版本按三段数值比较（`1.10.0` > `1.9.0`）。
- `jt808-migrate` 按 `database.type` 只执行对应目录，两套目录版本号同步推进。
- 如果 dev 库残留旧的 8 位数字版本（如 `20261118`），启动会明确报错并提示重建库，不会自动升级。
- `memory` 后端不执行迁移。

---

## 3. 备份与恢复

需要备份三类数据：数据库、`data/` 下的媒体/附件文件、配置与密钥。

### 3.1 SQLite

```bash
# 在线一致性备份（推荐，不中断服务）
sqlite3 ./data/jt808.db ".backup '/backup/jt808-$(date +%Y%m%d-%H%M).db'"

# 或停服后直接复制文件
cp ./data/jt808.db /backup/jt808-$(date +%Y%m%d-%H%M).db
```

### 3.2 MySQL

```bash
mysqldump --single-transaction --routines --triggers \
  -h <host> -u <user> -p <database> > /backup/jt808-$(date +%Y%m%d-%H%M).sql
```

### 3.3 文件目录

```bash
tar -czf /backup/jt808-files-$(date +%Y%m%d-%H%M).tgz ./data ./logs
```

媒体与附件目录可能较大，建议按天增量同步；附件目录结构为
`<dir>/<手机号>/<YYYYMMDD>/<报警编号>/`，可按日期裁剪。

### 3.4 恢复步骤

1. 停止服务（或进入维护窗口）。
2. 恢复数据库备份。
3. 执行迁移工具，把 schema 升到当前二进制要求的版本。
4. 恢复 `data/` 文件目录，保持属主与权限。
5. 启动服务，确认 `/readyz` 返回 `status: ready` 且 `database` 检查通过。
6. 抽查最近定位/报警查询与媒体下载。

---

## 4. 回滚

### 4.1 应用回滚

- 保留上一版本的二进制或镜像 tag，回滚时直接替换并重启。
- 数据库迁移默认只向前；如果新版本包含破坏性变更，**不能仅靠回退二进制**，
  必须恢复到升级前的数据库备份。
- 回滚后先执行 `jt808-migrate` 校验版本；版本不符时服务会拒绝启动，属预期行为。

### 4.2 配置回滚（无需回退代码）

| 场景 | 配置动作 |
|---|---|
| 下行 outbox 异常，需要临时直连下发 | `commandOutbox.mode: "off"` |
| 上行 at-least-once 导致终端频繁重传 | `uplink.reliability` 对应类别改回 `best_effort` |
| 附件/媒体清理误删风险 | 对应 `retentionDays: 0`（关闭时间清理） |
| 历史表裁剪影响排查 | `retention.locations.retentionDays: 0`、`maxPerDevice: 0` |
| API 鉴权挡住联调 | 临时清空 `api.auth.token`（仅内网，且必须尽快恢复） |

配置修改后重启生效；回滚前先备份当前配置。

---

## 5. 保留期与清理

| 数据 | 配置 | 默认 | 生效方式 |
|---|---|---|---|
| 定位历史 `locations` | `retention.locations.retentionDays` / `maxPerDevice` | 7 天 / 10000 条 | 写入时按设备裁剪；开启定期清理后同时按接收时间全局删除 |
| 报警历史 `alarms` | `retention.alarms.dedupWindowMs` / `confirmExpireMs` / `maxPerDevice` | 30 分钟去重 / 10 分钟确认关联 / 1000 条 | 写入时去重与裁剪 |
| 报警历史 `alarms` | `retention.alarms.retentionDays` | 0（关闭） | 定期清理按接收时间删除 |
| `0x0801` 媒体元数据与文件 | `retention.mediaFiles.retentionDays` | 0（关闭） | 定期清理：先删文件，再删 `media_files` 记录 |
| `0x0805` 拍摄应答 | `retention.mediaCaptures.retentionDays` | 0（关闭） | 定期清理 `media_captures` 与同事务删除 `media_capture_items` |
| `0x0802` 检索应答 | `retention.mediaSearchResults.retentionDays` | 0（关闭） | 定期清理 `media_search_results`，按自身接收时间分批删除 |
| 媒体目录孤儿文件 | `retention.orphanFiles.*` | 关闭（宽限 7 天） | 按文件 mtime 判断年龄，删除库中无对应记录的文件；跳过 `.part` 临时文件 |
| 媒体 `.h264` | `mediaServer.retentionDays` | 7 天 | 每小时扫描目录，跳过正在写入的流 |
| 苏标附件 | `alarmServer.retentionDays` | 7 天 | 每小时扫描目录，跳过未完成组装的临时文件 |

- 显式 `0` 表示关闭对应清理；负数、超过 36500 天、非法 `interval` 或 `batchSize` 越界会导致启动失败。
- 历史表定期清理由 `retention.cleanup.enabled` 控制，默认关闭；开启后按 `interval` 执行，按主键分批删除（默认每批 1000 行，上限 5000），批间让出。
- 历史表清理仅依赖 SQL 后端；`memory` 后端数据随进程结束释放，开启时只记录告警并跳过。
- 媒体文件清理顺序为先删文件、再删元数据；文件删除失败时保留记录并在下次运行重试，文件已不存在则视为删除成功。
- 孤儿文件扫描由 `retention.orphanFiles.enabled` 显式开启，默认关闭。判定依据是文件自身 mtime，而不是 `<手机号>/<YYYYMMDD>` 目录名，避免把补传到旧目录的新文件误删；只有 mtime 早于 `now - graceDays` 且 `media_files` 中无对应相对路径的文件才会删除。`.part` 临时文件与隐藏文件一律跳过；单次最多处理 `maxFilesPerRun` 个文件，按批比对数据库并在批间让出，随停机信号取消。`memory` 后端没有比对依据，不启用。
- 媒体 `.h264` 目录清理与数据库保留期清理相互独立。

---

## 6. 探针与指标

### 6.1 健康检查

| 路径 | 鉴权 | 语义 |
|---|---|---|
| `GET /healthz` | 无 | 进程存活，恒返回 `{"status":"ok"}` |
| `GET /readyz` | 无 | 就绪检查；任一检查失败返回 `503` 与 `status: not ready` |
| `GET /metrics` | Bearer token | Prometheus 文本格式（仅管理面） |

每个角色二进制通过 `probes.addr` 提供独立的 `/healthz` 与 `/readyz`（不承载业务接口、不做鉴权）；
`admin.enable` 与 `probes.addr` 可以同时开启，两者端口不能相同。

`/readyz` 当前包含：

- `database`：数据库 `Ping` 是否成功（3 秒超时）。
- `outbox_backlog`：下行 outbox 过期（超过 `expireAfter` 仍未投递）数量是否达到阈值（默认 100）；`commandOutbox.mode: off` 时不检查。
- `control_endpoint`：gateway-only 时 `uplink.http.url` 的 host 可解析；不可解析说明上行事件无处投递。
- `media_nodes`：control 且没有本地媒体时，`cluster.media.nodes` 非空、至少一个节点可达且仍有并发额度。
- `attachment_nodes`：control 时 `cluster.attachment.nodes` 非空、至少一个节点可达且仍有并发额度。
- `attachment_object_store`：attachment 角色已接入共享 objectStore 与共享附件元数据表。
- `media_disk`：媒体目录存在且可写，仅在 `mediaServer` 启用时检查。
- `attachment_disk`：附件目录（集群下为 `alarmServer.dir/staging`）存在且可写，仅在 `alarmServer` 启用时检查。
- `media_upload_disk`：0x0801 落盘目录存在且可写。

未启用的组件不参与判定，避免把“功能关闭”误报成“服务异常”。

### 6.2 指标清单

| 指标 | 含义 |
|---|---|
| `jt808_sessions_connected` | 当前活跃终端会话数（TCP + UDP） |
| `jt808_ack_pending` | 当前等待终端应答的在途项 |
| `jt808_ack_timeouts_total` | 重传耗尽仍未收到应答的累计次数 |
| `jt808_outbox_pending` | 下行 outbox 中 `pending` 的命令数 |
| `jt808_outbox_expired` | 超过 `expireAfter` 仍未投递成功的命令数 |
| `jt808_media_streams` | 进行中的 1078 媒体会话数 |
| `jt808_attachments_active` | 进行中的苏标附件组装数 |
| `jt808_attachments_completed_total` | 已完成附件累计数 |
| `jt808_retention_deleted_total{category}` | 定期清理累计删除行数，`category` 为 `locations` / `alarms` / `mediaFiles` / `mediaCaptures` / `mediaSearchResults` |
| `jt808_retention_failures_total{category}` | 定期清理累计失败批次数 |
| `jt808_retention_last_run_timestamp_seconds` | 最近一次清理运行时间；为 0 表示尚未运行 |
| `jt808_retention_last_run_duration_seconds` | 最近一次清理总耗时 |
| `jt808_orphan_files_scanned_total` / `deleted_total` / `skipped_total` | 孤儿文件扫描累计遍历、删除与跳过文件数 |
| `jt808_orphan_scan_failures_total` | 孤儿文件扫描累计失败次数 |
| `jt808_orphan_scan_last_run_timestamp_seconds` / `last_run_duration_seconds` | 最近一次孤儿扫描的运行时间与耗时 |
| `jt808_attachment_trigger_skipped_total` | `alarmServer.autoUpload` 因当前二进制没有 control 角色而未自动下发 0x9208 的次数 |
| `jt808_gateway_nodes_online` | 当前在线且未过期的网关节点数 |
| `jt808_gateway_nodes_offline` | 已优雅注销的网关节点数 |
| `jt808_gateway_nodes_expired` | 仍标记 online 但已超过 `expires_at` 的节点数 |
| `jt808_downlink_directory_hit_total` | `directory` 模式成功定位到可用归属网关的累计次数 |
| `jt808_downlink_offline_total` | `directory` 模式判定无可用归属、直接返回 `offline` 的累计次数 |
| `jt808_uplink_fenced_total` | 上行事件 generation 落后于目录、被丢弃的累计次数 |
| `jt808_gateway_node_fenced_total` | 节点心跳因实例已被新注册顶替而拒绝的累计次数 |
| `jt808_clock_skew_ms` | 最近一次进程时钟与数据库时间的偏差（毫秒）；正数表示进程时钟偏慢，仅观测 |
| `jt808_clock_skew_alerts_total` | 偏差绝对值超过 2 秒阈值的累计告警次数 |

采集示例：

```bash
curl -fsS -H "Authorization: Bearer $ADMIN_TOKEN" http://127.0.0.1:9000/metrics
```

### 6.3 告警建议

- `/readyz` 连续 3 次失败 → 页面告警。
- `jt808_outbox_expired > 0` 持续 10 分钟 → 检查终端在线率与 `commandOutbox` 配置。
- `jt808_ack_timeouts_total` 快速增长 → 检查网络质量、`ackTimeoutMs` 与终端重传配置。
- `jt808_attachments_active` 长期不下降 → 检查附件端口连通性与磁盘空间。
- `jt808_gateway_nodes_expired > 0` 持续存在 → 检查网关进程与心跳链路；节点过期会让其全部会话不可路由。
- `jt808_downlink_offline_total` 快速增长 → 对照 `jt808_gateway_nodes_*` 与会话目录，确认是终端下线还是节点失联。
- `jt808_gateway_node_fenced_total` 持续增长 → 同一 `gatewayId` 有多实例竞争，检查部署侧唯一性约束。
- `jt808_clock_skew_alerts_total` 增长或 `jt808_clock_skew_ms` 绝对值持续 > 2000 → 立即检查主机 NTP/chrony；时钟失步会让租约与节点过期判定同时失真，容差只能覆盖 2 秒以内的偏差。

---

## 7. 压测基线与结论

### 7.1 口径

- 脚本：`scripts/loadtest.sh`，构建真实 server 与模拟终端二进制。
- 存储：`memory` 后端，只压协议接入与业务处理路径，不把 SQLite 写入计入基线。
- 负载：200 个并发终端，每终端每秒上报 1 条 `0x0200`，保活周期 20 秒，持续 60 秒。
- 采集：结束时读取 `/metrics` 与 `/readyz`，并用 `top` 取一次 RSS/CPU 样本。

```bash
bash scripts/loadtest.sh 60
# 可用环境变量调整：LOAD_CONCURRENCY / LOAD_TCP_PORT / LOAD_HTTP_PORT / LOAD_ADMIN_PORT
```

### 7.2 基线结果（开发机，非生产容量规划依据）

| 指标 | 结果 |
|---|---|
| 并发终端 | 200 |
| 持续时长 | 60 秒 |
| 活跃会话 | 200 |
| 待应答 `jt808_ack_pending` | 0 |
| 应答超时 `jt808_ack_timeouts_total` | 0 |
| 下行 outbox `pending` / `expired` | 0 / 0 |
| 媒体 / 附件进行中 | 0 / 0 |
| 服务端 RSS | 约 33 MB |
| 服务端 CPU | 约 1.3%（单次采样） |
| `/readyz` | `ready`，`database` 与 `outbox_backlog` 均通过 |
| 服务端错误日志 | 201 行，全部为压测结束时客户端断开的 `EOF`，无业务错误 |

### 7.3 结论与限制

- 当前实现可稳定承载 200 个每秒上报的终端；应答无积压、无超时。
- 基线未覆盖：SQLite/MySQL 写入、媒体 RTP、苏标附件上传、下行命令风暴。
  这些场景需要单独压测，不能直接套用上表。
- 压测脚本默认关闭苏标附件与媒体服务，避免与本机端口冲突；需要时单独改配置。
- 内存后端重启即丢，基线数字只代表协议路径处理能力。

---

## 8. 常见问题

| 现象 | 排查方向 |
|---|---|
| 启动失败，提示 schema 版本不符 | 先执行 `jt808-migrate`，再启动服务 |
| 启动失败，提示 `20261118` 等旧数字版本 | dev 库早于语义化版本 `v1.0.0`，删除 SQLite 文件或清空表后重新执行 `jt808-migrate`；不支持自动升级 |
| `readyz` 中 `database` 失败 | 检查库文件权限/DSN、连接池与磁盘空间 |
| `outbox_backlog` 失败 | 查看 `jt808_outbox_expired`，确认终端在线率与 `expireAfter` |
| 端口占用 | 检查 `server.port`、`alarmServer.port/udpPort`、`mediaServer.port` 是否冲突 |
| 业务 API 全部 401 | 检查 `api.auth.token` 与请求头 `Authorization: Bearer <token>` |
| 业务 API 429 | 调整 `api.rateLimit.ratePerSecond` / `burst`，或按来源 IP 排查异常调用 |
| 附件目录持续增长 | 检查 `alarmServer.retentionDays`，确认清理任务未被关闭 |
| 终端持续重传定位 | 检查该类别是否配置为 `at_least_once`，以及数据库写入是否变慢 |
