# 附件服务与统一对象存储设计（E3b 前置）

本文件锁定 `jt-attachment` 独立二进制与“全集群共用一套 objectStore”的实现决策。
未实现的 FTP 服务端只预留结构与端口，不在本轮交付。

## 1. 角色与二进制

| 二进制 | 固定角色 | 依赖 |
|---|---|---|
| `jt-server-standalone` | `mode=standalone` | 本地目录，全角色同进程 |
| `jt-signal` | `gateway` | MySQL（设备档案/命令查询） |
| `jt-control` | `control` | MySQL、objectStore、附件节点、媒体节点 |
| `jt-media` | `media` | objectStore |
| `jt-attachment` | `attachment` | MySQL、objectStore |

- 附件不再并入 control；`jt-control` 的固定角色收紧为仅 `control`。
- `jt-server-standalone` 保持全量，附件仍在本进程内（本地目录模式）。
- 每个二进制必须配置 `probes.addr`，提供 `/healthz` 与 `/readyz`。

## 2. 存储后端

### 2.1 集群单一 objectStore

- 集群只允许一套 objectStore 配置，media / attachment / control 共用；不允许每个角色或节点各配一套。
- `sharedVolume` 仅用于单机或迁移过渡：`mode=cluster` 且 backend=sharedVolume 时启动打印 WARN，明确提示仅限单机/过渡。

### 2.2 key 前缀按来源分区

| 来源 | 前缀 | 业务表 |
|---|---|---|
| 媒体切片 | `media/segments/` | `media_files` |
| 苏标附件 | `attachment/subia/` | `attachment_files` |
| 1078 FTP 录像（预留） | `attachment/ftp1078/` | `attachment_files`（`source=ftp1078`） |

前缀定义集中在 `internal/storage`（对象存储包），禁止各角色自行拼字符串。

### 2.3 pending 回收按来源隔离

`SweepPending` 增加前缀参数，回收范围限定在该来源的前缀内：

- media 侧只扫描 `_pending/media/segments/`，业务行联查 `media_files`；
- attachment 侧只扫描 `_pending/attachment/`，业务行联查 `attachment_files` 的段列表。

理由：attachment 的 pending 标记与 media 共用 `_pending/` 根前缀；若沿用“全局扫描 + 只查
`media_files`”，已提交的附件会被误判为孤儿。前缀隔离同时满足按来源做生命周期规则。

### 2.4 保留期按来源执行

- media 的 retention / orphan scan 只看 `media/segments/` 前缀与 `media_files` 表。
- attachment 节点只看 `attachment/` 前缀与 `attachment_files` 表，并在自己的进程内执行；
  control 的 retention 不再触碰附件。
- 一个来源的文件只由一处清理。

## 3. 附件接入与存储解耦

接入层（苏标附件协议，未来 FTP）与存储后端解耦：收到数据即写本地暂存区，
按阈值切片上传对象存储；接入层不保存业务状态，状态在 MySQL。

### 3.1 流式与本地暂存

- 单文件按 `segmentSize`（默认 8 MiB，可配）切片；只保留**当前切片窗口**的本地暂存文件，
  窗口内数据到齐后立即 `Put -> Promote` 成该切片对象，然后推进窗口。
- 本地暂存文件是稀疏文件，带 `.part` 标记；不在内存里攒整个文件。
- 任意偏移的数据都写入暂存文件并计入覆盖区间；乱序部分留在本地，等当前窗口补齐后一并提交，
  缺口由后续 `0x9212` 补齐。暂存区总量有上限，超出后拒绝新的文件登记。

### 3.2 补传策略

苏标 `0x9212` 的补传偏移由平台按缺口生成，允许任意字节粒度。因此**不采用 S3 part 对齐**
（任意偏移会导致整 part 重传放大）。采用“本地暂存到阈值再整体上传”：

