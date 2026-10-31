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

type Session struct {
	ID           string // remote addr
	Conn         net.Conn
	Transport    TransportProtocol
	WriteMu      sync.Mutex
	serialMu     sync.Mutex
	serialNumber uint16
	AckTimeout   time.Duration
	MaxRetries   int
	pendingMu    sync.Mutex
	pending      map[uint32]*pendingPacket
	closed       bool
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
				if s.pending[key] == pending {
					delete(s.pending, key)
				}
				s.pendingMu.Unlock()
				_ = s.Conn.Close()
				return
			}
			timeout *= time.Duration(attempt + 2)
		}
		s.pendingMu.Lock()
		if s.pending[key] == pending {
			delete(s.pending, key)
			s.pendingMu.Unlock()
			_ = s.Conn.Close()
			return
		}
		s.pendingMu.Unlock()
	}()
}

func (s *Session) AcknowledgeResponse(msgID uint16, phone string) {
	if msgID == 0x0001 || msgID == 0x8001 {
		return
	}
	s.pendingMu.Lock()
	defer s.pendingMu.Unlock()
	for key, pending := range s.pending {
		if pending.phone == phone && pending.responseID == msgID {
			delete(s.pending, key)
			close(pending.done)
		}
	}
}

func (s *Session) Acknowledge(msgID, serial uint16) {
	key := uint32(msgID)<<16 | uint32(serial)
	s.pendingMu.Lock()
	if pending := s.pending[key]; pending != nil {
		delete(s.pending, key)
		close(pending.done)
	}
	s.pendingMu.Unlock()
}

func (s *Session) CancelReliable(msgID, serial uint16) {
	s.Acknowledge(msgID, serial)
}

func (s *Session) ClosePending() {
	s.pendingMu.Lock()
	s.closed = true
	for key, pending := range s.pending {
		delete(s.pending, key)
		close(pending.done)
	}
	s.pendingMu.Unlock()
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
