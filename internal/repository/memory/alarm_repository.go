package memory

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

type alarmCommand struct {
	phone       string
	alarmSerial uint16
	alarmType   uint32
	createdAt   time.Time
}
type memoryAlarmRepository struct {
	mu       sync.Mutex
	events   map[string][]*repository.AlarmEvent
	commands map[string]alarmCommand
	policy   repository.AlarmRetention
}

// NewAlarmRepository 创建独立的内存报警仓储。
// 不传策略时使用历史默认值（30 分钟去重 / 10 分钟确认关联 / 1000 条），
// 保证未配置 retention 时行为与旧版本一致。显式传入的策略中 0 表示关闭对应策略。
func NewAlarmRepository(policies ...repository.AlarmRetention) repository.AlarmRepository {
	policy := repository.AlarmRetention{
		DedupWindow:   repository.DefaultAlarmDedupWindow,
		ConfirmExpire: repository.DefaultAlarmConfirmExpire,
		MaxPerDevice:  repository.DefaultAlarmMaxPerDevice,
	}
	if len(policies) > 0 {
		policy = policies[0]
	}
	return &memoryAlarmRepository{
		events:   make(map[string][]*repository.AlarmEvent),
		commands: make(map[string]alarmCommand),
		policy:   policy,
	}
}

func (c *memoryAlarmRepository) Record(_ context.Context, phone string, serial uint16, alarmSign, latitude, longitude uint32, occurredAt string) error {
	if alarmSign == 0 {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.policy.DedupWindow > 0 {
		for _, event := range c.events[phone] {
			if event.MessageSerial == serial && time.Since(event.ReceivedAt) < c.policy.DedupWindow {
				event.AlarmSign = alarmSign
				return nil
			}
		}
	}
	c.events[phone] = append(c.events[phone], &repository.AlarmEvent{
		Phone: phone, MessageSerial: serial, AlarmSign: alarmSign,
		Latitude: latitude, Longitude: longitude, OccurredAt: occurredAt, ReceivedAt: time.Now(),
	})
	if c.policy.MaxPerDevice > 0 && len(c.events[phone]) > c.policy.MaxPerDevice {
		c.events[phone] = append([]*repository.AlarmEvent(nil), c.events[phone][len(c.events[phone])-c.policy.MaxPerDevice:]...)
	}
	return nil
}

func (c *memoryAlarmRepository) List(_ context.Context, phone string) ([]repository.AlarmEvent, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]repository.AlarmEvent, 0, len(c.events[phone]))
	for _, event := range c.events[phone] {
		out = append(out, *event)
	}
	return out, nil
}

// ListPage 按接收时间区间过滤内存报警，按接收时间倒序分页。
func (c *memoryAlarmRepository) ListPage(_ context.Context, query repository.HistoryQuery) ([]repository.AlarmEvent, int, error) {
	if err := query.Validate(); err != nil {
		return nil, 0, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	filtered := make([]repository.AlarmEvent, 0, len(c.events[query.Phone]))
	for _, event := range c.events[query.Phone] {
		if !query.From.IsZero() && event.ReceivedAt.Before(query.From) {
			continue
		}
		if !query.To.IsZero() && !event.ReceivedAt.Before(query.To) {
			continue
		}
		filtered = append(filtered, *event)
	}
	sort.Slice(filtered, func(i, j int) bool {
		if filtered[i].ReceivedAt.Equal(filtered[j].ReceivedAt) {
			return filtered[i].MessageSerial > filtered[j].MessageSerial
		}
		return filtered[i].ReceivedAt.After(filtered[j].ReceivedAt)
	})

	total := len(filtered)
	if query.Offset >= total {
		return []repository.AlarmEvent{}, total, nil
	}
	end := query.Offset + query.Limit
	if end > total {
		end = total
	}
	return filtered[query.Offset:end], total, nil
}

func (c *memoryAlarmRepository) RegisterConfirmation(_ context.Context, phone string, commandSerial, alarmSerial uint16, alarmType uint32) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, command := range c.commands {
		if time.Since(command.createdAt) > c.policy.ConfirmExpire {
			delete(c.commands, key)
		}
	}
	c.commands[alarmCommandKey(phone, commandSerial)] = alarmCommand{
		phone: phone, alarmSerial: alarmSerial, alarmType: alarmType, createdAt: time.Now(),
	}
	return nil
}

func (c *memoryAlarmRepository) ConfirmByCommand(_ context.Context, phone string, commandSerial uint16) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := alarmCommandKey(phone, commandSerial)
	command, ok := c.commands[key]
	if !ok {
		return false, nil
	}
	if time.Since(command.createdAt) > c.policy.ConfirmExpire {
		delete(c.commands, key)
		return false, nil
	}
	delete(c.commands, key)
	for _, event := range c.events[phone] {
		serialMatches := command.alarmSerial == 0 || command.alarmSerial == event.MessageSerial
		typeMatches := command.alarmType == 0 || command.alarmType&event.AlarmSign != 0
		if serialMatches && typeMatches {
			event.Confirmed = true
		}
	}
	return true, nil
}

func alarmCommandKey(phone string, serial uint16) string {
	return fmt.Sprintf("%s/%04x", phone, serial)
}
