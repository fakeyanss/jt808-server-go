package sqlstore

import (
	"context"
	stdsql "database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// terminalPropertiesRepository 是 repository.TerminalPropertiesRepository 的 SQL 实现。
// 除落库外，还在内存中保留每台设备的最新属性值，供同进程内查询直接命中。
type terminalPropertiesRepository struct {
	store *Store

	mu     sync.RWMutex
	latest map[string]*model.TerminalProperties
}

const (
	propertiesInsert = `INSERT INTO terminal_properties (
		phone, terminal_type, manufacturer_id, terminal_model, terminal_id, iccid,
		hardware_version, firmware_version, gnss_properties, communication_properties,
		extra_json, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`

	propertiesUpsertSQLite = propertiesInsert + `
	ON CONFLICT(phone) DO UPDATE SET
		terminal_type = excluded.terminal_type, manufacturer_id = excluded.manufacturer_id,
		terminal_model = excluded.terminal_model, terminal_id = excluded.terminal_id,
		iccid = excluded.iccid, hardware_version = excluded.hardware_version,
		firmware_version = excluded.firmware_version, gnss_properties = excluded.gnss_properties,
		communication_properties = excluded.communication_properties,
		extra_json = excluded.extra_json, updated_at = excluded.updated_at`

	propertiesUpsertMySQL = propertiesInsert + `
	ON DUPLICATE KEY UPDATE
		terminal_type = VALUES(terminal_type), manufacturer_id = VALUES(manufacturer_id),
		terminal_model = VALUES(terminal_model), terminal_id = VALUES(terminal_id),
		iccid = VALUES(iccid), hardware_version = VALUES(hardware_version),
		firmware_version = VALUES(firmware_version), gnss_properties = VALUES(gnss_properties),
		communication_properties = VALUES(communication_properties),
		extra_json = VALUES(extra_json), updated_at = VALUES(updated_at)`
)

const propertiesColumns = `phone, terminal_type, manufacturer_id, terminal_model, terminal_id, iccid,
	hardware_version, firmware_version, gnss_properties, communication_properties, extra_json, updated_at`

// Get 先读进程内最新值，未命中再回源库并回填。
func (r *terminalPropertiesRepository) Get(ctx context.Context, phone string) (*model.TerminalProperties, error) {
	r.mu.RLock()
	cached, ok := r.latest[phone]
	r.mu.RUnlock()
	if ok {
		return cloneProperties(cached), nil
	}

	var (
		properties   model.TerminalProperties
		extraJSON    []byte
		terminalType int64
		gnss         int16
		comm         int16
		updatedAt    int64
	)
	err := r.store.db.QueryRowContext(ctx,
		`SELECT `+propertiesColumns+` FROM terminal_properties WHERE phone = ?`, phone,
	).Scan(
		&properties.Phone, &terminalType, &properties.ManufacturerID, &properties.TerminalModel,
		&properties.TerminalID, &properties.ICCID, &properties.HardwareVersion,
		&properties.FirmwareVersion, &gnss, &comm, &extraJSON, &updatedAt,
	)
	if errors.Is(err, stdsql.ErrNoRows) {
		return nil, repository.ErrTerminalPropertiesNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get terminal properties %s: %w", phone, err)
	}
	properties.TerminalType = uint16(terminalType)
	properties.GNSSProperties = uint8(gnss)
	properties.CommunicationProperties = uint8(comm)
	properties.UpdatedAt = time.UnixMilli(updatedAt)
	if err := decodeExtra(extraJSON, properties.Phone, &properties); err != nil {
		return nil, err
	}

	r.mu.Lock()
	r.latest[phone] = cloneProperties(&properties)
	r.mu.Unlock()
	return &properties, nil
}

// Replace 覆盖写入属性档案：先落库，成功后更新进程内最新值。
func (r *terminalPropertiesRepository) Replace(ctx context.Context, properties *model.TerminalProperties) error {
	extraJSON, err := encodeExtra(properties)
	if err != nil {
		return err
	}
	stmt := propertiesUpsertSQLite
	if r.store.backend == BackendMySQL {
		stmt = propertiesUpsertMySQL
	}
	if _, err := r.store.db.ExecContext(ctx, stmt,
		properties.Phone, int64(properties.TerminalType), properties.ManufacturerID,
		properties.TerminalModel, properties.TerminalID, properties.ICCID,
		properties.HardwareVersion, properties.FirmwareVersion,
		int16(properties.GNSSProperties), int16(properties.CommunicationProperties),
		extraJSON, time.Now().UnixMilli()); err != nil {
		return fmt.Errorf("replace terminal properties %s: %w", properties.Phone, err)
	}

	r.mu.Lock()
	r.latest[properties.Phone] = cloneProperties(properties)
	r.mu.Unlock()
	return nil
}

// Delete 删除属性档案并清理进程内最新值。
func (r *terminalPropertiesRepository) Delete(ctx context.Context, phone string) error {
	if _, err := r.store.db.ExecContext(ctx,
		`DELETE FROM terminal_properties WHERE phone = ?`, phone); err != nil {
		return fmt.Errorf("delete terminal properties %s: %w", phone, err)
	}
	r.mu.Lock()
	delete(r.latest, phone)
	r.mu.Unlock()
	return nil
}

func encodeExtra(properties *model.TerminalProperties) ([]byte, error) {
	if len(properties.Extra) == 0 {
		return []byte("{}"), nil
	}
	payload, err := json.Marshal(properties.Extra)
	if err != nil {
		return nil, fmt.Errorf("encode terminal properties %s: %w", properties.Phone, err)
	}
	return payload, nil
}

func decodeExtra(payload []byte, phone string, properties *model.TerminalProperties) error {
	if len(payload) == 0 {
		return nil
	}
	if err := json.Unmarshal(payload, &properties.Extra); err != nil {
		return fmt.Errorf("decode terminal properties %s: %w", phone, err)
	}
	if len(properties.Extra) == 0 {
		properties.Extra = nil
	}
	return nil
}

func cloneProperties(p *model.TerminalProperties) *model.TerminalProperties {
	if p == nil {
		return nil
	}
	clone := *p
	if p.Extra != nil {
		clone.Extra = make(map[string]string, len(p.Extra))
		for k, v := range p.Extra {
			clone.Extra[k] = v
		}
	}
	return &clone
}

var _ repository.TerminalPropertiesRepository = (*terminalPropertiesRepository)(nil)
