package repository

import (
	"context"
	"fmt"
	"sort"
	"sync"
	"time"
)

// 报警保留策略默认值，与历史内存实现保持一致：
// 去重窗口 30 分钟、确认关联 10 分钟、每设备保留 1000 条。
const (
	DefaultAlarmDedupWindow   = 30 * time.Minute
	DefaultAlarmConfirmExpire = 10 * time.Minute
	DefaultAlarmMaxPerDevice  = 1000
)

type AlarmEvent struct {
	Phone         string    `json:"phone"`
	MessageSerial uint16    `json:"messageSerial"`
	AlarmSign     uint32    `json:"alarmSign"`
	Latitude      uint32    `json:"latitude"`
	Longitude     uint32    `json:"longitude"`
	OccurredAt    string    `json:"occurredAt"`
	ReceivedAt    time.Time `json:"receivedAt"`
	Confirmed     bool      `json:"confirmed"`
}

type alarmCommand struct {
	phone       string
	alarmSerial uint16
	alarmType   uint32
	createdAt   time.Time
}

// AlarmRepository 报警历史仓储（B 类）：按设备追加报警事件，并支持
// 报警确认命令下发后的回执关联。SQL 实现下确认命令关联为进程内协调态，
// 重启后未确认的关联失效；已确认结果写入 alarms 表。
type AlarmRepository interface {
	Record(ctx context.Context, phone string, serial uint16, alarmSign, latitude, longitude uint32, occurredAt string) error
	List(ctx context.Context, phone string) ([]AlarmEvent, error)
	// ListPage 返回设备在接收时间区间内的报警历史与总条数，按接收时间倒序。
	// 用于管理面分页查询；memory 与 SQL 后端均实现。
	ListPage(ctx context.Context, query HistoryQuery) ([]AlarmEvent, int, error)
	RegisterConfirmation(ctx context.Context, phone string, commandSerial, alarmSerial uint16, alarmType uint32) error
	ConfirmByCommand(ctx context.Context, phone string, commandSerial uint16) (bool, error)
}

type memoryAlarmRepository struct {
	mu       sync.Mutex
	events   map[string][]*AlarmEvent
	commands map[string]alarmCommand
	policy   AlarmRetention
}

// NewAlarmRepository 创建独立的内存报警仓储。
// 不传策略时使用历史默认值（30 分钟去重 / 10 分钟确认关联 / 1000 条），
// 保证未配置 retention 时行为与旧版本一致。显式传入的策略中 0 表示关闭对应策略。
func NewAlarmRepository(policies ...AlarmRetention) AlarmRepository {
	policy := AlarmRetention{
		DedupWindow:   DefaultAlarmDedupWindow,
		ConfirmExpire: DefaultAlarmConfirmExpire,
		MaxPerDevice:  DefaultAlarmMaxPerDevice,
	}
	if len(policies) > 0 {
		policy = policies[0]
	}
	return &memoryAlarmRepository{
		events:   make(map[string][]*AlarmEvent),
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
	c.events[phone] = append(c.events[phone], &AlarmEvent{
		Phone: phone, MessageSerial: serial, AlarmSign: alarmSign,
		Latitude: latitude, Longitude: longitude, OccurredAt: occurredAt, ReceivedAt: time.Now(),
	})
	if c.policy.MaxPerDevice > 0 && len(c.events[phone]) > c.policy.MaxPerDevice {
		c.events[phone] = append([]*AlarmEvent(nil), c.events[phone][len(c.events[phone])-c.policy.MaxPerDevice:]...)
	}
	return nil
}

func (c *memoryAlarmRepository) List(_ context.Context, phone string) ([]AlarmEvent, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]AlarmEvent, 0, len(c.events[phone]))
	for _, event := range c.events[phone] {
		out = append(out, *event)
	}
	return out, nil
}

// ListPage 按接收时间区间过滤内存报警，按接收时间倒序分页。
func (c *memoryAlarmRepository) ListPage(_ context.Context, query HistoryQuery) ([]AlarmEvent, int, error) {
	if err := query.Validate(); err != nil {
		return nil, 0, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	filtered := make([]AlarmEvent, 0, len(c.events[query.Phone]))
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
		return []AlarmEvent{}, total, nil
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

// 编译期确认内存实现满足接口。
var _ AlarmRepository = (*memoryAlarmRepository)(nil)
