package sqlstore

// 本文件集中放置建表 DDL。
// 类型选择同时兼容 SQLite 与 MySQL：VARCHAR 主键、BIGINT 时间戳、TEXT 承载 JSON。
// 大写列名保持与设计文档 §7 一致，避免两套方言的字段映射漂移。

const schemaMigrationsDDL = `
CREATE TABLE IF NOT EXISTS schema_migrations (
    version INTEGER PRIMARY KEY,
    applied_at BIGINT NOT NULL
)`

// devices 设备静态主数据（B 类，v1 形态）。
// 连接句柄 net.Conn 不可序列化，不入库。v3 会把 session_id/trans_proto/
// last_com_time_ms/status 四个运行时列迁出到 device_runtime，keepalive_ms 是代码
// 常量（model.NewDevice 固定 1 分钟），作为配置列保留在本表。
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

// terminal_properties 终端属性档案（B 类），只承载 0x0107 查询应答。
// 常用属性列化便于检索；extra_json 兜底协议扩展的长尾字段。
// 本表与 devices 独立：属性应答不回写 devices，避免触发其非主键列全量重写。
const terminalPropertiesDDL = `
CREATE TABLE IF NOT EXISTS terminal_properties (
    phone VARCHAR(32) PRIMARY KEY,
    terminal_type INTEGER NOT NULL DEFAULT 0,
    manufacturer_id VARCHAR(16) NOT NULL DEFAULT '',
    terminal_model VARCHAR(64) NOT NULL DEFAULT '',
    terminal_id VARCHAR(64) NOT NULL DEFAULT '',
    iccid VARCHAR(32) NOT NULL DEFAULT '',
    hardware_version VARCHAR(255) NOT NULL DEFAULT '',
    firmware_version VARCHAR(255) NOT NULL DEFAULT '',
    gnss_properties SMALLINT NOT NULL DEFAULT 0,
    communication_properties SMALLINT NOT NULL DEFAULT 0,
    extra_json TEXT NOT NULL DEFAULT '{}',
    updated_at BIGINT NOT NULL
)`

// device_runtime 设备运行时投影（B 类），phone 与 devices 同主键。
// 与 devices 拆表的理由：静态字段来自 0x0100/0x0102 主动上报，运行时列由心跳/位置等
// 高频消息刷新，二者写频率与生命周期不同。连接标识 session_id 只在进程内有效。
// last_com_time_ms 仅表示"上次交互时间"，不作为在线判定依据。
const deviceRuntimeDDL = `
CREATE TABLE IF NOT EXISTS device_runtime (
    phone VARCHAR(32) PRIMARY KEY,
    session_id VARCHAR(128) NOT NULL DEFAULT '',
    trans_proto VARCHAR(16) NOT NULL DEFAULT '',
    last_com_time_ms BIGINT NOT NULL DEFAULT 0,
    status SMALLINT NOT NULL DEFAULT 0,
    updated_at BIGINT NOT NULL
)`

// v3 把运行时列从 devices 拆到 device_runtime。存量运行时值（session_id/status）跨进程
// 本就无意义，迁移不搬运数据；启动时由 Store 统一清空 session_id 并把 status 归 offline。
const (
	dropDeviceRuntimeColumnSessionID   = `ALTER TABLE devices DROP COLUMN session_id`
	dropDeviceRuntimeColumnTransProto  = `ALTER TABLE devices DROP COLUMN trans_proto`
	dropDeviceRuntimeColumnLastComTime = `ALTER TABLE devices DROP COLUMN last_com_time_ms`
	dropDeviceRuntimeColumnStatus      = `ALTER TABLE devices DROP COLUMN status`
)

type migration struct {
	version int
	stmts   []string
}

var migrations = []migration{
	{version: 1, stmts: []string{devicesDDL, deviceParamsDDL}},
	{version: 2, stmts: []string{terminalPropertiesDDL}},
	{version: 3, stmts: []string{
		deviceRuntimeDDL,
		dropDeviceRuntimeColumnSessionID,
		dropDeviceRuntimeColumnTransProto,
		dropDeviceRuntimeColumnLastComTime,
		dropDeviceRuntimeColumnStatus,
	}},
}
