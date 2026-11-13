package sqlcore

import (
	"context"
	stdsql "database/sql"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

type alarmCommand struct {
	alarmSerial uint16
	alarmType   uint32
	createdAt   time.Time
}

// alarmRepository 是 repository.AlarmRepository 的 SQL 实现。
// 报警历史落库；0x8203 确认命令关联只保存在进程内，重启后未确认关联失效。
// 每设备条数上限在每次成功写入后执行，保证上限是硬约束；报警本身为低频事件。
type alarmRepository struct {
	store    *Store
	policy   repository.AlarmRetention
	mu       sync.Mutex
	commands map[string]alarmCommand
}

// AlarmRepository 返回报警仓储。policy 由配置层传入，0 表示关闭对应策略。
func (s *Store) AlarmRepository(policy repository.AlarmRetention) repository.AlarmRepository {
	return &alarmRepository{
		store:    s,
		policy:   policy,
		commands: make(map[string]alarmCommand),
	}
}

func (r *alarmRepository) Record(ctx context.Context, phone string, serial uint16, alarmSign, latitude, longitude uint32, occurredAt string) error {
	if alarmSign == 0 {
		return nil
	}
	now := time.Now().UnixMilli()
	occurredMS := hex.ParseTime(occurredAt).UnixMilli()

	if r.policy.DedupWindow > 0 {
		var existing int64
		err := r.store.db.QueryRowContext(ctx,
			`SELECT received_at_ms FROM alarms
			 WHERE phone = ? AND message_serial = ? AND received_at_ms >= ?
			 ORDER BY received_at_ms DESC LIMIT 1`,
			phone, int64(serial), now-r.policy.DedupWindow.Milliseconds()).Scan(&existing)
		switch {
		case err == nil:
			if _, err := r.store.db.ExecContext(ctx,
				`UPDATE alarms SET alarm_sign = ?
				 WHERE phone = ? AND message_serial = ? AND received_at_ms = ?`,
				int64(alarmSign), phone, int64(serial), existing); err != nil {
				return fmt.Errorf("dedup alarm %s/%d: %w", phone, serial, err)
			}
			return nil
		case errors.Is(err, stdsql.ErrNoRows):
			// 窗口内没有同流水号记录，按新报警追加。
		default:
			return fmt.Errorf("lookup alarm dedup %s/%d: %w", phone, serial, err)
		}
	} else {
		// 关闭去重时仍要保证 (phone, message_serial, received_at_ms) 主键唯一，
		// 同一毫秒内的重复上报顺延 1ms 追加，保持“每次上报一条”的语义。
		var last stdsql.NullInt64
		err := r.store.db.QueryRowContext(ctx,
			`SELECT MAX(received_at_ms) FROM alarms WHERE phone = ? AND message_serial = ?`,
			phone, int64(serial)).Scan(&last)
		if err != nil {
			return fmt.Errorf("lookup last alarm %s/%d: %w", phone, serial, err)
		}
		if last.Valid && now <= last.Int64 {
			now = last.Int64 + 1
		}
	}

	if _, err := r.store.db.ExecContext(ctx,
		`INSERT INTO alarms (phone, message_serial, received_at_ms, occurred_at_ms, alarm_sign, latitude, longitude, confirmed)
		 VALUES (?, ?, ?, ?, ?, ?, ?, 0)`,
		phone, int64(serial), now, occurredMS, int64(alarmSign), int64(latitude), int64(longitude)); err != nil {
		return fmt.Errorf("insert alarm %s/%d: %w", phone, serial, err)
	}
	if err := r.trim(ctx, phone); err != nil {
		return err
	}
	return nil
}

func (r *alarmRepository) List(ctx context.Context, phone string) ([]repository.AlarmEvent, error) {
	ctx, cancel := readContext(ctx)
	defer cancel()
	rows, err := r.store.readDB.QueryContext(ctx,
		`SELECT message_serial, alarm_sign, latitude, longitude, occurred_at_ms, received_at_ms, confirmed
		 FROM alarms WHERE phone = ? ORDER BY received_at_ms ASC, message_serial ASC`, phone)
	if err != nil {
		return nil, fmt.Errorf("list alarms %s: %w", phone, err)
	}
	defer rows.Close()

	var events []repository.AlarmEvent
	for rows.Next() {
		var (
			serial, alarmSign, latitude, longitude, occurredMS, receivedMS int64
			confirmed                                                      int64
		)
		if err := rows.Scan(&serial, &alarmSign, &latitude, &longitude, &occurredMS, &receivedMS, &confirmed); err != nil {
			return nil, fmt.Errorf("scan alarm %s: %w", phone, err)
		}
		events = append(events, repository.AlarmEvent{
			Phone:         phone,
			MessageSerial: uint16(serial),
			AlarmSign:     uint32(alarmSign),
			Latitude:      uint32(latitude),
			Longitude:     uint32(longitude),
			OccurredAt:    time.UnixMilli(occurredMS).In(hex.ProtocolTimeZone).Format("060102150405"),
			ReceivedAt:    time.UnixMilli(receivedMS),
			Confirmed:     confirmed != 0,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate alarms %s: %w", phone, err)
	}
	return events, nil
}

// ListPage 按服务端接收时间区间过滤，返回倒序分页结果与总数。
// 时间条件使用 received_at_ms 索引；Limit/Offset 由 HistoryQuery 校验保证有界。
func (r *alarmRepository) ListPage(ctx context.Context, query repository.HistoryQuery) ([]repository.AlarmEvent, int, error) {
	ctx, cancel := readContext(ctx)
	defer cancel()
	if err := query.Validate(); err != nil {
		return nil, 0, err
	}
	where := `phone = ?`
	args := []interface{}{query.Phone}
	if !query.From.IsZero() {
		where += ` AND received_at_ms >= ?`
		args = append(args, query.From.UnixMilli())
	}
	if !query.To.IsZero() {
		where += ` AND received_at_ms < ?`
		args = append(args, query.To.UnixMilli())
	}

	var total int
	if err := r.store.readDB.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM alarms WHERE `+where, args...).Scan(&total); err != nil {
		return nil, 0, fmt.Errorf("count alarms %s: %w", query.Phone, err)
	}

	pageArgs := append(append([]interface{}{}, args...), query.Limit, query.Offset)
	rows, err := r.store.readDB.QueryContext(ctx,
		`SELECT message_serial, alarm_sign, latitude, longitude, occurred_at_ms, received_at_ms, confirmed
		 FROM alarms WHERE `+where+
			` ORDER BY received_at_ms DESC, message_serial DESC LIMIT ? OFFSET ?`, pageArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("list alarms page %s: %w", query.Phone, err)
	}
	defer rows.Close()

	events := make([]repository.AlarmEvent, 0, query.Limit)
	for rows.Next() {
		var (
			serial, alarmSign, latitude, longitude, occurredMS, receivedMS int64
			confirmed                                                      int64
		)
		if err := rows.Scan(&serial, &alarmSign, &latitude, &longitude, &occurredMS, &receivedMS, &confirmed); err != nil {
			return nil, 0, fmt.Errorf("scan alarm page %s: %w", query.Phone, err)
		}
		events = append(events, repository.AlarmEvent{
			Phone:         query.Phone,
			MessageSerial: uint16(serial),
			AlarmSign:     uint32(alarmSign),
			Latitude:      uint32(latitude),
			Longitude:     uint32(longitude),
			OccurredAt:    time.UnixMilli(occurredMS).In(hex.ProtocolTimeZone).Format("060102150405"),
			ReceivedAt:    time.UnixMilli(receivedMS),
			Confirmed:     confirmed != 0,
		})
	}
	if err := rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("iterate alarms page %s: %w", query.Phone, err)
	}
	return events, total, nil
}

func (r *alarmRepository) RegisterConfirmation(_ context.Context, phone string, commandSerial, alarmSerial uint16, alarmType uint32) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for key, command := range r.commands {
		if time.Since(command.createdAt) > r.policy.ConfirmExpire {
			delete(r.commands, key)
		}
	}
	r.commands[alarmCommandKey(phone, commandSerial)] = alarmCommand{
		alarmSerial: alarmSerial, alarmType: alarmType, createdAt: time.Now(),
	}
	return nil
}

func (r *alarmRepository) ConfirmByCommand(ctx context.Context, phone string, commandSerial uint16) (bool, error) {
	key := alarmCommandKey(phone, commandSerial)
	r.mu.Lock()
	command, ok := r.commands[key]
	if ok {
		delete(r.commands, key)
	}
	r.mu.Unlock()
	if !ok {
		return false, nil
	}
	if time.Since(command.createdAt) > r.policy.ConfirmExpire {
		return false, nil
	}

	if _, err := r.store.db.ExecContext(ctx,
		`UPDATE alarms SET confirmed = 1
		 WHERE phone = ?
		   AND (? = 0 OR message_serial = ?)
		   AND (? = 0 OR (alarm_sign & ?) <> 0)`,
		phone,
		int64(command.alarmSerial), int64(command.alarmSerial),
		int64(command.alarmType), int64(command.alarmType)); err != nil {
		return false, fmt.Errorf("confirm alarms %s/%d: %w", phone, commandSerial, err)
	}
	return true, nil
}

// trim 执行每设备条数上限。按 (received_at_ms, message_serial) 保留最新 N 条。
func (r *alarmRepository) trim(ctx context.Context, phone string) error {
	if r.policy.MaxPerDevice <= 0 {
		return nil
	}
	var cutoff int64
	var cutoffSerial int64
	err := r.store.db.QueryRowContext(ctx,
		`SELECT received_at_ms, message_serial FROM alarms WHERE phone = ?
		 ORDER BY received_at_ms DESC, message_serial DESC LIMIT 1 OFFSET ?`,
		phone, r.policy.MaxPerDevice-1).Scan(&cutoff, &cutoffSerial)
	if errors.Is(err, stdsql.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("find alarm retention cutoff %s: %w", phone, err)
	}
	if _, err := r.store.db.ExecContext(ctx,
		`DELETE FROM alarms WHERE phone = ?
		   AND (received_at_ms < ? OR (received_at_ms = ? AND message_serial < ?))`,
		phone, cutoff, cutoff, cutoffSerial); err != nil {
		return fmt.Errorf("trim alarms %s: %w", phone, err)
	}
	return nil
}

func alarmCommandKey(phone string, serial uint16) string {
	return fmt.Sprintf("%s/%04x", phone, serial)
}

var _ repository.AlarmRepository = (*alarmRepository)(nil)
