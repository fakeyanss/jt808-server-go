package sqlcore

import (
	"context"
	stdsql "database/sql"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

type sessionDirectoryRepository struct {
	store *Store
}

// SessionDirectoryRepository 返回基于当前 SQL 连接池的会话目录。
func (s *Store) SessionDirectoryRepository() repository.SessionDirectoryRepository {
	return &sessionDirectoryRepository{store: s}
}

func (r *sessionDirectoryRepository) Claim(ctx context.Context, phone string, owner repository.SessionOwner, now time.Time) (uint64, error) {
	tx, err := r.store.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer func() { _ = tx.Rollback() }()

	var (
		generation uint64
		gatewayID  string
		instanceID string
		sessionID  string
	)
	err = tx.QueryRowContext(ctx, r.store.stmts.SelectSessionForUpdate(), phone).
		Scan(&generation, &gatewayID, &instanceID, &sessionID)
	nowMilli := now.UnixMilli()
	switch {
	case err == stdsql.ErrNoRows:
		if _, err = tx.ExecContext(ctx, r.store.stmts.InsertSessionDirectory(),
			phone, owner.GatewayID, owner.InstanceID, owner.SessionID, nowMilli); err != nil {
			return 0, err
		}
		generation = 1
	case err != nil:
		return 0, err
	case gatewayID == owner.GatewayID && instanceID == owner.InstanceID && sessionID == owner.SessionID:
		// 幂等：同一 owner 重复 claim 直接复用现有 generation。
	default:
		if _, err = tx.ExecContext(ctx, r.store.stmts.UpdateSessionDirectory(),
			owner.GatewayID, owner.InstanceID, owner.SessionID, nowMilli, phone); err != nil {
			return 0, err
		}
		if err = tx.QueryRowContext(ctx, r.store.stmts.SelectSessionGeneration(), phone).Scan(&generation); err != nil {
			return 0, err
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return generation, nil
}

func (r *sessionDirectoryRepository) Release(ctx context.Context, phone string, owner repository.SessionOwner, generation uint64) error {
	_, err := r.store.db.ExecContext(ctx, r.store.stmts.DeleteSessionDirectory(),
		phone, owner.GatewayID, owner.InstanceID, owner.SessionID, generation)
	return err
}

func (r *sessionDirectoryRepository) Validate(ctx context.Context, phone string, owner repository.SessionOwner, generation uint64) (bool, error) {
	var exists int
	err := r.store.readDB.QueryRowContext(ctx, r.store.stmts.ValidateSessionDirectory(),
		phone, owner.GatewayID, owner.InstanceID, owner.SessionID, generation).Scan(&exists)
	if err == stdsql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return true, nil
}

func (r *sessionDirectoryRepository) Get(ctx context.Context, phone string) (*repository.SessionRecord, error) {
	var (
		record    repository.SessionRecord
		claimedAt int64
	)
	err := r.store.readDB.QueryRowContext(ctx, r.store.stmts.SelectSessionDirectory(), phone).
		Scan(&record.Phone, &record.GatewayID, &record.InstanceID, &record.SessionID,
			&record.Generation, &claimedAt)
	if err == stdsql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	record.ClaimedAt = timeFromUnixMilli(claimedAt)
	return &record, nil
}

func (r *sessionDirectoryRepository) Route(ctx context.Context, phone, sessionID string, now time.Time) (string, bool, error) {
	var downlinkURL string
	err := r.store.readDB.QueryRowContext(ctx, r.store.stmts.RouteSessionDownlink(),
		phone, now.UnixMilli(), sessionID, sessionID).Scan(&downlinkURL)
	if err == stdsql.ErrNoRows {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if downlinkURL == "" {
		return "", false, nil
	}
	return downlinkURL, true, nil
}

var _ repository.SessionDirectoryRepository = (*sessionDirectoryRepository)(nil)
