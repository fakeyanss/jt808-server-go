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

const recordingSegmentColumns = `id, phone, channel, kind, object_key, local_path,
	start_at, end_at, size, keyframes, state, created_at`

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

// SelectCurrentTimeMillis 返回一条读取数据库当前时间（Unix 毫秒）的查询，
// 仅供时钟偏差探针观测，不参与任何判定。
func (statements) SelectCurrentTimeMillis() string {
	return `SELECT CAST(UNIX_TIMESTAMP(CURRENT_TIMESTAMP(3)) * 1000 AS SIGNED)`
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

func (statements) UpsertAttachmentFTPTask() string {
	return `INSERT INTO attachment_ftp_tasks
		(phone, serial, username, secret_hash, upload_path, channel, start_time, end_time,
		 alarm_sign, media_type, stream_type, storage_type, task_condition,
		 status, node_id, control_seq, total_bytes, revoked_at, created_at, updated_at, completed_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			username = VALUES(username), secret_hash = VALUES(secret_hash),
			upload_path = VALUES(upload_path), channel = VALUES(channel),
			start_time = VALUES(start_time), end_time = VALUES(end_time),
			alarm_sign = VALUES(alarm_sign), media_type = VALUES(media_type),
			stream_type = VALUES(stream_type), storage_type = VALUES(storage_type),
			task_condition = VALUES(task_condition), status = VALUES(status),
			node_id = VALUES(node_id),
			-- 控制序号只增不减：乱序到达的旧状态不能回退已下发的动作。
			control_seq = GREATEST(control_seq, VALUES(control_seq)),
			total_bytes = GREATEST(total_bytes, VALUES(total_bytes)), revoked_at = VALUES(revoked_at),
			updated_at = VALUES(updated_at), completed_at = VALUES(completed_at)`
}

func (statements) UpsertAttachmentNode() string {
	return `INSERT INTO attachment_nodes
		(node_id, instance_id, base_url, status, started_at, last_seen_at, expires_at)
		VALUES (?, ?, ?, 'online', ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			instance_id = VALUES(instance_id),
			base_url = VALUES(base_url),
			status = VALUES(status),
			started_at = VALUES(started_at),
			last_seen_at = VALUES(last_seen_at),
			expires_at = VALUES(expires_at)`
}

func (statements) UpsertRecordingSegment() string {
	return `INSERT INTO recording_segments (` + recordingSegmentColumns + `)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	ON DUPLICATE KEY UPDATE
		phone = VALUES(phone), channel = VALUES(channel), kind = VALUES(kind),
		object_key = VALUES(object_key), local_path = VALUES(local_path),
		start_at = VALUES(start_at), end_at = VALUES(end_at), size = VALUES(size),
		keyframes = VALUES(keyframes), state = VALUES(state)`
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

func (statements) UpsertGatewayNode() string {
	return `INSERT INTO gateway_nodes
		(gateway_id, instance_id, downlink_url, status, started_at, last_seen_at, expires_at)
		VALUES (?, ?, ?, 'online', ?, ?, ?)
		ON DUPLICATE KEY UPDATE
			instance_id = VALUES(instance_id),
			downlink_url = VALUES(downlink_url),
			status = VALUES(status),
			started_at = VALUES(started_at),
			last_seen_at = VALUES(last_seen_at),
			expires_at = VALUES(expires_at)`
}

func (statements) DeleteStaleSessionDirectory() string {
	return `DELETE FROM session_directory WHERE gateway_id = ? AND instance_id <> ?`
}

func (statements) SelectGatewayNodeStats() string {
	return `SELECT
		COALESCE(SUM(status = 'online' AND expires_at > ?), 0),
		COALESCE(SUM(status = 'offline'), 0),
		COALESCE(SUM(status = 'online' AND expires_at <= ?), 0)
		FROM gateway_nodes`
}

func (statements) HeartbeatGatewayNode() string {
	return `UPDATE gateway_nodes
		SET last_seen_at = ?, expires_at = ?, status = 'online'
		WHERE gateway_id = ? AND instance_id = ?`
}

func (statements) DeregisterGatewayNode() string {
	return `UPDATE gateway_nodes
		SET status = 'offline', expires_at = ?
		WHERE gateway_id = ? AND instance_id = ?`
}

func (statements) SelectGatewayNode() string {
	return `SELECT gateway_id, instance_id, downlink_url, status, started_at, last_seen_at, expires_at
		FROM gateway_nodes WHERE gateway_id = ?`
}

func (statements) SelectSessionForUpdate() string {
	return `SELECT generation, gateway_id, instance_id, session_id
		FROM session_directory WHERE phone = ? FOR UPDATE`
}

func (statements) InsertSessionDirectory() string {
	return `INSERT INTO session_directory
		(phone, gateway_id, instance_id, session_id, generation, claimed_at)
		VALUES (?, ?, ?, ?, 1, ?)`
}

func (statements) UpdateSessionDirectory() string {
	return `UPDATE session_directory
		SET gateway_id = ?, instance_id = ?, session_id = ?,
			generation = generation + 1, claimed_at = ?
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

func (statements) RouteSessionDownlink() string {
	return `SELECT g.downlink_url
		FROM session_directory s
		JOIN gateway_nodes g
			ON g.gateway_id = s.gateway_id AND g.instance_id = s.instance_id
		WHERE s.phone = ?
			AND g.status = 'online'
			AND g.expires_at > ?
			AND (? = '' OR s.session_id = ?)`
}
