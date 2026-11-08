-- 1078 媒体元数据表（2026-11-08）。
--
-- 落库范围：
--   * media_resource_lists：终端上行的音视频资源列表（0x1205）快照，按设备整体覆盖；
--   * media_upload_tasks：平台下发的文件上传指令（0x9206）元数据，按 (phone, 流水号) 保存；
--   * media_upload_notices：终端上行的文件上传完成通知（0x1206）结果，按 (phone, 流水号) 保存。
--
-- 方言约束同既有迁移文件：只使用 SQLite 与 MySQL 都支持的语法；
-- 不使用 TEXT/BLOB 默认值，不写 MySQL 专属子句。

CREATE TABLE IF NOT EXISTS media_resource_lists (
    phone VARCHAR(32) NOT NULL PRIMARY KEY,
    media_count BIGINT NOT NULL DEFAULT 0,
    items_json MEDIUMTEXT NOT NULL,
    updated_at BIGINT NOT NULL
);

CREATE TABLE IF NOT EXISTS media_upload_tasks (
    phone VARCHAR(32) NOT NULL,
    serial BIGINT NOT NULL,
    payload_json MEDIUMTEXT NOT NULL,
    updated_at BIGINT NOT NULL,
    PRIMARY KEY (phone, serial)
);

CREATE TABLE IF NOT EXISTS media_upload_notices (
    phone VARCHAR(32) NOT NULL,
    serial BIGINT NOT NULL,
    result SMALLINT NOT NULL DEFAULT 0,
    updated_at BIGINT NOT NULL,
    PRIMARY KEY (phone, serial)
);
CREATE INDEX idx_media_upload_notices_phone ON media_upload_notices (phone, updated_at);
