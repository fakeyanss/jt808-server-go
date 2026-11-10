package sqlstore

import (
	"context"
	stdsql "database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

const locationInsertChunkSize = 200

// locationRepository 是 repository.LocationRepository 的 SQL 实现。
// 写入按 200 行分块多行 INSERT；每次写入后按设备执行保留期与条数上限清理，
// 使条数上限成为硬约束。0x0200 为单条写入，0x0704 一次调用可写入整批数据项。
type locationRepository struct {
	store  *Store
	policy repository.LocationRetention

	idMu   sync.Mutex
	lastID int64
}

// LocationRepository 返回定位历史仓储。policy 由配置层传入，0 表示关闭对应策略。
func (s *Store) LocationRepository(policy repository.LocationRetention) repository.LocationRepository {
	return &locationRepository{
		store:  s,
		policy: policy,
	}
}

// nextRecordID 生成单调递增的记录 ID，避免同一毫秒内批量数据项主键冲突。
func (r *locationRepository) nextRecordID() int64 {
	r.idMu.Lock()
	defer r.idMu.Unlock()
	id := time.Now().UnixNano()
	if id <= r.lastID {
		id = r.lastID + 1
	}
	r.lastID = id
	return id
}

func (r *locationRepository) Record(ctx context.Context, records []repository.LocationRecord) error {
	if len(records) == 0 {
		return nil
	}
	phones := make(map[string]struct{}, len(records))
	for i := range records {
		if records[i].ReceivedAt.IsZero() {
			records[i].ReceivedAt = time.Now()
		}
		phones[records[i].Phone] = struct{}{}
	}

	for start := 0; start < len(records); start += locationInsertChunkSize {
		end := start + locationInsertChunkSize
		if end > len(records) {
			end = len(records)
		}
		if err := r.insertChunk(ctx, records[start:end]); err != nil {
			return err
		}
	}

	for phone := range phones {
		if err := r.cleanup(ctx, phone); err != nil {
			return err
		}
	}
	return nil
}

func (r *locationRepository) insertChunk(ctx context.Context, records []repository.LocationRecord) error {
	placeholders := make([]string, 0, len(records))
	args := make([]interface{}, 0, len(records)*14)
	for i := range records {
		record := records[i]
		payload, err := json.Marshal(record)
		if err != nil {
			return fmt.Errorf("encode location %s: %w", record.Phone, err)
		}
		placeholders = append(placeholders, "(?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)")
		args = append(args,
			record.Phone,
			r.nextRecordID(),
			int64(record.MsgID),
			hex.ParseTime(record.OccurredAt).UnixMilli(),
			record.ReceivedAt.UnixMilli(),
			int64(record.AlarmSign),
			int64(record.StatusSign),
			int64(record.Latitude),
			int64(record.Longitude),
			int64(record.Altitude),
			int64(record.Speed),
			int64(record.Direction),
			string(payload),
			nullString(record.DedupKey),
		)
	}
	stmt := `INSERT INTO locations
		(phone, record_id, msg_id, report_time_ms, received_at_ms,
		 alarm_sign, status_sign, latitude, longitude, altitude, speed, direction, payload_json, dedup_key)
		VALUES ` + strings.Join(placeholders, ", ")
	if r.store.backend == BackendMySQL {
		stmt = "INSERT IGNORE INTO locations" + strings.TrimPrefix(stmt, "INSERT INTO locations")
	} else {
		stmt = "INSERT OR IGNORE INTO locations" + strings.TrimPrefix(stmt, "INSERT INTO locations")
	}
	if _, err := r.store.db.ExecContext(ctx, stmt, args...); err != nil {
		return fmt.Errorf("insert %d locations: %w", len(records), err)
	}
	return nil
}

// nullString 把空串写成 NULL，让老数据与 standalone 不参与 (phone, dedup_key) 唯一约束。
func nullString(value string) interface{} {
	if value == "" {
		return nil
	}
	return value
}

func (r *locationRepository) List(ctx context.Context, phone string, limit int) ([]repository.LocationRecord, error) {
	query := `SELECT payload_json FROM locations WHERE phone = ? ORDER BY received_at_ms DESC, record_id DESC`
	args := []interface{}{phone}
	if limit > 0 {
		query += ` LIMIT ?`
		args = append(args, limit)
	}
	rows, err := r.store.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("list locations %s: %w", phone, err)
	}
	defer rows.Close()

	var records []repository.LocationRecord
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, fmt.Errorf("scan location %s: %w", phone, err)
		}
		var record repository.LocationRecord
		if err := json.Unmarshal(payload, &record); err != nil {
			return nil, fmt.Errorf("decode location %s: %w", phone, err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate locations %s: %w", phone, err)
	}
	return records, nil
}

