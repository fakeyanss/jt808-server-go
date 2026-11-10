-- 历史数据保留期清理所需索引（2026-11-15）。
--
-- 定期清理按“接收时间早于 cutoff”扫描并分批删除，需要独立的接收时间索引：
--   * locations.received_at_ms：既有索引以 (phone, report_time_ms) 与 report_time_ms 为主，
--     不覆盖按接收时间的全局扫描；
--   * media_files.received_at：既有索引以 phone 开头，无法支撑全局时间扫描；
--   * media_captures.received_at：同上；
--   * media_search_results.received_at：20261114 只建了 (phone, received_at)
--     复合索引，无法支撑按时间的全局扫描。
-- alarms.received_at_ms 已有 idx_alarms_received，无需重复创建。
--
-- 方言约束同既有迁移文件：只使用 SQLite 与 MySQL 都支持的语法；
-- 不使用 TEXT/BLOB 默认值，不写 MySQL 专属子句。

CREATE INDEX idx_locations_received ON locations (received_at_ms);
CREATE INDEX idx_media_files_received ON media_files (received_at);
CREATE INDEX idx_media_captures_received ON media_captures (received_at);
CREATE INDEX idx_media_search_results_received ON media_search_results (received_at);
