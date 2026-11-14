# 附件服务与统一对象存储设计（E3b 前置）

本文件锁定 `jt-attachment` 独立二进制与“全集群共用一套 objectStore”的实现决策，
以及苏标附件（0x1210/0x1211/0x1212）与 1078 FTP 录像（0x9206/0x9207/0x1206）两条接入链路。

## 1. 角色与二进制

| 二进制 | 固定角色 | 依赖 |
|---|---|---|
| `jt-server-standalone` | `mode=standalone` | 本地目录，全角色同进程 |
| `jt-signal` | `gateway` | MySQL（设备档案/命令查询） |
| `jt-control` | `control` | MySQL、objectStore、附件节点、媒体节点 |
| `jt-media` | `media` | objectStore |
| `jt-attachment` | `attachment` | objectStore（不连 MySQL） |

- 附件不再并入 control；`jt-control` 的固定角色收紧为仅 `control`。
- `jt-server-standalone` 保持全量，附件仍在本进程内（本地目录模式）。
- 每个二进制必须配置 `probes.addr`，提供 `/healthz` 与 `/readyz`。

**为什么附件节点不连业务库**：附件节点可能下沉到边缘、离终端更近，而 control 在中心，
两者不在同一内网。让附件节点直连中心库意味着把数据库端口对边缘开放，现场无法通过网络审批。
因此元数据面集中在 control，附件节点只出站访问对象存储与 control。

## 2. 存储后端

### 2.1 集群单一 objectStore

- 集群只允许一套 objectStore 配置，media / attachment / control 共用；不允许每个角色或节点各配一套。
- `sharedVolume` 仅用于单机或迁移过渡：`mode=cluster` 且 backend=sharedVolume 时启动打印 WARN，明确提示仅限单机/过渡。

### 2.2 key 前缀按来源分区

| 来源 | 前缀 | 业务表 |
|---|---|---|
| 媒体切片 | `media/segments/` | `recording_segments` |
| 苏标附件 | `attachment/subia/` | `attachment_files` |
| 1078 FTP 录像 | `attachment/ftp1078/` | `attachment_files`（`source=ftp1078`）+ `attachment_ftp_tasks` |

前缀定义集中在 `internal/storage`（对象存储包），禁止各角色自行拼字符串。

### 2.3 pending 回收按来源隔离

`SweepPending` 增加前缀参数，回收范围限定在该来源的前缀内：

- control 侧只扫描 `_pending/media/segments/`，业务行联查 `recording_segments`；
- control 侧只扫描 `_pending/attachment/`，业务行联查 `attachment_files` 的段列表。

理由：attachment 的 pending 标记与 media 共用 `_pending/` 根前缀；若沿用“全局扫描 + 只查
`media_files`”，已提交的附件会被误判为孤儿。前缀隔离同时满足按来源做生命周期规则。

### 2.4 保留期按来源执行

- media 的 retention / orphan scan 只看 `media/segments/` 前缀与 `recording_segments` 表。
- attachment 的 retention / pending / 过期标记只看 `attachment/` 前缀与 `attachment_files` 表，
  **统一在 control 执行**；附件节点不连库，无法判定对象是否仍被业务行引用。
- 一个来源的文件只由一处清理。

## 3. 附件接入与存储解耦

接入层（苏标附件协议，未来 FTP）与存储后端解耦：收到数据即写本地暂存区，
按阈值切片上传对象存储；接入层不保存业务状态，状态由 control 落库。

**定位：对象存储是切片的事实来源，`attachment_files` 是可重建的索引。**

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

- 每个暂存文件写本地 `.part` 标记，并随进度刷新 `.part.state.json`。
- 启动时只按本地规则对账：无活跃会话且超过宽限期的 `.part` → 直接删除；
  **不依赖数据库记录**，因为附件节点可能部署在边缘、对中心库不可见。
- 暂存区总量上限 `staging.maxBytes`：达到上限后**拒绝新连接**（返回明确错误并拒绝文件登记），
  不排队、不写满磁盘；已有连接继续完成。

### 3.4 元数据、恢复点与续传

