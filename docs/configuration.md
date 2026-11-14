# 配置说明

配置文件默认为 `configs/default.yaml`，完整注释见该文件与 `configs/cluster/*.yaml`。不带 `-c` 时使用编译进二进制的内嵌默认配置。

配置在启动时统一校验：端口越界、未知枚举、角色缺少必需项、目录或共享卷标记缺失等都会直接拒绝启动，不做运行期降级。

## 1. 服务基础

| 配置键 | 默认 | 说明 |
|---|---|---|
| `server.name` | `jt808-server-go` | 实例名，写入日志与启动横幅 |
| `server.mode` | `standalone` | `standalone` / `cluster`；`cluster` 时按 `cluster.roles` 装配 |
| `server.ackTimeoutMs` | `5000` | 下行命令等待终端应答的超时 |
| `server.maxRetransmissions` | `3` | 下行命令在会话内最多重传次数 |
| `server.port.tcpPort` | `8080` | 终端 TCP 接入；置空关闭 |
| `server.port.udpPort` | `8081` | 终端 UDP 接入；置空关闭 |
| `server.port.httpPort` | `8008` | 业务 HTTP API |
| `server.banner.enable` / `bannerPath` | `true` / `configs/banner.txt` | 启动横幅 |
| `server.action.http.url` | 空 | 生命周期 Hook 回调地址；空则不启用 |
| `server.action.http.timeoutMs` | `2000` | Hook 回调超时 |

## 2. 日志

| 配置键 | 默认 | 说明 |
|---|---|---|
| `log.consoleEnable` | `true` | 控制台输出 |
| `log.fileEnable` | `true` | 文件输出 |
| `log.printAsJson` | `false` | JSON 行格式 |
| `log.logLevel` | `DEBUG`（默认配置）/ 代码内置 `info` | `debug`/`info`/`warn`/`error` |
| `log.logDirectory` | `./logs/` | 日志目录 |
| `log.logFile` | `jt808-server-go.log` | 日志文件名 |
| `log.maxSizeOfRolling` | `50` | 单文件滚动大小（MB） |
| `log.maxBackupsOfRolling` | `128` | 保留份数 |
| `log.maxAgeOfRolling` | `7` | 保留天数 |

## 3. 数据库与保留策略

| 配置键 | 默认 | 说明 |
|---|---|---|
| `database.type` | `memory` | `memory` / `sqlite` / `mysql`；空值等价 `memory` |
| `database.sqlite.path` | `./data/jt808.db` | SQLite 库文件 |
| `database.sqlite.readConns` | `0` | 只读连接池；0 表示按 CPU 数，写池固定 1 连接 |
| `database.mysql.dsn` | 空 | 例如 `user:pass@tcp(host:3306)/jt808?parseTime=true&charset=utf8mb4` |
| `retention.locations.retentionDays` / `maxPerDevice` | `7` / `10000` | 定位历史保留；仅 SQL 后端生效 |
| `retention.alarms.dedupWindowMs` / `confirmExpireMs` / `maxPerDevice` | `1800000` / `600000` / `1000` | 报警去重、确认关联与单设备上限 |
| `retention.alarms.retentionDays` | `0` | 报警按接收时间清理；0 关闭 |
| `retention.mediaFiles.retentionDays` | `0` | `0x0801` 元数据与落盘文件清理；0 关闭 |
| `retention.mediaCaptures.retentionDays` | `0` | `0x0805` 拍摄记录清理；0 关闭 |
| `retention.mediaSearchResults.retentionDays` | `0` | `0x0802` 检索应答清理；0 关闭 |
| `retention.cleanup.enabled` / `interval` / `batchSize` | `false` / `1h` / `1000` | 历史表定期清理；batchSize 上限 5000 |
| `retention.orphanFiles.enabled` / `graceDays` / `interval` / `maxFilesPerRun` / `batchSize` | `false` / `7` / `24h` / `10000` / `500` | 媒体目录孤儿文件扫描；按文件 mtime 判断，仅 SQL 后端 |

- `memory` 后端下 `retention` 不生效；重启丢失业务数据。
- 定期清理只有 `retention.cleanup.enabled: true` 且对应 `retentionDays > 0` 才会执行。
- 负数、超过 36500 天、非法 `interval` 或越界 `batchSize` 会导致启动失败。
- 媒体文件清理先删文件、再删元数据；删除失败保留记录待下次重试。

## 4. 业务 API、管理面与探针

