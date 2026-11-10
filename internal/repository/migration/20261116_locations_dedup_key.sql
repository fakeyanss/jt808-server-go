-- E2：定位历史按事件幂等写入。
-- dedup_key = hash(eventId + itemIndex)，老数据与 standalone 写入保持 NULL；
-- 唯一索引允许多个 NULL，因此不影响既有数据。
ALTER TABLE locations ADD COLUMN dedup_key VARCHAR(64) NULL;
CREATE UNIQUE INDEX uq_locations_phone_dedup_key ON locations (phone, dedup_key);
