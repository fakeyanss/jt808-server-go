package model

import (
	"net"
	"sync"
	"time"
)

const (
	TCPProto TransportProtocol = "TCP"
	UDPProto TransportProtocol = "UDP"
)

type TransportProtocol string

type (
	SessionCtxKey struct{} // 定义全局session context key

	FrameCtxKey struct{}

	PacketDecodeCtxKey struct{}

	ProcessDataCtxKey struct{}

	IncomingMsgCtxKey struct{}

	OutgoingMsgCtxKey struct{}

	PacketEncodeCtxKey struct{}
)

// ReliableStatus 描述可靠下行报文的最终投递结果。
type ReliableStatus string

const (
	// ReliableStatusAcked 表示已收到终端应答。
	ReliableStatusAcked ReliableStatus = "acked"
	// ReliableStatusFailed 表示写失败、重传耗尽或会话关闭。
	ReliableStatusFailed ReliableStatus = "failed"
)

// ReliableResult 是可靠下行报文的投递结果事件，供 outbox 回填状态。
type ReliableResult struct {
	MsgID  uint16
	Serial uint16
	Phone  string
	Status ReliableStatus
	Reason string
	// TerminalResult 来自 0x0001 报文体：0 成功、1 失败、2 消息有误、3 不支持。
	// 非 0x0001 应答与超时事件为 nil，语义按成功或传输失败处理。
	TerminalResult *uint8
}

const (
	// ReliableReasonRetransmissionExhausted 标记应答重传耗尽，是应答超时的终态。
	ReliableReasonRetransmissionExhausted = "retransmission exhausted"
)

type Session struct {
	ID        string // remote addr
	Conn      net.Conn
	Transport TransportProtocol
	// OnReliableResult 在可靠报文确认、重传失败或会话关闭时回调。
	// 必须在会话开始收发前设置；回调实现不得阻塞读路径。
	OnReliableResult func(ReliableResult)
	// ConnectedAt 是连接建立时间，仅在创建会话时写入一次，不参与消息处理热路径。
	ConnectedAt  time.Time
	WriteMu      sync.Mutex
	serialMu     sync.Mutex
	serialNumber uint16
	AckTimeout   time.Duration
	MaxRetries   int
	pendingMu    sync.Mutex
	pending      map[uint32]*pendingPacket
	closed       bool
	// closeAfterResponse 用于拒绝冒名连接：应答帧发送完成后由服务层关闭连接。
	closeAfterResponse bool
}

const (
	defaultAckTimeout        = 5 * time.Second
	defaultMaxRetransmission = 3
)

type pendingPacket struct {
	frame      []byte
	phone      string
	responseID uint16
	done       chan struct{}
}

func (s *Session) TrackReliable(msgID, serial uint16, phone string, frame []byte) {
	if msgID == 0x0001 || msgID == 0x8001 {
		return
	}
	key := uint32(msgID)<<16 | uint32(serial)
	if msgID == 0x8100 { // Registration is followed by 0x0102 authentication.
		return
	}
	responseID := uint16(0x0001)
	if msgID < 0x8000 {
		responseID = 0x8001
	}
	switch msgID {
	case 0x0100:
		responseID = 0x8100
	case 0x8104:
		responseID = 0x0104
	case 0x9205:
		responseID = 0x1205
	case 0x8608:
		responseID = 0x0608
	case 0x8700:
		responseID = 0x0700
	case 0x8702:
		responseID = 0x0702
	case 0x0801:
		responseID = 0x8800
	case 0x8801:
		responseID = 0x0805
	case 0x8802:
		responseID = 0x0802
	}
	pending := &pendingPacket{frame: append([]byte(nil), frame...), phone: phone, responseID: responseID, done: make(chan struct{})}
	s.pendingMu.Lock()
	if s.closed {
		s.pendingMu.Unlock()
		return
	}
	if s.pending == nil {
		s.pending = make(map[uint32]*pendingPacket)
	}
	if previous := s.pending[key]; previous != nil {
		close(previous.done)
	}
	s.pending[key] = pending
	s.pendingMu.Unlock()

	go func() {
		ackTimeout := s.AckTimeout
		if ackTimeout <= 0 {
			ackTimeout = defaultAckTimeout
		}
		maxRetries := s.MaxRetries
		if maxRetries <= 0 {
			maxRetries = defaultMaxRetransmission
		}
		timeout := ackTimeout
		for attempt := 0; attempt < maxRetries; attempt++ {
			timer := time.NewTimer(timeout)
			select {
			case <-pending.done:
				timer.Stop()
				return
			case <-timer.C:
			}
			s.pendingMu.Lock()
			if s.pending[key] != pending {
				s.pendingMu.Unlock()
				return
			}
			s.pendingMu.Unlock()

			if err := s.WriteFrame(pending.frame); err != nil {
				s.pendingMu.Lock()
				matched := s.pending[key] == pending
				if matched {
					delete(s.pending, key)
				}
				s.pendingMu.Unlock()
				if matched {
					s.notifyReliable(ReliableResult{MsgID: msgID, Serial: serial, Phone: phone, Status: ReliableStatusFailed, Reason: "write failed"})
				}
				_ = s.Conn.Close()
				return
			}
			timeout *= time.Duration(attempt + 2)
		}
		s.pendingMu.Lock()
		if s.pending[key] == pending {
			delete(s.pending, key)
			s.pendingMu.Unlock()
			s.notifyReliable(ReliableResult{MsgID: msgID, Serial: serial, Phone: phone, Status: ReliableStatusFailed, Reason: ReliableReasonRetransmissionExhausted})
			_ = s.Conn.Close()
			return
		}
		s.pendingMu.Unlock()
	}()
}

