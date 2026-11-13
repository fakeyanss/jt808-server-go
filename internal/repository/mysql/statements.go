package mysql

import "github.com/fakeyanss/jt808-server-go/internal/repository/sqlcore"

// statements 是 MySQL 方言语句集。
//
// 只写 MySQL 自己的语法：冲突处理使用 INSERT IGNORE / ON DUPLICATE KEY UPDATE，
// 批量插入使用 INSERT IGNORE，流水号游标读取使用 SELECT ... FOR UPDATE 行锁。
type statements struct{}

var _ sqlcore.Statements = statements{}

// Statements 返回 MySQL 方言语句集。
func Statements() sqlcore.Statements { return statements{} }

const deviceStaticColumns = `phone, id, plate, keepalive_ms, version_desc, protocol_version,
	auth_code, imei, software_version, updated_at`

const attachmentColumns = `source, phone, alarm_no, file_name, terminal_id, alarm_sign, alarm_serial,
	file_type, total_size, received_bytes, watermark, staging_path, staging_base, ranges, segments,
	status, created_at, updated_at, completed_at`

func (statements) SchemaMigrationsTableExistsQuery() string {
	return `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema = DATABASE() AND table_name = 'schema_migrations'`
}

func (statements) UpsertDevice() string {
	return `INSERT INTO devices (` + deviceStaticColumns + `)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON DUPLICATE KEY UPDATE
		id = VALUES(id), plate = VALUES(plate), keepalive_ms = VALUES(keepalive_ms),
		version_desc = VALUES(version_desc), protocol_version = VALUES(protocol_version),
		auth_code = VALUES(auth_code), imei = VALUES(imei),
		software_version = VALUES(software_version), updated_at = VALUES(updated_at)`
}

func (statements) UpsertDeviceRegistration() string {
	return `INSERT INTO devices (phone, id, plate, keepalive_ms, version_desc,
		protocol_version, auth_code, imei, software_version, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, '', '', ?)
		ON DUPLICATE KEY UPDATE
			id = VALUES(id), plate = VALUES(plate),
			version_desc = VALUES(version_desc), protocol_version = VALUES(protocol_version),
			auth_code = VALUES(auth_code), updated_at = VALUES(updated_at)`
}

func (statements) UpsertDeviceParams() string {
	return `INSERT INTO device_params (phone, param_cnt, params_json, updated_at)
	VALUES (?, ?, ?, ?)
	ON DUPLICATE KEY UPDATE
		param_cnt = VALUES(param_cnt), params_json = VALUES(params_json),
		updated_at = VALUES(updated_at)`
}

func (statements) UpsertUpgradeResult() string {
	return `INSERT INTO upgrade_results (phone, serial, upgrade_type, result, received_at)
	VALUES (?, ?, ?, ?, ?)
	ON DUPLICATE KEY UPDATE
		upgrade_type = VALUES(upgrade_type), result = VALUES(result),
		received_at = VALUES(received_at)`
}

func (statements) InsertCommandIgnoringDuplicate() string {
	return `INSERT IGNORE INTO command_outbox
		(command_id, idempotency_key, phone, msg_id, serial, payload, status, attempts, last_error, created_at, updated_at,
		 last_attempt_at, next_attempt_at, expires_at, lease_owner, lease_expires_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`
}

func (statements) EnsureCommandSerialRow() string {
	// ON DUPLICATE KEY UPDATE 对已存在的行取得排他锁；INSERT IGNORE 只加共享锁，
	// 随后 SELECT ... FOR UPDATE 的锁升级在并发下会触发 1213 死锁。
	return `INSERT INTO command_serial (phone, next_serial, updated_at)
		VALUES (?, ?, ?) ON DUPLICATE KEY UPDATE next_serial = next_serial`
}

func (statements) LockCommandSerialRow() string { return " FOR UPDATE" }

func (statements) UpsertTerminalProperties() string {
	return `INSERT INTO terminal_properties (
		phone, terminal_type, manufacturer_id, terminal_model, terminal_id, iccid,
		hardware_version, firmware_version, gnss_properties, communication_properties,
		extra_json, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON DUPLICATE KEY UPDATE
		terminal_type = VALUES(terminal_type), manufacturer_id = VALUES(manufacturer_id),
		terminal_model = VALUES(terminal_model), terminal_id = VALUES(terminal_id),
		iccid = VALUES(iccid), hardware_version = VALUES(hardware_version),
		firmware_version = VALUES(firmware_version), gnss_properties = VALUES(gnss_properties),
		communication_properties = VALUES(communication_properties),
		extra_json = VALUES(extra_json), updated_at = VALUES(updated_at)`
}

