package control

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	actionpkg "github.com/fakeyanss/jt808-server-go/internal/action"
	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

type fakeAttachmentAllocator struct {
	requests []contracts.AttachmentLeaseRequest
	lease    contracts.AttachmentLease
	err      error
}

func (f *fakeAttachmentAllocator) Allocate(_ context.Context, request contracts.AttachmentLeaseRequest) (contracts.AttachmentLease, error) {
	f.requests = append(f.requests, request)
	return f.lease, f.err
}

func (f *fakeAttachmentAllocator) Release(context.Context, string) error { return nil }

type fakeDownlinkSender struct {
	requests []contracts.DownlinkRequest
}

func (f *fakeDownlinkSender) Send(_ context.Context, request contracts.DownlinkRequest) (contracts.DownlinkReceipt, error) {
	f.requests = append(f.requests, request)
	return contracts.DownlinkReceipt{Status: contracts.DownlinkStatusAccepted}, nil
}

// 控制面识别到带报警标识号的报警后，应自动下发 0x9208 到选中的附件节点。
func TestAutoUploadTriggerSends9208WithAllocatedEndpoint(t *testing.T) {
	allocator := &fakeAttachmentAllocator{lease: contracts.AttachmentLease{
		LeaseID:  "att-1",
		Endpoint: contracts.AttachmentEndpoint{IP: "10.0.0.9", TCPPort: 7400, UDPPort: 7401},
	}}
	sender := &fakeDownlinkSender{}
	trigger := NewAutoUploadTrigger(actionpkg.Noop{}, allocator, sender, 0)
	alarmID := []byte("ABCDEFGHIJKLMNOP")
	event := actionpkg.AlarmEvent{Phone: "13800138000", AlarmSign: 1, AlarmID: alarmID}

	trigger.OnAlarm(context.Background(), event)
	trigger.OnAlarm(context.Background(), event) // 同一报警标识号去重

	require.Len(t, allocator.requests, 1)
	require.Equal(t, "13800138000", allocator.requests[0].Phone)
	require.Len(t, sender.requests, 1)
	msg, ok := sender.requests[0].Message.(*model.Msg9208)
	require.True(t, ok)
	require.Equal(t, "10.0.0.9", msg.IP)
	require.EqualValues(t, 7400, msg.TCPPort)
	require.EqualValues(t, 7401, msg.UDPPort)
	require.Equal(t, alarmID, msg.AlarmSign)
	require.Equal(t, "13800138000", msg.Header.PhoneNumber)
}

// 没有可用节点时不能静默成功：不发送下行，等待人工或下一轮报警。
func TestAutoUploadTriggerSkipsWhenNoNode(t *testing.T) {
	allocator := &fakeAttachmentAllocator{err: &contracts.AttachmentUnavailableError{
		Code: contracts.AttachmentCodeNodesUnavailable, Reason: "all nodes are full",
	}}
	sender := &fakeDownlinkSender{}
	trigger := NewAutoUploadTrigger(actionpkg.Noop{}, allocator, sender, 0)
	trigger.OnAlarm(context.Background(), actionpkg.AlarmEvent{
		Phone: "13800138000", AlarmSign: 1, AlarmID: []byte("ABCDEFGHIJKLMNOP"),
	})
	require.Len(t, allocator.requests, 1)
	require.Empty(t, sender.requests)
}
