-- 0003 设备运行时投影拆表。
-- 存量 session_id / status 跨进程本就无意义，迁移不搬运数据；
-- 启动时由 Store 统一清空 session_id 并把 status 归 offline。
CREATE TABLE IF NOT EXISTS device_runtime (
    phone VARCHAR(32) PRIMARY KEY,
    session_id VARCHAR(128) NOT NULL DEFAULT '',
    trans_proto VARCHAR(16) NOT NULL DEFAULT '',
    last_com_time_ms BIGINT NOT NULL DEFAULT 0,
    status SMALLINT NOT NULL DEFAULT 0,
    updated_at BIGINT NOT NULL
);

ALTER TABLE devices DROP COLUMN session_id;
ALTER TABLE devices DROP COLUMN trans_proto;
ALTER TABLE devices DROP COLUMN last_com_time_ms;
ALTER TABLE devices DROP COLUMN status;
