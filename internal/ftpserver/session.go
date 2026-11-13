package ftpserver

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

const maxCommandLine = 2048

// session 是一条 FTP 控制连接。
type session struct {
	srv  *Server
	conn net.Conn
	rd   *bufio.Reader

	writeMu sync.Mutex

	username   string
	task       *LoginResult
	pasv       net.Listener
	restOffset int64
	hasRest    bool

	transferMu sync.Mutex
	current    *upload
	data       net.Conn
}

func newSession(srv *Server, conn net.Conn) *session {
	return &session{srv: srv, conn: conn, rd: bufio.NewReaderSize(conn, maxCommandLine)}
}

func (s *session) serve() {
	defer func() {
		s.closePassive()
		if s.data != nil {
			_ = s.data.Close()
		}
		_ = s.conn.Close()
	}()
	_ = s.reply("220 JT/T 1078 file upload service ready")
	for {
		_ = s.conn.SetReadDeadline(time.Now().Add(s.srv.cfg.IdleTimeout))
		line, err := s.rd.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		if strings.TrimSpace(line) == "" {
			continue
		}
		cmd, arg := splitCommand(line)
		quit, fatal := s.dispatch(cmd, arg)
		if fatal {
			return
		}
		if quit {
			return
		}
	}
}

func splitCommand(line string) (string, string) {
	line = strings.TrimLeft(line, " \t")
	if idx := strings.IndexAny(line, " \t"); idx >= 0 {
		return strings.ToUpper(line[:idx]), strings.TrimSpace(line[idx+1:])
	}
	return strings.ToUpper(line), ""
}

func (s *session) dispatch(cmd, arg string) (quit bool, fatal bool) {
	switch cmd {
	case "USER":
		if arg == "" {
			_ = s.reply("501 USER requires a username")
			return false, false
		}
		s.username = arg
		_ = s.reply("331 Password required")
	case "PASS":
		return false, s.login(arg)
	case "SYST":
		_ = s.reply("215 UNIX Type: L8")
	case "FEAT":
		_ = s.multiline("211-Features:", []string{" PASV", " EPSV", " REST STREAM", " SIZE", " UTF8"}, "211 End")
	case "OPTS":
		_ = s.reply("200 UTF8 enabled")
	case "PWD":
		if s.requireLogin() {
			_ = s.reply(`257 "/" is the current directory`)
		}
	case "CWD", "CDUP":
		// 虚拟根按任务隔离：任何路径切换都保持在任务根目录，越权路径被忽略。
		if s.requireLogin() {
			_ = s.reply("250 Directory unchanged: task root is isolated")
		}
	case "TYPE":
		if strings.EqualFold(arg, "I") {
			_ = s.reply("200 Type set to I")
		} else {
			_ = s.reply("504 Only binary mode is supported")
		}
	case "MODE":
		if strings.EqualFold(arg, "S") {
			_ = s.reply("200 Mode set to S")
		} else {
			_ = s.reply("504 Only stream mode is supported")
		}
	case "STRU":
		if strings.EqualFold(arg, "F") {
			_ = s.reply("200 Structure set to F")
		} else {
			_ = s.reply("504 Only file structure is supported")
		}
	case "NOOP":
		_ = s.reply("200 OK")
	case "QUIT":
		_ = s.reply("221 Goodbye")
		return true, false
	case "PORT", "EPRT":
		_ = s.reply("502 PORT/EPRT is disabled; use PASV or EPSV")
	case "PASV":
		s.handlePASV(false)
	case "EPSV":
		s.handlePASV(true)
	case "SIZE":
		s.handleSIZE(arg)
	case "MKD":
		if s.requireLogin() {
			_ = s.reply("257 \"/" + sanitizeVirtualPath(arg) + "\" created")
		}
	case "REST":
		s.handleREST(arg)
	case "ABOR":
		s.handleABOR()
	case "STOR":
		s.handleStore(arg, false)
	case "APPE":
		s.handleStore(arg, true)
	default:
		_ = s.reply("500 Command not recognized")
	}
	return false, false
}

