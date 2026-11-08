package alarmserver

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/protocol"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/pkg/routines"
)

const (
	// 附件连接的空闲读超时。文件级无进展由 Assembler 按 SessionTimeout 统一清理。
	connReadTimeout = 10 * time.Minute
	// 应答写超时。
	connWriteTimeout = 10 * time.Second
	// 单次连接允许的最大连续失步字节数，超过后关闭连接，避免死循环。
	maxDesyncBytes = 64
)

// connState 保存一条附件连接上最近一次 0x1210 登记的报警上下文。
type connState struct {
	mu     sync.Mutex
	info   AttachmentInfo
	serial uint16
}

func (cs *connState) setInfo(info AttachmentInfo) {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.info = info
}

func (cs *connState) getInfo() AttachmentInfo {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	return cs.info
}

func (cs *connState) nextSerial() uint16 {
	cs.mu.Lock()
	defer cs.mu.Unlock()
	cs.serial++
	if cs.serial == 0 {
		cs.serial = 1
	}
	return cs.serial
}

// Server 是苏标主动安全附件服务器，独立端口承载 0x1210/0x1211/0x1212 信令
// 与 0x30 0x31 0x63 0x64 文件码流帧。
type Server struct {
	cfg       Config
	assembler *Assembler
	codec     *protocol.JT808PacketCodec

	listener  net.Listener
	closeOnce sync.Once
	done      chan struct{}
	wg        sync.WaitGroup

	connMu sync.Mutex
	conns  map[net.Conn]struct{}
}

func NewServer(cfg Config) *Server {
	cfg = cfg.Normalize()
	return &Server{
		cfg:       cfg,
		assembler: NewAssembler(cfg),
		codec:     protocol.NewJT808PacketCodec(),
		done:      make(chan struct{}),
		conns:     make(map[net.Conn]struct{}),
	}
}

func (s *Server) Assembler() *Assembler { return s.assembler }

func (s *Server) Listen(addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen attachment server: %w", err)
	}
	s.listener = listener
	log.Info().Str("addr", listener.Addr().String()).Str("dir", s.cfg.Dir).Msg("Attachment server listening")
	return nil
}

func (s *Server) Addr() string {
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

// Start 先对账落盘目录，再进入阻塞式 accept 循环，由调用方放入独立 goroutine。
func (s *Server) Start() {
	if s.listener == nil {
		log.Error().Msg("Attachment server is not listening")
		return
	}
	if err := s.assembler.Reconcile(); err != nil {
		log.Warn().Err(err).Str("dir", s.cfg.Dir).Msg("Fail to reconcile attachment directory")
	}
	routines.GoSafe(s.cleanupLoop)

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.done:
				return
			default:
			}
			log.Error().Err(err).Msg("Fail to accept attachment connection")
			continue
		}
		s.track(conn)
		s.wg.Add(1)
		routines.GoSafe(func() {
			defer s.wg.Done()
			defer s.untrack(conn)
			defer conn.Close()
			s.serveConn(conn)
		})
	}
}

func (s *Server) Stop() {
	s.closeOnce.Do(func() {
		close(s.done)
		if s.listener != nil {
			_ = s.listener.Close()
		}
		s.connMu.Lock()
		for conn := range s.conns {
			_ = conn.Close()
		}
		s.connMu.Unlock()
	})
	s.assembler.Close()
}

func (s *Server) track(conn net.Conn) {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	s.conns[conn] = struct{}{}
}

func (s *Server) untrack(conn net.Conn) {
	s.connMu.Lock()
	defer s.connMu.Unlock()
	delete(s.conns, conn)
}

func (s *Server) cleanupLoop() {
	interval := s.cfg.SessionTimeout / 2
	if interval > time.Minute {
		interval = time.Minute
	}
	if interval < 10*time.Second {
		interval = 10 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-s.done:
			return
		case now := <-ticker.C:
			s.assembler.CleanupStale(now, s.cfg.SessionTimeout)
		}
	}
}

func (s *Server) serveConn(conn net.Conn) {
	state := &connState{}
	reader := bufio.NewReaderSize(conn, 64*1024)
	desync := 0
	for {
		_ = conn.SetReadDeadline(time.Now().Add(connReadTimeout))
		first, err := reader.Peek(1)
		if err != nil {
			if !isConnClosed(err) {
				log.Debug().Err(err).Str("remote", conn.RemoteAddr().String()).Msg("Attachment connection read finished")
			}
			return
		}
		switch first[0] {
		case 0x7e:
			frame, readErr := readJT808Frame(reader)
			if readErr != nil {
				log.Warn().Err(readErr).Str("remote", conn.RemoteAddr().String()).Msg("Discard malformed attachment frame")
				desync++
				if desync >= maxDesyncBytes {
					return
				}
				continue
			}
			desync = 0
			s.handleJT808(conn, state, frame)
		case FrameMagic[0]:
			magic, peekErr := reader.Peek(len(FrameMagic))
			if peekErr != nil || !bytes.Equal(magic, FrameMagic[:]) {
				_, _ = reader.Discard(1)
				desync++
				if desync >= maxDesyncBytes {
					return
				}
				continue
			}
			fileName, offset, data, readErr := readDataFrame(reader)
			if readErr != nil {
				log.Warn().Err(readErr).Str("remote", conn.RemoteAddr().String()).Msg("Discard malformed attachment data frame")
				// 数据帧长度异常时流已无法可靠同步，直接断开。
				return
			}
			desync = 0
			s.handleChunk(state, fileName, offset, data)
		default:
			_, _ = reader.Discard(1)
			desync++
			if desync == 1 {
				log.Warn().Str("remote", conn.RemoteAddr().String()).Uint8("byte", first[0]).Msg("Skip unexpected byte on attachment stream")
			}
			if desync >= maxDesyncBytes {
				return
			}
		}
	}
}

