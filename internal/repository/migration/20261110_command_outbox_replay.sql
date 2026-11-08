-- 下行命令 outbox 重投闭环字段（2026-11-10）。
--
-- 增列均带默认值，兼容升级前已存在的 pending 记录：
--   next_attempt_at = 0 表示立即可投；
--   expires_at = 0 表示不设过期（新写入的记录由应用层按配置填充）；
--   lease_expires_at = 0 表示未占用。
-- lease_owner / lease_expires_at 是多实例并发保护：同一命令在租约有效期内
-- 只会被一个实例下发；租约到期后允许重新占用，因此整体语义是 at-least-once。
--
-- 方言约束同既有迁移文件：只使用 SQLite 与 MySQL 都支持的语法，
-- 每次 ALTER 只增一列，避免方言差异。
-- 注：迁移工具要求同日唯一，本文件日期顺延至 20261110。

ALTER TABLE command_outbox ADD COLUMN last_attempt_at BIGINT NOT NULL DEFAULT 0;
ALTER TABLE command_outbox ADD COLUMN next_attempt_at BIGINT NOT NULL DEFAULT 0;
ALTER TABLE command_outbox ADD COLUMN expires_at BIGINT NOT NULL DEFAULT 0;
ALTER TABLE command_outbox ADD COLUMN lease_owner VARCHAR(128) NOT NULL DEFAULT '';
ALTER TABLE command_outbox ADD COLUMN lease_expires_at BIGINT NOT NULL DEFAULT 0;

CREATE INDEX idx_command_outbox_due ON command_outbox (status, next_attempt_at, expires_at);
CREATE INDEX idx_command_outbox_lease ON command_outbox (lease_expires_at);
