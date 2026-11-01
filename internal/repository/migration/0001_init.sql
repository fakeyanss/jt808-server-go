-- 0001 初始表：设备静态主数据与终端参数。
-- devices 的运行时列在 0003 拆出到 device_runtime。
CREATE TABLE IF NOT EXISTS devices (
    phone VARCHAR(32) PRIMARY KEY,
    id VARCHAR(128) NOT NULL DEFAULT '',
    plate VARCHAR(64) NOT NULL DEFAULT '',
    session_id VARCHAR(128) NOT NULL DEFAULT '',
    trans_proto VARCHAR(16) NOT NULL DEFAULT '',
    keepalive_ms BIGINT NOT NULL DEFAULT 0,
    last_com_time_ms BIGINT NOT NULL DEFAULT 0,
    status SMALLINT NOT NULL DEFAULT 0,
    version_desc SMALLINT NOT NULL DEFAULT 0,
    protocol_version SMALLINT NOT NULL DEFAULT 0,
    auth_code VARCHAR(64) NOT NULL DEFAULT '',
    imei VARCHAR(64) NOT NULL DEFAULT '',
    software_version VARCHAR(64) NOT NULL DEFAULT '',
    updated_at BIGINT NOT NULL
);

-- 终端参数按设计决策 D1 使用整体 JSON 单字段。
CREATE TABLE IF NOT EXISTS device_params (
    phone VARCHAR(32) PRIMARY KEY,
    param_cnt SMALLINT NOT NULL DEFAULT 0,
    params_json TEXT NOT NULL,
    updated_at BIGINT NOT NULL
);
