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
-- media_resource_lists.items_json 是 []DeviceMedia 的 JSON 数组，
-- 时间只出现在 $[*].startTime 与 $[*].endTime，旧值为 UTC 标签的 RFC3339，
-- 例如 "2026-11-12T10:30:00Z"。保持设备墙钟不变、把 UTC 标签改成显式 +08:00，
-- 得到的就是真实瞬时值（等价于减 8 小时，且不需要日期运算）。
-- 该结构里除时间外没有以 Z 结尾的字符串字段（其余为数字或 null），
-- 因此按后缀替换是精确的，且对已是 +08:00 的值天然幂等。
-- 这张表是 0x1205 对 0x9205 查询的应答快照，不是定时上报；删除会让缓存列表
-- 在下一次手工查询前不可用，所以选择原地平移而不是清空。
--
-- 已知边界：旧数据中 BCD 解析失败、退回 time.Now() 的退化行也会被平移 8 小时；
-- 这类行的时间语义本身已损坏，正常数据中应不存在。
--
-- 回滚方式（必须先停服务并备份数据库，且必须在启动新版本前完成）：
--   UPDATE locations SET report_time_ms = report_time_ms + 28800000 WHERE report_time_ms > 0;
--   UPDATE alarms SET occurred_at_ms = occurred_at_ms + 28800000 WHERE occurred_at_ms > 0;
--   UPDATE media_files SET location_time = location_time + 28800000 WHERE location_time > 0;
--   UPDATE media_resource_lists
--     SET items_json = REPLACE(items_json, '+08:00"', 'Z"')
--     WHERE items_json LIKE '%+08:00"%';
-- 若新版本已经写入过资源列表，回滚会把新数据也改回伪 UTC 表示，
-- 因此回滚窗口仅限“迁移完成、新版本尚未对外服务”这段时间。
-- 本文件由 jt808-migrate 按版本执行一次；不要手工重复执行。
-- 方言约束同既有迁移文件：只使用 SQLite 与 MySQL 都支持的语法。

UPDATE locations SET report_time_ms = report_time_ms - 28800000 WHERE report_time_ms > 0;

UPDATE alarms SET occurred_at_ms = occurred_at_ms - 28800000 WHERE occurred_at_ms > 0;

UPDATE media_files SET location_time = location_time - 28800000 WHERE location_time > 0;

UPDATE media_resource_lists
SET items_json = REPLACE(items_json, 'Z"', '+08:00"')
WHERE items_json LIKE '%Z"%';

