-- 0002 终端属性档案：只承载 0x0107 查询应答，与 devices 独立存储。
CREATE TABLE IF NOT EXISTS terminal_properties (
    phone VARCHAR(32) PRIMARY KEY,
    terminal_type INTEGER NOT NULL DEFAULT 0,
    manufacturer_id VARCHAR(16) NOT NULL DEFAULT '',
    terminal_model VARCHAR(64) NOT NULL DEFAULT '',
    terminal_id VARCHAR(64) NOT NULL DEFAULT '',
    iccid VARCHAR(32) NOT NULL DEFAULT '',
    hardware_version VARCHAR(255) NOT NULL DEFAULT '',
    firmware_version VARCHAR(255) NOT NULL DEFAULT '',
    gnss_properties SMALLINT NOT NULL DEFAULT 0,
    communication_properties SMALLINT NOT NULL DEFAULT 0,
    extra_json TEXT NOT NULL DEFAULT '{}',
    updated_at BIGINT NOT NULL
);
