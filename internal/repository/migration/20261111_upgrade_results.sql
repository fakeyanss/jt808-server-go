-- 终端升级结果（2026-11-11）。
--
-- 保存终端 0x0108 升级结果通知，供升级验收按设备查询最近一次结果。
-- 同一 (phone, serial) 重复上报时覆盖；查询按 received_at 倒序取最新。
--
-- 方言约束同既有迁移文件：只使用 SQLite 与 MySQL 都支持的语法；
-- 不使用 TEXT/BLOB 默认值，不写 MySQL 专属子句。
-- 注：迁移工具要求同日唯一，20261109/20261110 已被 outbox 迁移占用，
-- 本文件日期顺延至 20261111。

CREATE TABLE IF NOT EXISTS upgrade_results (
    phone VARCHAR(32) NOT NULL,
    serial INTEGER NOT NULL,
    upgrade_type SMALLINT NOT NULL DEFAULT 0,
    result SMALLINT NOT NULL DEFAULT 0,
    received_at BIGINT NOT NULL,
    PRIMARY KEY (phone, serial)
);
CREATE INDEX idx_upgrade_results_phone_received ON upgrade_results (phone, received_at);
