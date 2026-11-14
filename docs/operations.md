# 运维手册

本文覆盖部署后的日常运维：探针与指标、保留期清理、备份恢复、回滚、集群注意事项、压测基线与常见问题。部署步骤见[安装与部署](installation.md)，配置项见[配置说明](configuration.md)。

## 1. 探针与健康检查

每个角色二进制通过 `probes.addr` 暴露独立探针，只提供两个接口，不做鉴权：

| 接口 | 语义 |
|---|---|
| `GET /healthz` | 进程存活，恒返回 `200 {"status":"ok"}` |
| `GET /readyz` | 逐项执行真实依赖检查；任一失败返回 `503` 与 `status: not ready` |

`admin.enable=true` 时管理面也提供 `/healthz`、`/readyz`，并额外提供 `/metrics` 等接口。管理面与探针端口不能相同。

`/readyz` 检查项按角色装配：

| 检查项 | 适用角色 | 说明 |
|---|---|---|
| `database` | control | 数据库 Ping（3 秒超时） |
| `outbox_backlog` | control | 过期 pending 命令数是否低于阈值（默认 100）；`mode=off` 时不检查 |
| `control_endpoint` | gateway | `uplink.http.url` 的 host 可解析 |
| `media_nodes` | control（无本地媒体） | 注册表中至少一个节点在线、未过期且仍有容量；注册表为空时退回静态清单 |
| `attachment_nodes` | control | `cluster.attachment.nodes` 至少一个可达且仍有并发额度 |
| `attachment_object_store` | attachment | 已接入共享对象存储与共享附件元数据 |
| `media_disk` / `attachment_disk` / `media_upload_disk` | 对应角色 | 目录存在且可写 |

集群本地联调开启 `cluster.allowEmbeddedStorage` 时，`/readyz` 返回 `degraded`（HTTP 200，但 `status` 不是 `ready`）。生产禁止开启。

## 2. 指标

`/metrics` 为 Prometheus 文本格式，管理面 Bearer token 鉴权：

```sh
curl -fsS -H "Authorization: Bearer $ADMIN_TOKEN" http://127.0.0.1:9000/metrics
```

核心指标：

| 指标 | 含义 |
|---|---|
| `jt808_sessions_connected` | 当前活跃终端会话数（TCP + UDP） |
| `jt808_ack_pending` | 当前等待终端应答的在途项 |
| `jt808_ack_timeouts_total` | 重传耗尽仍未收到应答的累计次数 |
| `jt808_outbox_pending` / `jt808_outbox_expired` | outbox 待投递与过期命令数 |
| `jt808_media_streams` | 进行中的 1078 媒体会话数 |
| `jt808_attachments_active` / `jt808_attachments_completed_total` | 进行中与已完成附件数 |
| `jt808_media_files` / `jt808_media_file_bytes` | `0x0801` 落盘文件数量与总字节数 |
| `jt808_retention_deleted_total{category}` / `jt808_retention_failures_total{category}` | 定期清理累计删除行数与失败批次数 |
| `jt808_retention_last_run_timestamp_seconds` / `last_run_duration_seconds` | 最近一次清理时间与耗时 |
| `jt808_orphan_files_scanned_total` / `deleted_total` / `skipped_total` | 孤儿文件扫描计数 |
| `jt808_orphan_scan_failures_total` | 孤儿文件扫描失败次数 |
| `jt808_gateway_nodes_online` / `offline` / `expired` | 网关节点状态 |
| `jt808_downlink_directory_hit_total` / `jt808_downlink_offline_total` | `directory` 模式命中与离线次数 |
| `jt808_uplink_fenced_total` / `jt808_gateway_node_fenced_total` | 旧 generation 上行与旧实例心跳被拒绝次数 |
| `jt808_clock_skew_ms` / `jt808_clock_skew_alerts_total` | 进程时钟与数据库时间偏差及超阈值告警 |
| `jt808_ftp1078_done_anomaly_total` / `jt808_ftp1078_control_push_failed_total` | FTP 完成清单异常与 0x9207 推送失败次数 |
| `jt808_attachment_trigger_skipped_total` | 当前进程无 control 角色时跳过自动 `0x9208` 的次数 |

建议告警：

- `/readyz` 连续 3 次失败；
- `jt808_outbox_expired > 0` 持续 10 分钟；
- `jt808_ack_timeouts_total` 快速增长；
- `jt808_attachments_active` 长期不下降；
- `jt808_gateway_nodes_expired > 0` 持续存在；
- `jt808_downlink_offline_total` 快速增长；
- `jt808_gateway_node_fenced_total` 持续增长（同一 `gatewayId` 多实例竞争）；
- `jt808_clock_skew_alerts_total` 增长或 `jt808_clock_skew_ms` 绝对值持续超过 2000ms。

