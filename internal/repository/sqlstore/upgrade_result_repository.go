package sqlstore

import (
	"context"
	stdsql "database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

const (
	upgradeResultUpsertSQLite = `INSERT INTO upgrade_results (phone, serial, upgrade_type, result, received_at)
	VALUES (?, ?, ?, ?, ?)
	ON CONFLICT(phone, serial) DO UPDATE SET
		upgrade_type = excluded.upgrade_type, result = excluded.result,
		received_at = excluded.received_at`

	upgradeResultUpsertMySQL = `INSERT INTO upgrade_results (phone, serial, upgrade_type, result, received_at)
	VALUES (?, ?, ?, ?, ?)
	ON DUPLICATE KEY UPDATE
		upgrade_type = VALUES(upgrade_type), result = VALUES(result),
		received_at = VALUES(received_at)`
)

// upgradeResultRepository 是 repository.UpgradeResultRepository 的 SQL 实现。
type upgradeResultRepository struct {
	store *Store
}

// UpgradeResultRepository 返回终端升级结果仓储。
func (s *Store) UpgradeResultRepository() repository.UpgradeResultRepository {
	return &upgradeResultRepository{store: s}
}

func (r *upgradeResultRepository) Save(ctx context.Context, result *repository.UpgradeResult) error {
	if result == nil || result.Phone == "" {
		return fmt.Errorf("save upgrade result: nil result or empty phone")
	}
	stmt := upgradeResultUpsertSQLite
	if r.store.backend == BackendMySQL {
		stmt = upgradeResultUpsertMySQL
	}
	if _, err := r.store.db.ExecContext(ctx, stmt,
		result.Phone, int64(result.Serial), int16(result.UpgradeType), int16(result.Result),
		result.ReceivedAt.UnixMilli()); err != nil {
		return fmt.Errorf("save upgrade result %s/%d: %w", result.Phone, result.Serial, err)
	}
	return nil
}

func (r *upgradeResultRepository) Latest(ctx context.Context, phone string) (*repository.UpgradeResult, error) {
	var (
		serial      int64
		upgradeType int16
		resultCode  int16
		receivedAt  int64
	)
	err := r.store.db.QueryRowContext(ctx,
		`SELECT serial, upgrade_type, result, received_at FROM upgrade_results
		WHERE phone = ? ORDER BY received_at DESC, serial DESC LIMIT 1`, phone,
	).Scan(&serial, &upgradeType, &resultCode, &receivedAt)
	if errors.Is(err, stdsql.ErrNoRows) {
		return nil, repository.ErrUpgradeResultNotFound
	}
	if err != nil {
		return nil, fmt.Errorf("get upgrade result %s: %w", phone, err)
	}
	return &repository.UpgradeResult{
		Phone:       phone,
		UpgradeType: uint8(upgradeType),
		Result:      uint8(resultCode),
		Serial:      uint16(serial),
		ReceivedAt:  time.UnixMilli(receivedAt),
	}, nil
}

var _ repository.UpgradeResultRepository = (*upgradeResultRepository)(nil)
