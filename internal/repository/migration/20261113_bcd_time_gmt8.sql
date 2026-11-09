-- 终端 BCD 时间从“伪 UTC”统一为固定 GMT+8（2026-11-13）。
--
-- 背景：旧版本用 time.Parse 解析 JT/T 808 BCD 时间，得到的是把设备墙钟时间
-- 标记为 UTC 的“伪瞬时值”。协议规定 BCD 时间为 GMT+8，因此同一墙钟的真实
-- 瞬时值应减去 8 小时，即 8 * 60 * 60 * 1000 = 28800000 毫秒。
--
-- 只平移由终端 BCD 时间派生的列：
--   * locations.report_time_ms  <- report_time_ms - 28800000
--   * alarms.occurred_at_ms     <- occurred_at_ms - 28800000（0 表示终端未给时间）
--   * media_files.location_time <- location_time - 28800000（0 表示未知）
-- 服务端接收/处理时间（received_at_ms、received_at、updated_at、created_at 等）
-- 一律不动；它们本来就是正确瞬时值，平移会引入新错误。
--
-- media_resource_lists.items_json 内嵌终端上报时间的 RFC3339 字符串，
-- 跨 SQLite/MySQL 无法逐元素平移；该表是 0x1205 整包覆盖的快照，
-- 直接清空，设备下次上报时按新口径重建。
--
-- 已知边界：旧数据中 BCD 解析失败、退回 time.Now() 的退化行也会被平移 8 小时；
-- 这类行的时间语义本身已损坏，正常数据中应不存在。
--
-- 回滚方式（必须先停服务并备份数据库）：对上述三列执行 + 28800000，
-- 并接受 media_resource_lists 已在迁移时被清空、需等设备重新上报。
-- 本文件由 jt808-migrate 按版本执行一次；不要手工重复执行。
-- 方言约束同既有迁移文件：只使用 SQLite 与 MySQL 都支持的语法。

UPDATE locations SET report_time_ms = report_time_ms - 28800000 WHERE report_time_ms > 0;

UPDATE alarms SET occurred_at_ms = occurred_at_ms - 28800000 WHERE occurred_at_ms > 0;

UPDATE media_files SET location_time = location_time - 28800000 WHERE location_time > 0;

DELETE FROM media_resource_lists;
