package sqlstore

import (
	"context"
	stdsql "database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

const (
	mediaListUpsertSQLite = `INSERT INTO media_resource_lists (phone, media_count, items_json, updated_at)
	VALUES (?, ?, ?, ?)
	ON CONFLICT(phone) DO UPDATE SET
		media_count = excluded.media_count, items_json = excluded.items_json,
		updated_at = excluded.updated_at`

	mediaListUpsertMySQL = `INSERT INTO media_resource_lists (phone, media_count, items_json, updated_at)
	VALUES (?, ?, ?, ?)
	ON DUPLICATE KEY UPDATE
		media_count = VALUES(media_count), items_json = VALUES(items_json),
		updated_at = VALUES(updated_at)`

	mediaTaskUpsertSQLite = `INSERT INTO media_upload_tasks (phone, serial, payload_json, updated_at)
	VALUES (?, ?, ?, ?)
	ON CONFLICT(phone, serial) DO UPDATE SET
		payload_json = excluded.payload_json, updated_at = excluded.updated_at`

	mediaTaskUpsertMySQL = `INSERT INTO media_upload_tasks (phone, serial, payload_json, updated_at)
	VALUES (?, ?, ?, ?)
	ON DUPLICATE KEY UPDATE
		payload_json = VALUES(payload_json), updated_at = VALUES(updated_at)`

	mediaNoticeUpsertSQLite = `INSERT INTO media_upload_notices (phone, serial, result, updated_at)
	VALUES (?, ?, ?, ?)
	ON CONFLICT(phone, serial) DO UPDATE SET
		result = excluded.result, updated_at = excluded.updated_at`

	mediaNoticeUpsertMySQL = `INSERT INTO media_upload_notices (phone, serial, result, updated_at)
	VALUES (?, ?, ?, ?)
	ON DUPLICATE KEY UPDATE
		result = VALUES(result), updated_at = VALUES(updated_at)`
)

// mediaRepository 是 repository.MediaMetadataRepository 的 SQL 实现。
// 资源列表按设备整包覆盖；上传指令与完成通知按 (phone, 流水号) 唯一。
type mediaRepository struct {
	store *Store
}

// MediaMetadataRepository 返回媒体元数据仓储。
func (s *Store) MediaMetadataRepository() repository.MediaMetadataRepository {
	return &mediaRepository{store: s}
}

// mediaUploadTaskPayload 是 0x9206 落库字段，不含消息头。
type mediaUploadTaskPayload struct {
	ServerIP       string     `json:"serverIp"`
	Port           uint16     `json:"port"`
	Username       string     `json:"username"`
	Password       string     `json:"password"`
	UploadPath     string     `json:"uploadPath"`
	LogicChannelID uint8      `json:"logicChannelId"`
	StartTime      *time.Time `json:"startTime"`
	EndTime        *time.Time `json:"endTime"`
	AlarmSign      uint64     `json:"alarmSign"`
	MediaType      uint8      `json:"mediaType"`
	StreamType     uint8      `json:"streamType"`
	StorageType    uint8      `json:"storageType"`
	TaskCondition  uint8      `json:"taskCondition"`
}

func (r *mediaRepository) ReplaceResourceList(ctx context.Context, phone string, list *model.Msg1205) error {
	if list == nil {
		return fmt.Errorf("replace media resource list: nil message")
	}
	payload, err := json.Marshal(list.Items)
	if err != nil {
		return fmt.Errorf("encode media resource list %s: %w", phone, err)
	}
	stmt := mediaListUpsertSQLite
	if r.store.backend == BackendMySQL {
		stmt = mediaListUpsertMySQL
	}
	if _, err := r.store.db.ExecContext(ctx, stmt,
		phone, int64(list.MediaCount), payload, time.Now().UnixMilli()); err != nil {
		return fmt.Errorf("replace media resource list %s: %w", phone, err)
	}
	return nil
}

func (r *mediaRepository) ResourceList(ctx context.Context, phone string) (*model.Msg1205, error) {
	var (
		mediaCount int64
		itemsJSON  []byte
	)
	err := r.store.db.QueryRowContext(ctx,
		`SELECT media_count, items_json FROM media_resource_lists WHERE phone = ?`, phone,
	).Scan(&mediaCount, &itemsJSON)
	if errors.Is(err, stdsql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get media resource list %s: %w", phone, err)
	}

	var items []model.DeviceMedia
	if err := json.Unmarshal(itemsJSON, &items); err != nil {
		return nil, fmt.Errorf("decode media resource list %s: %w", phone, err)
	}
	return &model.Msg1205{
		Header:     &model.MsgHeader{PhoneNumber: phone},
		MediaCount: uint32(mediaCount),
		Items:      items,
	}, nil
}

func (r *mediaRepository) SaveUploadTask(ctx context.Context, phone string, task *model.Msg9206) error {
	if task == nil || task.Header == nil {
		return fmt.Errorf("save media upload task: nil message or header")
	}
	payload, err := json.Marshal(mediaUploadTaskPayload{
		ServerIP:       task.ServerIP,
		Port:           task.Port,
		Username:       task.Username,
		Password:       task.Password,
		UploadPath:     task.UploadPath,
		LogicChannelID: task.LogicChannelID,
		StartTime:      task.StartTime,
		EndTime:        task.EndTime,
		AlarmSign:      task.AlarmSign,
		MediaType:      task.MediaType,
		StreamType:     task.StreamType,
		StorageType:    task.StorageType,
		TaskCondition:  task.TaskCondition,
	})
	if err != nil {
		return fmt.Errorf("encode media upload task %s/%d: %w", phone, task.Header.SerialNumber, err)
	}
	stmt := mediaTaskUpsertSQLite
	if r.store.backend == BackendMySQL {
		stmt = mediaTaskUpsertMySQL
	}
	if _, err := r.store.db.ExecContext(ctx, stmt,
		phone, int64(task.Header.SerialNumber), payload, time.Now().UnixMilli()); err != nil {
		return fmt.Errorf("save media upload task %s/%d: %w", phone, task.Header.SerialNumber, err)
	}
	return nil
}

func (r *mediaRepository) UploadTask(ctx context.Context, phone string, serial uint16) (*model.Msg9206, error) {
	var payloadJSON []byte
	err := r.store.db.QueryRowContext(ctx,
		`SELECT payload_json FROM media_upload_tasks WHERE phone = ? AND serial = ?`, phone, int64(serial),
	).Scan(&payloadJSON)
	if errors.Is(err, stdsql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get media upload task %s/%d: %w", phone, serial, err)
	}

	var payload mediaUploadTaskPayload
	if err := json.Unmarshal(payloadJSON, &payload); err != nil {
		return nil, fmt.Errorf("decode media upload task %s/%d: %w", phone, serial, err)
	}
	return &model.Msg9206{
		Header:         &model.MsgHeader{PhoneNumber: phone, SerialNumber: serial},
		ServerIP:       payload.ServerIP,
		Port:           payload.Port,
		Username:       payload.Username,
		Password:       payload.Password,
		UploadPath:     payload.UploadPath,
		LogicChannelID: payload.LogicChannelID,
		StartTime:      payload.StartTime,
		EndTime:        payload.EndTime,
		AlarmSign:      payload.AlarmSign,
		MediaType:      payload.MediaType,
		StreamType:     payload.StreamType,
		StorageType:    payload.StorageType,
		TaskCondition:  payload.TaskCondition,
	}, nil
}

func (r *mediaRepository) SaveUploadNotice(ctx context.Context, phone string, notice *model.Msg1206) error {
	if notice == nil {
		return fmt.Errorf("save media upload notice: nil message")
	}
	stmt := mediaNoticeUpsertSQLite
	if r.store.backend == BackendMySQL {
		stmt = mediaNoticeUpsertMySQL
	}
	if _, err := r.store.db.ExecContext(ctx, stmt,
		phone, int64(notice.AnswerSerialNumber), int16(notice.Result), time.Now().UnixMilli()); err != nil {
		return fmt.Errorf("save media upload notice %s/%d: %w", phone, notice.AnswerSerialNumber, err)
	}
	return nil
}

func (r *mediaRepository) UploadNotice(ctx context.Context, phone string, serial uint16) (*model.Msg1206, error) {
	var result int16
	err := r.store.db.QueryRowContext(ctx,
		`SELECT result FROM media_upload_notices WHERE phone = ? AND serial = ?`, phone, int64(serial),
	).Scan(&result)
	if errors.Is(err, stdsql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get media upload notice %s/%d: %w", phone, serial, err)
	}
	return &model.Msg1206{
		Header:             &model.MsgHeader{PhoneNumber: phone, SerialNumber: serial},
		AnswerSerialNumber: serial,
		Result:             uint8(result),
	}, nil
}

var _ repository.MediaMetadataRepository = (*mediaRepository)(nil)
