package sqlite

import "github.com/fakeyanss/jt808-server-go/internal/repository/sqlcore"

// statements 是 SQLite 方言语句集。
//
// 只写 SQLite 自己的语法：冲突处理使用 ON CONFLICT ... DO UPDATE/NOTHING，
// 批量插入使用 INSERT OR IGNORE；SQLite 是单写者模型，流水号游标不加行锁。
type statements struct{}

var _ sqlcore.Statements = statements{}

// Statements 返回 SQLite 方言语句集。
func Statements() sqlcore.Statements { return statements{} }

const deviceStaticColumns = `phone, id, plate, keepalive_ms, version_desc, protocol_version,
	auth_code, imei, software_version, updated_at`

const attachmentColumns = `source, phone, alarm_no, file_name, terminal_id, alarm_sign, alarm_serial,
	file_type, total_size, received_bytes, watermark, staging_path, staging_base, ranges, segments,
	status, created_at, updated_at, completed_at`

func (statements) SchemaMigrationsTableExistsQuery() string {
	return `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'schema_migrations'`
}

func (statements) UpsertDevice() string {
	return `INSERT INTO devices (` + deviceStaticColumns + `)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(phone) DO UPDATE SET
		id = excluded.id, plate = excluded.plate, keepalive_ms = excluded.keepalive_ms,
		version_desc = excluded.version_desc, protocol_version = excluded.protocol_version,
		auth_code = excluded.auth_code, imei = excluded.imei,
		software_version = excluded.software_version, updated_at = excluded.updated_at`
}

func (statements) UpsertDeviceRegistration() string {
	return `INSERT INTO devices (phone, id, plate, keepalive_ms, version_desc,
		protocol_version, auth_code, imei, software_version, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, '', '', ?)
		ON CONFLICT(phone) DO UPDATE SET
			id = excluded.id, plate = excluded.plate,
			version_desc = excluded.version_desc, protocol_version = excluded.protocol_version,
			auth_code = excluded.auth_code, updated_at = excluded.updated_at`
}

func (statements) UpsertDeviceParams() string {
	return `INSERT INTO device_params (phone, param_cnt, params_json, updated_at)
	VALUES (?, ?, ?, ?)
	ON CONFLICT(phone) DO UPDATE SET
		param_cnt = excluded.param_cnt, params_json = excluded.params_json,
		updated_at = excluded.updated_at`
}

func (statements) UpsertUpgradeResult() string {
	return `INSERT INTO upgrade_results (phone, serial, upgrade_type, result, received_at)
	VALUES (?, ?, ?, ?, ?)
	ON CONFLICT(phone, serial) DO UPDATE SET
		upgrade_type = excluded.upgrade_type, result = excluded.result,
		received_at = excluded.received_at`
}

func (statements) InsertCommandIgnoringDuplicate() string {
	return `INSERT INTO command_outbox
		(command_id, idempotency_key, phone, msg_id, serial, payload, status, attempts, last_error, created_at, updated_at,
		 last_attempt_at, next_attempt_at, expires_at, lease_owner, lease_expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(phone, idempotency_key) DO NOTHING`
}

func (statements) EnsureCommandSerialRow() string {
	return `INSERT INTO command_serial (phone, next_serial, updated_at)
		VALUES (?, ?, ?) ON CONFLICT(phone) DO NOTHING`
}

func (statements) LockCommandSerialRow() string { return "" }

func (statements) UpsertTerminalProperties() string {
	return `INSERT INTO terminal_properties (
		phone, terminal_type, manufacturer_id, terminal_model, terminal_id, iccid,
		hardware_version, firmware_version, gnss_properties, communication_properties,
		extra_json, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(phone) DO UPDATE SET
		terminal_type = excluded.terminal_type, manufacturer_id = excluded.manufacturer_id,
		terminal_model = excluded.terminal_model, terminal_id = excluded.terminal_id,
		iccid = excluded.iccid, hardware_version = excluded.hardware_version,
		firmware_version = excluded.firmware_version, gnss_properties = excluded.gnss_properties,
		communication_properties = excluded.communication_properties,
		extra_json = excluded.extra_json, updated_at = excluded.updated_at`
}

func (statements) UpsertMediaResourceList() string {
	return `INSERT INTO media_resource_lists (phone, media_count, items_json, updated_at)
	VALUES (?, ?, ?, ?)
	ON CONFLICT(phone) DO UPDATE SET
		media_count = excluded.media_count, items_json = excluded.items_json,
		updated_at = excluded.updated_at`
}

func (statements) UpsertMediaUploadTask() string {
	return `INSERT INTO media_upload_tasks (phone, serial, payload_json, updated_at)
	VALUES (?, ?, ?, ?)
	ON CONFLICT(phone, serial) DO UPDATE SET
		payload_json = excluded.payload_json, updated_at = excluded.updated_at`
}

func (statements) UpsertMediaUploadNotice() string {
	return `INSERT INTO media_upload_notices (phone, serial, result, updated_at)
	VALUES (?, ?, ?, ?)
	ON CONFLICT(phone, serial) DO UPDATE SET
		result = excluded.result, updated_at = excluded.updated_at`
}