// notifyReliable 派发投递结果事件；回调必须非阻塞。
func (s *Session) notifyReliable(result ReliableResult) {
	if s.OnReliableResult != nil {
		s.OnReliableResult(result)
	}
}

// Acknowledge 按（消息 ID，流水号）精确清算一条待确认项，返回是否命中。
// 重复应答、错会话应答或找不到等待项时返回 false，且不会清除任何等待项。
func (s *Session) Acknowledge(msgID, serial uint16) bool {
	key := uint32(msgID)<<16 | uint32(serial)
	s.pendingMu.Lock()
	pending := s.pending[key]
	if pending == nil {
		s.pendingMu.Unlock()
		return false
	}
	delete(s.pending, key)
	close(pending.done)
	s.pendingMu.Unlock()
	s.notifyReliable(ReliableResult{MsgID: msgID, Serial: serial, Phone: pending.phone, Status: ReliableStatusAcked})
	return true
}

// AcknowledgeSerial 按被应答的消息流水号精确清算。
// 流水号在单个会话内唯一，无需再区分被应答的消息 ID；
// 同时校验报文头手机号，避免同号多会话场景下错清其它会话的等待项。
func (s *Session) AcknowledgeSerial(serial uint16, phone string) bool {
	return s.acknowledgeSerial(serial, phone, nil)
}

// AcknowledgeSerialWithResult 与 AcknowledgeSerial 相同，但把 0x0001 的
// 应答结果一并带给 outbox，用于区分成功、终端执行失败与协议错误。
func (s *Session) AcknowledgeSerialWithResult(serial uint16, phone string, result uint8) bool {
	return s.acknowledgeSerial(serial, phone, &result)
}

func (s *Session) acknowledgeSerial(serial uint16, phone string, terminalResult *uint8) bool {
	var (
		msgID     uint16
		serialHit uint16
		matched   bool
	)
	s.pendingMu.Lock()
	for key, pending := range s.pending {
		if pending.phone != phone || uint16(key) != serial {
			continue
		}
		delete(s.pending, key)
		close(pending.done)
		msgID = uint16(key >> 16)
		serialHit = uint16(key)
		matched = true
		break
	}
	s.pendingMu.Unlock()
	if matched {
		s.notifyReliable(ReliableResult{
			MsgID:          msgID,
			Serial:         serialHit,
			Phone:          phone,
			Status:         ReliableStatusAcked,
			TerminalResult: terminalResult,
		})
	}
	return matched
}