`attachment_files` 由 control 维护，至少包含：

- `source`（`subia` / `ftp1078`）、`phone`、`terminal_id`
- `alarm_sign`、`alarm_no`、`alarm_serial`、`file_name`、`file_type`
- `total_size`、`received_bytes`、`watermark`
- `staging_path`、`staging_base`（仅排查用，终态清空）
- `segments`（JSON：序号、对象 key、偏移、大小、状态）
- `status`（`receiving` / `completed` / `failed` / `expired`）
- `created_at`、`updated_at`、`completed_at`

**对象 key 自带元数据（硬要求）**：

```text
苏标附件：attachment/subia/<phone>/<alarmNo>/<fileName>/<offset>_<size>_<total>_<index>
FTP 切片：attachment/ftp1078/<phone>/<serial>/<fileName>/<offset>_<size>_<index>
FTP 清单：attachment/ftp1078/<phone>/<serial>/<fileName>.done
```

终端、报警号（FTP 为任务流水号）、文件名、偏移、段大小与文件总长全部编码在 key 里。
control 只扫描前缀就能重建“覆盖到哪、是否完整”的进度，不依赖任何上报内容；
无法解析的 key 跳过并告警。

FTP 上传在连接结束前不知道文件总长，因此切片 key 不写 total，完整性由 `.done` 清单给出：

- `.done` **只在所有切片 Promote（并 MarkCommitted）完成后才写**；内容是
  `{fileName,totalSize,segmentSize,segments,completedAt}`。
- 扫描时 `.done` 存在但切片不连续或缺片 → **不判完成**，保持 `receiving`，
  并计入 `jt808_ftp1078_done_anomaly_total`。
- 保留期清理删除附件记录时，`.done` 与切片一并删除；同名重传或续传改写尾部前先删旧清单。

**恢复点以 control 为权威**：

1. 附件节点断线重连或重启后，先 `GET /internal/v1/attachment/progress` 取回水位与段列表；
2. control 不可达时**不阻断接收**：以本地 `.part.state.json` 的暂存区间 +
   `attachment/` 前缀扫描到的已 `Promote` 段继续工作，进度事后由出站上报补齐；
3. 总长与恢复点不一致时按新文件重收，避免把旧文件的切片拼进新文件。

进度上报走 `POST /internal/v1/attachment/progress`（Bearer token、幂等 upsert）。
上报允许丢失，control 周期性扫描 `attachment/` 前缀补齐索引。

## 4. 角色协作

### 4.1 附件节点

- 自己监听 TCP + UDP，自行解析 0x7e 转义、校验与消息头（复用 alarmserver 的接入实现）。
- 自己写入 objectStore（集群）或本地目录（standalone）。
- **不装配任何 repository，不连数据库**：水位、段列表、终态、过期与保留期都由 control 负责。
- 内部 HTTP：
  - `GET /internal/attachment/status`：本进程活跃文件数、并发上限、剩余额度、TCP/UDP 端口、通告 IP。
  - 活跃数只统计本进程会话，避免多节点部署时互相扣减额度。
- 出站 HTTP：
  - `POST /internal/v1/attachment/progress` 上报进度快照；
  - `GET /internal/v1/attachment/progress` 查询恢复点。
- 不依赖 control 进程内对象，也不要求 control 反向连接附件节点。

### 4.2 控制面

- `cluster.attachment.nodes` 是可用附件节点清单；控制面按状态选点。
- 手工 `0x9208`：API 从节点状态取得 IP + TCP/UDP 端口；无可用节点返回
  `503 {code: no_attachment_node_configured | no_attachment_node_available, message}`。
- 自动触发：报警识别在 control；控制面收到带报警位的 0x0200 后，按报警 ID 去重，
  选附件节点并**经 downlink 下发 0x9208**。不再依赖 gateway 与 control 同进程。
- 附件进度与完成元数据由 control 落库并查询；文件内容通过共享 objectStore 读取，
  与媒体走同一条 `BlobStore.OpenRange` 路径。
