package server

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/fakeYanss/jt808-server-go/internal/protocol"
	"github.com/fakeYanss/jt808-server-go/internal/protocol/model"
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
	go serv.monitorSessionCnt()

	for {
		conn, err := serv.listener.Accept()
		if err != nil {
			log.Error().Err(err).Msg("Fail to accept ")
		} else {
			session := serv.accept(conn)
			go serv.serve(session)
		}
	}
}

func (serv *TCPServer) Stop() {
	serv.listener.Close()
}

func (serv *TCPServer) monitorSessionCnt() {
	for {
		log.Debug().
			Int("total_conn_cnt", len(serv.sessions)).
			Msg("Monitoring total conn count")

		time.Sleep(10 * time.Second)
	}
}

// 将conn封装为逻辑session
func (serv *TCPServer) accept(conn net.Conn) *model.Session {
	remoteAddr := conn.RemoteAddr().String()

	serv.mutex.Lock()
	defer serv.mutex.Unlock()

	session := &model.Session{
		Conn: conn,
		ID:   remoteAddr, // using remote addr default
	}

	serv.sessions[remoteAddr] = session

	log.Debug().
		Str("remote_addr", remoteAddr).
		Int("total_conn_cnt", len(serv.sessions)).
		Msg("Accepting connection from remtote.")

	return session
}

func (serv *TCPServer) remove(session *model.Session) {
	serv.mutex.Lock()
	defer serv.mutex.Unlock()

	delete(serv.sessions, session.ID)

	log.Debug().
		Str("id", session.ID).
		Int("total_conn_cnt", len(serv.sessions)).
		Msg("Closing connection from remote.")
}

// 处理每个session的消息
func (serv *TCPServer) serve(session *model.Session) {
	defer serv.remove(session)

	processGroup := &protocol.ProcessGroup{
		FH: protocol.NewJT808FrameHandler(session.Conn),
		PC: protocol.NewJT808PacketCodec(),
		MH: protocol.NewJT808MsgHandler(),
	}

	for {
		// 记录value ctx
		ctx := context.WithValue(context.Background(), model.SessionCtxKey{}, session)

		err := protocol.ProcessConn(ctx, processGroup)

		if err == nil {
			continue
		}

		log.Error().
			Err(err).
			Str("id", session.ID).
			Msg("Failed to serve session")
		if errors.Is(err, io.EOF) {
			break // close connection when EOF
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

	frameHandler := protocol.NewJT808FrameHandler(session.Conn)

	packet, err := cmd.Encode()
	if err != nil {
		log.Error().
			Err(err).
			Str("id", id).
			Msg("Fail to encode cmd to packet.")
		return
	}
	err = frameHandler.Send(packet)
	if err != nil {
		log.Error().
			Err(err).
			Str("device", id).
			Msg("Fail to send cmd to device.")
		return
	}
}
