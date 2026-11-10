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

1. 配置文件中的 `storage.type` 决定持久化后端：`memory | sqlite | mysql`。
   - `memory` 只用于演示/压测，重启即丢，生产不得使用。
   - `sqlite` 需要可写的库文件路径（默认 `./data/jt808.db`）。
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

进程收到 `SIGTERM`/`SIGINT` 后按以下顺序优雅退出：
业务 API → 终端 TCP 接入与存量会话 → UDP 接入 → 下行 outbox 当前轮次 → 媒体与附件落盘 → 管理面 → 存储。
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
- 迁移文件位于 `internal/repository/migration/`，按版本顺序应用；工具会打印已应用数量。
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
5. 启动服务，确认 `/readyz` 返回 `status: ready` 且 `storage` 检查通过。
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
| `GET /metrics` | Bearer token | Prometheus 文本格式 |

`/readyz` 当前包含：

- `storage`：数据库 `Ping` 是否成功（3 秒超时）。
- `outbox_backlog`：下行 outbox 过期（超过 `expireAfter` 仍未投递）数量是否达到阈值（默认 100）；`commandOutbox.mode: off` 时不检查。
- `media_disk`：媒体目录存在且可写，仅在 `mediaServer` 启用时检查。
- `attachment_disk`：附件目录存在且可写，仅在 `alarmServer` 启用时检查。

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

采集示例：

```bash
curl -fsS -H "Authorization: Bearer $ADMIN_TOKEN" http://127.0.0.1:9000/metrics
```

### 6.3 告警建议

- `/readyz` 连续 3 次失败 → 页面告警。
- `jt808_outbox_expired > 0` 持续 10 分钟 → 检查终端在线率与 `commandOutbox` 配置。
- `jt808_ack_timeouts_total` 快速增长 → 检查网络质量、`ackTimeoutMs` 与终端重传配置。
- `jt808_attachments_active` 长期不下降 → 检查附件端口连通性与磁盘空间。

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
| `/readyz` | `ready`，`storage` 与 `outbox_backlog` 均通过 |
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
| `readyz` 中 `storage` 失败 | 检查库文件权限/DSN、连接池与磁盘空间 |
| `outbox_backlog` 失败 | 查看 `jt808_outbox_expired`，确认终端在线率与 `expireAfter` |
| 端口占用 | 检查 `server.port`、`alarmServer.port/udpPort`、`mediaServer.port` 是否冲突 |
| 业务 API 全部 401 | 检查 `api.auth.token` 与请求头 `Authorization: Bearer <token>` |
| 业务 API 429 | 调整 `api.rateLimit.ratePerSecond` / `burst`，或按来源 IP 排查异常调用 |
| 附件目录持续增长 | 检查 `alarmServer.retentionDays`，确认清理任务未被关闭 |
| 终端持续重传定位 | 检查该类别是否配置为 `at_least_once`，以及数据库写入是否变慢 |
