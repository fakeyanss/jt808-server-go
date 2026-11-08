-- 下行命令 outbox（2026-11-09）。
--
-- 每条平台下行命令先落库再发送，状态机为 pending -> acked | failed。
-- idempotency_key 按设备唯一，重复提交返回已有 command_id，不会二次下发。
-- payload 保存消息体编码（不含报文头），保留字段供后续重投与审计使用。
--
-- 方言约束同既有迁移文件：只使用 SQLite 与 MySQL 都支持的语法；
-- 不使用 TEXT/BLOB 默认值，不写 MySQL 专属子句。
-- 注：20261108 已被媒体元数据迁移占用，按迁移工具「同日唯一」约束顺延一天。

CREATE TABLE IF NOT EXISTS command_outbox (
    command_id VARCHAR(64) NOT NULL PRIMARY KEY,
    idempotency_key VARCHAR(128) NOT NULL,
    phone VARCHAR(32) NOT NULL,
    msg_id INTEGER NOT NULL,
    serial INTEGER NOT NULL,
    payload MEDIUMBLOB NOT NULL,
    status VARCHAR(16) NOT NULL,
    attempts INTEGER NOT NULL,
    last_error VARCHAR(255) NOT NULL,
    created_at BIGINT NOT NULL,
    updated_at BIGINT NOT NULL
);
CREATE UNIQUE INDEX idx_command_outbox_idempotency ON command_outbox (phone, idempotency_key);
CREATE INDEX idx_command_outbox_routing ON command_outbox (phone, msg_id, serial);
