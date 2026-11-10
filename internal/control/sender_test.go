package control

import (
	"context"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

// senderFunc 把旧式发送函数适配成 contracts.DownlinkSender，供测试复用。
type senderFunc func(sessionID string, msg model.JT808Msg, serial uint16) error

func (f senderFunc) Send(_ context.Context, request contracts.DownlinkRequest) (contracts.DownlinkReceipt, error) {
	if err := f(request.SessionID, request.Message, request.Serial); err != nil {
		return contracts.DownlinkReceipt{Status: contracts.DownlinkStatusRejected, Reason: err.Error()}, nil
	}
	return contracts.DownlinkReceipt{Serial: request.Serial, Status: contracts.DownlinkStatusAccepted}, nil
}
