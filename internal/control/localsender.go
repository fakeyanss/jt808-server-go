package control

import (
	"context"
	"fmt"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/runtime"
)

// LocalSenderDependencies 是进程内直连发送的依赖。
type LocalSenderDependencies struct {
	Conn runtime.ConnectionRegistry
	Send func(sessionID string, message model.JT808Msg, serial uint16) error
}

// LocalSender 是 standalone / 同进程形态下的 DownlinkSender 实现。
//
// 它只做会话定位与发送，不等待应答；应答仍由会话可靠投递回调或
// 0x0001 上行事件推进 outbox。
type LocalSender struct {
	conn runtime.ConnectionRegistry
	send func(sessionID string, message model.JT808Msg, serial uint16) error
}

var _ contracts.DownlinkSender = (*LocalSender)(nil)

// NewLocalSender 构造进程内发送器。
func NewLocalSender(deps LocalSenderDependencies) (*LocalSender, error) {
	if deps.Conn == nil {
		return nil, fmt.Errorf("local downlink sender: connection registry is required")
	}
	if deps.Send == nil {
		return nil, fmt.Errorf("local downlink sender: send function is required")
	}
	return &LocalSender{conn: deps.Conn, send: deps.Send}, nil
}

// Send 实现 contracts.DownlinkSender。
func (s *LocalSender) Send(ctx context.Context, request contracts.DownlinkRequest) (contracts.DownlinkReceipt, error) {
	if s == nil {
		return contracts.DownlinkReceipt{}, fmt.Errorf("local downlink sender is not configured")
	}
	if request.Message == nil || request.Message.GetHeader() == nil {
		return contracts.DownlinkReceipt{}, fmt.Errorf("downlink message header is required")
	}
	sessionID := request.SessionID
	if sessionID == "" {
		session, err := s.conn.GetByPhone(ctx, request.Phone)
		if err != nil || session == nil {
			return contracts.DownlinkReceipt{Status: contracts.DownlinkStatusOffline}, nil
		}
		sessionID = session.ID
	}
	serial := request.Serial
	if serial == 0 {
		session, err := s.conn.Get(ctx, sessionID)
		if err != nil || session == nil {
			return contracts.DownlinkReceipt{Status: contracts.DownlinkStatusOffline}, nil
		}
		serial = session.GetNextSerialNum()
		request.Message.GetHeader().SerialNumber = serial
	}
	if err := s.send(sessionID, request.Message, serial); err != nil {
		// 会话在发送前失效属于可重试，不当作传输层错误。
		return contracts.DownlinkReceipt{Status: contracts.DownlinkStatusRejected, Reason: err.Error()}, nil
	}
	return contracts.DownlinkReceipt{
		Serial: serial,
		Status: contracts.DownlinkStatusAccepted,
	}, nil
}
