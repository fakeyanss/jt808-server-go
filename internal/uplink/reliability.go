package uplink

import (
	"strings"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

// Reliability 是单条上行业务记录的投递语义。
type Reliability string

const (
	// BestEffort 先应答后落库：落库失败只记日志，不等终端重传。
	BestEffort Reliability = "best_effort"
	// AtLeastOnce 先落库后应答：落库失败不应答，终端会按协议重传。
	// 依赖现有 SQL 持久化，不引入本地文件队列。
	AtLeastOnce Reliability = "at_least_once"
)

// Class 是上行消息的可靠性分类。
type Class string

const (
	ClassDefault       Class = "default"
	ClassLocation      Class = "location"
	ClassBatchLocation Class = "batch_location"
	ClassAlarm         Class = "alarm"
	// ClassSessionClosed 是会话结束事件 session.closed 的独立分类。
	// 它不携带终端报文，也没有终端重传来兜底，通常需要比默认更多的重试。
	ClassSessionClosed Class = "session_closed"
)

// Policy 按消息分类选择投递语义。
type Policy struct {
	Default       Reliability
	Location      Reliability
	BatchLocation Reliability
	Alarm         Reliability
}

// NewPolicy 从配置值构造策略；无法识别的值按 best_effort 处理。
func NewPolicy(defaultReliability, location, batchLocation, alarm string) Policy {
	return Policy{
		Default:       ParseReliability(defaultReliability),
		Location:      ParseReliability(location),
		BatchLocation: ParseReliability(batchLocation),
		Alarm:         ParseReliability(alarm),
	}
}

// ParseReliability 归一化配置值：只有显式 at_least_once 才升级语义。
func ParseReliability(value string) Reliability {
	if strings.EqualFold(strings.TrimSpace(value), string(AtLeastOnce)) {
		return AtLeastOnce
	}
	return BestEffort
}

// Classify 判定消息分类：
//   - 0x0704 为批量定位；
//   - 0x0200 带报警标志位为报警，否则为实时定位；
//   - 其余走 default。
func (p Policy) Classify(msgID uint16, alarmSign uint32) Class {
	switch msgID {
	case 0x0704:
		return ClassBatchLocation
	case 0x0200:
		if alarmSign != 0 {
			return ClassAlarm
		}
		return ClassLocation
	default:
		return ClassDefault
	}
}

// For 返回分类对应的投递语义。
func (p Policy) For(class Class) Reliability {
	switch class {
	case ClassLocation:
		return p.Location
	case ClassBatchLocation:
		return p.BatchLocation
	case ClassAlarm:
		return p.Alarm
	default:
		return p.Default
	}
}

// AtLeastOnce 判定该条上行消息是否必须先完成落库再应答。
// 报警分类需要看消息体里的报警标志位，其余分类只依赖消息 ID。
func (p Policy) AtLeastOnce(msgID uint16, incoming model.JT808Msg) bool {
	var alarmSign uint32
	if msg, ok := incoming.(*model.Msg0200); ok && msg != nil {
		alarmSign = msg.AlarmSign
	}
	return p.For(p.Classify(msgID, alarmSign)) == AtLeastOnce
}

// AttemptOverrides 按分类覆盖单条事件的投递次数（含首次）。
// 零值表示未配置，回落到全局 maxAttempts。
type AttemptOverrides struct {
	Default       int
	Location      int
	BatchLocation int
	Alarm         int
	SessionClosed int
}

// AttemptPolicy 按分类选择单条事件的投递次数。
type AttemptPolicy struct {
	fallback  int
	overrides AttemptOverrides
}

// NewAttemptPolicy 构造投递次数策略；fallback 非法时按 1 次处理。
func NewAttemptPolicy(fallback int, overrides AttemptOverrides) AttemptPolicy {
	if fallback < 1 {
		fallback = 1
	}
	return AttemptPolicy{fallback: fallback, overrides: overrides}
}

// For 返回分类对应的投递次数：未配置的分类回落到全局次数。
func (p AttemptPolicy) For(class Class) int {
	var attempts int
	switch class {
	case ClassLocation:
		attempts = p.overrides.Location
	case ClassBatchLocation:
		attempts = p.overrides.BatchLocation
	case ClassAlarm:
		attempts = p.overrides.Alarm
	case ClassSessionClosed:
		attempts = p.overrides.SessionClosed
	default:
		attempts = p.overrides.Default
	}
	if attempts < 1 {
		attempts = p.fallback
	}
	return attempts
}

// ForEvent 按出口事件判定分类并返回投递次数。
func (p AttemptPolicy) ForEvent(event contracts.UplinkEvent) int {
	return p.For(ClassifyEvent(event))
}

// ClassifyEvent 判定出口事件的上行分类。
// 分类口径与可靠性 Policy 一致，另外把 session.closed 单列为会话结束。
func ClassifyEvent(event contracts.UplinkEvent) Class {
	if event.Event == "session.closed" {
		return ClassSessionClosed
	}
	var alarmSign uint32
	if msg, ok := event.Payload.(*model.Msg0200); ok && msg != nil {
		alarmSign = msg.AlarmSign
	}
	switch event.MsgID {
	case 0x0704:
		return ClassBatchLocation
	case 0x0200:
		if alarmSign != 0 {
			return ClassAlarm
		}
		return ClassLocation
	default:
		return ClassDefault
	}
}