// readJT808Frame 读取一个完整的 7E 定界帧，支持粘包与半包。
func readJT808Frame(reader *bufio.Reader) ([]byte, error) {
	first, err := reader.ReadByte()
	if err != nil {
		return nil, err
	}
	if first != 0x7e {
		return nil, protocol.ErrMalformedFrame
	}
	frame := make([]byte, 0, protocol.MaxFrameLen)
	frame = append(frame, first)
	for {
		b, err := reader.ReadByte()
		if err != nil {
			return nil, err
		}
		if b == 0x7e {
			if len(frame) == 1 {
				// 连续两个边界符，忽略空帧并重新开始。
				continue
			}
			frame = append(frame, b)
			return frame, nil
		}
		if len(frame) >= protocol.MaxFrameLen {
			return nil, protocol.ErrFrameTooLong
		}
		frame = append(frame, b)
	}
}

// readDataFrame 读取苏标 4.6.4 文件码流帧：
// 帧头(4) + 文件名(50) + 偏移(4) + 长度(4) + 数据体(n)。
func readDataFrame(reader *bufio.Reader) (string, uint32, []byte, error) {
	header := make([]byte, FrameHeaderSize)
	if _, err := io.ReadFull(reader, header); err != nil {
		return "", 0, nil, err
	}
	if !bytes.Equal(header[:len(FrameMagic)], FrameMagic[:]) {
		return "", 0, nil, ErrFrameMagic
	}
	nameBytes := header[len(FrameMagic) : len(FrameMagic)+FileNameFieldSize]
	fileName := strings.TrimRight(string(nameBytes), "\x00")
	offset := binary.BigEndian.Uint32(header[len(FrameMagic)+FileNameFieldSize : len(FrameMagic)+FileNameFieldSize+4])
	length := binary.BigEndian.Uint32(header[len(FrameMagic)+FileNameFieldSize+4:])
	if length == 0 || length > FramePayloadLimit {
		return "", 0, nil, ErrInvalidChunkLength
	}
	data := make([]byte, length)
	if _, err := io.ReadFull(reader, data); err != nil {
		return "", 0, nil, err
	}
	return fileName, offset, data, nil
}

func (s *Server) handleChunk(state *connState, fileName string, offset uint32, data []byte) {
	info := state.getInfo()
	if info.Phone == "" {
		log.Warn().Str("file", fileName).Msg("Receive attachment data before alarm registration")
		return
	}
	if err := s.assembler.WriteChunk(info.Phone, fileName, offset, data); err != nil {
		log.Warn().Err(err).Str("phone", info.Phone).Str("file", fileName).Uint32("offset", offset).
			Int("length", len(data)).Msg("Reject attachment data chunk")
	}
}

func (s *Server) handleJT808(conn net.Conn, state *connState, frame []byte) {
	pkt, err := s.codec.Decode(frame)
	if err != nil {
		log.Warn().Err(err).Msg("Fail to decode attachment signaling frame")
		return
	}
	switch pkt.Header.MsgID {
	case 0x1210:
		s.handleAlarmInfo(conn, state, pkt)
	case 0x1211:
		s.handleFileInfo(conn, state, pkt)
	case 0x1212:
		s.handleFileComplete(conn, state, pkt)
	default:
		log.Debug().Uint16("msg_id", pkt.Header.MsgID).Str("phone", pkt.Header.PhoneNumber).
			Msg("Ignore unsupported message on attachment server")
	}
}

func (s *Server) handleAlarmInfo(conn net.Conn, state *connState, pkt *model.PacketData) {
	msg := &model.Msg1210{}
	if err := decodeSafely(msg, pkt); err != nil {
		log.Warn().Err(err).Msg("Fail to decode 0x1210")
		return
	}
	info := AttachmentInfo{
		Phone:      pkt.Header.PhoneNumber,
		TerminalID: msg.TerminalID,
		AlarmSign:  append([]byte(nil), msg.AlarmSign...),
		AlarmNo:    msg.AlarmNo,
		InfoType:   msg.InfoType,
	}
	for _, item := range msg.Attachments {
		info.Attachments = append(info.Attachments, AttachmentEntry{FileName: item.FileName, FileSize: item.FileSize})
	}
	state.setInfo(info)

	result := uint8(model.ResultSuccess)
	if err := s.assembler.RegisterAlarm(info); err != nil {
		log.Warn().Err(err).Str("phone", info.Phone).Str("alarm", info.AlarmNo).Msg("Reject 0x1210 attachment registration")
		result = uint8(model.ResultFail)
	}
	s.sendAck(conn, state, pkt, result)
}

