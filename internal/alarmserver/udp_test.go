package alarmserver

import (
	"crypto/md5"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/protocol"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

// udpBlockSize 是测试用数据体长度。
// UDP 单个数据报上限约 65507 字节（含 62 字节帧头），且部分平台
// 默认更低（macOS 的 net.inet.udp.maxdgram 约 9216），因此测试取 4K。
const udpBlockSize = 4 * 1024

func startUDPTestServer(t *testing.T) *Server {
	t.Helper()
	srv := NewServer(Config{
		Dir:            t.TempDir(),
		MaxFileSize:    16 * 1024 * 1024,
		MaxConcurrent:  4,
		SessionTimeout: time.Minute,
	})
	require.NoError(t, srv.ListenUDP("127.0.0.1:0"))
	go srv.Start()
	t.Cleanup(srv.Stop)
	return srv
}

func dialUDP(t *testing.T, srv *Server) *net.UDPConn {
	t.Helper()
	addr, err := net.ResolveUDPAddr("udp", srv.UDPAddr())
	require.NoError(t, err)
	conn, err := net.DialUDP("udp", nil, addr)
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return conn
}

func sendUDPMsg(t *testing.T, conn *net.UDPConn, msg model.JT808Msg) {
	t.Helper()
	pkt, err := protocol.NewJT808PacketCodec().Encode(msg)
	require.NoError(t, err)
	_, err = conn.Write(pkt)
	require.NoError(t, err)
}

func sendUDPChunk(t *testing.T, conn *net.UDPConn, fileName string, offset uint32, data []byte) {
	t.Helper()
	_, err := conn.Write(buildDataFrame(fileName, offset, data))
	require.NoError(t, err)
}

func receiveUDPMessage(t *testing.T, conn *net.UDPConn) *model.PacketData {
	t.Helper()
	require.NoError(t, conn.SetReadDeadline(time.Now().Add(5*time.Second)))
	buffer := make([]byte, 64*1024)
	n, err := conn.Read(buffer)
	require.NoError(t, err)
	pkt, err := protocol.NewJT808PacketCodec().Decode(buffer[:n])
	require.NoError(t, err)
	return pkt
}

func receiveUDP9212(t *testing.T, conn *net.UDPConn) *model.Msg9212 {
	t.Helper()
	pkt := receiveUDPMessage(t, conn)
	require.Equal(t, uint16(0x9212), pkt.Header.MsgID)
	msg := &model.Msg9212{}
	require.NoError(t, msg.Decode(pkt))
	return msg
}

// UDP 上传链路：登记、乱序数据帧、0x1211 通用应答、0x1212 缺口回报与补传收敛。
// 三个数据报套接字模拟不同源端口，验证会话按手机号 + 文件（含报警编号）定位，
// 不依赖单一连接对象。
func TestServerUDPUploadRetransmission(t *testing.T) {
	srv := startUDPTestServer(t)
	register := dialUDP(t, srv)
	chunks := dialUDP(t, srv)
	complete := dialUDP(t, srv)

	phone := "013800000001"
	fileName := "00_64_6401_01_ALARM.jpg"
	size := 11*udpBlockSize + 1234
	source := buildSource(size)

	sendUDPMsg(t, register, &model.Msg1210{
		Header:     newAlarmHeader(0x1210, 1),
		TerminalID: "ABC1234",
		AlarmSign:  testAlarmSign(),
		AlarmNo:    "ALARM-0003",
		Attachments: []model.AlarmAttachmentEntry{
			{FileName: fileName, FileSize: uint32(size)},
		},
	})
	ack := receiveUDPMessage(t, register)
	require.Equal(t, uint16(0x0001), ack.Header.MsgID)

	dropped := map[int]bool{2: true, 6: true, 11: true}
	for index := 0; index < 12; index++ {
		if dropped[index] {
			continue
		}
		offset := index * udpBlockSize
		end := offset + udpBlockSize
		if end > size {
			end = size
		}
		sendUDPChunk(t, chunks, fileName, uint32(offset), source[offset:end])
	}
	// 用同一套接字的 0x1211 应答作为屏障，确认之前的数据报都已处理。
	sendUDPMsg(t, chunks, &model.Msg1211{
		Header:   newAlarmHeader(0x1211, 2),
		FileName: fileName,
		FileType: 0,
		FileSize: uint32(size),
	})
	ack = receiveUDPMessage(t, chunks)
	require.Equal(t, uint16(0x0001), ack.Header.MsgID)

	sendUDPMsg(t, complete, &model.Msg1212{
		Header:   newAlarmHeader(0x1212, 3),
		FileName: fileName,
		FileType: 0,
		FileSize: uint32(size),
	})
	retransmit := receiveUDP9212(t, complete)
	require.Equal(t, uint8(1), retransmit.Result)
	require.Equal(t, []model.RetransmitPacket{
		{Offset: 2 * udpBlockSize, Length: udpBlockSize},
		{Offset: 6 * udpBlockSize, Length: udpBlockSize},
		{Offset: 11 * udpBlockSize, Length: 1234},
	}, retransmit.RetransmitPackets)

	for index := range dropped {
		offset := index * udpBlockSize
		end := offset + udpBlockSize
		if end > size {
			end = size
		}
		sendUDPChunk(t, chunks, fileName, uint32(offset), source[offset:end])
	}
	sendUDPMsg(t, chunks, &model.Msg1211{
		Header:   newAlarmHeader(0x1211, 4),
		FileName: fileName,
		FileType: 0,
		FileSize: uint32(size),
	})
	ack = receiveUDPMessage(t, chunks)
	require.Equal(t, uint16(0x0001), ack.Header.MsgID)

	sendUDPMsg(t, complete, &model.Msg1212{
		Header:   newAlarmHeader(0x1212, 5),
		FileName: fileName,
		FileType: 0,
		FileSize: uint32(size),
	})
	completed := receiveUDP9212(t, complete)
	require.Equal(t, uint8(0), completed.Result)
	require.Empty(t, completed.RetransmitPackets)

	finalPath := filepath.Join(srv.Assembler().Config().Dir, phone, "20260107", "ALARM-0003", fileName)
	onDisk, err := os.ReadFile(finalPath)
	require.NoError(t, err)
	require.Equal(t, fmt.Sprintf("%x", md5.Sum(source)), fmt.Sprintf("%x", md5.Sum(onDisk)))

	meta, err := srv.Assembler().FindMetadata(phone, fileName)
	require.NoError(t, err)
	require.Equal(t, fmt.Sprintf("%x", md5.Sum(source)), meta.MD5)
}

// 单个 UDP 数据报可以携带多个数据帧，按帧头长度逐帧消费。
func TestServerUDPDatagramMultipleDataFrames(t *testing.T) {
	srv := startUDPTestServer(t)
	conn := dialUDP(t, srv)
	phone := "013800000001"
	fileName := "01_64_6401_01_ALARM.bin"
	size := 2 * udpBlockSize
	source := buildSource(size)

	sendUDPMsg(t, conn, &model.Msg1210{
		Header:     newAlarmHeader(0x1210, 1),
		TerminalID: "ABC1234",
		AlarmSign:  testAlarmSign(),
		AlarmNo:    "ALARM-0004",
		Attachments: []model.AlarmAttachmentEntry{
			{FileName: fileName, FileSize: uint32(size)},
		},
	})
	require.Equal(t, uint16(0x0001), receiveUDPMessage(t, conn).Header.MsgID)

	datagram := append(buildDataFrame(fileName, 0, source[:udpBlockSize]),
		buildDataFrame(fileName, udpBlockSize, source[udpBlockSize:])...)
	_, err := conn.Write(datagram)
	require.NoError(t, err)

	sendUDPMsg(t, conn, &model.Msg1212{
		Header:   newAlarmHeader(0x1212, 2),
		FileName: fileName,
		FileType: 0,
		FileSize: uint32(size),
	})
	result := receiveUDP9212(t, conn)
	require.Equal(t, uint8(0), result.Result)

	onDisk, err := os.ReadFile(filepath.Join(srv.Assembler().Config().Dir, phone, "20260107", "ALARM-0004", fileName))
	require.NoError(t, err)
	require.Equal(t, source, onDisk)
}
