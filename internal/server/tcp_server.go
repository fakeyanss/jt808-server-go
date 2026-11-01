package server

import (
	"context"
	"io"
	"net"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/action"
	"github.com/fakeyanss/jt808-server-go/internal/protocol"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/storage"
	"github.com/fakeyanss/jt808-server-go/pkg/routines"
)

type TCPServer struct {
	listener           net.Listener
	ackTimeout         time.Duration
	maxRetransmissions int
	hooks              action.Hooks

	// sessions map[string]*model.Session
	mutex *sync.Mutex
}

// SetHooks 设置生命周期钩子。未设置时使用 action.Noop。
func (serv *TCPServer) SetHooks(hooks action.Hooks) {
	serv.hooks = hooks
}

func (serv *TCPServer) SetReliability(ackTimeout time.Duration, maxRetransmissions int) {
	serv.ackTimeout = ackTimeout
	serv.maxRetransmissions = maxRetransmissions
}

func NewTCPServer() *TCPServer {
	return &TCPServer{
		mutex: &sync.Mutex{},
		// sessions: make(map[string]*model.Session),
	}
}

func (serv *TCPServer) Listen(addr string) error {
	l, err := net.Listen("tcp", addr)
	if err == nil {
		serv.listener = l
		log.Debug().Msgf("Listening on %v", addr)
	}

	return err
}

func (serv *TCPServer) Start() {
	for {
		conn, err := serv.listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return
			}
			log.Error().Err(err).Msg("Fail to do listener accept")
		} else {
			session := serv.accept(conn)
			routines.GoSafe(func() { serv.serve(session) })
		}
	}
}

func (serv *TCPServer) Stop() {
	serv.listener.Close()
}

// 将conn封装为逻辑session
func (serv *TCPServer) accept(conn net.Conn) *model.Session {
	remoteAddr := conn.RemoteAddr().String()
	serv.mutex.Lock()
	session := &model.Session{
		Conn:       conn,
		ID:         remoteAddr, // using remote addr default
		AckTimeout: serv.ackTimeout,
		MaxRetries: serv.maxRetransmissions,
	}
	// serv.sessions[remoteAddr] = session
	_ = storage.Default().Conn.Bind(context.Background(), session)
	serv.mutex.Unlock()

	return session
}

func (serv *TCPServer) remove(session *model.Session) {
	serv.mutex.Lock()
	defer serv.mutex.Unlock()

	session.Conn.Close()
	storage.Default().Conn.Unbind(context.Background(), session.ID)
	// delete(serv.sessions, session.ID)

	log.Debug().Str("id", session.ID).Msg("Closing connection from remote.")
}

// 处理每个session的消息
func (serv *TCPServer) serve(session *model.Session) {
	serveSession(session, serv.hooks)
}

func serveSession(session *model.Session, hooks action.Hooks) {
	if hooks == nil {
		hooks = action.Noop{}
	}
	defer func() {
		session.ClosePending()
		session.Conn.Close()
		storage.Default().Conn.Unbind(context.Background(), session.ID)
		hooks.OnDisconnect(context.Background(), action.DisconnectEvent{SessionID: session.ID, Transport: string(session.GetTransProto())})
		log.Debug().Str("id", session.ID).Msg("Closing connection from remote.")
	}()

	pg := protocol.NewPipeline(session.Conn, protocol.WithHooks(hooks))
	for {
		_ = session.Conn.SetReadDeadline(time.Now().Add(2 * time.Minute))
		// 记录value ctx
		ctx := context.WithValue(context.Background(), model.SessionCtxKey{}, session)

		err := pg.ProcessConnRead(ctx)

		if err == nil {
			continue
		}
		if errors.Is(err, protocol.ErrFrameReadEmpty) {
			continue
		}

		log.Error().Err(err).Str("id", session.ID).Msg("Failed to serve session")

		switch {
		case errors.Is(err, io.EOF), errors.Is(err, io.ErrClosedPipe), errors.Is(err, net.ErrClosed), errors.Is(err, storage.ErrDeviceNotFound), isTimeout(err):
			return // close connection when EOF or closed
		default:
			time.Sleep(1 * time.Second)
		}
	}
}

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

// 发送消息到终端设备, 外部调用
func (serv *TCPServer) Send(id string, msg model.JT808Msg) {
	// session := serv.sessions[id]
	session, err := storage.Default().Conn.Get(context.Background(), id)
	if err != nil && errors.Is(err, storage.ErrSessionClosed) {
		log.Warn().Str("id", id).Msg("Fail to get session from cache, maybe conn was closed.")
		return
	}

	pg := protocol.NewPipeline(session.Conn, protocol.WithHooks(serv.hooks))

	// 记录value ctx
	ctx := context.WithValue(context.Background(), model.SessionCtxKey{}, session)
	ctx = context.WithValue(ctx, model.ProcessDataCtxKey{}, &model.ProcessData{Outgoing: msg})

	err = pg.ProcessConnWrite(ctx)

	if err == nil {
		return
	}

	if errors.Is(err, io.ErrClosedPipe) || errors.Is(err, net.ErrClosed) {
		serv.remove(session)
	}

	log.Error().Err(err).Str("device", id).Msg("Failed to send jtmsg to device")
}
