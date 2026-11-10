-- 0x0802 存储多媒体数据检索应答（2026-11-14）。
--
-- 终端应答平台 0x8802 检索命令时上报的媒体清单，按设备与应答流水号整体覆盖：
--   * 主键 (phone, answer_serial, item_index) 保证同一次应答重传幂等；
--   * location_time 来自记录内嵌位置体的 BCD 时间，非法或缺失时为 0；
--   * received_at 是平台接收时间，用于“查最近一次检索结果”。
--
-- 方言约束同既有迁移文件：只使用 SQLite 与 MySQL 都支持的语法；
-- 不使用 TEXT/BLOB 默认值，不写 MySQL 专属子句。

CREATE TABLE IF NOT EXISTS media_search_results (
    phone VARCHAR(32) NOT NULL,
    answer_serial INTEGER NOT NULL,
    item_index INTEGER NOT NULL,
    media_id BIGINT NOT NULL,
    media_type SMALLINT NOT NULL DEFAULT 0,
    channel_id SMALLINT NOT NULL DEFAULT 0,
    event_id SMALLINT NOT NULL DEFAULT 0,
    alarm_sign BIGINT NOT NULL DEFAULT 0,
    status_sign BIGINT NOT NULL DEFAULT 0,
    latitude BIGINT NOT NULL DEFAULT 0,
    longitude BIGINT NOT NULL DEFAULT 0,
    altitude INTEGER NOT NULL DEFAULT 0,
    speed INTEGER NOT NULL DEFAULT 0,
    direction INTEGER NOT NULL DEFAULT 0,
    location_time BIGINT NOT NULL DEFAULT 0,
    received_at BIGINT NOT NULL,
    PRIMARY KEY (phone, answer_serial, item_index)
);
-- 查询方式为“按设备取最近检索结果”，按接收时间倒序扫描。
CREATE INDEX idx_media_search_results_phone_time ON media_search_results (phone, received_at);