| 配置键 | 默认 | 说明 |
|---|---|---|
| `api.auth.token` | 空 | Bearer token；空表示关闭鉴权，生产必须配置 |
| `api.rateLimit.ratePerSecond` / `burst` | `50` / `100` | 按来源 IP 的令牌桶 |
| `admin.enable` | `false` | 是否启动独立管理面 |
| `admin.addr` / `token` | `127.0.0.1:9000` / 空 | 开启时 token 必填 |
| `admin.rateLimit.ratePerSecond` / `burst` / `keyBy` | `10` / `20` / `credential` | `keyBy` 可选 `credential` 或 `source` |
| `probes.addr` | 空 | 独立探针监听；五个角色二进制必须配置 |

管理面与探针可以同时开启，但端口不能相同。探针只暴露 `/healthz` 与 `/readyz`，不鉴权。

## 5. 上行出口与可靠性

| 配置键 | 默认 | 说明 |
|---|---|---|
| `uplink.sink` | `local` | `local`（进程内处理）/ `http`（投递到 control） |
| `uplink.http.url` | 空 | `sink=http` 时必填，例如 `http://control:9102/internal/v1/uplink` |
| `uplink.http.token` | 空 | `sink=http` 时必填，需等于 `cluster.control.token` |
| `uplink.http.timeoutMs` | `3000` | 单次投递超时；confirm-before-ack 必须小于终端重传窗口 |
| `uplink.http.queueSize` / `workers` | `1024` / `4` | best-effort 有界队列与后台 worker |
| `uplink.http.maxAttempts` | `1` | 单条事件投递次数上限，范围 1–3 |
| `uplink.http.gatewayId` | 主机名-进程号 | 事件信封中的网关标识 |
| `uplink.http.authGraceMs` | `1800000` | 缓存档案过期且 control 不可达时的放行宽限期；从未查到档案始终拒绝 |
| `uplink.reliability.default` | `best_effort` | 其余上行 |
| `uplink.reliability.location` | `best_effort` | 普通定位 `0x0200` |
| `uplink.reliability.batchLocation` | `at_least_once` | 批量定位 `0x0704` |
| `uplink.reliability.alarm` | `at_least_once` | 报警定位 `0x0200` |

`best_effort` 先应答后落库，可能丢一条；`at_least_once` 先落库再应答，落库失败终端重传，可能重复落库。注册、鉴权、查询应答等需要业务结果决定应答内容的消息始终先执行业务。

## 6. 下行 outbox

| 配置键 | 默认 | 说明 |
|---|---|---|
| `commandOutbox.mode` | 按后端归一化 | `off` / `memory` / `durable`；memory 后端默认 `memory`，SQL 后端默认 `durable` |
| `commandOutbox.pollInterval` / `batchSize` | `10s` / `50` | 重投轮询周期与批次 |
| `commandOutbox.maxAttempts` | `5` | 总下发次数上限（含首次） |
| `commandOutbox.leaseDuration` | `1m` | 单次投递租约；到期可被其他实例占用 |
| `commandOutbox.initialWait` | `1m` | 首次下发后等待应答/会话重传的窗口 |
| `commandOutbox.retryBase` / `retryMax` | `5s` / `5m` | 退避区间 |
| `commandOutbox.offlineRetry` | `15s` | 设备离线时的短退避 |
| `commandOutbox.expireAfter` | `24h` | 超过该时长仍未投递成功转 `failed` |
| `commandOutbox.retryOnTerminalFailure` | `false` | 终端明确执行失败后是否重投；默认不重投，避免重复副作用 |

重投复用原始流水号，语义为 at-least-once，终端可能收到并执行重复命令。

## 7. 苏标附件

| 配置键 | 默认 | 说明 |
|---|---|---|
| `alarmServer.port` / `udpPort` | `7200` / `7201` | TCP/UDP 附件监听；两者都为空时整段不启动 |
| `alarmServer.httpPort` | 空 | 附件节点内部 HTTP；`attachment` 角色必填 |
| `alarmServer.dir` | `./data/alarms` | 落盘目录；集群下为本地暂存根目录 |
| `alarmServer.maxFileSize` / `maxConcurrent` | `104857600` / `16` | 单附件上限与并发组装数 |
| `alarmServer.completedTTL` | `20m` | 已完成附件内存状态保留时长 |
| `alarmServer.retentionDays` | `7` | 已完成文件保留天数；0 不清理 |
| `alarmServer.segmentSizeBytes` | `8388608` | 集群对象切片大小（8 MiB） |
| `alarmServer.stagingMaxBytes` | `2147483648` | 附件节点本地暂存总量上限（2 GiB） |
| `alarmServer.autoUpload` / `uploadIP` | `false` / 空 | 报警首次上报时自动下发 `0x9208`；开启时 `uploadIP` 必填 |
| `alarmServer.progressReport.url` / `token` / `timeoutMs` | 空 / 空 / `3000` | 附件节点向 control 上报进度；token 需等于 `cluster.control.token` |

