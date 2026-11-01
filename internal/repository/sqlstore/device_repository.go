package sqlstore

import (
	"context"
	stdsql "database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// deviceRepository 是 repository.DeviceRepository 的 SQL 实现。
// 静态字段存 devices，运行时投影存 device_runtime；读取时按 phone 合并两表，
// 对外仍返回完整的 model.Device 快照，调用方修改返回值不影响库内数据。
type deviceRepository struct {
	store *Store
}

// deviceSelectColumns 按 phone 合并两表。device_runtime 可能尚无行（新设备首次写入前），
// 用 COALESCE 保证列扫描出零值。
const deviceSelectColumns = `d.phone, d.id, d.plate, COALESCE(r.session_id, ''),
	COALESCE(r.trans_proto, ''), d.keepalive_ms, COALESCE(r.last_com_time_ms, 0),
	COALESCE(r.status, 0), d.version_desc, d.protocol_version, d.auth_code, d.imei,
	d.software_version, d.updated_at`

const (
	deviceStaticColumns = `phone, id, plate, keepalive_ms, version_desc, protocol_version,
		auth_code, imei, software_version, updated_at`

	deviceInsert = `INSERT INTO devices (` + deviceStaticColumns + `)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	deviceUpsertSQLite = deviceInsert + `
	ON CONFLICT(phone) DO UPDATE SET
		id = excluded.id, plate = excluded.plate, keepalive_ms = excluded.keepalive_ms,
		version_desc = excluded.version_desc, protocol_version = excluded.protocol_version,
		auth_code = excluded.auth_code, imei = excluded.imei,
		software_version = excluded.software_version, updated_at = excluded.updated_at`

	deviceUpsertMySQL = deviceInsert + `
	ON DUPLICATE KEY UPDATE
		id = VALUES(id), plate = VALUES(plate), keepalive_ms = VALUES(keepalive_ms),
		version_desc = VALUES(version_desc), protocol_version = VALUES(protocol_version),
		auth_code = VALUES(auth_code), imei = VALUES(imei),
		software_version = VALUES(software_version), updated_at = VALUES(updated_at)`

	deviceRuntimeInsert = `INSERT INTO device_runtime
		(phone, session_id, trans_proto, last_com_time_ms, status, updated_at)
		VALUES (?, ?, ?, ?, ?, ?)`

	deviceRuntimeUpsertSQLite = deviceRuntimeInsert + `
	ON CONFLICT(phone) DO UPDATE SET
		session_id = excluded.session_id, trans_proto = excluded.trans_proto,
		last_com_time_ms = excluded.last_com_time_ms, status = excluded.status,
		updated_at = excluded.updated_at`

	deviceRuntimeUpsertMySQL = deviceRuntimeInsert + `
	ON DUPLICATE KEY UPDATE
		session_id = VALUES(session_id), trans_proto = VALUES(trans_proto),
		last_com_time_ms = VALUES(last_com_time_ms), status = VALUES(status),
		updated_at = VALUES(updated_at)`
)

// deviceRow 是 devices + device_runtime 合并后的行值，便于读写复用同一套列顺序。
type deviceRow struct {
	phone           string
	id              string
	plate           string
	sessionID       string
	transProto      string
	keepaliveMS     int64
	lastComTimeMS   int64
	status          int16
	versionDesc     int16
	protocolVersion int16
	authCode        string
	imei            string
	softwareVersion string
	updatedAt       int64
}

func newDeviceRow(d *model.Device) deviceRow {
	lastCom := int64(0)
	if !d.LastestComTime.IsZero() {
		lastCom = d.LastestComTime.UnixMilli()
	}
	return deviceRow{
		phone:           d.Phone,
		id:              d.ID,
		plate:           d.Plate,
		sessionID:       d.SessionID,
		transProto:      string(d.TransProto),
		keepaliveMS:     d.Keepalive.Milliseconds(),
		lastComTimeMS:   lastCom,
		status:          int16(d.Status),
		versionDesc:     int16(d.VersionDesc),
		protocolVersion: int16(d.ProtocolVersion),
		authCode:        d.AuthCode,
		imei:            d.IMEI,
		softwareVersion: d.SoftwareVersion,
		updatedAt:       time.Now().UnixMilli(),
	}
}

// staticArgs 只包含 devices 归属列，避免运行时高频刷新导致静态列无谓重写。
func (r deviceRow) staticArgs() []any {
	return []any{
		r.phone, r.id, r.plate, r.keepaliveMS, r.versionDesc, r.protocolVersion,
		r.authCode, r.imei, r.softwareVersion, r.updatedAt,
	}
}

// runtimeArgs 只包含 device_runtime 归属列。
func (r deviceRow) runtimeArgs() []any {
	return []any{
		r.phone, r.sessionID, r.transProto, r.lastComTimeMS, r.status, r.updatedAt,
	}
}

func (r deviceRow) device() *model.Device {
	lastCom := time.Time{}
	if r.lastComTimeMS != 0 {
		lastCom = time.UnixMilli(r.lastComTimeMS)
	}
	d := &model.Device{
		ID:              r.id,
		Plate:           r.plate,
		Phone:           r.phone,
		SessionID:       r.sessionID,
		TransProto:      model.TransportProtocol(r.transProto),
		Keepalive:       time.Duration(r.keepaliveMS) * time.Millisecond,
		LastestComTime:  lastCom,
		Status:          model.DeviceStatus(r.status),
		VersionDesc:     model.VersionType(r.versionDesc),
		ProtocolVersion: uint8(r.protocolVersion),
		AuthCode:        r.authCode,
		IMEI:            r.imei,
		SoftwareVersion: r.softwareVersion,
	}
	// 运行时投影带 TTL：非离线但已超过 keepalive + last_com_time 的残留行，
	// 在读取时收敛为离线并清空 session_id，避免崩溃重启后把死连接当成在线。
	// last_com_time 本身保留，只作"上次在线时间"展示。
	if d.Status != model.DeviceStatusOffline &&
		time.Now().UnixMilli() > r.lastComTimeMS+r.keepaliveMS {
		d.Status = model.DeviceStatusOffline
		d.SessionID = ""
	}
	return d
}

func scanDeviceRow(scanner interface{ Scan(...any) error }) (*model.Device, error) {
	var r deviceRow
	if err := scanner.Scan(
		&r.phone, &r.id, &r.plate, &r.sessionID, &r.transProto, &r.keepaliveMS,
		&r.lastComTimeMS, &r.status, &r.versionDesc, &r.protocolVersion, &r.authCode,
		&r.imei, &r.softwareVersion, &r.updatedAt,
	); err != nil {
		return nil, err
	}
	return r.device(), nil
}

func (r *deviceRepository) GetByPhone(ctx context.Context, phone string) (*model.Device, error) {
	row := r.store.db.QueryRowContext(ctx,
		`SELECT `+deviceSelectColumns+`
		FROM devices d LEFT JOIN device_runtime r ON r.phone = d.phone
		WHERE d.phone = ?`, phone)
	device, err := scanDeviceRow(row)
	if errors.Is(err, stdsql.ErrNoRows) {
		return nil, repository.ErrDeviceNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get device %s: %w", phone, err)
	}
	return device, nil
}

func (r *deviceRepository) GetByPlate(ctx context.Context, plate string) (*model.Device, error) {
	row := r.store.db.QueryRowContext(ctx,
		`SELECT `+deviceSelectColumns+`
		FROM devices d LEFT JOIN device_runtime r ON r.phone = d.phone
		WHERE d.plate = ? ORDER BY d.updated_at DESC, d.phone ASC LIMIT 1`, plate)
	device, err := scanDeviceRow(row)
	if errors.Is(err, stdsql.ErrNoRows) {
		return nil, repository.ErrDeviceNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get device by plate %s: %w", plate, err)
	}
	return device, nil
}

func (r *deviceRepository) List(ctx context.Context) ([]*model.Device, error) {
	rows, err := r.store.db.QueryContext(ctx,
		`SELECT `+deviceSelectColumns+`
		FROM devices d LEFT JOIN device_runtime r ON r.phone = d.phone
		ORDER BY d.phone ASC`)
	if err != nil {
		return nil, fmt.Errorf("list devices: %w", err)
	}
	defer rows.Close()

	devices := make([]*model.Device, 0)
	for rows.Next() {
		device, err := scanDeviceRow(rows)
		if err != nil {
			return nil, fmt.Errorf("scan device row: %w", err)
		}
		devices = append(devices, device)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate device rows: %w", err)
	}
	return devices, nil
}

// Upsert 在同一事务内按归属列分写两张表，保证读路径不会看到半条设备。
func (r *deviceRepository) Upsert(ctx context.Context, device *model.Device) error {
	row := newDeviceRow(device)
	deviceStmt, runtimeStmt := deviceUpsertSQLite, deviceRuntimeUpsertSQLite
	if r.store.backend == BackendMySQL {
		deviceStmt, runtimeStmt = deviceUpsertMySQL, deviceRuntimeUpsertMySQL
	}

	tx, err := r.store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin device upsert %s: %w", device.Phone, err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, deviceStmt, row.staticArgs()...); err != nil {
		return fmt.Errorf("upsert device %s: %w", device.Phone, err)
	}
	if _, err := tx.ExecContext(ctx, runtimeStmt, row.runtimeArgs()...); err != nil {
		return fmt.Errorf("upsert device runtime %s: %w", device.Phone, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit device upsert %s: %w", device.Phone, err)
	}
	return nil
}

func (r *deviceRepository) DeleteByPhone(ctx context.Context, phone string) error {
	tx, err := r.store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin device delete %s: %w", phone, err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx, `DELETE FROM device_runtime WHERE phone = ?`, phone); err != nil {
		return fmt.Errorf("delete device runtime %s: %w", phone, err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM devices WHERE phone = ?`, phone); err != nil {
		return fmt.Errorf("delete device %s: %w", phone, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit device delete %s: %w", phone, err)
	}
	return nil
}

func (r *deviceRepository) DeleteByPlate(ctx context.Context, plate string) error {
	tx, err := r.store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin delete devices by plate %s: %w", plate, err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM device_runtime WHERE phone IN (SELECT phone FROM devices WHERE plate = ?)`,
		plate); err != nil {
		return fmt.Errorf("delete device runtime by plate %s: %w", plate, err)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM devices WHERE plate = ?`, plate); err != nil {
		return fmt.Errorf("delete devices by plate %s: %w", plate, err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit delete devices by plate %s: %w", plate, err)
	}
	return nil
}

var _ repository.DeviceRepository = (*deviceRepository)(nil)
