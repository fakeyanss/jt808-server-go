package sqlcore

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

	// 鉴权只写终端信息（运行时在线态不落库，由各进程 runtime 注册表持有）。
	deviceAuthStateUpdateSQL = `UPDATE devices
		SET imei = ?, software_version = ?, updated_at = ?
		WHERE phone = ?`

	deviceExistsSQL = `SELECT COUNT(1) FROM devices WHERE phone = ?`
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
	ctx, cancel := readContext(ctx)
	defer cancel()
	row := r.store.readDB.QueryRowContext(ctx,
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
	ctx, cancel := readContext(ctx)
	defer cancel()
	row := r.store.readDB.QueryRowContext(ctx,
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
	ctx, cancel := readContext(ctx)
	defer cancel()
	rows, err := r.store.readDB.QueryContext(ctx,
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
	stmt := r.store.stmts.UpsertDevice()
	if _, err := r.store.db.ExecContext(ctx, stmt, row.args()...); err != nil {
		return fmt.Errorf("upsert device %s: %w", device.Phone, err)
	}
	return nil
}

// SaveRegistration 只写 0x0100 拥有的静态列。
//
// 更新时不触碰 imei/software_version：0x0100 与 0x0102 在集群中可能乱序，
// 整行覆盖会把鉴权写入的终端信息回退成空值。
func (r *deviceRepository) SaveRegistration(ctx context.Context, device *model.Device) error {
	if device == nil || device.Phone == "" {
		return fmt.Errorf("save registration: empty phone")
	}
	keepalive := device.Keepalive
	if keepalive <= 0 {
		keepalive = time.Minute
	}
	stmt := r.store.stmts.UpsertDeviceRegistration()
	if _, err := r.store.db.ExecContext(ctx, stmt,
		device.Phone, device.ID, device.Plate, keepalive.Milliseconds(),
		int16(device.VersionDesc), int16(device.ProtocolVersion), device.AuthCode,
		time.Now().UnixMilli(),
	); err != nil {
		return fmt.Errorf("save device registration %s: %w", device.Phone, err)
	}
	return nil
}

// SaveAuthState 只写 0x0102 拥有的终端信息；档案不存在返回 ErrDeviceNotFound。
// 线上状态、会话与最后通信时间属于进程内运行时状态，不写入 devices。
func (r *deviceRepository) SaveAuthState(ctx context.Context, device *model.Device) error {
	if device == nil || device.Phone == "" {
		return repository.ErrDeviceNotFound
	}
	result, err := r.store.db.ExecContext(ctx, deviceAuthStateUpdateSQL,
		device.IMEI, device.SoftwareVersion, time.Now().UnixMilli(), device.Phone)
	if err != nil {
		return fmt.Errorf("save device auth state %s: %w", device.Phone, err)
	}
	affected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("save device auth state %s rows affected: %w", device.Phone, err)
	}
	if affected > 0 {
		return nil
	}
	// MySQL 默认返回“实际变更行数”：值未变化时 affected=0，需再确认档案是否存在。
	var exists int
	if err := r.store.db.QueryRowContext(ctx, deviceExistsSQL, device.Phone).Scan(&exists); err != nil {
		return fmt.Errorf("check device %s: %w", device.Phone, err)
	}
	if exists == 0 {
		return repository.ErrDeviceNotFound
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