func (s *session) login(password string) bool {
	if s.task != nil {
		_ = s.reply("230 Already logged in")
		return false
	}
	if s.username == "" {
		_ = s.reply("503 Login with USER first")
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	result, err := s.srv.cfg.Control.Login(ctx, s.srv.cfg.NodeID, s.username, password)
	if err != nil {
		if err == ErrLoginRejected {
			_ = s.reply("530 Invalid or revoked credentials")
			return false
		}
		// control 不可达：拒绝新登录，但已建立连接继续工作。
		_ = s.reply("421 Control is unavailable; try again later")
		log.Warn().Err(err).Str("username", s.username).
			Msg("FTP login rejected because control is unreachable")
		return true
	}
	s.task = &result
	_ = s.reply("230 Login successful")
	return false
}

func (s *session) requireLogin() bool {
	if s.task == nil {
		_ = s.reply("530 Please login first")
		return false
	}
	return true
}

func (s *session) handlePASV(extended bool) {
	if !s.requireLogin() {
		return
	}
	s.closePassive()
	ln, port, err := s.srv.openPassive()
	if err != nil {
		_ = s.reply("425 Cannot open passive connection")
		return
	}
	s.pasv = ln
	if extended {
		_ = s.reply(fmt.Sprintf("229 Entering Extended Passive Mode (|||%d|)", port))
		return
	}
	ip := s.srv.advertiseIP(s.conn.LocalAddr())
	parts := strings.Split(ip, ".")
	if len(parts) != 4 {
		_ = s.reply("425 Cannot advertise passive address")
		return
	}
	_ = s.reply(fmt.Sprintf("227 Entering Passive Mode (%s,%s,%s,%s,%d,%d)",
		parts[0], parts[1], parts[2], parts[3], port/256, port%256))
}

func (s *session) handleSIZE(arg string) {
	if !s.requireLogin() {
		return
	}
	name := baseName(arg)
	if name == "" {
		_ = s.reply("550 Invalid file name")
		return
	}
	up := s.srv.newUpload(*s.task, name)
	info, err := os.Stat(up.staging)
	if err != nil {
		_ = s.reply("213 0")
		return
	}
	_ = s.reply(fmt.Sprintf("213 %d", info.Size()))
}

func (s *session) handleREST(arg string) {
	if !s.requireLogin() {
		return
	}
	offset, err := strconv.ParseInt(strings.TrimSpace(arg), 10, 64)
	if err != nil || offset < 0 {
		_ = s.reply("501 Invalid restart offset")
		return
	}
	s.restOffset = offset
	s.hasRest = true
	_ = s.reply(fmt.Sprintf("350 Restarting at %d", offset))
}

func (s *session) handleABOR() {
	s.transferMu.Lock()
	up := s.current
	data := s.data
	s.transferMu.Unlock()
	if up == nil {
		_ = s.reply("226 Nothing to abort")
		return
	}
	up.cancel()
	if data != nil {
		_ = data.Close()
	}
	_ = s.reply("426 Transfer aborted")
}

func (s *session) handleStore(arg string, appendMode bool) {
	if !s.requireLogin() {
		return
	}
	name := baseName(arg)
	if name == "" {
		_ = s.reply("550 Invalid file name")
		return
	}
	if s.pasv == nil {
		_ = s.reply("425 Use PASV or EPSV first")
		return
	}
	ln := s.pasv
	s.pasv = nil
	if err := s.reply("150 Opening binary data connection"); err != nil {
		_ = ln.Close()
		return
	}
	_ = ln.(*net.TCPListener).SetDeadline(time.Now().Add(s.srv.cfg.DataTimeout))
	data, err := ln.Accept()
	_ = ln.Close()
	if err != nil {
		_ = s.reply("425 Cannot accept data connection")
		return
	}
	defer data.Close()

	up := s.srv.newUpload(*s.task, name)
	wantOffset := int64(0)
	overwrite := !appendMode
	if appendMode {
		overwrite = false
	} else if s.hasRest {
		wantOffset = s.restOffset
		overwrite = wantOffset <= 0
	}
	if err := up.open(context.Background(), wantOffset, overwrite); err != nil {
		_ = s.reply("550 " + err.Error())
		up.closeLocal()
		return
	}
	s.restOffset = 0
	s.hasRest = false

	key := taskKey(s.task.Phone, s.task.Serial)
	if paused, cancelled := s.srv.taskControlState(key); cancelled {
		_ = s.reply("550 Upload task has been cancelled")
		up.abort(context.Background())
		return
	} else if paused {
		// 终端在暂停后才开始传输：保持阻塞，等待 control 的继续指令。
		up.pause()
	}
	if !s.srv.beginTransfer(key, up, data) {
		_ = s.reply("450 Too many concurrent uploads")
		up.closeLocal()
		return
	}
	s.transferMu.Lock()
	s.current = up
	s.data = data
	s.transferMu.Unlock()

	s.pump(up, data)

	s.transferMu.Lock()
	s.current = nil
	s.data = nil
	s.transferMu.Unlock()
	s.srv.endTransfer(key)
}

func (s *session) pump(up *upload, data net.Conn) {
	ctx := context.Background()
	buf := make([]byte, 32*1024)
	for {
		if up.cancelled.Load() {
			up.abort(ctx)
			_ = s.reply("426 Transfer cancelled")
			return
		}
		n, err := data.Read(buf)
		if n > 0 {
			if werr := up.Write(ctx, buf[:n]); werr != nil {
				if werr == ErrUploadCancelled {
					up.abort(ctx)
					_ = s.reply("426 Transfer cancelled")
					return
				}
				log.Warn().Err(werr).Str("phone", up.task.Phone).Uint16("serial", up.task.Serial).
					Str("file", up.fileName).Msg("FTP upload write failed")
				up.interrupt()
				_ = s.reply("451 " + werr.Error())
				return
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			if up.cancelled.Load() {
				up.abort(ctx)
				_ = s.reply("426 Transfer cancelled")
				return
			}
			up.interrupt()
			_ = s.reply("426 Data connection closed before completion")
			return
		}
	}
	if err := up.finish(ctx); err != nil {
		up.interrupt()
		_ = s.reply("451 " + err.Error())
		return
	}
	_ = s.reply("226 Transfer complete")
}

func (s *session) closePassive() {
	if s.pasv != nil {
		_ = s.pasv.Close()
		s.pasv = nil
	}
}

func (s *session) reply(line string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if _, err := io.WriteString(s.conn, line+"\r\n"); err != nil {
		return err
	}
	return nil
}

func (s *session) multiline(first string, lines []string, last string) error {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	if _, err := io.WriteString(s.conn, first+"\r\n"); err != nil {
		return err
	}
	for _, line := range lines {
		if _, err := io.WriteString(s.conn, line+"\r\n"); err != nil {
			return err
		}
	}
	_, err := io.WriteString(s.conn, last+"\r\n")
	return err
}

// baseName 只取路径最后一段，忽略任何目录层级。
//
// 虚拟根按任务隔离，CWD/CDUP 不改变落点；文件名只作为对象的最后一层。
func baseName(arg string) string {
	arg = strings.ReplaceAll(strings.TrimSpace(arg), "\\", "/")
	name := path.Base(arg)
	if name == "" || name == "." || name == "/" {
		return ""
	}
	if len(name) > 128 {
		name = name[:128]
	}
	return name
}

func sanitizeVirtualPath(arg string) string {
	name := strings.Trim(strings.TrimSpace(arg), "/")
	if name == "" {
		return ""
	}
	return name
}
