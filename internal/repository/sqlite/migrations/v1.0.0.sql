-- v1.0.0 baseline: schema_migrations + all business tables.

-- 迁移元数据表：与业务表一起合并在 v1.0.0 基线内，不再单独维护 bootstrap 文件。
-- version 存语义化版本字符串（如 1.0.0），排序由迁移器按三段数值比较，不依赖 SQL 排序。
CREATE TABLE IF NOT EXISTS schema_migrations (
    version TEXT NOT NULL PRIMARY KEY,
    applied_at INTEGER NOT NULL
);

-- v1.0.0 是开发阶段的唯一基线，包含此前全部增量变更的最终形态。
-- 后续变更以 v<MAJOR>.<MINOR>.<PATCH>.sql 增加新文件，版本号同步推进。
-- 文件开头的日期即版本号，迁移按日期升序执行；同一天只允许一个迁移文件。
--
-- 方言约束：本文件只供 SQLite 执行。
--   * 主键显式写 NOT NULL。
--   * 不使用 BLOB 的 DEFAULT。
--   * 不写 ENGINE/CHARSET/COLLATE 等 MySQL 专属子句。
--   * 索引在版本化迁移内执行，无需 IF NOT EXISTS 兜底。

-- 设备静态主数据。运行时字段（session/在线态/最后通信时间）由进程内 runtime 持有，不落库。
CREATE TABLE IF NOT EXISTS devices (
    phone TEXT NOT NULL PRIMARY KEY,
    id TEXT NOT NULL DEFAULT '',
    plate TEXT NOT NULL DEFAULT '',
    keepalive_ms INTEGER NOT NULL DEFAULT 0,
    version_desc INTEGER NOT NULL DEFAULT 0,
    protocol_version INTEGER NOT NULL DEFAULT 0,
    auth_code TEXT NOT NULL DEFAULT '',
    imei TEXT NOT NULL DEFAULT '',
    software_version TEXT NOT NULL DEFAULT '',
    updated_at INTEGER NOT NULL
);
-- 车牌普通索引（非唯一）：设备身份以 phone 为准，plate 允许重复，仅用于加速 GetByPlate。
CREATE INDEX idx_devices_plate ON devices (plate);

-- 终端参数按设计决策 D1 使用整体 JSON 单字段。
CREATE TABLE IF NOT EXISTS device_params (
    phone TEXT NOT NULL PRIMARY KEY,
    param_cnt INTEGER NOT NULL DEFAULT 0,
    params_json TEXT NOT NULL,
    updated_at INTEGER NOT NULL
);

-- 终端属性档案：只承载 0x0107 查询应答，与 devices 独立存储。
CREATE TABLE IF NOT EXISTS terminal_properties (
    phone TEXT NOT NULL PRIMARY KEY,
    terminal_type INTEGER NOT NULL DEFAULT 0,
    manufacturer_id TEXT NOT NULL DEFAULT '',
    terminal_model TEXT NOT NULL DEFAULT '',
    terminal_id TEXT NOT NULL DEFAULT '',
    iccid TEXT NOT NULL DEFAULT '',
    hardware_version TEXT NOT NULL DEFAULT '',
    firmware_version TEXT NOT NULL DEFAULT '',
    gnss_properties INTEGER NOT NULL DEFAULT 0,
    communication_properties INTEGER NOT NULL DEFAULT 0,
    extra_json TEXT NOT NULL,
    updated_at INTEGER NOT NULL
);

-- 持久化数据表（2026-11-02）。
-- 批次 A：终端下行配置目录（terminal_catalogs）与区域/路线（region_items）。
-- 批次 B：定位历史（locations）与报警（alarms）；本文件一次性建表，代码分批接入。
--
-- 方言约束同 20261101_init.sql：本文件只供 SQLite 执行。
-- payload_json 使用 TEXT。

