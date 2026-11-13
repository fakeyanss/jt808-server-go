package sqlcore

import (
	"context"
	"fmt"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

const mediaSearchColumns = `phone, answer_serial, item_index, media_id, media_type, channel_id, event_id,
	alarm_sign, status_sign, latitude, longitude, altitude, speed, direction, location_time, received_at`

// mediaSearchRepository 是 repository.MediaSearchRepository 的 SQL 实现。
type mediaSearchRepository struct {
	store *Store
}

// MediaSearchRepository 返回 0x0802 检索结果仓储。
func (s *Store) MediaSearchRepository() repository.MediaSearchRepository {
	return &mediaSearchRepository{store: s}
}

// Replace 以 (phone, answer_serial) 为单位整体覆盖一次检索应答，
// 先删除旧结果再写入，避免重传或重查后残留过期条目。
func (r *mediaSearchRepository) Replace(ctx context.Context, phone string, answerSerial uint16, records []repository.MediaSearchRecord) error {
	if err := repository.ValidateMediaSearch(phone, records); err != nil {
		return err
	}
	normalized := repository.NormalizeMediaSearchRecords(phone, answerSerial, records)

	tx, err := r.store.db.BeginTx(ctx, nil)
	if err != nil {
		return fmt.Errorf("begin media search transaction %s/%d: %w", phone, answerSerial, err)
	}
	defer func() { _ = tx.Rollback() }()

	if _, err := tx.ExecContext(ctx,
		`DELETE FROM media_search_results WHERE phone = ? AND answer_serial = ?`,
		phone, int64(answerSerial)); err != nil {
		return fmt.Errorf("reset media search results %s/%d: %w", phone, answerSerial, err)
	}
	for _, record := range normalized {
		if _, err := tx.ExecContext(ctx,
			`INSERT INTO media_search_results (`+mediaSearchColumns+`)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
			phone, int64(answerSerial), int64(record.Index), int64(record.MediaID),
			int16(record.MediaType), int16(record.ChannelID), int16(record.EventID),
			int64(record.AlarmSign), int64(record.StatusSign), int64(record.Latitude),
			int64(record.Longitude), int64(record.Altitude), int64(record.Speed),
			int64(record.Direction), mediaLocationMillis(record.LocationTime),
			record.ReceivedAt.UnixMilli()); err != nil {
			return fmt.Errorf("save media search result %s/%d/%d: %w", phone, answerSerial, record.Index, err)
		}
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit media search %s/%d: %w", phone, answerSerial, err)
	}
	return nil
}

func (r *mediaSearchRepository) List(ctx context.Context, phone string, limit int) ([]repository.MediaSearchRecord, error) {
	ctx, cancel := readContext(ctx)
	defer cancel()
	rows, err := r.store.readDB.QueryContext(ctx,
		`SELECT `+mediaSearchColumns+` FROM media_search_results
		WHERE phone = ? ORDER BY received_at DESC, answer_serial DESC, item_index ASC LIMIT ?`,
		phone, normalizeMediaLimit(limit))
	if err != nil {
		return nil, fmt.Errorf("list media search results %s: %w", phone, err)
	}
	defer rows.Close()

	records := make([]repository.MediaSearchRecord, 0)
	for rows.Next() {
		var (
			record                                             repository.MediaSearchRecord
			answerSerial, index, mediaID                       int64
			mediaType, channelID, eventID                      int16
			alarmSign, statusSign, latitude, longitude         int64
			altitude, speed, direction, locationTime, received int64
		)
		if err := rows.Scan(&record.Phone, &answerSerial, &index, &mediaID,
			&mediaType, &channelID, &eventID, &alarmSign, &statusSign,
			&latitude, &longitude, &altitude, &speed, &direction,
			&locationTime, &received); err != nil {
			return nil, fmt.Errorf("scan media search result %s: %w", phone, err)
		}
		record.AnswerSerial = uint16(answerSerial)
		record.Index = int(index)
		record.MediaID = uint32(mediaID)
		record.MediaType = uint8(mediaType)
		record.ChannelID = uint8(channelID)
		record.EventID = uint8(eventID)
		record.AlarmSign = uint32(alarmSign)
		record.StatusSign = uint32(statusSign)
		record.Latitude = uint32(latitude)
		record.Longitude = uint32(longitude)
		record.Altitude = uint16(altitude)
		record.Speed = uint16(speed)
		record.Direction = uint16(direction)
		if locationTime != 0 {
			record.LocationTime = time.UnixMilli(locationTime)
		}
		record.ReceivedAt = time.UnixMilli(received)
		records = append(records, record)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate media search results %s: %w", phone, err)
	}
	return records, nil
}

var _ repository.MediaSearchRepository = (*mediaSearchRepository)(nil)
