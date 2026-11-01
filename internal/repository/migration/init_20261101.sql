-- 初始 schema（2026-11-01）。
-- 开发阶段不保留增量迁移，直接给出最终形态；后续变更按 <描述>_<YYYYMMDD>.sql 追加。
-- 文件末尾的日期即版本号，迁移按日期升序执行。
--
-- 方言约束：本文件同时供 SQLite 与 MySQL 执行，只使用两者都支持的语法。
--   * 主键显式写 NOT NULL（SQLite 的非 INTEGER 主键不隐含 NOT NULL）。
--   * 不使用 TEXT/BLOB/JSON 的 DEFAULT（MySQL 不允许字面量默认值）。
--   * 不写 ENGINE/CHARSET/COLLATE 等 MySQL 专属子句，依赖服务端默认（utf8mb4 + InnoDB）。
--   * 不使用 CREATE INDEX IF NOT EXISTS（MySQL 不支持），索引重复执行问题由版本表规避。

-- 设备静态主数据。运行时字段（session/在线态/最后通信时间）在 device_runtime。
CREATE TABLE IF NOT EXISTS devices (
    phone VARCHAR(32) NOT NULL PRIMARY KEY,
    id VARCHAR(128) NOT NULL DEFAULT '',
    plate VARCHAR(64) NOT NULL DEFAULT '',
    keepalive_ms BIGINT NOT NULL DEFAULT 0,
    version_desc SMALLINT NOT NULL DEFAULT 0,
    protocol_version SMALLINT NOT NULL DEFAULT 0,
    auth_code VARCHAR(64) NOT NULL DEFAULT '',
    imei VARCHAR(64) NOT NULL DEFAULT '',
    software_version VARCHAR(64) NOT NULL DEFAULT '',
    updated_at BIGINT NOT NULL
);

-- 终端参数按设计决策 D1 使用整体 JSON 单字段。
CREATE TABLE IF NOT EXISTS device_params (
    phone VARCHAR(32) NOT NULL PRIMARY KEY,
    param_cnt SMALLINT NOT NULL DEFAULT 0,
    params_json TEXT NOT NULL,
    updated_at BIGINT NOT NULL
);

-- 终端属性档案：只承载 0x0107 查询应答，与 devices 独立存储。
CREATE TABLE IF NOT EXISTS terminal_properties (
    phone VARCHAR(32) NOT NULL PRIMARY KEY,
    terminal_type INTEGER NOT NULL DEFAULT 0,
    manufacturer_id VARCHAR(16) NOT NULL DEFAULT '',
    terminal_model VARCHAR(64) NOT NULL DEFAULT '',
    terminal_id VARCHAR(64) NOT NULL DEFAULT '',
    iccid VARCHAR(32) NOT NULL DEFAULT '',
    hardware_version VARCHAR(255) NOT NULL DEFAULT '',
    firmware_version VARCHAR(255) NOT NULL DEFAULT '',
    gnss_properties SMALLINT NOT NULL DEFAULT 0,
    communication_properties SMALLINT NOT NULL DEFAULT 0,
    extra_json TEXT NOT NULL,
    updated_at BIGINT NOT NULL
);

-- 设备运行时投影。存量 session/status 跨进程无意义，无需搬运数据。
-- 不在启动时批量重置：单机由优雅停机清空，异常重启后由读路径 TTL 判定收敛为离线。
CREATE TABLE IF NOT EXISTS device_runtime (
    phone VARCHAR(32) NOT NULL PRIMARY KEY,
    session_id VARCHAR(128) NOT NULL DEFAULT '',
    trans_proto VARCHAR(16) NOT NULL DEFAULT '',
    last_com_time_ms BIGINT NOT NULL DEFAULT 0,
    status SMALLINT NOT NULL DEFAULT 0,
    updated_at BIGINT NOT NULL
);

-- 车牌普通索引（非唯一）：设备身份以 phone 为准，plate 允许重复。
-- 该索引只用于加速 GetByPlate 查询。
CREATE INDEX idx_devices_plate ON devices (plate);
