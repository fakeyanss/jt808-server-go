package control

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
)

func attachmentStatusServer(t *testing.T, status attachmentStatus) string {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/internal/attachment/status", r.URL.Path)
		_ = json.NewEncoder(w).Encode(status)
	}))
	t.Cleanup(server.Close)
	return server.URL
}

// 选点优先剩余额度，并且同一 (phone, alarm) 在租约内保持粘性。
func TestRemoteAttachmentAllocatesStickyNode(t *testing.T) {
	busyURL := attachmentStatusServer(t, attachmentStatus{
		NodeID: "busy", Active: 4, MaxConcurrent: 4, Available: 0, AdvertiseIP: "10.0.0.1", TCPPort: 7400,
	})
	freeURL := attachmentStatusServer(t, attachmentStatus{
		NodeID: "free", Active: 0, MaxConcurrent: 4, Available: 4, AdvertiseIP: "10.0.0.2", TCPPort: 7400, UDPPort: 7401,
	})
	allocator := NewRemoteAttachment(RemoteAttachmentOptions{Nodes: []AttachmentNode{
		{Name: "busy", URL: busyURL},
		{Name: "free", URL: freeURL},
	}})
	lease, err := allocator.Allocate(context.Background(), contracts.AttachmentLeaseRequest{Phone: "13800138000", AlarmID: "A1"})
	require.NoError(t, err)
	require.Equal(t, "10.0.0.2", lease.Endpoint.IP)
	require.EqualValues(t, 7400, lease.Endpoint.TCPPort)
	require.EqualValues(t, 7401, lease.Endpoint.UDPPort)

	// 即使 free 节点额度更高，同一业务键仍复用第一次选中的节点。
	again, err := allocator.Allocate(context.Background(), contracts.AttachmentLeaseRequest{Phone: "13800138000", AlarmID: "A1"})
	require.NoError(t, err)
	require.Equal(t, lease.Endpoint, again.Endpoint)
}

// 没有节点或全部满载时返回明确错误码，而不是静默成功。
func TestRemoteAttachmentReturnsExplicitErrors(t *testing.T) {
	empty := NewRemoteAttachment(RemoteAttachmentOptions{})
	_, err := empty.Allocate(context.Background(), contracts.AttachmentLeaseRequest{Phone: "13800138000", AlarmID: "A1"})
	var unavailable *contracts.AttachmentUnavailableError
	require.ErrorAs(t, err, &unavailable)
	require.Equal(t, contracts.AttachmentCodeNoNodes, unavailable.Code)

	fullURL := attachmentStatusServer(t, attachmentStatus{
		NodeID: "full", Active: 8, MaxConcurrent: 8, Available: 0, AdvertiseIP: "10.0.0.3", TCPPort: 7400,
	})
	full := NewRemoteAttachment(RemoteAttachmentOptions{Nodes: []AttachmentNode{{Name: "full", URL: fullURL}}})
	_, err = full.Allocate(context.Background(), contracts.AttachmentLeaseRequest{Phone: "13800138000", AlarmID: "A1"})
	require.ErrorAs(t, err, &unavailable)
	require.Equal(t, contracts.AttachmentCodeNodesUnavailable, unavailable.Code)
}
