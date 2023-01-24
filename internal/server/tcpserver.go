package server

import (
	"encoding/json"
	"io"
	"net"
	"sync"
	"time"

	"github.com/fakeYanss/jt808-server-go/internal/protocol"
	"github.com/fakeYanss/jt808-server-go/internal/protocol/model"
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

	frameHandler := protocol.NewJT808FrameHandler(session.conn)
	packetCodec := protocol.NewJT808PacketCodec()
	msgHandler := protocol.NewJT808MsgHandler()

	for {
		// read from the connection and decode the frame
		framePayload, err := frameHandler.Recv()
		if err != nil {
			if err == io.EOF {
				break // close connection when EOF
			} else {
				log.Error().
					Err(err).
					Str("session", session.id).
					Msg("Failed to read stream to framePayload, continue...")
				continue
			}
		}

		if len(framePayload) == 0 {
			continue
		}

		log.Debug().
			Str("session", session.id).
			Int("frame_len", len(framePayload)).
			Hex("frame_payload", framePayload). // for debug
			Msg("Received frame")

		// 按jt808协议解析消息
		packet, err := packetCodec.Decode(framePayload)
		if err != nil {
			log.Error().
				Err(err).
				Str("session", session.id).
				Msg("Failed to decode packet from []byte to Packet, continue...")
			continue
		}

		// handle jt808 msg
		jtmsg, err := msgHandler.ProcessPacket(packet)
		if err != nil {
			log.Error().
				Err(err).
				Str("session", session.id).
				Msg("Failed to handle packet to JT808Msg, continue...")
			continue
		}

		msgJson, _ := json.Marshal(jtmsg)
		log.Debug().
			Str("session", session.id).
			RawJSON("msg", msgJson).
			Msg("Received jt808 msg")

		// 回复消息
		jtcmd, err := msgHandler.ProcessMsg(jtmsg)
		if jtcmd == nil { // 不需要回复
			continue
		}
		if err != nil {
			log.Error().
				Err(err).
				Str("session", session.id).
				Msg("Failed to handle msg")
		}

		cmdJson, _ := json.Marshal(jtcmd)
		log.Debug().
			Str("session", session.id).
			RawJSON("cmd", cmdJson).
			Msg("Sent jt808 cmd")

		pkt, err := packetCodec.Encode(jtcmd)
		if err != nil {
			log.Error().
				Err(err).
				Str("session", session.id).
				Msg("Failed to encode cmd")
		}
		frameHandler.Send(pkt)
		if err == io.EOF {
			break // close connection when EOF
		}
	}
}

// 发送消息到终端设备
func (serv *TcpServer) Send(id string, cmd model.JT808Cmd) {
	session := serv.sessions[id]
	frameHandler := protocol.NewJT808FrameHandler(session.conn)
	packet, err := cmd.Encode()
	if err != nil {
		log.Error().
			Err(err).
			Str("session", id).
			Msg("Failed to send cmd to device")
		return
	}
	frameHandler.Send(packet)
}