- 缺口在当前窗口内：按实际缺口回报，终端补传后窗口补齐即上传。
- 缺口跨越已上传窗口：按当前窗口边界回报，已上传对象不再修改。
- 一个逻辑文件 = 多个对象切片；元数据记录切片列表，读取按切片顺序拼接。
- 每个切片在写入对象存储后先保持 pending，整个文件全部切片提交完成后统一 `MarkCommitted`，
  业务查询只返回 `completed` 的文件。

### 3.3 暂存区崩溃残留与容量

- 每个暂存文件配一条 DB 记录（`attachment_files`），并写本地 `.part` 标记。
- 启动时对账：DB 记录超过宽限期且无活跃连接 → 删除暂存文件与记录；无 DB 记录的 `.part`
  → 超过宽限期直接删除。
- 暂存区总量上限 `staging.maxBytes`：达到上限后**拒绝新连接**（返回明确错误并拒绝文件登记），
  不排队、不写满磁盘；已有连接继续完成。

### 3.4 元数据与续传

`attachment_files` 至少包含：

- `source`（`subia` / `ftp1078`）、`phone`、`terminal_id`
- `alarm_sign`、`alarm_no`、`alarm_serial`、`file_name`、`file_type`
- `total_size`、`received_bytes`、`watermark`
- `staging_path`、`staging_base`
- `segments`（JSON：序号、对象 key、大小、状态）
- `status`（`receiving` / `completed` / `failed` / `expired`）
- `created_at`、`updated_at`、`completed_at`

断线重连或进程重启后按 `phone + alarm_no + file_name` 恢复水位与切片列表。

## 4. 角色协作

### 4.1 附件节点

- 自己监听 TCP + UDP，自行解析 0x7e 转义、校验与消息头（复用 alarmserver 的接入实现）。
- 自己写入 objectStore（集群）或本地目录（standalone），自己执行保留期清理。
- 内部 HTTP：
  - `GET /internal/attachment/status`：本进程活跃文件数、并发上限、剩余额度、TCP/UDP 端口、通告 IP。
  - `GET /internal/attachment/files`：按手机号/报警号/文件名查询共享库里的进度。
  - 活跃数只统计本进程会话，避免多节点共用一张表时互相扣减额度。
- 不依赖 control 进程内对象。

### 4.2 控制面

- `cluster.attachment.nodes` 是可用附件节点清单；控制面按状态选点。
- 手工 `0x9208`：API 从节点状态取得 IP + TCP/UDP 端口；无可用节点返回
  `503 {code: no_attachment_node_configured | no_attachment_node_available, message}`。
- 自动触发：报警识别在 control；控制面收到带报警位的 0x0200 后，按报警 ID 去重，
  选附件节点并**经 downlink 下发 0x9208**。不再依赖 gateway 与 control 同进程。
- 附件进度与完成元数据直接查共享库；文件内容通过共享 objectStore 读取，
  与媒体走同一条 `BlobStore.OpenRange` 路径。

## 5. 端口规划

| 用途 | 默认端口 | 说明 |
|---|---|---|
| 808 主连接 TCP | 8080 | gateway |
| 808 主连接 UDP | 8081 | gateway |
| 业务 HTTP API | 8008 | control |
| 1078 媒体 TCP/UDP | 7300 | media（单端口模式） |
| 1078 媒体 HTTP | 7301 | media 内部 HTTP + FLV |
| 附件 TCP | 7400 | attachment |
| 附件 UDP | 7401 | attachment |
| 附件内部 HTTP | 7402 | attachment status/files |
| 未来 1078 FTP 控制 | 7500 | 预留，不实现 |
| 未来 1078 FTP 数据 | 7501 | 预留，不实现 |
| gateway 下行契约 | 9101 | signal |
| control 上行入口 | 9102 | control |
| admin | 9000 | control |
| probes | 9105-9109 | 每角色独立 |

同一主机部署全部角色时必须逐项改端口，运行手册给出完整样例。

## 6. 明确不做

- FTP 服务端（只预留表结构、前缀、端口）。
- control 多副本选主、同 phone 顺序保证、`command_outbox` 保留期清理。
