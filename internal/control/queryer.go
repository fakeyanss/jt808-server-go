package control

import (
	"context"
	"fmt"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

// DownlinkQueryer 用下行契约实现控制面的查询 broker：网关负责等待终端应答，
// 管控只构造查询消息并接收应答体。
type DownlinkQueryer struct {
	Sender contracts.DownlinkSender
}

// Query 发送查询并等待应答体。
func (q *DownlinkQueryer) Query(ctx context.Context, phone string, msg model.JT808Msg, expectMsgID uint16, timeout time.Duration) ([]byte, error) {
	if q == nil || q.Sender == nil {
		return nil, fmt.Errorf("downlink queryer is not configured")
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	receipt, err := q.Sender.Send(ctx, contracts.DownlinkRequest{
		Phone:               phone,
		IdempotencyKey:      fmt.Sprintf("api-query-%s-%04X-%d", phone, expectMsgID, time.Now().UnixNano()),
		Serial:              0,
		Message:             msg,
		ExpectResponseMsgID: expectMsgID,
		ResponseTimeout:     timeout,
	})
	if err != nil {
		return nil, err
	}
	if receipt.Status != contracts.DownlinkStatusAcked {
		reason := receipt.Reason
		if reason == "" {
			reason = receipt.Status
		}
		return nil, fmt.Errorf("query was not acknowledged: %s", reason)
	}
	if len(receipt.ResponseBody) == 0 {
		return nil, fmt.Errorf("query response body is empty")
	}
	return receipt.ResponseBody, nil
}
