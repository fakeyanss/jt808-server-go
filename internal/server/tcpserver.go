package server

import (
	"bufio"
	"encoding/json"
	"io"
	"net"
	"sync"
	"time"

	"github.com/fakeYanss/jt808-server-go/internal/protocol"
	"github.com/rs/zerolog/log"
)

type TcpServer struct {
	listener net.Listener
	sessions map[string]*session
	mutex    *sync.Mutex
}

type session struct {
	conn   net.Conn
	id     string
	writer struct{}
}

func NewTcpServer() *TcpServer {
	return &TcpServer{
		mutex:    &sync.Mutex{},
		sessions: make(map[string]*session),
	}
}

func (serv *TcpServer) Listen(addr string) error {
	l, err := net.Listen("tcp", addr)
	if err == nil {
		serv.listener = l
		log.Debug().Msgf("Listening on %v", addr)
		return nil
	}

	log.Error().Err(err).Str("addr", addr).Msg("Failed to listen addr")
	return err
}

func (serv *TcpServer) Start() {
	go serv.monitorSessionCnt()
	for {
		conn, err := serv.listener.Accept()
		if err != nil {
			log.Error().Err(err).Msg("Failed to accept ")
		} else {
			session := serv.accept(conn)
			go serv.serve(session)
		}
	}
}

func (serv *TcpServer) Stop() {
	serv.listener.Close()
}

func (serv *TcpServer) monitorSessionCnt() {
	for {
		log.Debug().
			Int("total_session_cnt", len(serv.sessions)).
			Msg("Monitoring total session count")

		time.Sleep(10 * time.Second)
	}
}

func (serv *TcpServer) accept(conn net.Conn) *session {
	remoteAddr := conn.RemoteAddr().String()
	log.Debug().
		Str("remote_addr", remoteAddr).
		Int("total_session_cnt", len(serv.sessions)+1).
		Msg("Accepting connection from remtote")

	serv.mutex.Lock()
	defer serv.mutex.Unlock()

	session := &session{
		conn:   conn,
		id:     remoteAddr, // using remote addr default
		writer: struct{}{},
	}

	serv.sessions[session.id] = session

	return session
}

func (serv *TcpServer) remove(session *session) {
	serv.mutex.Lock()
	defer serv.mutex.Unlock()

	delete(serv.sessions, session.id)
	log.Debug().
		Str("remote_addr", session.conn.RemoteAddr().String()).
		Msg("Closing connection from remote")
}

func (serv *TcpServer) serve(session *session) {
	defer serv.remove(session)

	frameCodec := protocol.NewJT808FrameHandler()
	packetCodec := protocol.NewJT808PacketCodec()
	rbuf := bufio.NewReader(session.conn)

	for {
		// read from the connection and decode the frame
		framePayload, err := frameCodec.Read(rbuf)

		if err == io.EOF {
			break // close connection when EOF
		}

		if err != nil && err != io.EOF {
			log.Error().
				Err(err).
				Str("session", session.id).
				Msg("Failed to decode frame")
			continue
		}

		if len(framePayload) == 0 {
			continue
		}

		log.Debug().
			Str("session", session.id).
			Int("frame_len", len(framePayload)).
			Hex("frame_payload", framePayload). // for debug
			Msg("Received frame")

		jtmsg, err := packetCodec.Decode(framePayload)
		if err != nil {
			log.Error().
				Str("session", session.id).
				Msg("Failed to decode packet")
			continue
		}

		// handle jt808 msg
		msgJson, _ := json.Marshal(jtmsg)
		log.Debug().
			Str("session", session.id).
			RawJSON("msg", msgJson).
			Msg("Handle jt808 msg")

		ack := "OK\n"
		err = frameCodec.Write(session.conn, []byte(ack))
		if err != nil {
			log.Error().Msg("Failed to encode frame")
			return
		}

		if err == io.EOF {
			break // close connection when EOF
		}
	}
}

// 发送消息到终端设备
func (serv *TcpServer) Send(id string, JTMsg interface{}) {

}
