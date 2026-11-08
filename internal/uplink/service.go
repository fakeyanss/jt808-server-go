package uplink

import (
	"context"
	"fmt"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// Service 负责上行 at-least-once 的落盘与重放。
// 生命周期：
//   - BeforeAck：在应答前把关键记录写入 spool，写入失败则不应答；
//   - AfterBusiness：业务成功后删除 spool 记录；
//   - Replay：启动时重放上次进程遗留的记录。
type Service struct {
	policy    Policy
	spool     *Spool
	locations repository.LocationRepository
	alarms    repository.AlarmRepository
}

// NewService 构造上行可靠性服务。
func NewService(policy Policy, spoolDir string, locations repository.LocationRepository, alarms repository.AlarmRepository) (*Service, error) {
	spool, err := NewSpool(spoolDir)
	if err != nil {
		return nil, err
	}
	return &Service{policy: policy, spool: spool, locations: locations, alarms: alarms}, nil
}

// Policy 返回当前策略，供测试与诊断使用。
func (s *Service) Policy() Policy { return s.policy }

// SpoolDir 返回 spool 目录，供测试与诊断使用。
func (s *Service) SpoolDir() string { return s.spool.Dir() }

// BeforeAck 在应答前持久化关键上行记录。
// 返回空列表表示该消息按 best_effort 处理，不需要 spool。
// 任一记录写盘失败都返回错误，调用方必须放弃本次应答，让终端重传。
func (s *Service) BeforeAck(_ context.Context, msgID uint16, pd *model.ProcessData) ([]string, error) {
	if s == nil || s.spool == nil || pd == nil || pd.Incoming == nil {
		return nil, nil
	}
	records := extractRecords(msgID, pd.Incoming)
	if len(records) == 0 {
		return nil, nil
	}
	if s.policy.For(s.policy.Classify(msgID, records[0].AlarmSign)) != AtLeastOnce {
		return nil, nil
	}

	ids := make([]string, 0, len(records))
	for _, record := range records {
		id, err := s.spool.Put(record)
		if err != nil {
			// 已写入的记录保留在 spool，重启后会重放；重复处理符合 at-least-once。
			return ids, fmt.Errorf("persist uplink record before ack: %w", err)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// AfterBusiness 在业务处理成功后清理 spool 记录。
// 清理失败只记日志：记录会在下次启动重放，最多重复处理。
func (s *Service) AfterBusiness(ids []string) {
	if s == nil || s.spool == nil {
		return
	}
	for _, id := range ids {
		if err := s.spool.Ack(id); err != nil {
			log.Error().Err(err).Str("spool_id", id).Msg("Fail to ack uplink spool record")
		}
	}
}

// Replay 重放启动前遗留的 spool 记录并清理。
// 只重放定位历史与报警，运行期缓存（geo/设备状态）不回填。
func (s *Service) Replay(ctx context.Context) error {
	if s == nil || s.spool == nil {
		return nil
	}
	pending, err := s.spool.Pending()
	if err != nil {
		return err
	}
	if len(pending) == 0 {
		return nil
	}

	locations := make([]repository.LocationRecord, 0, len(pending))
	for _, item := range pending {
		locations = append(locations, item.Record.LocationRecord)
	}
	if s.locations != nil {
		if err := s.locations.Record(ctx, locations); err != nil {
			return fmt.Errorf("replay %d uplink records: %w", len(locations), err)
		}
	}
	if s.alarms != nil {
		for _, item := range pending {
			record := item.Record
			if record.Serial == 0 || record.AlarmSign == 0 {
				continue
			}
			if err := s.alarms.Record(ctx, record.Phone, record.Serial, record.AlarmSign,
				record.Latitude, record.Longitude, record.OccurredAt); err != nil {
				return fmt.Errorf("replay alarm record %s: %w", item.ID, err)
			}
		}
	}
	for _, item := range pending {
		if err := s.spool.Ack(item.ID); err != nil {
			return fmt.Errorf("ack replayed uplink record %s: %w", item.ID, err)
		}
	}
	log.Info().Int("records", len(pending)).Msg("Replayed uplink spool records")
	return nil
}

// extractRecords 从已解码的上行消息中抽取定位记录。
func extractRecords(msgID uint16, incoming model.JT808Msg) []SpoolRecord {
	switch msg := incoming.(type) {
	case *model.Msg0200:
		if msg.Header == nil {
			return nil
		}
		return []SpoolRecord{recordFromLocation(msgID, msg.Header.SerialNumber, msg)}
	case *model.Msg0704:
		records := make([]SpoolRecord, 0, len(msg.Items))
		for i := range msg.Items {
			loc := &msg.Items[i].Loc
			if loc.Header == nil {
				continue
			}
			records = append(records, recordFromLocation(msgID, 0, loc))
		}
		return records
	default:
		return nil
	}
}

func recordFromLocation(msgID uint16, serial uint16, loc *model.Msg0200) SpoolRecord {
	return SpoolRecord{
		LocationRecord: repository.LocationRecord{
			Phone:      loc.Header.PhoneNumber,
			MsgID:      msgID,
			AlarmSign:  loc.AlarmSign,
			StatusSign: loc.StatusSign,
			Latitude:   loc.Latitude,
			Longitude:  loc.Longitude,
			Altitude:   loc.Altitude,
			Speed:      loc.Speed,
			Direction:  loc.Direction,
			ExtItems:   loc.ExtItems,
			OccurredAt: loc.Time,
			ReceivedAt: time.Now(),
		},
		Serial: serial,
	}
}
