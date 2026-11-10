-- 下行命令流水号分配表（2026-11-17）。
--
-- outbox 启用后下行流水号由管控侧统一分配，保证重启与多副本下仍然确定性：
--   - 每条 phone 一行，next_serial 记录下一次候选值；
--   - 分配时跳过 0，并在 1..65535 间循环；
--   - 回绕后必须跳过同一 phone、同一 msg_id 仍为 pending 的组合，
--     避免覆盖尚未终结命令的应答匹配键 (phone, msg_id, serial)。
--
-- 方言约束同既有迁移文件：只使用 SQLite 与 MySQL 都支持的语法。

CREATE TABLE IF NOT EXISTS command_serial (
    phone VARCHAR(32) NOT NULL PRIMARY KEY,
    next_serial INTEGER NOT NULL,
    updated_at BIGINT NOT NULL
);
