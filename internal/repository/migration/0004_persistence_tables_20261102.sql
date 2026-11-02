-- 持久化数据表（2026-11-02）。
-- 批次 A：终端下行配置目录（terminal_catalogs）与区域/路线（region_items）。
-- 批次 B：定位历史（locations）与报警（alarms）；本文件一次性建表，代码分批接入。
--
-- 方言约束同 init_20261101.sql：只使用 SQLite 与 MySQL 都支持的语法；
-- 不使用 TEXT/BLOB 默认值，不写 MySQL 专属子句。
-- payload_json 使用 MEDIUMTEXT，MySQL 下可容纳升级包等较大 JSON；
-- SQLite 对未知类型名按 TEXT 亲和性处理。

-- 终端下行配置整包快照。
-- catalog_type: event(0x8301) / info_menu(0x8303) / phonebook(0x8401)
--               / upgrade(0x8108) / transparent_down(0x8900)
-- item_key：事件 ID、菜单 ID、电话号码；单条快照固定为 latest。
CREATE TABLE IF NOT EXISTS terminal_catalogs (
    phone VARCHAR(32) NOT NULL,
    catalog_type VARCHAR(16) NOT NULL,
    item_key VARCHAR(255) NOT NULL,
    payload_json MEDIUMTEXT NOT NULL,
    updated_at BIGINT NOT NULL,
    PRIMARY KEY (phone, catalog_type, item_key)
);
CREATE INDEX idx_terminal_catalogs_phone ON terminal_catalogs (phone, catalog_type);

-- 区域/路线目录。region_type: 1 圆形 / 2 矩形 / 3 多边形 / 4 路线。
-- “清空后重设”由应用层在同一事务内完成 DELETE + INSERT。
CREATE TABLE IF NOT EXISTS region_items (
    phone VARCHAR(32) NOT NULL,
    region_type SMALLINT NOT NULL,
    item_id BIGINT NOT NULL,
    payload_json TEXT NOT NULL,
    updated_at BIGINT NOT NULL,
    PRIMARY KEY (phone, region_type, item_id)
);
CREATE INDEX idx_region_items_phone ON region_items (phone, region_type);

-- 定位历史。record_id 由应用层生成（纳秒时间戳 + 进程内递增），
-- 避免同一毫秒内多条 0x0704 数据项共用 (phone, received_at_ms) 造成主键冲突。
CREATE TABLE IF NOT EXISTS locations (
    phone VARCHAR(32) NOT NULL,
    record_id BIGINT NOT NULL,
    msg_id SMALLINT NOT NULL,
    report_time_ms BIGINT NOT NULL,
    received_at_ms BIGINT NOT NULL,
    alarm_sign BIGINT NOT NULL DEFAULT 0,
    status_sign BIGINT NOT NULL DEFAULT 0,
    latitude BIGINT NOT NULL DEFAULT 0,
    longitude BIGINT NOT NULL DEFAULT 0,
    altitude INTEGER NOT NULL DEFAULT 0,
    speed INTEGER NOT NULL DEFAULT 0,
    direction INTEGER NOT NULL DEFAULT 0,
    payload_json TEXT NOT NULL,
    PRIMARY KEY (phone, record_id)
);
CREATE INDEX idx_locations_phone_report_time ON locations (phone, report_time_ms);
CREATE INDEX idx_locations_report_time ON locations (report_time_ms);

-- 报警历史。同一 (phone, message_serial) 在去重窗口内更新 alarm_sign，
-- 窗口外以新的 received_at_ms 追加；confirmed 由 0x8203 人工确认置位。
CREATE TABLE IF NOT EXISTS alarms (
    phone VARCHAR(32) NOT NULL,
    message_serial BIGINT NOT NULL,
    received_at_ms BIGINT NOT NULL,
    occurred_at_ms BIGINT NOT NULL DEFAULT 0,
    alarm_sign BIGINT NOT NULL DEFAULT 0,
    latitude BIGINT NOT NULL DEFAULT 0,
    longitude BIGINT NOT NULL DEFAULT 0,
    confirmed SMALLINT NOT NULL DEFAULT 0,
    PRIMARY KEY (phone, message_serial, received_at_ms)
);
CREATE INDEX idx_alarms_phone_received ON alarms (phone, received_at_ms);
CREATE INDEX idx_alarms_received ON alarms (received_at_ms);