- control 侧周期任务：receiving 超时转 expired、`_pending/attachment/` 回收、
  按保留期删除对象与行、扫描 `attachment/` 前缀补齐缺失索引。

### 4.3 1078 FTP 上传链路

协议链路：`0x9206`（平台下发 FTP 参数）→ 终端 `0x1210/0x1211/0x30316364/0x1212` 走附件端口
上传 → 终端 FTP 上传 → `0x1206`（完成通知）→ 平台 `0x9212`。FTP 服务端在附件节点，
元数据与任务状态全部在 control；附件节点仍不连数据库。

**任务与凭据**

- `0x9206` 由 control 的 `POST /api/device/:phone/ftp1078/upload` 下发。任务主键是
  `(phone, 报文头流水号)`，与 `0x1206/0x9207` 的应答流水号对应；落 `attachment_ftp_tasks`。
- 账号是任务级一次性凭据：用户名 `ftp1078_<phone>_<serial>`，密码随机生成，
  库里只存 sha256。`0x1206` 完成/失败、取消或超时后 `revoked_at` 置位，不可再次登录。
- 附件节点收到 `USER/PASS` 后**必须出站调用 control**（`POST /internal/v1/attachment/ftp/login`）
  校验；control 不可达时拒绝新登录（`421`），已建立连接继续读写，不做本地凭据自校验。

**FTP 行为**

- 只支持被动模式：`PASV/EPSV`，数据端口在配置段内轮转（默认 `7501-7510`）；
  `PORT/EPRT` 返回 `502`，禁止服务端反向连终端。
- 控制端口默认 `7500`；`PASV` 通告地址由 `ftp1078.advertiseIp` 指定，缺省取控制连接本地地址。
- 虚拟根按任务隔离：`CWD/CDUP` 一律停留在任务根并返回成功，越权路径被忽略；
  落库只取文件 basename，不解析厂商命名。
- `STOR` 同名重传整文件覆盖（先清旧切片与 `.done`）；`APPE`/`REST` 续传映射到现有暂存偏移，
  偏移对不上时从头覆盖。上传内容按不透明字节存储，不转封装、不做播放。
- 切片达到 `segmentSize` 立即 `Put → Promote`；连接正常结束后提交尾部、`MarkCommitted`、
  最后写 `.done` 清单，再上报 `completed`。连接中断则保留暂存与已提交切片，上报当前水位，
  后续 `APPE/REST` 从连续前缀继续。

**0x9207 控制**

- control 收到 `POST /api/device/:phone/ftp1078/control` 后：向终端下发 `0x9207`，
  同时推送到任务绑定的附件节点 `POST /internal/attachment/ftp/control`。
- 推送失败按有限重试处理并计入 `jt808_ftp1078_control_push_failed_total`，不回滚已下发的终端指令。
- 节点按单调递增的 `seq` 去重，重复推送不会让同一次暂停/继续/取消执行两次；
  `pause` 阻塞数据写入，`resume` 继续，`cancel` 关闭数据连接并清理暂存与对象。

**节点注册**

- 附件节点启动后向 control 注册 `nodeId + instanceId + baseUrl`，每 10s 心跳；
  `attachment_nodes` 与 `gateway_nodes` 同构：TTL 30s、过期判死、新 `instance_id` 覆盖旧记录，
  旧实例心跳返回 `409` 并重新注册。control 只向在线且未过期的地址推送 `0x9207`。

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
| 附件内部 HTTP | 7402 | attachment status（进度走 control 的 9102） |
| 1078 FTP 控制 | 7500 | attachment |
| 1078 FTP 被动数据 | 7501-7510 | attachment，可配端口段 |
| gateway 下行契约 | 9101 | signal |
| control 上行入口 | 9102 | control |
| admin | 9000 | control |
| probes | 9105-9109 | 每角色独立 |

同一主机部署全部角色时必须逐项改端口，运行手册给出完整样例。

## 6. 明确不做

- FTP 主动模式（PORT/EPRT）与 FTP 服务端反向连接终端。
- 对 FTP 上传内容做转封装、解析或播放。
- control 多副本选主、同 phone 顺序保证、`command_outbox` 保留期清理。