## 3. 保留期与清理

| 数据 | 配置 | 默认 | 生效方式 |
|---|---|---|---|
| 定位历史 | `retention.locations.retentionDays` / `maxPerDevice` | 7 天 / 10000 条 | 写入时裁剪；开启定期清理后全局删除 |
| 报警历史 | `retention.alarms.dedupWindowMs` / `confirmExpireMs` / `maxPerDevice` | 30 分钟 / 10 分钟 / 1000 条 | 写入时去重与裁剪 |
| 报警历史 | `retention.alarms.retentionDays` | 0（关闭） | 定期清理按接收时间删除 |
| `0x0801` 媒体 | `retention.mediaFiles.retentionDays` | 0（关闭） | 先删文件，再删元数据 |
| `0x0805` 拍摄记录 | `retention.mediaCaptures.retentionDays` | 0（关闭） | 清理主表与明细 |
| `0x0802` 检索应答 | `retention.mediaSearchResults.retentionDays` | 0（关闭） | 按自身接收时间清理 |
| 媒体目录孤儿文件 | `retention.orphanFiles.*` | 关闭（宽限 7 天） | 按文件 mtime 与库记录比对；跳过 `.part` 与隐藏文件 |
| 媒体裸流文件 | `mediaServer.retentionDays` | 7 天 | 每小时扫描目录，跳过正在写入的流 |
| 苏标附件 | `alarmServer.retentionDays` | 7 天 | 每小时扫描，跳过未完成组装的临时文件 |

- `retention.cleanup.enabled=true` 才会执行历史表定期清理；按主键分批，批间让出。
- 清理仅支持 SQL 后端；`memory` 后端只记录告警并跳过。
- 显式 `0` 表示关闭；负数、超过 36500 天、非法 `interval` 或越界 `batchSize` 会导致启动失败。
- `objectStore` 模式下不要开启 `retention.orphanFiles`，应使用桶生命周期规则。

## 4. 备份与恢复

需要备份三类数据：数据库、`data/` 下的媒体/附件/对象文件、配置与密钥。

```sh
# SQLite 在线一致性备份
sqlite3 ./data/jt808.db ".backup '/backup/jt808-$(date +%Y%m%d-%H%M).db'"

# MySQL
mysqldump --single-transaction --routines --triggers \
  -h <host> -u <user> -p <database> > /backup/jt808-$(date +%Y%m%d-%H%M).sql

# 文件目录
tar -czf /backup/jt808-files-$(date +%Y%m%d-%H%M).tgz ./data ./logs
```

恢复步骤：

1. 停止服务或进入维护窗口；
2. 恢复数据库备份；
3. 执行 `jt808-migrate` 把 schema 升到当前二进制要求的版本；
4. 恢复 `data/`，保持属主与权限；
5. 启动服务，确认 `/readyz` 为 `ready`；
6. 抽查最近定位/报警查询、媒体下载与附件下载。

使用 `objectStore` 时，数据库与对象存储需要保持同一备份时间点；只恢复数据库会导致索引指向不存在或旧版本的对象。

## 5. 回滚

- 保留上一版本二进制或镜像 tag，回滚时替换并重启。
- 数据库迁移默认只向前；包含破坏性变更时不能只回退二进制，必须恢复到升级前的数据库备份。
- 回滚后先执行 `jt808-migrate` 校验版本；版本不符时服务拒绝启动，属预期行为。

配置级回滚（无需回退代码）：

| 场景 | 配置动作 |
|---|---|
| outbox 异常，需要临时直连下发 | `commandOutbox.mode: "off"` |
| at-least-once 导致终端频繁重传 | 对应 `uplink.reliability` 改回 `best_effort` |
| 附件/媒体清理误删风险 | 对应 `retentionDays: 0` |
| 历史裁剪影响排查 | `retention.locations.retentionDays: 0`、`maxPerDevice: 0` |
| API 鉴权挡住联调 | 临时清空 `api.auth.token`（仅内网，尽快恢复） |

配置修改后重启生效；修改前先备份当前配置。

## 6. 日志与审计

- 日志通过 `log.*` 配置控制台/文件输出、级别、滚动大小、保留份数与天数。
- 业务 API 的变更类请求（POST/PUT/PATCH/DELETE）、鉴权失败与限流拒绝写入审计日志，只记录方法、路径、来源 IP、状态码、手机号与耗时，不记录请求体与 token。
- 网关鉴权降级会打印 WARN：`gateway serves cached device profile within the configured grace period`；control 恢复后会自动回到正常路径。
- 集群节点被顶替、上行被 fenced、时钟偏差超阈值等都会打 WARN 并累加指标。

