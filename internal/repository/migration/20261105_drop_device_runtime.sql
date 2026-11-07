-- 移除设备运行时投影表（2026-11-05）。
--
-- 运行时状态（session_id / trans_proto / last_com_time_ms / status）不再进入数据库，
-- 改由进程内 runtime 注册表持有：连接在则有效，进程重启后自然归零。
-- devices 表只保留设备静态档案与 keepalive_ms，作为跨进程可恢复的注册信息。
--
-- DROP TABLE IF EXISTS 同时兼容 SQLite 与 MySQL，并保证重复执行/部分失败后重跑幂等。
-- 本迁移不搬运存量运行时数据：这些字段跨进程无意义，升级后直接丢弃。

DROP TABLE IF EXISTS device_runtime;
