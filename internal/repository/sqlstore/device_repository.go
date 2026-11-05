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
// 只持久化设备静态档案（devices）；session/在线态/最后通信时间等运行时状态
// 由进程内 runtime 注册表持有，重启后自然归零。
// 因此读路径返回的 model.Device 运行时字段恒为零值（离线、空会话、零时间）。
type deviceRepository struct {
	store *Store
}

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
)

// deviceRow 是 devices 静态行值，便于读写复用同一套列顺序。
type deviceRow struct {
	phone           string
	id              string
	plate           string
	keepaliveMS     int64
	versionDesc     int16
	protocolVersion int16
	authCode        string
	imei            string
	softwareVersion string
	updatedAt       int64
}

func newDeviceRow(d *model.Device) deviceRow {
	return deviceRow{
		phone:           d.Phone,
		id:              d.ID,
		plate:           d.Plate,
		keepaliveMS:     d.Keepalive.Milliseconds(),
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
		r.phone, r.id, r.plate, r.keepaliveMS, r.versionDesc, r.protocolVersion,
		r.authCode, r.imei, r.softwareVersion, r.updatedAt,
	}
}

// device 只填充静态档案；运行时字段保留 model.Device 的零值。
func (r deviceRow) device() *model.Device {
	return &model.Device{
		ID:              r.id,
		Plate:           r.plate,
		Phone:           r.phone,
		Keepalive:       time.Duration(r.keepaliveMS) * time.Millisecond,
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
		&r.phone, &r.id, &r.plate, &r.keepaliveMS, &r.versionDesc, &r.protocolVersion,
		&r.authCode, &r.imei, &r.softwareVersion, &r.updatedAt,
	); err != nil {
		return nil, err
	}
	return r.device(), nil
}

func (r *deviceRepository) GetByPhone(ctx context.Context, phone string) (*model.Device, error) {
	row := r.store.db.QueryRowContext(ctx,
		`SELECT `+deviceStaticColumns+` FROM devices WHERE phone = ?`, phone)
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
		`SELECT `+deviceStaticColumns+`
		FROM devices WHERE plate = ? ORDER BY updated_at DESC, phone ASC LIMIT 1`, plate)
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
		`SELECT `+deviceStaticColumns+` FROM devices ORDER BY phone ASC`)
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

// Upsert 只写 devices 静态列；设备结构体上的运行时字段被忽略，不落库。
func (r *deviceRepository) Upsert(ctx context.Context, device *model.Device) error {
	row := newDeviceRow(device)
	stmt := deviceUpsertSQLite
	if r.store.backend == BackendMySQL {
		stmt = deviceUpsertMySQL
	}
	if _, err := r.store.db.ExecContext(ctx, stmt, row.args()...); err != nil {
		return fmt.Errorf("upsert device %s: %w", device.Phone, err)
	}
	return nil
}

func (r *deviceRepository) DeleteByPhone(ctx context.Context, phone string) error {
	if _, err := r.store.db.ExecContext(ctx, `DELETE FROM devices WHERE phone = ?`, phone); err != nil {
		return fmt.Errorf("delete device %s: %w", phone, err)
	}
	return nil
}

func (r *deviceRepository) DeleteByPlate(ctx context.Context, plate string) error {
	if _, err := r.store.db.ExecContext(ctx, `DELETE FROM devices WHERE plate = ?`, plate); err != nil {
		return fmt.Errorf("delete devices by plate %s: %w", plate, err)
	}
	return nil
}

var _ repository.DeviceRepository = (*deviceRepository)(nil)