func (statements) UpsertMediaFile() string {
	return `INSERT INTO media_files (
		phone, media_id, media_type, media_format, event_id, channel_id,
		alarm_sign, status_sign, latitude, longitude, altitude, speed, direction,
		location_time, relative_path, file_size, md5, received_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(phone, media_id) DO UPDATE SET
		media_type = excluded.media_type, media_format = excluded.media_format,
		event_id = excluded.event_id, channel_id = excluded.channel_id,
		alarm_sign = excluded.alarm_sign, status_sign = excluded.status_sign,
		latitude = excluded.latitude, longitude = excluded.longitude,
		altitude = excluded.altitude, speed = excluded.speed,
		direction = excluded.direction, location_time = excluded.location_time,
		relative_path = excluded.relative_path, file_size = excluded.file_size,
		md5 = excluded.md5, received_at = excluded.received_at`
}

func (statements) UpsertMediaCapture() string {
	return `INSERT INTO media_captures (phone, record_id, answer_serial, result, received_at)
	VALUES (?, ?, ?, ?, ?)
	ON CONFLICT(phone, record_id) DO UPDATE SET
		answer_serial = excluded.answer_serial, result = excluded.result,
		received_at = excluded.received_at`
}

func (statements) UpsertAttachmentFile() string {
	return `INSERT INTO attachment_files (` + attachmentColumns + `)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON CONFLICT(source, phone, alarm_no, file_name) DO UPDATE SET
		terminal_id = excluded.terminal_id, alarm_sign = excluded.alarm_sign,
		alarm_serial = excluded.alarm_serial, file_type = excluded.file_type,
		total_size = excluded.total_size,
		-- 进度列只增不减：迟到的旧快照不能把已提交的水位改小。
		received_bytes = MAX(received_bytes, excluded.received_bytes),
		watermark = MAX(watermark, excluded.watermark), staging_path = excluded.staging_path,
		staging_base = excluded.staging_base, ranges = excluded.ranges,
		segments = excluded.segments, status = excluded.status,
		updated_at = excluded.updated_at, completed_at = excluded.completed_at`
}

func (statements) InsertLocationsIgnoringDuplicates(placeholders string) string {
	return `INSERT OR IGNORE INTO locations
		(phone, record_id, msg_id, report_time_ms, received_at_ms,
		 alarm_sign, status_sign, latitude, longitude, altitude, speed, direction, payload_json, dedup_key)
		VALUES ` + placeholders
}

func (statements) EnsureTaskLeaseRow() string {
	return `INSERT OR IGNORE INTO task_leases (name, owner, expires_at, updated_at)
		VALUES (?, '', 0, ?)`
}

func (statements) EnsureDedupKeyRow() string {
	return `INSERT OR IGNORE INTO dedup_keys (dedup_key, expires_at, updated_at)
		VALUES (?, 0, ?)`
}

func (statements) UpsertGatewayNode() string {
	return `INSERT INTO gateway_nodes
		(gateway_id, instance_id, downlink_url, status, started_at, last_seen_at, expires_at)
		VALUES (?, ?, ?, 'online',
			strftime('%Y-%m-%d %H:%M:%f','now'),
			strftime('%Y-%m-%d %H:%M:%f','now'),
			strftime('%Y-%m-%d %H:%M:%f','now','+30 seconds'))
		ON CONFLICT(gateway_id) DO UPDATE SET
			instance_id = excluded.instance_id,
			downlink_url = excluded.downlink_url,
			status = excluded.status,
			started_at = excluded.started_at,
			last_seen_at = excluded.last_seen_at,
			expires_at = excluded.expires_at`
}

func (statements) HeartbeatGatewayNode() string {
	return `UPDATE gateway_nodes
		SET last_seen_at = strftime('%Y-%m-%d %H:%M:%f','now'),
			expires_at = strftime('%Y-%m-%d %H:%M:%f','now','+30 seconds'),
			status = 'online'
		WHERE gateway_id = ? AND instance_id = ?`
}

func (statements) DeregisterGatewayNode() string {
	return `UPDATE gateway_nodes
		SET status = 'offline',
			expires_at = strftime('%Y-%m-%d %H:%M:%f','now')
		WHERE gateway_id = ? AND instance_id = ?`
}

func (statements) SelectGatewayNode() string {
	return `SELECT gateway_id, instance_id, downlink_url, status, started_at, last_seen_at, expires_at
		FROM gateway_nodes WHERE gateway_id = ?`
}

func (statements) SelectSessionForUpdate() string {
	return `SELECT generation, gateway_id, instance_id, session_id
		FROM session_directory WHERE phone = ?`
}

func (statements) InsertSessionDirectory() string {
	return `INSERT INTO session_directory
		(phone, gateway_id, instance_id, session_id, generation, claimed_at)
		VALUES (?, ?, ?, ?, 1, strftime('%Y-%m-%d %H:%M:%f','now'))`
}

func (statements) UpdateSessionDirectory() string {
	return `UPDATE session_directory
		SET gateway_id = ?, instance_id = ?, session_id = ?,
			generation = generation + 1,
			claimed_at = strftime('%Y-%m-%d %H:%M:%f','now')
		WHERE phone = ?`
}

func (statements) SelectSessionGeneration() string {
	return `SELECT generation FROM session_directory WHERE phone = ?`
}

func (statements) DeleteSessionDirectory() string {
	return `DELETE FROM session_directory
		WHERE phone = ? AND gateway_id = ? AND instance_id = ? AND session_id = ? AND generation = ?`
}

func (statements) ValidateSessionDirectory() string {
	return `SELECT 1 FROM session_directory
		WHERE phone = ? AND gateway_id = ? AND instance_id = ? AND session_id = ? AND generation <= ?`
}

func (statements) SelectSessionDirectory() string {
	return `SELECT phone, gateway_id, instance_id, session_id, generation, claimed_at
		FROM session_directory WHERE phone = ?`
}
