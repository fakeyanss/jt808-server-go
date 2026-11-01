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
// 每次读取都从库里重建快照，调用方修改返回值不影响库内数据。
type deviceRepository struct {
	store *Store
}

const deviceColumns = `phone, id, plate, session_id, trans_proto, keepalive_ms,
	last_com_time_ms, status, version_desc, protocol_version, auth_code, imei,
	software_version, updated_at`

const (
	deviceInsert = `INSERT INTO devices (` + deviceColumns + `)
	VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	deviceUpsertSQLite = deviceInsert + `
	ON CONFLICT(phone) DO UPDATE SET
		id = excluded.id, plate = excluded.plate, session_id = excluded.session_id,
		trans_proto = excluded.trans_proto, keepalive_ms = excluded.keepalive_ms,
		last_com_time_ms = excluded.last_com_time_ms, status = excluded.status,
		version_desc = excluded.version_desc, protocol_version = excluded.protocol_version,
		auth_code = excluded.auth_code, imei = excluded.imei,
		software_version = excluded.software_version, updated_at = excluded.updated_at`

	deviceUpsertMySQL = deviceInsert + `
	ON DUPLICATE KEY UPDATE
		id = VALUES(id), plate = VALUES(plate), session_id = VALUES(session_id),
		trans_proto = VALUES(trans_proto), keepalive_ms = VALUES(keepalive_ms),
		last_com_time_ms = VALUES(last_com_time_ms), status = VALUES(status),
		version_desc = VALUES(version_desc), protocol_version = VALUES(protocol_version),
		auth_code = VALUES(auth_code), imei = VALUES(imei),
		software_version = VALUES(software_version), updated_at = VALUES(updated_at)`
)

// deviceRow 是 devices 表的行值，便于读写复用同一套列顺序。
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

func (r deviceRow) args() []any {
	return []any{
		r.phone, r.id, r.plate, r.sessionID, r.transProto, r.keepaliveMS,
		r.lastComTimeMS, r.status, r.versionDesc, r.protocolVersion, r.authCode,
		r.imei, r.softwareVersion, r.updatedAt,
	}
}

func (r deviceRow) device() *model.Device {
	lastCom := time.Time{}
	if r.lastComTimeMS != 0 {
		lastCom = time.UnixMilli(r.lastComTimeMS)
	}
	return &model.Device{
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
		`SELECT `+deviceColumns+` FROM devices WHERE phone = ?`, phone)
	device, err := scanDeviceRow(row)
	if errors.Is(err, stdsql.ErrNoRows) {
		return nil, repository.ErrDeviceNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("查询设备 %s 失败: %w", phone, err)
	}
	return device, nil
}

func (r *deviceRepository) GetByPlate(ctx context.Context, plate string) (*model.Device, error) {
	row := r.store.db.QueryRowContext(ctx,
		`SELECT `+deviceColumns+` FROM devices WHERE plate = ? ORDER BY updated_at DESC, phone ASC LIMIT 1`,
		plate)
	device, err := scanDeviceRow(row)
	if errors.Is(err, stdsql.ErrNoRows) {
		return nil, repository.ErrDeviceNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("查询车牌 %s 失败: %w", plate, err)
	}
	return device, nil
}

func (r *deviceRepository) List(ctx context.Context) ([]*model.Device, error) {
	rows, err := r.store.db.QueryContext(ctx, `SELECT `+deviceColumns+` FROM devices ORDER BY phone ASC`)
	if err != nil {
		return nil, fmt.Errorf("查询设备列表失败: %w", err)
	}
	defer rows.Close()

	devices := make([]*model.Device, 0)
	for rows.Next() {
		device, err := scanDeviceRow(rows)
		if err != nil {
			return nil, fmt.Errorf("扫描设备行失败: %w", err)
		}
		devices = append(devices, device)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("遍历设备列表失败: %w", err)
	}
	return devices, nil
}

func (r *deviceRepository) Upsert(ctx context.Context, device *model.Device) error {
	stmt := deviceUpsertSQLite
	if r.store.backend == BackendMySQL {
		stmt = deviceUpsertMySQL
	}
	if _, err := r.store.db.ExecContext(ctx, stmt, newDeviceRow(device).args()...); err != nil {
		return fmt.Errorf("写入设备 %s 失败: %w", device.Phone, err)
	}
	return nil
}

func (r *deviceRepository) DeleteByPhone(ctx context.Context, phone string) error {
	if _, err := r.store.db.ExecContext(ctx, `DELETE FROM devices WHERE phone = ?`, phone); err != nil {
		return fmt.Errorf("删除设备 %s 失败: %w", phone, err)
	}
	return nil
}

func (r *deviceRepository) DeleteByPlate(ctx context.Context, plate string) error {
	if _, err := r.store.db.ExecContext(ctx, `DELETE FROM devices WHERE plate = ?`, plate); err != nil {
		return fmt.Errorf("删除车牌 %s 失败: %w", plate, err)
	}
	return nil
}

var _ repository.DeviceRepository = (*deviceRepository)(nil)