-- 终端下行配置整包快照。
-- catalog_type: event(0x8301) / info_menu(0x8303) / phonebook(0x8401)
--               / upgrade(0x8108) / transparent_down(0x8900)
-- item_key：事件 ID、菜单 ID、电话号码；单条快照固定为 latest。
CREATE TABLE IF NOT EXISTS terminal_catalogs (
    phone TEXT NOT NULL,
    catalog_type TEXT NOT NULL,
    item_key TEXT NOT NULL,
    payload_json TEXT NOT NULL,
    updated_at INTEGER NOT NULL,
    PRIMARY KEY (phone, catalog_type, item_key)
);
CREATE INDEX idx_terminal_catalogs_phone ON terminal_catalogs (phone, catalog_type);

-- 区域/路线目录。region_type: 1 圆形 / 2 矩形 / 3 多边形 / 4 路线。
-- “清空后重设”由应用层在同一事务内完成 DELETE + INSERT。
CREATE TABLE IF NOT EXISTS region_items (
    phone TEXT NOT NULL,
    region_type INTEGER NOT NULL,
    item_id INTEGER NOT NULL,
    payload_json TEXT NOT NULL,
    updated_at INTEGER NOT NULL,
    PRIMARY KEY (phone, region_type, item_id)
);
CREATE INDEX idx_region_items_phone ON region_items (phone, region_type);

-- 定位历史。record_id 由应用层生成（纳秒时间戳 + 进程内递增），
-- 避免同一毫秒内多条 0x0704 数据项共用 (phone, received_at_ms) 造成主键冲突。
-- dedup_key = hash(eventId + itemIndex)，老数据与 standalone 写入保持 NULL；
-- 唯一索引允许多个 NULL，因此不影响既有数据。
CREATE TABLE IF NOT EXISTS locations (
    phone TEXT NOT NULL,
    record_id INTEGER NOT NULL,
    msg_id INTEGER NOT NULL,
    report_time_ms INTEGER NOT NULL,
    received_at_ms INTEGER NOT NULL,
    alarm_sign INTEGER NOT NULL DEFAULT 0,
    status_sign INTEGER NOT NULL DEFAULT 0,
    latitude INTEGER NOT NULL DEFAULT 0,
    longitude INTEGER NOT NULL DEFAULT 0,
    altitude INTEGER NOT NULL DEFAULT 0,
    speed INTEGER NOT NULL DEFAULT 0,
    direction INTEGER NOT NULL DEFAULT 0,
    payload_json TEXT NOT NULL,
    dedup_key TEXT NULL,
    PRIMARY KEY (phone, record_id)
);
CREATE INDEX idx_locations_phone_report_time ON locations (phone, report_time_ms);
CREATE INDEX idx_locations_report_time ON locations (report_time_ms);
CREATE INDEX idx_locations_received ON locations (received_at_ms);
CREATE UNIQUE INDEX uq_locations_phone_dedup_key ON locations (phone, dedup_key);

-- 报警历史。同一 (phone, message_serial) 在去重窗口内更新 alarm_sign，
-- 窗口外以新的 received_at_ms 追加；confirmed 由 0x8203 人工确认置位。
CREATE TABLE IF NOT EXISTS alarms (
    phone TEXT NOT NULL,
    message_serial INTEGER NOT NULL,
    received_at_ms INTEGER NOT NULL,
    occurred_at_ms INTEGER NOT NULL DEFAULT 0,
    alarm_sign INTEGER NOT NULL DEFAULT 0,
    latitude INTEGER NOT NULL DEFAULT 0,
    longitude INTEGER NOT NULL DEFAULT 0,
    confirmed INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (phone, message_serial, received_at_ms)
);
CREATE INDEX idx_alarms_phone_received ON alarms (phone, received_at_ms);
CREATE INDEX idx_alarms_received ON alarms (received_at_ms);

-- 1078 媒体元数据表（2026-11-08）。
--
-- 落库范围：
--   * media_resource_lists：终端上行的音视频资源列表（0x1205）快照，按设备整体覆盖；
--   * media_upload_tasks：平台下发的文件上传指令（0x9206）元数据，按 (phone, 流水号) 保存；
--   * media_upload_notices：终端上行的文件上传完成通知（0x1206）结果，按 (phone, 流水号) 保存。
--
-- 方言约束同既有迁移文件：只使用 SQLite 语法；
-- 不使用 BLOB 默认值，不写 MySQL 专属类型与子句。

