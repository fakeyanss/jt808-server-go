package repository

import (
	"fmt"
	"sync"
	"time"
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
// 报警确认命令下发后的回执关联。内存实现仅保留有界窗口（每设备 1000 条）。
type AlarmRepository interface {
	Record(phone string, serial uint16, alarmSign, latitude, longitude uint32, occurredAt string)
	List(phone string) []AlarmEvent
	RegisterConfirmation(phone string, commandSerial, alarmSerial uint16, alarmType uint32)
	ConfirmByCommand(phone string, commandSerial uint16) bool
}

type memoryAlarmRepository struct {
	mu       sync.Mutex
	events   map[string][]*AlarmEvent
	commands map[string]alarmCommand
}

// NewAlarmRepository 创建独立的内存报警仓储。
func NewAlarmRepository() AlarmRepository { return newMemoryAlarmRepository() }

func newMemoryAlarmRepository() *memoryAlarmRepository {
	return &memoryAlarmRepository{events: make(map[string][]*AlarmEvent), commands: make(map[string]alarmCommand)}
}

func (c *memoryAlarmRepository) Record(phone string, serial uint16, alarmSign, latitude, longitude uint32, occurredAt string) {
	if alarmSign == 0 {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, event := range c.events[phone] {
		if event.MessageSerial == serial && time.Since(event.ReceivedAt) < 30*time.Minute {
			event.AlarmSign = alarmSign
			return
		}
	}
	c.events[phone] = append(c.events[phone], &AlarmEvent{Phone: phone, MessageSerial: serial, AlarmSign: alarmSign, Latitude: latitude, Longitude: longitude, OccurredAt: occurredAt, ReceivedAt: time.Now()})
	if len(c.events[phone]) > 1000 {
		c.events[phone] = append([]*AlarmEvent(nil), c.events[phone][len(c.events[phone])-1000:]...)
	}
}

func (c *memoryAlarmRepository) List(phone string) []AlarmEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]AlarmEvent, 0, len(c.events[phone]))
	for _, event := range c.events[phone] {
		out = append(out, *event)
	}
	return out
}

func (c *memoryAlarmRepository) RegisterConfirmation(phone string, commandSerial, alarmSerial uint16, alarmType uint32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for key, command := range c.commands {
		if time.Since(command.createdAt) > 10*time.Minute {
			delete(c.commands, key)
		}
	}
	c.commands[alarmCommandKey(phone, commandSerial)] = alarmCommand{phone: phone, alarmSerial: alarmSerial, alarmType: alarmType, createdAt: time.Now()}
}

func (c *memoryAlarmRepository) ConfirmByCommand(phone string, commandSerial uint16) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	key := alarmCommandKey(phone, commandSerial)
	command, ok := c.commands[key]
	if !ok {
		return false
	}
	delete(c.commands, key)
	for _, event := range c.events[phone] {
		serialMatches := command.alarmSerial == 0 || command.alarmSerial == event.MessageSerial
		typeMatches := command.alarmType == 0 || command.alarmType&event.AlarmSign != 0
		if serialMatches && typeMatches {
			event.Confirmed = true
		}
	}
	return true
}

func alarmCommandKey(phone string, serial uint16) string {
	return fmt.Sprintf("%s/%04x", phone, serial)
}

// 编译期确认内存实现满足接口。
var _ AlarmRepository = (*memoryAlarmRepository)(nil)