落盘路径为 `<dir>/<手机号>/<YYYYMMDD>/<报警编号>/<文件名>`。`0x9208` 通告的端口取实际监听值，未配置的一侧通告为 `0`。

## 8. 1078 媒体

| 配置键 | 默认 | 说明 |
|---|---|---|
| `mediaServer.port` / `tcpPort` | 空 / 空 | RTP UDP / TCP 监听；任一非空即启用，可只启用一个 |
| `mediaServer.httpPort` | UDP 端口 + 1 | 媒体节点内部 HTTP 与 FLV 播放 |
| `mediaServer.advertiseIp` | `127.0.0.1` | 下行信令与播放地址中填写的节点地址 |
| `mediaServer.dir` | `./data/media` | `.h264/.g711a/.g711u/.aac` 落盘目录 |
| `mediaServer.maxConcurrent` / `maxTCPConns` | `16` / `64` | 并发流上限与 TCP 连接数上限 |
| `mediaServer.retentionDays` | `7` | 音视频文件保留天数；0 不清理 |
| `mediaServer.record.mode` | standalone `legacy` / cluster `off` | `off` 不落盘；`legacy` 持续写裸流；`segmented` 按周期切 MPEG-TS 并上传对象存储 |
| `mediaServer.record.segmentMinutes` | `10` | 切片周期 |
| `mediaServer.playback.output` | `flv` | `flv` / `rtmp` / `both` |
| `mediaServer.playback.rtmp.url` | 空 | `rtmp`/`both` 必填，必须是 `rtmp://` 地址 |
| `mediaServer.playback.rtmp.streamPrefix` / `playUrlPrefix` | 空 | 推流与播放地址前缀 |
| `mediaServer.ringBytes` / `ringSeconds` | 32 MiB / 60s | 内存帧环容量与时长 |
| `mediaServer.maxBitrateKbps` | `0` | 节点入流码率容量；0 表示不限 |
| `mediaServer.segmentReport.url` / `token` / `timeoutMs` | 空 / 空 / `3000` | 切片完成后向 control 上报索引 |

`record.mode=rtmp` 等非法值、`output=rtmp` 缺少 URL、超时或队列为负数都会导致启动失败。发布到外部 RTMP 服务器（SRS/lal）时，需保持服务端 `gop_cache` 开启。

## 9. 多媒体落盘

| 配置键 | 默认 | 说明 |
|---|---|---|
| `mediaUpload.dir` | `./data/media-uploads` | `0x0801` 图片落盘目录，始终启用；集群下需位于共享卷 |

文件路径为 `<dir>/<手机号>/<YYYYMMDD>/<媒体ID>.<扩展名>`，写临时文件后原子 rename，同一 mediaId 幂等覆盖。

## 10. 集群配置

仅在 `server.mode=cluster` 时生效；`standalone` 下该段必须为空。五个角色二进制会强制校验 `cluster.roles` 与二进制匹配。

### 10.1 角色与内部契约

| 配置键 | 说明 |
|---|---|
| `cluster.roles` | `gateway` / `control` / `media` / `attachment` 的组合 |
| `cluster.gateway.addr` | 网关下行契约监听地址；`gateway` 角色必填 |
| `cluster.gateway.nodeId` | 网关节点标识，必须全局唯一；空值取主机名 |
| `cluster.gateway.advertiseUrl` | 网关告诉 control 的下行入口；空值回退 `url` |
| `cluster.gateway.token` | control → gateway 共享密钥；`gateway` 角色必填 |
| `cluster.gateway.routing` | control 的下行模式：`directory` 或 `fixed`；`control` 角色必填 |
| `cluster.gateway.url` | `fixed` 模式的唯一下行入口；`directory` 模式必须留空 |
| `cluster.control.addr` | control 上行 HTTP 入口；`control` 角色必填 |
| `cluster.control.token` | gateway/media/attachment → control 的共享密钥 |

### 10.2 节点清单