func (s *Server) handleFileInfo(conn net.Conn, state *connState, pkt *model.PacketData) {
	msg := &model.Msg1211{}
	if err := decodeSafely(msg, pkt); err != nil {
		log.Warn().Err(err).Msg("Fail to decode 0x1211")
		return
	}
	info := state.getInfo()
	if info.Phone == "" {
		info.Phone = pkt.Header.PhoneNumber
	}
	result := uint8(model.ResultSuccess)
	if _, err := s.assembler.RegisterFile(info, msg.FileName, msg.FileType, msg.FileSize); err != nil {
		log.Warn().Err(err).Str("phone", info.Phone).Str("file", msg.FileName).Msg("Reject 0x1211 file registration")
		result = uint8(model.ResultFail)
	}
	// 标准 4.6.3 只要求 0x1211 回通用应答；缺口回报仅由 0x1212 触发。
	s.sendAck(conn, state, pkt, result)
}

func (s *Server) handleFileComplete(conn net.Conn, state *connState, pkt *model.PacketData) {
	msg := &model.Msg1212{}
	if err := decodeSafely(msg, pkt); err != nil {
		log.Warn().Err(err).Msg("Fail to decode 0x1212")
		return
	}
	info := state.getInfo()
	if info.Phone == "" {
		info.Phone = pkt.Header.PhoneNumber
	}
	if _, err := s.assembler.RegisterFile(info, msg.FileName, msg.FileType, msg.FileSize); err != nil {
		log.Warn().Err(err).Str("phone", info.Phone).Str("file", msg.FileName).Msg("Fail to register file on 0x1212")
	}
	s.sendCompletionCheck(conn, state, pkt, info.Phone, msg.FileName, msg.FileType)
}

func (s *Server) sendCompletionCheck(conn net.Conn, state *connState, pkt *model.PacketData, phone, fileName string, fileType uint8) {
	result, err := s.assembler.CompleteCheck(phone, fileName)
	if err != nil {
		log.Warn().Err(err).Str("phone", phone).Str("file", fileName).Msg("Fail to check attachment completion")
		result = CompletionResult{Complete: false}
	}
	s.send9212Response(conn, state, pkt, fileName, fileType, result)
}

func (s *Server) sendAck(conn net.Conn, state *connState, pkt *model.PacketData, result uint8) {
	msg := &model.Msg0001{
		Header:             replyHeader(pkt.Header, 0x0001, state.nextSerial()),
		AnswerSerialNumber: pkt.Header.SerialNumber,
		AnswerMessageID:    pkt.Header.MsgID,
		Result:             result,
	}
	s.write(conn, msg)
}

func (s *Server) send9212Response(conn net.Conn, state *connState, pkt *model.PacketData, fileName string, fileType uint8, result CompletionResult) {
	msg := &model.Msg9212{
		Header:   replyHeader(pkt.Header, 0x9212, state.nextSerial()),
		FileName: fileName,
		FileType: fileType,
	}
	if result.Complete {
		msg.Result = 0
	} else {
		msg.Result = 1
		for _, item := range result.Missing {
			msg.RetransmitPackets = append(msg.RetransmitPackets, model.RetransmitPacket{Offset: item.Offset, Length: item.Length})
		}
	}
	s.write(conn, msg)
}

func (s *Server) write(conn net.Conn, msg model.JT808Msg) {
	pkt, err := s.codec.Encode(msg)
	if err != nil {
		log.Error().Err(err).Uint16("msg_id", msg.GetHeader().MsgID).Msg("Fail to encode attachment response")
		return
	}
	_ = conn.SetWriteDeadline(time.Now().Add(connWriteTimeout))
	if _, err := conn.Write(pkt); err != nil {
		log.Warn().Err(err).Str("remote", conn.RemoteAddr().String()).Msg("Fail to write attachment response")
	}
}

// replyHeader 复制上行消息头作为下行应答头，并清空分包与长度字段。
func replyHeader(in *model.MsgHeader, msgID uint16, serial uint16) *model.MsgHeader {
	header := *in
	header.MsgID = msgID
	attr := *in.Attr
	attr.BodyLength = 0
	attr.Encryption = uint8(model.EncryptionNone)
	attr.EncryptionDesc = model.EncryptionNone
	attr.PacketFragmented = 0
	attr.PacketFragmentedDesc = model.PacketFragmentedFalse
	header.Attr = &attr
	header.Frag = nil
	header.SerialNumber = serial
	header.Idx = 0
	return &header
}

func decodeSafely(msg model.JT808Msg, pkt *model.PacketData) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("malformed attachment message body: %v", recovered)
		}
	}()
	return msg.Decode(pkt)
}

func isConnClosed(err error) bool {
	return err == io.EOF || err == io.ErrUnexpectedEOF || err == net.ErrClosed
}