CREATE TABLE IF NOT EXISTS media_resource_lists (
    phone TEXT NOT NULL PRIMARY KEY,
    media_count INTEGER NOT NULL DEFAULT 0,
    items_json TEXT NOT NULL,
    updated_at INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS media_upload_tasks (
    phone TEXT NOT NULL,
    serial INTEGER NOT NULL,
    payload_json TEXT NOT NULL,
    updated_at INTEGER NOT NULL,
    PRIMARY KEY (phone, serial)
);

CREATE TABLE IF NOT EXISTS media_upload_notices (
    phone TEXT NOT NULL,
    serial INTEGER NOT NULL,
    result INTEGER NOT NULL DEFAULT 0,
    updated_at INTEGER NOT NULL,
    PRIMARY KEY (phone, serial)
);
CREATE INDEX idx_media_upload_notices_phone ON media_upload_notices (phone, updated_at);

-- 下行命令 outbox（2026-11-09）。
--
-- 每条平台下行命令先落库再发送，状态机为 pending -> acked | failed。
-- idempotency_key 按设备唯一，重复提交返回已有 command_id，不会二次下发。
-- payload 保存消息体编码（不含报文头），保留字段供后续重投与审计使用。
--
-- 方言约束同既有迁移文件：只使用 SQLite 语法；
-- 不使用 BLOB 默认值，不写 MySQL 专属类型与子句。
-- 注：20261108 已被媒体元数据迁移占用，按迁移工具「同日唯一」约束顺延一天。

CREATE TABLE IF NOT EXISTS command_outbox (
    command_id TEXT NOT NULL PRIMARY KEY,
    idempotency_key TEXT NOT NULL,
    phone TEXT NOT NULL,
    msg_id INTEGER NOT NULL,
    serial INTEGER NOT NULL,
    payload BLOB NOT NULL,
    status TEXT NOT NULL,
    attempts INTEGER NOT NULL,
    last_error TEXT NOT NULL,
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    -- 重投闭环字段均带默认值，兼容升级前已存在的 pending 记录：
    --   next_attempt_at = 0 表示立即可投；
    --   expires_at = 0 表示不设过期（新写入的记录由应用层按配置填充）；
    --   lease_expires_at = 0 表示未占用。
    -- lease_owner / lease_expires_at 是多实例并发保护：同一命令在租约有效期内
    -- 只会被一个实例下发；租约到期后允许重新占用，因此整体语义是 at-least-once。
    last_attempt_at INTEGER NOT NULL DEFAULT 0,
    next_attempt_at INTEGER NOT NULL DEFAULT 0,
    expires_at INTEGER NOT NULL DEFAULT 0,
    lease_owner TEXT NOT NULL DEFAULT '',
    lease_expires_at INTEGER NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX idx_command_outbox_idempotency ON command_outbox (phone, idempotency_key);
CREATE INDEX idx_command_outbox_routing ON command_outbox (phone, msg_id, serial);
CREATE INDEX idx_command_outbox_due ON command_outbox (status, next_attempt_at, expires_at);
CREATE INDEX idx_command_outbox_lease ON command_outbox (lease_expires_at);

-- 终端升级结果（2026-11-11）。
--
-- 保存终端 0x0108 升级结果通知，供升级验收按设备查询最近一次结果。
-- 同一 (phone, serial) 重复上报时覆盖；查询按 received_at 倒序取最新。
--
-- 方言约束同既有迁移文件：只使用 SQLite 语法；
-- 不使用 BLOB 默认值，不写 MySQL 专属类型与子句。
-- 注：迁移工具要求同日唯一，20261109/20261110 已被 outbox 迁移占用，
-- 本文件日期顺延至 20261111。

CREATE TABLE IF NOT EXISTS upgrade_results (
    phone TEXT NOT NULL,
    serial INTEGER NOT NULL,
    upgrade_type INTEGER NOT NULL DEFAULT 0,
    result INTEGER NOT NULL DEFAULT 0,
    received_at INTEGER NOT NULL,
    PRIMARY KEY (phone, serial)
);
CREATE INDEX idx_upgrade_results_phone_received ON upgrade_results (phone, received_at);

-- 0x0801 多媒体文件与 0x0805 拍摄应答（2026-11-12）。
--
-- 落库范围：
--   * media_files：0x0801 落盘文件的元数据，按 (phone, media_id) 唯一；
--   * media_captures：0x0805 摄像头立即拍摄命令应答，按 (phone, record_id) 唯一；
--   * media_capture_items：0x0805 返回的 mediaId 列表，按 (phone, record_id, item_index) 唯一。
--
-- 方言约束同既有迁移文件：只使用 SQLite 语法；
-- 不使用 BLOB 默认值，不写 MySQL 专属类型与子句。

CREATE TABLE IF NOT EXISTS media_files (
    phone TEXT NOT NULL,
    media_id INTEGER NOT NULL,
    media_type INTEGER NOT NULL DEFAULT 0,
    media_format INTEGER NOT NULL DEFAULT 0,
    event_id INTEGER NOT NULL DEFAULT 0,
    channel_id INTEGER NOT NULL DEFAULT 0,
    alarm_sign INTEGER NOT NULL DEFAULT 0,
    status_sign INTEGER NOT NULL DEFAULT 0,
    latitude INTEGER NOT NULL DEFAULT 0,
    longitude INTEGER NOT NULL DEFAULT 0,
    altitude INTEGER NOT NULL DEFAULT 0,
    speed INTEGER NOT NULL DEFAULT 0,
    direction INTEGER NOT NULL DEFAULT 0,
    location_time INTEGER NOT NULL DEFAULT 0,
    relative_path TEXT NOT NULL,
    file_size INTEGER NOT NULL DEFAULT 0,
    md5 TEXT NOT NULL DEFAULT '',
    received_at INTEGER NOT NULL,
    PRIMARY KEY (phone, media_id)
);
CREATE INDEX idx_media_files_phone_time ON media_files (phone, location_time, received_at);
CREATE INDEX idx_media_files_alarm ON media_files (phone, alarm_sign);
CREATE INDEX idx_media_files_received ON media_files (received_at);

CREATE TABLE IF NOT EXISTS media_captures (
    phone TEXT NOT NULL,
    record_id TEXT NOT NULL,
    answer_serial INTEGER NOT NULL,
    result INTEGER NOT NULL DEFAULT 0,
    received_at INTEGER NOT NULL,
    PRIMARY KEY (phone, record_id)
);
CREATE INDEX idx_media_captures_phone_time ON media_captures (phone, received_at);
CREATE INDEX idx_media_captures_received ON media_captures (received_at);

CREATE TABLE IF NOT EXISTS media_capture_items (
    phone TEXT NOT NULL,
    record_id TEXT NOT NULL,
    item_index INTEGER NOT NULL,
    media_id INTEGER NOT NULL,
    PRIMARY KEY (phone, record_id, item_index)
);
CREATE INDEX idx_media_capture_items_media ON media_capture_items (phone, media_id);

-- 终端 BCD 时间从“伪 UTC”统一为固定 GMT+8（2026-11-13）。
--
-- 背景：旧版本用 time.Parse 解析 JT/T 808 BCD 时间，得到的是把设备墙钟时间
-- 标记为 UTC 的“伪瞬时值”。协议规定 BCD 时间为 GMT+8，因此同一墙钟的真实
-- 瞬时值应减去 8 小时，即 8 * 60 * 60 * 1000 = 28800000 毫秒。
--
-- 只平移由终端 BCD 时间派生的列：
--   * locations.report_time_ms  <- report_time_ms - 28800000
--   * alarms.occurred_at_ms     <- occurred_at_ms - 28800000（0 表示终端未给时间）
--   * media_files.location_time <- location_time - 28800000（0 表示未知）
-- 服务端接收/处理时间（received_at_ms、received_at、updated_at、created_at 等）
-- 一律不动；它们本来就是正确瞬时值，平移会引入新错误。
--
-- media_resource_lists.items_json 是 []DeviceMedia 的 JSON 数组，
-- 时间只出现在 $[*].startTime 与 $[*].endTime，旧值为 UTC 标签的 RFC3339，
-- 例如 "2026-11-12T10:30:00Z"。保持设备墙钟不变、把 UTC 标签改成显式 +08:00，
-- 得到的就是真实瞬时值（等价于减 8 小时，且不需要日期运算）。
-- 该结构里除时间外没有以 Z 结尾的字符串字段（其余为数字或 null），
-- 因此按后缀替换是精确的，且对已是 +08:00 的值天然幂等。
-- 这张表是 0x1205 对 0x9205 查询的应答快照，不是定时上报；删除会让缓存列表
-- 在下一次手工查询前不可用，所以选择原地平移而不是清空。
--
-- 已知边界：旧数据中 BCD 解析失败、退回 time.Now() 的退化行也会被平移 8 小时；
-- 这类行的时间语义本身已损坏，正常数据中应不存在。
--
-- 回滚方式（必须先停服务并备份数据库，且必须在启动新版本前完成）：
--   UPDATE locations SET report_time_ms = report_time_ms + 28800000 WHERE report_time_ms > 0;
--   UPDATE alarms SET occurred_at_ms = occurred_at_ms + 28800000 WHERE occurred_at_ms > 0;
--   UPDATE media_files SET location_time = location_time + 28800000 WHERE location_time > 0;
--   UPDATE media_resource_lists
--     SET items_json = REPLACE(items_json, '+08:00"', 'Z"')
--     WHERE items_json LIKE '%+08:00"%';
-- 若新版本已经写入过资源列表，回滚会把新数据也改回伪 UTC 表示，
-- 因此回滚窗口仅限“迁移完成、新版本尚未对外服务”这段时间。
-- 本文件由 jt808-migrate 按版本执行一次；不要手工重复执行。
-- 方言约束同既有迁移文件：只使用 SQLite 语法。

UPDATE locations SET report_time_ms = report_time_ms - 28800000 WHERE report_time_ms > 0;

UPDATE alarms SET occurred_at_ms = occurred_at_ms - 28800000 WHERE occurred_at_ms > 0;

UPDATE media_files SET location_time = location_time - 28800000 WHERE location_time > 0;

UPDATE media_resource_lists
SET items_json = REPLACE(items_json, 'Z"', '+08:00"')
WHERE items_json LIKE '%Z"%';


-- 0x0802 存储多媒体数据检索应答（2026-11-14）。
--
-- 终端应答平台 0x8802 检索命令时上报的媒体清单，按设备与应答流水号整体覆盖：
--   * 主键 (phone, answer_serial, item_index) 保证同一次应答重传幂等；
--   * location_time 来自记录内嵌位置体的 BCD 时间，非法或缺失时为 0；
--   * received_at 是平台接收时间，用于“查最近一次检索结果”。
--
-- 方言约束同既有迁移文件：只使用 SQLite 语法；
-- 不使用 BLOB 默认值，不写 MySQL 专属类型与子句。

CREATE TABLE IF NOT EXISTS media_search_results (
    phone TEXT NOT NULL,
    answer_serial INTEGER NOT NULL,
    item_index INTEGER NOT NULL,
    media_id INTEGER NOT NULL,
    media_type INTEGER NOT NULL DEFAULT 0,
    channel_id INTEGER NOT NULL DEFAULT 0,
    event_id INTEGER NOT NULL DEFAULT 0,
    alarm_sign INTEGER NOT NULL DEFAULT 0,
    status_sign INTEGER NOT NULL DEFAULT 0,
    latitude INTEGER NOT NULL DEFAULT 0,
    longitude INTEGER NOT NULL DEFAULT 0,
    altitude INTEGER NOT NULL DEFAULT 0,
    speed INTEGER NOT NULL DEFAULT 0,
    direction INTEGER NOT NULL DEFAULT 0,
    location_time INTEGER NOT NULL DEFAULT 0,
    received_at INTEGER NOT NULL,
    PRIMARY KEY (phone, answer_serial, item_index)
);
-- 查询方式为“按设备取最近检索结果”；接收时间索引供保留期清理按全局时间扫描。
CREATE INDEX idx_media_search_results_phone_time ON media_search_results (phone, received_at);
CREATE INDEX idx_media_search_results_received ON media_search_results (received_at);

-- 下行命令流水号分配表（2026-11-17）。
--
-- outbox 启用后下行流水号由管控侧统一分配，保证重启与多副本下仍然确定性：
--   - 每条 phone 一行，next_serial 记录下一次候选值；
--   - 分配时跳过 0，并在 1..65535 间循环；
--   - 回绕后必须跳过同一 phone、同一 msg_id 仍为 pending 的组合，
--     避免覆盖尚未终结命令的应答匹配键 (phone, msg_id, serial)。
--
-- 方言约束同既有迁移文件：只使用 SQLite 语法。

CREATE TABLE IF NOT EXISTS command_serial (
    phone TEXT NOT NULL PRIMARY KEY,
    next_serial INTEGER NOT NULL,
    updated_at INTEGER NOT NULL
);

-- 附件接收元数据（2026-11-18）。
--
-- 集群下附件节点与控制面共用一张表：
--   * 附件节点按 (source, phone, alarm_no, file_name) 唯一写入进度；
--   * 控制面按手机号/报警号查询进度与完成结果；
--   * source 区分苏标附件（subia）与将来的 1078 FTP 录像（ftp1078）。
-- segments/ranges 存 JSON，不在数据库里展开，避免每个切片一行带来的写放大。
--
-- 方言约束：仅使用 SQLite 语法；不使用 TEXT 默认值。

CREATE TABLE IF NOT EXISTS attachment_files (
    source TEXT NOT NULL,
    phone TEXT NOT NULL,
    alarm_no TEXT NOT NULL DEFAULT '',
    file_name TEXT NOT NULL,
    terminal_id TEXT NOT NULL DEFAULT '',
    alarm_sign TEXT NOT NULL DEFAULT '',
    alarm_serial TEXT NOT NULL DEFAULT '',
    file_type INTEGER NOT NULL DEFAULT 0,
    total_size INTEGER NOT NULL DEFAULT 0,
    received_bytes INTEGER NOT NULL DEFAULT 0,
    watermark INTEGER NOT NULL DEFAULT 0,
    staging_path TEXT NOT NULL DEFAULT '',
    staging_base TEXT NOT NULL DEFAULT '',
    ranges TEXT NOT NULL,
    segments TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'receiving',
    created_at INTEGER NOT NULL,
    updated_at INTEGER NOT NULL,
    completed_at INTEGER NOT NULL DEFAULT 0,
    PRIMARY KEY (source, phone, alarm_no, file_name)
);
CREATE INDEX idx_attachment_files_phone ON attachment_files (source, phone, updated_at);
CREATE INDEX idx_attachment_files_status ON attachment_files (status, updated_at);

-- 多副本协调（2026-11-19）。
-- task_leases：周期性任务的单实例租约，name 为任务名，expires_at 到期后可被其它副本接手。
-- dedup_keys：按业务键的共享 TTL 去重，dedup_key 形如 auto-9208:<phone>:<alarmID>。
CREATE TABLE IF NOT EXISTS task_leases (
    name TEXT NOT NULL PRIMARY KEY,
    owner TEXT NOT NULL DEFAULT '',
    expires_at INTEGER NOT NULL DEFAULT 0,
    updated_at INTEGER NOT NULL DEFAULT 0
);

CREATE TABLE IF NOT EXISTS dedup_keys (
    dedup_key TEXT NOT NULL PRIMARY KEY,
    expires_at INTEGER NOT NULL DEFAULT 0,
    updated_at INTEGER NOT NULL DEFAULT 0
);

-- 网关节点注册（E4.1）。时间列存 ISO8601 毫秒 TEXT，过期判断由 SQL 的 strftime 裁决。
CREATE TABLE IF NOT EXISTS gateway_nodes (
    gateway_id TEXT NOT NULL PRIMARY KEY,
    instance_id TEXT NOT NULL,
    downlink_url TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'online',
    started_at TEXT NOT NULL,
    last_seen_at TEXT NOT NULL,
    expires_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_gateway_nodes_status ON gateway_nodes (status, expires_at);
