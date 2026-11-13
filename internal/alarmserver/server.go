package alarmserver

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
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

// bindFile 对 TCP 是空操作：数据帧天然属于收到它的连接上下文。
func (cs *connState) bindFile(string) {}

// attachmentSession 是附件信令需要的最小会话状态。
// TCP 每条连接一个；UDP 无连接，按手机号复用同一份状态。
type attachmentSession interface {
	getInfo() AttachmentInfo
	setInfo(AttachmentInfo)
	nextSerial() uint16
	bindFile(fileName string)
}

// frameWriter 把已编码的完整帧写回发送方。
// TCP 写连接，UDP 写数据报的源地址，因此处理逻辑无需感知传输方式。
type frameWriter func(pkt []byte) error

// udpSession 是 UDP 侧按手机号复用的会话。
type udpSession struct {
	state *connState
	store *udpSessionStore
}

func (u *udpSession) getInfo() AttachmentInfo     { return u.state.getInfo() }
func (u *udpSession) setInfo(info AttachmentInfo) { u.state.setInfo(info) }
func (u *udpSession) nextSerial() uint16          { return u.state.nextSerial() }

func (u *udpSession) bindFile(fileName string) {
	info := u.state.getInfo()
	u.store.bindFile(info.Phone, fileName)
}

// udpSessionStore 维护 UDP 侧的手机号会话与文件名归属。
//
// UDP 没有连接对象可依赖，数据帧只能靠文件名反查归属；
// 归属信息来自 0x1210/0x1211/0x1212 中携带的手机号与报警编号
// （报警编号已经体现在文件名与 AttachmentInfo 中）。
type udpSessionStore struct {
	mu        sync.Mutex
	byPhone   map[string]*udpSession
	fileOwner map[string]string
}

func newUDPSessionStore() *udpSessionStore {
	return &udpSessionStore{
		byPhone:   make(map[string]*udpSession),
		fileOwner: make(map[string]string),
	}
}

func (s *udpSessionStore) session(phone string) *udpSession {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess, ok := s.byPhone[phone]
	if !ok {
		sess = &udpSession{state: &connState{}, store: s}
		s.byPhone[phone] = sess
	}
	return sess
}

func (s *udpSessionStore) bindFile(phone, fileName string) {
	if phone == "" || fileName == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.fileOwner[fileName] = phone
}

func (s *udpSessionStore) phoneForFile(fileName string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	phone, ok := s.fileOwner[fileName]
	return phone, ok
}

// Server 是苏标主动安全附件服务器，承载 0x1210/0x1211/0x1212 信令
// 与 0x30 0x31 0x63 0x64 文件码流帧。
// TCP 与 UDP 可同时监听，二者共用同一套组装逻辑。
type Server struct {
	cfg       Config
	assembler *Assembler
	codec     *protocol.JT808PacketCodec

	listener    net.Listener
	udpConn     *net.UDPConn
	udpSessions *udpSessionStore

	closeOnce sync.Once
	done      chan struct{}
	wg        sync.WaitGroup
	cleanupWG sync.WaitGroup

	connMu sync.Mutex
	conns  map[net.Conn]struct{}
}

func NewServer(cfg Config) *Server {
	cfg = cfg.Normalize()
	s := &Server{
		cfg:         cfg,
		assembler:   NewAssembler(cfg),
		codec:       protocol.NewJT808PacketCodec(),
		udpSessions: newUDPSessionStore(),
		done:        make(chan struct{}),
		conns:       make(map[net.Conn]struct{}),
	}
	// 清理任务与服务器生命周期绑定：Stop 关闭 done 后等待其退出。
	s.cleanupWG.Add(1)
	routines.GoSafe(func() {
		defer s.cleanupWG.Done()
		s.cleanupLoop()
	})
	return s
}

func (s *Server) Assembler() *Assembler { return s.assembler }

// Listen 监听 TCP 附件端口。
func (s *Server) Listen(addr string) error {
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("listen attachment server: %w", err)
	}
	s.listener = listener
	log.Info().Str("network", "tcp").Str("addr", listener.Addr().String()).Str("dir", s.cfg.Dir).Msg("Attachment server listening")
	return nil
}

// ListenUDP 监听 UDP 附件端口，与 TCP 共用同一套组装逻辑。
func (s *Server) ListenUDP(addr string) error {
	udpAddr, err := net.ResolveUDPAddr("udp", addr)
	if err != nil {
		return fmt.Errorf("resolve attachment udp addr: %w", err)
	}
	conn, err := net.ListenUDP("udp", udpAddr)
	if err != nil {
		return fmt.Errorf("listen attachment server udp: %w", err)
	}
	s.udpConn = conn
	log.Info().Str("network", "udp").Str("addr", conn.LocalAddr().String()).Str("dir", s.cfg.Dir).Msg("Attachment server listening")
	return nil
}