func (statements) UpsertMediaResourceList() string {
	return `INSERT INTO media_resource_lists (phone, media_count, items_json, updated_at)
	VALUES (?, ?, ?, ?)
	ON DUPLICATE KEY UPDATE
		media_count = VALUES(media_count), items_json = VALUES(items_json),
		updated_at = VALUES(updated_at)`
}

func (statements) UpsertMediaUploadTask() string {
	return `INSERT INTO media_upload_tasks (phone, serial, payload_json, updated_at)
	VALUES (?, ?, ?, ?)
	ON DUPLICATE KEY UPDATE
		payload_json = VALUES(payload_json), updated_at = VALUES(updated_at)`
}

func (statements) UpsertMediaUploadNotice() string {
	return `INSERT INTO media_upload_notices (phone, serial, result, updated_at)
	VALUES (?, ?, ?, ?)
	ON DUPLICATE KEY UPDATE
		result = VALUES(result), updated_at = VALUES(updated_at)`
}

func (statements) UpsertMediaFile() string {
	return `INSERT INTO media_files (
		phone, media_id, media_type, media_format, event_id, channel_id,
		alarm_sign, status_sign, latitude, longitude, altitude, speed, direction,
		location_time, relative_path, file_size, md5, received_at)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON DUPLICATE KEY UPDATE
		media_type = VALUES(media_type), media_format = VALUES(media_format),
		event_id = VALUES(event_id), channel_id = VALUES(channel_id),
		alarm_sign = VALUES(alarm_sign), status_sign = VALUES(status_sign),
		latitude = VALUES(latitude), longitude = VALUES(longitude),
		altitude = VALUES(altitude), speed = VALUES(speed),
		direction = VALUES(direction), location_time = VALUES(location_time),
		relative_path = VALUES(relative_path), file_size = VALUES(file_size),
		md5 = VALUES(md5), received_at = VALUES(received_at)`
}

func (statements) UpsertMediaCapture() string {
	return `INSERT INTO media_captures (phone, record_id, answer_serial, result, received_at)
	VALUES (?, ?, ?, ?, ?)
	ON DUPLICATE KEY UPDATE
		answer_serial = VALUES(answer_serial), result = VALUES(result),
		received_at = VALUES(received_at)`
}

func (statements) UpsertAttachmentFile() string {
	return `INSERT INTO attachment_files (` + attachmentColumns + `)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON DUPLICATE KEY UPDATE
		terminal_id = VALUES(terminal_id), alarm_sign = VALUES(alarm_sign),
		alarm_serial = VALUES(alarm_serial), file_type = VALUES(file_type),
		total_size = VALUES(total_size),
		-- 进度列只增不减：迟到的旧快照不能把已提交的水位改小。
		received_bytes = GREATEST(received_bytes, VALUES(received_bytes)),
		watermark = GREATEST(watermark, VALUES(watermark)), staging_path = VALUES(staging_path),
		staging_base = VALUES(staging_base), ranges = VALUES(ranges),
		segments = VALUES(segments), status = VALUES(status),
		updated_at = VALUES(updated_at), completed_at = VALUES(completed_at)`
}

func (statements) InsertLocationsIgnoringDuplicates(placeholders string) string {
	return `INSERT IGNORE INTO locations
		(phone, record_id, msg_id, report_time_ms, received_at_ms,
		 alarm_sign, status_sign, latitude, longitude, altitude, speed, direction, payload_json, dedup_key)
		VALUES ` + placeholders
}

func (statements) EnsureTaskLeaseRow() string {
	return `INSERT IGNORE INTO task_leases (name, owner, expires_at, updated_at)
		VALUES (?, '', 0, ?)`
}

func (statements) EnsureDedupKeyRow() string {
	return `INSERT IGNORE INTO dedup_keys (dedup_key, expires_at, updated_at)
		VALUES (?, 0, ?)`
}
