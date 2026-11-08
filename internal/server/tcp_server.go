package server

import (
	"context"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/action"
	"github.com/fakeyanss/jt808-server-go/internal/protocol"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
	"github.com/fakeyanss/jt808-server-go/internal/runtime"
	"github.com/fakeyanss/jt808-server-go/internal/storage"
	"github.com/fakeyanss/jt808-server-go/pkg/routines"
)

type TCPServer struct {
	listener           net.Listener
	ackTimeout         time.Duration
	maxRetransmissions int
	hooks              action.Hooks
	deps               protocol.ProcessorDeps
	// reliableResultHandler 覆盖默认的 outbox 回填逻辑；
	// 设置后由重投服务决定失败结果是立即重试还是转 failed。
	reliableResultHandler func(model.ReliableResult)

	// sessions map[string]*model.Session
	mutex *sync.Mutex
}

// SetReliableResultHandler 设置可靠投递结果处理器，必须在 Start 之前调用。
// 未设置时回退到默认行为：直接按 (Phone, MsgID, Serial) 回填 outbox 终态。
func (serv *TCPServer) SetReliableResultHandler(handler func(model.ReliableResult)) {
	serv.mutex.Lock()
	defer serv.mutex.Unlock()
	serv.reliableResultHandler = handler
}

// SetDeps 设置处理器依赖；未注入的字段回退到包级默认单例。
func (serv *TCPServer) SetDeps(deps protocol.ProcessorDeps) {
	serv.deps = deps
}

func (serv *TCPServer) runtimeStores() *runtime.Stores {
	if serv.deps.Runtime != nil {
		return serv.deps.Runtime
	}
	return runtime.Default()
}

// commandRepository 返回下行命令 outbox；未注入时回退到进程默认存储。
func (serv *TCPServer) commandRepository() repository.CommandRepository {
	if serv.deps.Storage != nil {
		return serv.deps.Storage.Commands
	}
	return storage.Default().Commands
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
		Conn:        conn,
		ID:          remoteAddr, // using remote addr default
		ConnectedAt: time.Now(),
		AckTimeout:  serv.ackTimeout,
		MaxRetries:  serv.maxRetransmissions,
	}
	handler := serv.reliableResultHandler
	if handler != nil {
		session.OnReliableResult = func(result model.ReliableResult) {
			go handler(result)
		}
	} else if commands := serv.commandRepository(); commands != nil {
		session.OnReliableResult = func(result model.ReliableResult) {
			go recordReliableResult(commands, result)
		}
	}
	// serv.sessions[remoteAddr] = session
	_ = serv.runtimeStores().Conn.Bind(context.Background(), session)
	serv.mutex.Unlock()

	return session
}

func (serv *TCPServer) remove(session *model.Session) {
	serv.mutex.Lock()
	defer serv.mutex.Unlock()

	session.Conn.Close()
	serv.runtimeStores().Conn.Unbind(context.Background(), session.ID)
	// delete(serv.sessions, session.ID)

	log.Debug().Str("id", session.ID).Msg("Closing connection from remote.")
}

// 处理每个session的消息
func (serv *TCPServer) serve(session *model.Session) {
	serveSession(session, serv.hooks, serv.deps)
}

func serveSession(session *model.Session, hooks action.Hooks, deps protocol.ProcessorDeps) {
	if hooks == nil {
		hooks = action.Noop{}
	}
	if deps.Runtime == nil {
		deps.Runtime = runtime.Default()
	}
	defer func() {
		session.ClosePending()
		session.Conn.Close()
		deps.Runtime.Conn.Unbind(context.Background(), session.ID)
		hooks.OnDisconnect(context.Background(), action.DisconnectEvent{SessionID: session.ID, Transport: string(session.GetTransProto())})
		log.Debug().Str("id", session.ID).Msg("Closing connection from remote.")
	}()

	pg := protocol.NewPipeline(session.Conn, protocol.WithHooks(hooks), protocol.WithDeps(deps))
	for {
		_ = session.Conn.SetReadDeadline(time.Now().Add(2 * time.Minute))
		// 记录value ctx
		ctx := context.WithValue(context.Background(), model.SessionCtxKey{}, session)

		err := pg.ProcessConnRead(ctx)

		// 应答已发送：拒绝冒名连接（鉴权失败且手机号已有在线会话）后关闭本连接。
		if session.ShouldCloseAfterResponse() {
			return
		}
		if err == nil {
			continue
		}
		if errors.Is(err, protocol.ErrFrameReadEmpty) {
			continue
		}

		log.Error().Err(err).Str("id", session.ID).Msg("Failed to serve session")

		switch {
		case errors.Is(err, io.EOF), errors.Is(err, io.ErrClosedPipe), errors.Is(err, net.ErrClosed), errors.Is(err, repository.ErrDeviceNotFound), isTimeout(err):
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

// SendOption 配置一次下行发送。
type SendOption func(*sendSettings)

type sendSettings struct {
	serial    uint16
	useSerial bool
}

// WithSerial 使用调用方预分配的流水号，编码阶段不再改写。
// outbox 需要在下发前用确定的流水号落库，才能把应答精确回填到命令记录。
func WithSerial(serial uint16) SendOption {
	return func(settings *sendSettings) {
		settings.serial = serial
		settings.useSerial = true
	}
}

// 发送消息到终端设备, 外部调用。
// 返回错误供 outbox 标记失败；既有调用方可忽略返回值。
func (serv *TCPServer) Send(id string, msg model.JT808Msg, opts ...SendOption) error {
	var settings sendSettings
	for _, opt := range opts {
		opt(&settings)
	}
	// session := serv.sessions[id]
	session, err := serv.runtimeStores().Conn.Get(context.Background(), id)
	if err != nil {
		if errors.Is(err, runtime.ErrSessionClosed) {
			log.Warn().Str("id", id).Msg("Fail to get session from cache, maybe conn was closed.")
		}
		return fmt.Errorf("get session %s: %w", id, err)
	}

	pg := protocol.NewPipeline(session.Conn, protocol.WithHooks(serv.hooks), protocol.WithDeps(serv.deps))

	// 记录value ctx
	ctx := context.WithValue(context.Background(), model.SessionCtxKey{}, session)
	ctx = context.WithValue(ctx, model.ProcessDataCtxKey{}, &model.ProcessData{Outgoing: msg})
	if settings.useSerial && msg.GetHeader() != nil {
		msg.GetHeader().SerialNumber = settings.serial
		ctx = protocol.WithPreassignedSerial(ctx)
	}

	err = pg.ProcessConnWrite(ctx)

	if err == nil {
		return nil
	}

	if errors.Is(err, io.ErrClosedPipe) || errors.Is(err, net.ErrClosed) {
		serv.remove(session)
	}

	log.Error().Err(err).Str("device", id).Msg("Failed to send jtmsg to device")
	return err
}