// ListPage 按终端上报时间区间过滤，返回倒序分页结果与总数。
// 时间条件使用 report_time_ms 索引；Limit/Offset 由 HistoryQuery 校验保证有界。
func (r *locationRepository) ListPage(ctx context.Context, query repository.HistoryQuery) ([]repository.LocationRecord, int, error) {
	if err := query.Validate(); err != nil {
		return nil, 0, err
	}
	where := `phone = ?`
	args := []interface{}{query.Phone}
	if !query.From.IsZero() {
		where += ` AND report_time_ms >= ?`
		args = append(args, query.From.UnixMilli())
	}
	if !query.To.IsZero() {
		where += ` AND report_time_ms < ?`
		args = append(args, query.To.UnixMilli())
	}

	var total int
	if err := r.store.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM locations WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count locations %s: %w", query.Phone, err)
	}

	pageArgs := append(append([]interface{}{}, args...), query.Limit, query.Offset)
	rows, err := r.store.db.QueryContext(ctx,
		`SELECT payload_json FROM locations WHERE `+where+
			` ORDER BY report_time_ms DESC, record_id DESC LIMIT ? OFFSET ?`, pageArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list locations page %s: %w", query.Phone, err)
	}
	defer rows.Close()

	records := make([]repository.LocationRecord, 0, query.Limit)
	for rows.Next() {
		var payload []byte
		if err := rows.Scan(&payload); err != nil {
			return nil, 0, fmt.Errorf("scan location page %s: %w", query.Phone, err)
		}
		var record repository.LocationRecord
		if err := json.Unmarshal(payload, &record); err != nil {
			return nil, 0, fmt.Errorf("decode location page %s: %w", query.Phone, err)
		}
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate locations page %s: %w", query.Phone, err)
	}
	return records, total, nil
}

// cleanup 执行时间清理与每设备条数上限，两者在同一事务内完成。
func (r *locationRepository) cleanup(ctx context.Context, phone string) error {
	if r.policy.MaxAge <= 0 && r.policy.MaxPerDevice <= 0 {
		return nil
	}
	tx, err := r.store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin location cleanup %s: %w", phone, err)
	}
	defer func() { _ = tx.Rollback() }()

	if r.policy.MaxAge > 0 {
		cutoff := time.Now().Add(-r.policy.MaxAge).UnixMilli()
		if _, err := tx.ExecContext(ctx,
			`DELETE FROM locations WHERE phone = ? AND received_at_ms < ?`, phone, cutoff); err != nil {
			return fmt.Errorf("delete expired locations %s: %w", phone, err)
		}
	}
	if r.policy.MaxPerDevice > 0 {
		var cutoff, cutoffID int64
		err := tx.QueryRowContext(ctx,
			`SELECT received_at_ms, record_id FROM locations WHERE phone = ?
			 ORDER BY received_at_ms DESC, record_id DESC LIMIT 1 OFFSET ?`,
			phone, r.policy.MaxPerDevice-1).Scan(&cutoff, &cutoffID)
		switch {
		case errors.Is(err, stdsql.ErrNoRows):
			// 条数未超上限。
		case err != nil:
			return fmt.Errorf("find location retention cutoff %s: %w", phone, err)
		default:
			if _, err := tx.ExecContext(ctx,
				`DELETE FROM locations WHERE phone = ?
				   AND (received_at_ms < ? OR (received_at_ms = ? AND record_id < ?))`,
				phone, cutoff, cutoff, cutoffID); err != nil {
				return fmt.Errorf("trim locations %s: %w", phone, err)
			}
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit location cleanup %s: %w", phone, err)
	}
	return nil
}

var _ repository.LocationRepository = (*locationRepository)(nil)
