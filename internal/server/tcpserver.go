package server

import (
	"context"
	"io"
	"net"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"

	"github.com/fakeYanss/jt808-server-go/internal/protocol"
	"github.com/fakeYanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeYanss/jt808-server-go/pkg/routines"
)

type TCPServer struct {
	listener net.Listener
	sessions map[string]*model.Session
	mutex    *sync.Mutex
}

func NewTCPServer() *TCPServer {
	return &TCPServer{
		mutex:    &sync.Mutex{},
		sessions: make(map[string]*model.Session),
	}
}

func (serv *TCPServer) Listen(addr string) error {
	l, err := net.Listen("tcp", addr)
	if err == nil {
		serv.listener = l
		log.Debug().Msgf("Listening on %v", addr)
		return nil
	}

	return err
}

func (serv *TCPServer) Start() {
	serv.monitorSessionCnt()

	for {
		conn, err := serv.listener.Accept()
		if err != nil {
			log.Error().Err(err).Msg("Fail to accept ")
		} else {
			session := serv.accept(conn)
			routines.GoSafe(func() { serv.serve(session) })
		}
	}
}

func (serv *TCPServer) Stop() {
	serv.listener.Close()
}

func (serv *TCPServer) monitorSessionCnt() {
	routines.GoSafe(func() {
		for {
			log.Debug().
				Int("total_conn_cnt", len(serv.sessions)).
				Msg("Monitoring total conn count")

			time.Sleep(10 * time.Second)
		}
	})
}

// 将conn封装为逻辑session
func (serv *TCPServer) accept(conn net.Conn) *model.Session {
	remoteAddr := conn.RemoteAddr().String()
	serv.mutex.Lock()
	session := &model.Session{
		Conn: conn,
		ID:   remoteAddr, // using remote addr default
	}
	serv.sessions[remoteAddr] = session
	serv.mutex.Unlock()

	log.Debug().
		Str("remote_addr", remoteAddr).
		Int("total_conn_cnt", len(serv.sessions)).
		Msg("Accepting connection from remtote.")

	return session
}

func (serv *TCPServer) remove(session *model.Session) {
	serv.mutex.Lock()
	defer serv.mutex.Unlock()

	session.Conn.Close()
	delete(serv.sessions, session.ID)

	log.Debug().
		Str("id", session.ID).
		Int("total_conn_cnt", len(serv.sessions)).
		Msg("Closing connection from remote.")
}

// 处理每个session的消息
func (serv *TCPServer) serve(session *model.Session) {
	defer serv.remove(session)

	pg := protocol.NewProcessGroup(session.Conn)
	for {
		// 记录value ctx
		ctx := context.WithValue(context.Background(), model.SessionCtxKey{}, session)

		err := pg.ProcessConnRead(ctx)

		if err == nil {
			continue
		}

		log.Error().
			Err(err).
			Str("id", session.ID).
			Msg("Failed to serve session")

		switch {
		case errors.Is(err, io.EOF), errors.Is(err, io.ErrClosedPipe), errors.Is(err, net.ErrClosed):
			return // close connection when EOF or closed
		default:
			time.Sleep(1 * time.Second)
		}
	}
}

// 发送消息到终端设备, 外部调用
func (serv *TCPServer) Send(id string, cmd model.JT808Cmd) {
	session := serv.sessions[id]
	if session == nil {
		log.Warn().
			Str("id", id).
			Msg("Fail to get session from cache, maybe conn was closed.")
		return
	}

	pg := protocol.NewProcessGroup(session.Conn)

	// 记录value ctx
	ctx := context.WithValue(context.Background(), model.CmdCtxKey{}, cmd)

	err := pg.ProcessConnWrite(ctx)

	if err == nil {
		return
	}

	if errors.Is(err, io.ErrClosedPipe) {
		serv.remove(session)
	}

	errMsg := "Failed to send jtcmd to device"
	log.Error().
		Err(err).
		Str("device", id).
		Msg(errMsg)
}