## 7. 集群运维要点

1. **时间同步**：所有主机启用 NTP/chrony，`chronyc tracking` 的 `System time` 偏差应在 100ms 内。租约接管使用 `expires_at + cluster.leaseTolerance < now`，默认容差 2 秒。
2. **节点判死**：网关与媒体节点 10 秒心跳，30 秒未续期判死。节点过期后其会话不可路由，下行返回 `offline` 并由 outbox 重试。
3. **同一 `gatewayId` 唯一**：同 ID 多进程会互相顶替并清理旧目录，导致归属抖动。部署侧必须保证唯一性。
4. **共享存储一致性**：所有角色指向同一 `root` 或同一对象存储桶；`sharedVolume` 必须创建 `.jt808-shared-volume` 标记文件。
5. **附件暂存**：`alarmServer.stagingMaxBytes` 到顶后拒绝新文件登记；崩溃残留按 `.part` 与宽限期清理。
6. **FTP 端口**：放行控制端口 7500 与被动端口段 7501–7510；NAT 场景必须配置 `ftp1078.advertiseIp`。
7. **对象存储清理**：`objectStore` 下按前缀使用桶生命周期；不要用本地孤儿扫描。
8. **关停顺序**：服务收到 `SIGINT/SIGTERM` 后按业务 API → 终端接入与会话 → UDP → outbox 当前轮次 → 媒体/附件落盘 → 管理面 → 探针 → 存储的顺序优雅退出。编排系统预留至少 10 秒。

## 8. 压测基线

脚本：`scripts/loadtest.sh`，构建真实 server 与模拟终端二进制。

```sh
bash scripts/loadtest.sh 60
# 可用 LOAD_CONCURRENCY / LOAD_TCP_PORT / LOAD_HTTP_PORT / LOAD_ADMIN_PORT 调整
```

开发机基线（`memory` 后端，200 并发终端，每终端每秒 1 条 `0x0200`，持续 60 秒）：

| 指标 | 结果 |
|---|---|
| 活跃会话 | 200 |
| `jt808_ack_pending` | 0 |
| `jt808_ack_timeouts_total` | 0 |
| outbox pending / expired | 0 / 0 |
| 服务端 RSS | 约 33 MB |
| 服务端 CPU | 约 1.3%（单次采样） |
| `/readyz` | `ready` |

该基线只覆盖协议接入与内存处理路径，不代表 SQLite/MySQL 写入、媒体 RTP、附件上传或命令风暴的容量。上述场景需要单独压测。

## 9. 常见问题

| 现象 | 排查方向 |
|---|---|
| 启动失败，提示 schema 版本不符 | 先执行 `jt808-migrate` |
| 启动失败，提示旧 8 位版本（如 `20261118`） | dev 库早于语义化基线，重建库后重新迁移；不支持自动升级 |
| 启动失败，提示角色与二进制不匹配 | 使用错误提示中建议的二进制，或修正 `cluster.roles` |
| 启动失败，提示共享卷标记缺失 | 创建 `cluster.storage.root` 与 `.jt808-shared-volume` |
| `readyz` 的 `database` 失败 | 检查库文件权限/DSN、连接池与磁盘空间 |
| `outbox_backlog` 失败 | 查看 `jt808_outbox_expired`，确认终端在线率与 `expireAfter` |
| 业务 API 全部 401 | 检查 `api.auth.token` 与 `Authorization: Bearer <token>` |
| 业务 API 429 | 调整 `api.rateLimit`，或按来源 IP 排查异常调用 |
| 下行一直 `offline` | 检查 `session_directory`、网关节点在线状态与 `cluster.gateway.routing` |
| 播放返回 503 | 检查媒体节点注册、容量、`cluster.media.nodes` 与节点 HTTP 连通性 |
| 附件上传返回 503 | 检查 `cluster.attachment.nodes`、节点状态与并发额度 |
| FTP 登录 421 | 附件节点到 control 的网络不通；检查 `progressReport` 与 token |
| 附件目录持续增长 | 检查 `alarmServer.retentionDays` 与清理任务 |
| 终端持续重传定位 | 检查该类别是否为 `at_least_once`，以及数据库写入是否变慢 |
| 集群时间偏差告警 | 立即检查 NTP/chrony；时钟失步会让租约与节点判死同时失真 |