| 配置键 | 说明 |
|---|---|
| `cluster.media.nodeId` | 媒体节点标识；空值按通告地址推导 |
| `cluster.media.nodes[].name` / `url` | control 的静态媒体节点清单，`url` 为媒体节点 `mediaServer.httpPort` 基址；注册表为空时兜底 |
| `cluster.media.controlUrl` | 媒体节点注册与心跳的 control 基址；空值从 `segmentReport.url` 反推 |
| `cluster.media.advertiseUrl` | control 可达的本节点内部 HTTP 基址 |
| `cluster.media.token` | media → control 的内部令牌；空值回退 `segmentReport.token` |
| `cluster.attachment.nodeId` | 附件节点稳定标识 |
| `cluster.attachment.nodes[].name` / `url` | control 的静态附件节点清单，`url` 为附件节点 `alarmServer.httpPort` 基址 |
| `cluster.attachment.advertiseUrl` | 附件节点登记到 control 的内部 HTTP 基址 |
| `cluster.attachment.ftp1078.port` | 1078 FTP 控制端口，默认 `7500`；缺省关闭 FTP |
| `cluster.attachment.ftp1078.passivePorts` | 被动端口段，如 `7501-7510`；只接受 PASV/EPSV |
| `cluster.attachment.ftp1078.advertiseIp` | 0x9206/PASV 通告给终端的地址；NAT 场景必须显式配置 |

### 10.3 内部契约与存储

| 配置键 | 默认 | 说明 |
|---|---|---|
| `cluster.downlink.timeoutMs` | `8000` | sync_ack 等待上限；不得超过 `ackTimeoutMs × (maxRetransmissions+1)` |
| `cluster.downlink.idempotencyTTL` | `2m` | 网关在途记录与幂等窗口 |
| `cluster.downlink.maxInFlight` | `256` | 并发 sync_ack 等待上限，超出返回 429 |
| `cluster.uplink.dedupTTL` / `dedupMaxEntries` | `10m` / `100000` | 上行 eventId 去重窗口 |
| `cluster.uplink.maxBodyBytes` | `4194304` | 单个上行请求体上限 |
| `cluster.storage.backend` | `local` | media/attachment 角色必须为 `sharedVolume` 或 `objectStore` |
| `cluster.storage.sharedVolume` | `false` | 必须显式 `true`，避免把本机目录误当共享卷 |
| `cluster.storage.marker` | `.jt808-shared-volume` | 共享卷标记文件；必须真实存在 |
| `cluster.storage.root` | `./data/blobs` | 共享卷根目录 |
| `cluster.storage.objectStore.endpoint` / `region` / `bucket` / `accessKey` / `secretKey` | 空 | S3 兼容对象存储 |
| `cluster.storage.objectStore.pathStyle` / `prefix` / `retries` | `true` / 空 / 0 | MinIO 等自建存储使用 path-style |
| `cluster.allowEmbeddedStorage` | `false` | 仅本地联调：允许多角色共用 SQLite，生产禁止 |
| `cluster.leaseTolerance` | `2s` | 跨副本抢租约的容差；`expires_at + tolerance < now` 才接管 |

`objectStore` 模式下不要开启 `retention.orphanFiles`，应使用桶生命周期规则清理孤儿对象。

## 11. 配置示例

### 11.1 单机 SQLite + 鉴权 + durable outbox

```yaml
database:
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

commandOutbox:
  mode: "durable"
```

### 11.2 集群最小组合

```yaml
# jt-signal
server:
  mode: "cluster"
probes:
  addr: "0.0.0.0:9106"
uplink:
  sink: "http"
  http:
    url: "http://control:9102/internal/v1/uplink"
    token: "uplink-token"
cluster:
  roles: ["gateway"]
  gateway:
    addr: "0.0.0.0:9101"
    advertiseUrl: "http://signal-1:9101"
    token: "cluster-shared-token"
  control:
    token: "uplink-token"
  storage:
    backend: "local"
```

```yaml
# jt-control
server:
  mode: "cluster"
probes:
  addr: "0.0.0.0:9108"
database:
  type: "mysql"
  mysql:
    dsn: "root:password@tcp(mysql:3306)/jt808?parseTime=true&charset=utf8mb4"
commandOutbox:
  mode: "durable"
cluster:
  roles: ["control"]
  gateway:
    routing: "directory"
    token: "cluster-shared-token"
  control:
    addr: "0.0.0.0:9102"
    token: "uplink-token"
  media:
    nodes:
      - name: "media-1"
        url: "http://media-1:7301"
  attachment:
    nodes:
      - name: "attachment-1"
        url: "http://attachment-1:7402"
  storage:
    backend: "sharedVolume"
    sharedVolume: true
    root: "/shared/jt808"
    marker: ".jt808-shared-volume"
```

更完整的四角色样例见 `configs/cluster/`；部署步骤见[安装与部署](installation.md#6-集群角色部署)。
