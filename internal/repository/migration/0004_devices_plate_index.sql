-- 0004 设备车牌普通索引（非唯一）。
-- 设备身份仍以 phone 为准，plate 允许重复（空车牌或同牌设备），
-- 该索引只用于加速 GetByPlate 查询。
CREATE INDEX idx_devices_plate ON devices (plate);