// Addr 返回 TCP 监听地址；未启用 TCP 时返回空串。
func (s *Server) Addr() string {
	if s.listener == nil {
		return ""
	}
	return s.listener.Addr().String()
}

// UDPAddr 返回 UDP 监听地址；未启用 UDP 时返回空串。
func (s *Server) UDPAddr() string {
	if s.udpConn == nil {
		return ""
	}
	return s.udpConn.LocalAddr().String()
}

// Start 先对账落盘目录，再同时进入 TCP accept 与 UDP 收包循环，由调用方放入独立 goroutine。
func (s *Server) Start() {
	if s.listener == nil && s.udpConn == nil {
		log.Error().Msg("Attachment server is not listening")
		return
	}
	if err := s.assembler.Reconcile(); err != nil {
		log.Warn().Err(err).Str("dir", s.cfg.Dir).Msg("Fail to reconcile attachment directory")
	}

	if s.udpConn != nil {
		s.wg.Add(1)
		routines.GoSafe(func() {
			defer s.wg.Done()
			s.serveUDP()
		})
	}

	if s.listener == nil {
		<-s.done
		return
	}
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
		if s.udpConn != nil {
			_ = s.udpConn.Close()
		}
		s.connMu.Lock()
		for conn := range s.conns {
			_ = conn.Close()
		}
		s.connMu.Unlock()
	})
	s.cleanupWG.Wait()
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
	if s.cfg.CompletedTTL > 0 && s.cfg.CompletedTTL/2 < interval {
		interval = s.cfg.CompletedTTL / 2
	}
	if interval > time.Minute {
		interval = time.Minute
	}
	if interval < 10*time.Second {
		interval = 10 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	var lastPrune time.Time
	for {
		select {
		case <-s.done:
			return
		case now := <-ticker.C:
			s.assembler.CleanupStale(now, s.cfg.SessionTimeout)
			s.assembler.CleanupCompleted(now, s.cfg.CompletedTTL)
			// 每小时扫一次：pending 回收始终按来源前缀执行；保留期清理只做附件来源。
			if lastPrune.IsZero() || now.Sub(lastPrune) >= time.Hour {
				lastPrune = now
				if s.cfg.Storage != nil {
					if expired := s.assembler.expireReceivingBlob(now, s.cfg.SessionTimeout); expired > 0 {
						log.Info().Int("expired", expired).Msg("Marked stale attachment receiving records expired")
					}
					s.assembler.pruneBlobPending()
				}
				if s.cfg.Retention > 0 {
					s.pruneExpiredFiles(now)
				}
			}
		}
	}
}

// pruneExpiredFiles 删除超过保留期且不在组装中的附件文件。
func (s *Server) pruneExpiredFiles(now time.Time) {
	if s.cfg.Retention <= 0 {
		return
	}
	if s.cfg.Storage != nil {
		// 对象存储模式只清理附件来源前缀与 attachment_files 表，不触碰媒体来源。
		if removed := s.assembler.pruneBlobRetention(now); removed > 0 {
			log.Info().Int("removed", removed).Str("staging", s.cfg.Storage.StagingDir).
				Msg("Pruned expired attachment records and objects")
		}
		return
	}
	cutoff := now.Add(-s.cfg.Retention)
	active := s.assembler.ActivePaths()

	removed := 0
	walkErr := filepath.WalkDir(s.cfg.Dir, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			log.Warn().Err(err).Str("path", path).Msg("Fail to scan attachment dir")
			return nil
		}
		if entry.IsDir() {
			return nil
		}
		if _, ok := active[path]; ok {
			return nil
		}
		info, infoErr := entry.Info()
		if infoErr != nil {
			return nil
		}
		if !info.ModTime().Before(cutoff) {
			return nil
		}
		if removeErr := os.Remove(path); removeErr != nil {
			log.Warn().Err(removeErr).Str("path", path).Msg("Fail to prune expired attachment file")
			return nil
		}
		removed++
		return nil
	})
	if walkErr != nil {
		log.Warn().Err(walkErr).Str("dir", s.cfg.Dir).Msg("Fail to walk attachment dir")
	}
	if removed > 0 {
		log.Info().Int("removed", removed).Str("dir", s.cfg.Dir).Msg("Pruned expired attachment files")
	}
}

