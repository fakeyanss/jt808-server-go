package server

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/action"
	"github.com/fakeyanss/jt808-server-go/internal/protocol"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/storage"
	"github.com/fakeyanss/jt808-server-go/pkg/routines"
)

// UDPServer adapts each remote endpoint to a net.Conn-like session so the same
// JT808 pipeline can process TCP streams and UDP datagrams.
type UDPServer struct {
	mu                 sync.Mutex
	listener           *net.UDPConn
	sessions           map[string]*udpSessionConn
	ackTimeout         time.Duration
	maxRetransmissions int
	hooks              action.Hooks
}

func NewUDPServer() *UDPServer {
	return &UDPServer{sessions: make(map[string]*udpSessionConn)}
}

// SetHooks 设置生命周期钩子。未设置时使用 action.Noop。
func (s *UDPServer) SetHooks(hooks action.Hooks) {
	s.hooks = hooks
}

func (s *UDPServer) SetReliability(ackTimeout time.Duration, maxRetransmissions int) {
	s.ackTimeout = ackTimeout
	s.maxRetransmissions = maxRetransmissions
}

func (s *UDPServer) Listen(addr string) error {
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return err
	}
	s.listener, err = net.ListenUDP("udp", udpAddr)
	return err
}

func (s *UDPServer) Start() {
	buf := make([]byte, 64*1024)
	for {
		n, remote, err := s.listener.ReadFromUDP(buf)
		if err != nil {
			if !errors.Is(err, net.ErrClosed) {
				log.Error().Err(err).Msg("Fail to read UDP datagram")
			}
			return
		}
		key := remote.String()
		s.mu.Lock()
		conn := s.sessions[key]
		if conn == nil {
			conn = newUDPSessionConn(s.listener, remote, func() { s.remove(key) })
			s.sessions[key] = conn
			session := &model.Session{ID: key, Conn: conn, Transport: model.UDPProto, AckTimeout: s.ackTimeout, MaxRetries: s.maxRetransmissions}
			_ = storage.Default().Conn.Bind(context.Background(), session)
			routines.GoSafe(func() { serveSession(session, s.hooks) })
		}
		s.mu.Unlock()
		if !conn.deliver(buf[:n]) {
			log.Debug().Str("remote", key).Msg("Dropped UDP datagram for closed session")
		}
	}
}

func (s *UDPServer) Stop() {
	if s.listener != nil {
		_ = s.listener.Close()
	}
	s.mu.Lock()
	connections := make([]*udpSessionConn, 0, len(s.sessions))
	for _, conn := range s.sessions {
		connections = append(connections, conn)
	}
	s.mu.Unlock()
	for _, conn := range connections {
		_ = conn.Close()
	}
}

func (s *UDPServer) remove(key string) {
	s.mu.Lock()
	delete(s.sessions, key)
	s.mu.Unlock()
	storage.Default().Conn.Unbind(context.Background(), key)
}

type udpSessionConn struct {
	listener    *net.UDPConn
	remote      *net.UDPAddr
	packets     chan []byte
	done        chan struct{}
	once        sync.Once
	mu          sync.Mutex
	current     []byte
	datagramEnd bool
	readDL      time.Time
	closed      bool
	onClose     func()
}

func newUDPSessionConn(listener *net.UDPConn, remote *net.UDPAddr, onClose func()) *udpSessionConn {
	return &udpSessionConn{
		listener: listener,
		remote:   remote,
		packets:  make(chan []byte, 128),
		done:     make(chan struct{}),
		onClose:  onClose,
	}
}

func (c *udpSessionConn) deliver(packet []byte) bool {
	copyOfPacket := append([]byte(nil), packet...)
	select {
	case c.packets <- copyOfPacket:
		return true
	case <-c.done:
		return false
	default:
		return false
	}
}

func (c *udpSessionConn) Read(p []byte) (int, error) {
	for {
		c.mu.Lock()
		if len(c.current) > 0 {
			n := copy(p, c.current)
			c.current = c.current[n:]
			if len(c.current) == 0 {
				c.datagramEnd = true
			}
			c.mu.Unlock()
			return n, nil
		}
		if c.datagramEnd {
			c.datagramEnd = false
			c.mu.Unlock()
			return 0, protocol.ErrDatagramBoundary
		}
		if c.closed {
			c.mu.Unlock()
			return 0, net.ErrClosed
		}
		deadline := c.readDL
		c.mu.Unlock()

		if deadline.IsZero() {
			select {
			case packet := <-c.packets:
				c.mu.Lock()
				c.current = packet
				c.mu.Unlock()
			case <-c.done:
				return 0, net.ErrClosed
			}
			continue
		}
		timer := time.NewTimer(time.Until(deadline))
		select {
		case packet := <-c.packets:
			timer.Stop()
			c.mu.Lock()
			c.current = packet
			c.mu.Unlock()
		case <-c.done:
			timer.Stop()
			return 0, net.ErrClosed
		case <-timer.C:
			return 0, timeoutError{}
		}
	}
}

func (c *udpSessionConn) Write(p []byte) (int, error) {
	select {
	case <-c.done:
		return 0, net.ErrClosed
	default:
	}
	return c.listener.WriteToUDP(p, c.remote)
}

func (c *udpSessionConn) Close() error {
	c.once.Do(func() {
		c.mu.Lock()
		c.closed = true
		c.mu.Unlock()
		close(c.done)
		if c.onClose != nil {
			c.onClose()
		}
	})
	return nil
}

func (c *udpSessionConn) LocalAddr() net.Addr  { return c.listener.LocalAddr() }
func (c *udpSessionConn) RemoteAddr() net.Addr { return c.remote }
func (c *udpSessionConn) SetDeadline(t time.Time) error {
	c.SetReadDeadline(t)
	return nil
}
func (c *udpSessionConn) SetReadDeadline(t time.Time) error {
	c.mu.Lock()
	c.readDL = t
	c.mu.Unlock()
	return nil
}
func (c *udpSessionConn) SetWriteDeadline(time.Time) error { return nil }

type timeoutError struct{}

func (timeoutError) Error() string   { return "i/o timeout" }
func (timeoutError) Timeout() bool   { return true }
func (timeoutError) Temporary() bool { return true }

var _ net.Conn = (*udpSessionConn)(nil)