// AcknowledgeTypedResponse 用于协议体不含应答流水号的应答（0x0608/0x0702/0x8800）。
// 这类应答无法精确到单条，只有在同类等待项唯一时才清算，避免一条应答误清多条。
func (s *Session) AcknowledgeTypedResponse(responseID uint16, phone string) bool {
	s.pendingMu.Lock()
	var matchedKey uint32
	matched := 0
	for key, pending := range s.pending {
		if pending.phone == phone && pending.responseID == responseID {
			matchedKey = key
			matched++
		}
	}
	if matched != 1 {
		s.pendingMu.Unlock()
		return false
	}
	pending := s.pending[matchedKey]
	delete(s.pending, matchedKey)
	close(pending.done)
	s.pendingMu.Unlock()
	s.notifyReliable(ReliableResult{
		MsgID:  uint16(matchedKey >> 16),
		Serial: uint16(matchedKey),
		Phone:  pending.phone,
		Status: ReliableStatusAcked,
	})
	return true
}

// PendingCount 返回当前等待应答的条目数，用于观测与测试。
func (s *Session) PendingCount() int {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	return len(s.pending)
}

// CancelReliable 放弃一条可靠报文（例如写帧失败），按投递失败上报。
func (s *Session) CancelReliable(msgID, serial uint16) {
	key := uint32(msgID)<<16 | uint32(serial)
	s.pendingMu.Lock()
	pending := s.pending[key]
	if pending == nil {
		s.pendingMu.Unlock()
		return
	}
	delete(s.pending, key)
	close(pending.done)
	s.pendingMu.Unlock()
	s.notifyReliable(ReliableResult{
		MsgID:  msgID,
		Serial: serial,
		Phone:  pending.phone,
		Status: ReliableStatusFailed,
		Reason: "send failed",
	})
}

func (s *Session) ClosePending() {
	s.pendingMu.Lock()
	s.closed = true
	results := make([]ReliableResult, 0, len(s.pending))
	for key, pending := range s.pending {
		delete(s.pending, key)
		close(pending.done)
		results = append(results, ReliableResult{
			MsgID:  uint16(key >> 16),
			Serial: uint16(key),
			Phone:  pending.phone,
			Status: ReliableStatusFailed,
			Reason: "session closed",
		})
	}
	s.pendingMu.Unlock()
	for _, result := range results {
		s.notifyReliable(result)
	}
}

// MarkCloseAfterResponse 标记本会话在发送完当前应答后关闭，用于拒绝冒名连接。
func (s *Session) MarkCloseAfterResponse() {
	s.pendingMu.Lock()
	s.closeAfterResponse = true
	s.pendingMu.Unlock()
}

// ShouldCloseAfterResponse 返回本会话是否已被标记为应答后关闭。
func (s *Session) ShouldCloseAfterResponse() bool {
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	return s.closeAfterResponse
}

func (s *Session) WriteFrame(frame []byte) error {
	s.WriteMu.Lock()
	defer s.WriteMu.Unlock()
	_ = s.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
	for len(frame) > 0 {
		n, err := s.Conn.Write(frame)
		if err != nil {
			return err
		}
		if n == 0 {
			return net.ErrClosed
		}
		frame = frame[n:]
	}
	return nil
}

func (s *Session) GetTransProto() TransportProtocol {
	if s.Transport != "" {
		return s.Transport
	}
	if s.Conn != nil {
		return TCPProto
	}
	return UDPProto
}

func (s *Session) GetNextSerialNum() uint16 {
	s.serialMu.Lock()
	defer s.serialMu.Unlock()
	return s.nextSerialNumLocked()
}

func (s *Session) GetNextSerialNums(count int) []uint16 {
	if count < 0 {
		return nil
	}
	s.serialMu.Lock()
	defer s.serialMu.Unlock()
	serials := make([]uint16, count)
	for i := range serials {
		serials[i] = s.nextSerialNumLocked()
	}
	return serials
}

func (s *Session) nextSerialNumLocked() uint16 {
	if s.serialNumber == 65535 {
		s.serialNumber = 1
	} else {
		s.serialNumber++
	}
	return s.serialNumber
}

// 定义Packet Data结构
type PacketData struct {
	Header       *MsgHeader // 消息头
	Body         []byte     // 消息体
	VerifyCode   byte       // 校验码
	SegCompleted bool       // 是否分包传输结束
}

// 定义消息处理结果数据
type ProcessData struct {
	Incoming JT808Msg // 收到的消息
	Outgoing JT808Msg // 发出的消息, 无需回复时可为nil
}
