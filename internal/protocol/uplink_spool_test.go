package protocol

import (
	"context"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/runtime"
	"github.com/fakeyanss/jt808-server-go/internal/storage"
	"github.com/fakeyanss/jt808-server-go/internal/uplink"
)

// ackObservingConn 在第一次写应答时同步检查 spool 目录，用于证明「应答前已落盘」。
type ackObservingConn struct {
	net.Conn
	checked  *atomic.Bool
	observed *atomic.Bool
	spoolDir string
}

func (c *ackObservingConn) Write(b []byte) (int, error) {
	if c.checked.CompareAndSwap(false, true) {
		c.observed.Store(spoolHasJSONRecord(c.spoolDir))
	}
	return c.Conn.Write(b)
}

func spoolHasJSONRecord(dir string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".json") {
			return true
		}
	}
	return false
}

// 报警定位属于 at_least_once：应答写出前 spool 必须已有记录，业务成功后清理。
func TestAtLeastOnceSpoolPersistsBeforeAcknowledgement(t *testing.T) {
	const phone = "188765499021"
	ctx := context.Background()
	stores := storage.NewMemory()
	require.NoError(t, stores.Devices.Upsert(ctx, &model.Device{Phone: phone, Keepalive: time.Minute}))

	spoolDir := filepath.Join(t.TempDir(), "spool")
	service, err := uplink.NewService(
		uplink.NewPolicy("best_effort", "best_effort", "at_least_once", "at_least_once"),
		spoolDir, stores.Locations, stores.Alarms)
	require.NoError(t, err)

	serverEnd, peerEnd := net.Pipe()
	defer serverEnd.Close()
	defer peerEnd.Close()
	go func() {
		buf := make([]byte, 4096)
		for {
			if _, readErr := peerEnd.Read(buf); readErr != nil {
				return
			}
		}
	}()

	var checked, observed atomic.Bool
	conn := &ackObservingConn{Conn: serverEnd, checked: &checked, observed: &observed, spoolDir: spoolDir}
	session := &model.Session{ID: "uplink-spool", Conn: conn, AckTimeout: time.Hour, MaxRetries: 1}
	defer session.ClosePending()

	p := NewPipeline(conn, WithDeps(ProcessorDeps{Storage: stores, Runtime: runtime.NewStores(), Uplink: service}))
	packet := &model.PacketData{
		Header: &model.MsgHeader{
			MsgID:        0x0200,
			SerialNumber: 33,
			PhoneNumber:  phone,
			Attr:         &model.MsgBodyAttr{},
		},
		Body: locationReportBody(1),
	}
	readCtx := context.WithValue(ctx, model.SessionCtxKey{}, session)
	readCtx = context.WithValue(readCtx, model.PacketDecodeCtxKey{}, packet)

	_, err = process()(readCtx, p)
	require.NoError(t, err)
	require.True(t, checked.Load(), "acknowledgement must be written")
	require.True(t, observed.Load(), "spool record must exist before the acknowledgement is written")
	require.False(t, spoolHasJSONRecord(spoolDir), "spool records must be cleared after successful business processing")
}
