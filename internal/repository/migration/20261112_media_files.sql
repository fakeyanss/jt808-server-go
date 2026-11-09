-- 0x0801 多媒体文件与 0x0805 拍摄应答（2026-11-12）。
--
-- 落库范围：
--   * media_files：0x0801 落盘文件的元数据，按 (phone, media_id) 唯一；
--   * media_captures：0x0805 摄像头立即拍摄命令应答，按 (phone, record_id) 唯一；
--   * media_capture_items：0x0805 返回的 mediaId 列表，按 (phone, record_id, item_index) 唯一。
--
-- 方言约束同既有迁移文件：只使用 SQLite 与 MySQL 都支持的语法；
-- 不使用 TEXT/BLOB 默认值，不写 MySQL 专属子句。

CREATE TABLE IF NOT EXISTS media_files (
    phone VARCHAR(32) NOT NULL,
    media_id BIGINT NOT NULL,
    media_type SMALLINT NOT NULL DEFAULT 0,
    media_format SMALLINT NOT NULL DEFAULT 0,
    event_id SMALLINT NOT NULL DEFAULT 0,
    channel_id SMALLINT NOT NULL DEFAULT 0,
    alarm_sign BIGINT NOT NULL DEFAULT 0,
    status_sign BIGINT NOT NULL DEFAULT 0,
    latitude BIGINT NOT NULL DEFAULT 0,
    longitude BIGINT NOT NULL DEFAULT 0,
    altitude INTEGER NOT NULL DEFAULT 0,
    speed INTEGER NOT NULL DEFAULT 0,
    direction INTEGER NOT NULL DEFAULT 0,
    location_time BIGINT NOT NULL DEFAULT 0,
    relative_path VARCHAR(512) NOT NULL,
    file_size BIGINT NOT NULL DEFAULT 0,
    md5 VARCHAR(32) NOT NULL DEFAULT '',
    received_at BIGINT NOT NULL,
    PRIMARY KEY (phone, media_id)
);
CREATE INDEX idx_media_files_phone_time ON media_files (phone, location_time, received_at);
CREATE INDEX idx_media_files_alarm ON media_files (phone, alarm_sign);

CREATE TABLE IF NOT EXISTS media_captures (
    phone VARCHAR(32) NOT NULL,
    record_id VARCHAR(64) NOT NULL,
    answer_serial BIGINT NOT NULL,
    result SMALLINT NOT NULL DEFAULT 0,
    received_at BIGINT NOT NULL,
    PRIMARY KEY (phone, record_id)
);
CREATE INDEX idx_media_captures_phone_time ON media_captures (phone, received_at);

CREATE TABLE IF NOT EXISTS media_capture_items (
    phone VARCHAR(32) NOT NULL,
    record_id VARCHAR(64) NOT NULL,
    item_index INTEGER NOT NULL,
    media_id BIGINT NOT NULL,
    PRIMARY KEY (phone, record_id, item_index)
);
CREATE INDEX idx_media_capture_items_media ON media_capture_items (phone, media_id);
