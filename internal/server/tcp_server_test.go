package server

import (
	"net"
	"sync"
	"testing"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

func TestTCPServer_serve(t *testing.T) {
	type fields struct {
		listener net.Listener
		sessions map[string]*model.Session
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
				sessions: tt.fields.sessions,
				mutex:    tt.fields.mutex,
			}
			serv.serve(tt.args.session)
		})
	}
}
