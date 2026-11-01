package sqlstore

// 本文件集中放置建表 DDL。
// 类型选择同时兼容 SQLite 与 MySQL：VARCHAR 主键、BIGINT 时间戳、TEXT 承载 JSON。
// 大写列名保持与设计文档 §7 一致，避免两套方言的字段映射漂移。

const schemaMigrationsDDL = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at BIGINT NOT NULL
)`

// devices 设备主数据（B 类）。
// 连接句柄 net.Conn 不可序列化，不入库；session_id/status 等运行时投影按现有
// DeviceRepository 的快照语义一并持久化，拆分 device_runtime 留待后续里程碑。
const devicesDDL = `
CREATE TABLE IF NOT EXISTS devices (
    phone VARCHAR(32) PRIMARY KEY,
    id VARCHAR(128) NOT NULL DEFAULT '',
    plate VARCHAR(64) NOT NULL DEFAULT '',
    session_id VARCHAR(128) NOT NULL DEFAULT '',
    trans_proto VARCHAR(16) NOT NULL DEFAULT '',
    keepalive_ms BIGINT NOT NULL DEFAULT 0,
    last_com_time_ms BIGINT NOT NULL DEFAULT 0,
    status SMALLINT NOT NULL DEFAULT 0,
    version_desc SMALLINT NOT NULL DEFAULT 0,
    protocol_version SMALLINT NOT NULL DEFAULT 0,
    auth_code VARCHAR(64) NOT NULL DEFAULT '',
    imei VARCHAR(64) NOT NULL DEFAULT '',
    software_version VARCHAR(64) NOT NULL DEFAULT '',
    updated_at BIGINT NOT NULL
)`

// device_params 终端参数（B 类），按设计决策 D1 使用整体 JSON 单字段。
const deviceParamsDDL = `
CREATE TABLE IF NOT EXISTS device_params (
    phone VARCHAR(32) PRIMARY KEY,
    param_cnt SMALLINT NOT NULL DEFAULT 0,
    params_json TEXT NOT NULL,
    updated_at BIGINT NOT NULL
)`

type migration struct {
	version int
	stmts   []string
}

var migrations = []migration{
	{version: 1, stmts: []string{devicesDDL, deviceParamsDDL}},
}
