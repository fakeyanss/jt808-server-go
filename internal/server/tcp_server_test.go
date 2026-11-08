package server

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

func TestTCPServerShutdownClosesActiveSessions(t *testing.T) {
	serv := NewTCPServer()
	require.NoError(t, serv.Listen("127.0.0.1:0"))
	go serv.Start()

	conn, err := net.Dial("tcp", serv.Addr())
	require.NoError(t, err)
	defer conn.Close()

	require.Eventually(t, func() bool {
		serv.mutex.Lock()
		defer serv.mutex.Unlock()
		return len(serv.sessions) == 1
	}, 2*time.Second, 10*time.Millisecond, "server should register the accepted session")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	require.NoError(t, serv.Shutdown(ctx))

	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, err = conn.Read(make([]byte, 1))
	require.Error(t, err, "client connection must be closed by shutdown")

	serv.mutex.Lock()
	defer serv.mutex.Unlock()
	require.Empty(t, serv.sessions)
}

func TestTCPServer_serve(t *testing.T) {
	type fields struct {
		listener net.Listener
		mutex    *sync.Mutex
	}
	type args struct {
		session *model.Session
	}
	tests := []struct {
		name   string
		fields fields
		args   args
	}{
		// TODO: Add test cases.
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			serv := &TCPServer{
				listener: tt.fields.listener,
				mutex:    tt.fields.mutex,
			}
			serv.serve(tt.args.session)
		})
	}
}