func (s *Server) serveConn(conn net.Conn) {
	state := &connState{}
	write := func(pkt []byte) error {
		_ = conn.SetWriteDeadline(time.Now().Add(connWriteTimeout))
		_, err := conn.Write(pkt)
		return err
	}
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
			s.handleJT808(state, frame, write)
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
			info := state.getInfo()
			s.writeChunk(info.Phone, fileName, offset, data)
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

// parseUDPDataFrame 解析单个 UDP 数据报内的文件码流帧，返回消耗的字节数。
// UDP 是数据报语义，一次 ReadFromUDP 可能包含多帧，因此按帧头中的长度字段逐帧消费。
func parseUDPDataFrame(frame []byte) (string, uint32, []byte, int, error) {
	if len(frame) < FrameHeaderSize {
		return "", 0, nil, 0, ErrInvalidChunkLength
	}
	if !bytes.Equal(frame[:len(FrameMagic)], FrameMagic[:]) {
		return "", 0, nil, 0, ErrFrameMagic
	}
	nameBytes := frame[len(FrameMagic) : len(FrameMagic)+FileNameFieldSize]
	fileName := strings.TrimRight(string(nameBytes), "\x00")
	offset := binary.BigEndian.Uint32(frame[len(FrameMagic)+FileNameFieldSize:])
	length := binary.BigEndian.Uint32(frame[len(FrameMagic)+FileNameFieldSize+4:])
	if length == 0 || length > FramePayloadLimit {
		return "", 0, nil, 0, ErrInvalidChunkLength
	}
	total := FrameHeaderSize + int(length)
	if total > len(frame) {
		return "", 0, nil, 0, io.ErrUnexpectedEOF
	}
	return fileName, offset, frame[FrameHeaderSize:total], total, nil
}

// serveUDP 串行处理 UDP 数据报，保证同一终端的数据帧与信令保持到达顺序。
func (s *Server) serveUDP() {
	buffer := make([]byte, 64*1024)
	for {
		_ = s.udpConn.SetReadDeadline(time.Now().Add(connReadTimeout))
		n, remote, err := s.udpConn.ReadFromUDP(buffer)
		if err != nil {
			select {
			case <-s.done:
				return
			default:
			}
			if netErr, ok := err.(net.Error); ok && netErr.Timeout() {
				continue
			}
			log.Warn().Err(err).Msg("Fail to read attachment UDP datagram")
			continue
		}
		payload := append([]byte(nil), buffer[:n]...)
		s.handleUDPDatagram(remote, payload)
	}
}

func (s *Server) handleUDPDatagram(remote *net.UDPAddr, payload []byte) {
	write := s.udpWriter(remote)
	for len(payload) > 0 {
		if payload[0] == 0x7e {
			// 苏标帧内的 0x7e 已被转义成 0x7d 0x02，因此下一个 0x7e 就是帧尾。
			end := bytes.IndexByte(payload[1:], 0x7e)
			if end < 0 {
				log.Warn().Str("remote", remote.String()).Msg("Discard truncated attachment signaling frame on UDP")
				return
			}
			frame := payload[:end+2]
			payload = payload[end+2:]
			pkt, err := s.codec.Decode(frame)
			if err != nil {
				log.Warn().Err(err).Str("remote", remote.String()).Msg("Discard malformed attachment signaling frame on UDP")
				continue
			}
			s.handlePacket(s.udpSessions.session(pkt.Header.PhoneNumber), pkt, write)
			continue
		}
		if bytes.HasPrefix(payload, FrameMagic[:]) {
			fileName, offset, data, consumed, err := parseUDPDataFrame(payload)
			if err != nil {
				log.Warn().Err(err).Str("remote", remote.String()).Msg("Discard malformed attachment data frame on UDP")
				return
			}
			payload = payload[consumed:]
			phone, ok := s.udpSessions.phoneForFile(fileName)
			if !ok {
				log.Warn().Str("file", fileName).Msg("Receive attachment data before alarm registration on UDP")
				continue
			}
			s.writeChunk(phone, fileName, offset, data)
			continue
		}
		log.Warn().Str("remote", remote.String()).Uint8("byte", payload[0]).Msg("Discard unexpected byte in attachment UDP datagram")
		return
	}
}

func (s *Server) udpWriter(remote *net.UDPAddr) frameWriter {
	return func(pkt []byte) error {
		_ = s.udpConn.SetWriteDeadline(time.Now().Add(connWriteTimeout))
		_, err := s.udpConn.WriteToUDP(pkt, remote)
		return err
	}
}

func (s *Server) writeChunk(phone, fileName string, offset uint32, data []byte) {
	if phone == "" {
		log.Warn().Str("file", fileName).Msg("Receive attachment data before alarm registration")
		return
	}
	if err := s.assembler.WriteChunk(phone, fileName, offset, data); err != nil {
		log.Warn().Err(err).Str("phone", phone).Str("file", fileName).Uint32("offset", offset).
			Int("length", len(data)).Msg("Reject attachment data chunk")
	}
}

func (s *Server) handleJT808(sess attachmentSession, frame []byte, write frameWriter) {
	pkt, err := s.codec.Decode(frame)
	if err != nil {
		log.Warn().Err(err).Msg("Fail to decode attachment signaling frame")
		return
	}
	s.handlePacket(sess, pkt, write)
}

func (s *Server) handlePacket(sess attachmentSession, pkt *model.PacketData, write frameWriter) {
	switch pkt.Header.MsgID {
	case 0x1210:
		s.handleAlarmInfo(sess, pkt, write)
	case 0x1211:
		s.handleFileInfo(sess, pkt, write)
	case 0x1212:
		s.handleFileComplete(sess, pkt, write)
	default:
		log.Debug().Uint16("msg_id", pkt.Header.MsgID).Str("phone", pkt.Header.PhoneNumber).
			Msg("Ignore unsupported message on attachment server")
	}
}

func (s *Server) handleAlarmInfo(sess attachmentSession, pkt *model.PacketData, write frameWriter) {
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
	sess.setInfo(info)
	for _, item := range msg.Attachments {
		sess.bindFile(item.FileName)
	}

	result := uint8(model.ResultSuccess)
	if err := s.assembler.RegisterAlarm(info); err != nil {
		log.Warn().Err(err).Str("phone", info.Phone).Str("alarm", info.AlarmNo).Msg("Reject 0x1210 attachment registration")
		result = uint8(model.ResultFail)
	}
	s.sendAck(sess, pkt, result, write)
}

func (s *Server) handleFileInfo(sess attachmentSession, pkt *model.PacketData, write frameWriter) {
	msg := &model.Msg1211{}
	if err := decodeSafely(msg, pkt); err != nil {
		log.Warn().Err(err).Msg("Fail to decode 0x1211")
		return
	}
	info := sess.getInfo()
	if info.Phone == "" {
		info.Phone = pkt.Header.PhoneNumber
		sess.setInfo(info)
	}
	sess.bindFile(msg.FileName)
	result := uint8(model.ResultSuccess)
	if _, err := s.assembler.RegisterFile(info, msg.FileName, msg.FileType, msg.FileSize); err != nil {
		log.Warn().Err(err).Str("phone", info.Phone).Str("file", msg.FileName).Msg("Reject 0x1211 file registration")
		result = uint8(model.ResultFail)
	}
	// 标准 4.6.3 只要求 0x1211 回通用应答；缺口回报仅由 0x1212 触发。
	s.sendAck(sess, pkt, result, write)
}

func (s *Server) handleFileComplete(sess attachmentSession, pkt *model.PacketData, write frameWriter) {
	msg := &model.Msg1212{}
	if err := decodeSafely(msg, pkt); err != nil {
		log.Warn().Err(err).Msg("Fail to decode 0x1212")
		return
	}
	info := sess.getInfo()
	if info.Phone == "" {
		info.Phone = pkt.Header.PhoneNumber
		sess.setInfo(info)
	}
	sess.bindFile(msg.FileName)
	if _, err := s.assembler.RegisterFile(info, msg.FileName, msg.FileType, msg.FileSize); err != nil {
		log.Warn().Err(err).Str("phone", info.Phone).Str("file", msg.FileName).Msg("Fail to register file on 0x1212")
	}
	s.sendCompletionCheck(sess, pkt, info.Phone, msg.FileName, msg.FileType, write)
}

func (s *Server) sendCompletionCheck(sess attachmentSession, pkt *model.PacketData, phone, fileName string, fileType uint8, write frameWriter) {
	result, err := s.assembler.CompleteCheck(phone, fileName)
	if err != nil {
		log.Warn().Err(err).Str("phone", phone).Str("file", fileName).Msg("Fail to check attachment completion")
		result = CompletionResult{Complete: false}
	}
	s.send9212Response(sess, pkt, fileName, fileType, result, write)
}

func (s *Server) sendAck(sess attachmentSession, pkt *model.PacketData, result uint8, write frameWriter) {
	msg := &model.Msg0001{
		Header:             replyHeader(pkt.Header, 0x0001, sess.nextSerial()),
		AnswerSerialNumber: pkt.Header.SerialNumber,
		AnswerMessageID:    pkt.Header.MsgID,
		Result:             result,
	}
	s.writeFrame(write, msg)
}

func (s *Server) send9212Response(sess attachmentSession, pkt *model.PacketData, fileName string, fileType uint8, result CompletionResult, write frameWriter) {
	msg := &model.Msg9212{
		Header:   replyHeader(pkt.Header, 0x9212, sess.nextSerial()),
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
	s.writeFrame(write, msg)
}

func (s *Server) writeFrame(write frameWriter, msg model.JT808Msg) {
	pkt, err := s.codec.Encode(msg)
	if err != nil {
		log.Error().Err(err).Uint16("msg_id", msg.GetHeader().MsgID).Msg("Fail to encode attachment response")
		return
	}
	if err := write(pkt); err != nil {
		log.Warn().Err(err).Uint16("msg_id", msg.GetHeader().MsgID).Msg("Fail to write attachment response")
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
