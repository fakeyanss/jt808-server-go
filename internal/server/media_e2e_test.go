package server

import (
	"context"
	"crypto/md5"
	"encoding/binary"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/media"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/runtime"
)

func TestE2E1078MediaIngestionOverUDPSocket(t *testing.T) {
	mediaDir := t.TempDir()
	mediaServer := media.NewServer(media.Config{Dir: mediaDir, MaxConcurrent: 2})
	require.NoError(t, mediaServer.Listen("127.0.0.1:0"))
	go mediaServer.Start()
	t.Cleanup(mediaServer.Stop)

	server := NewTCPServer()
	require.NoError(t, server.Listen("127.0.0.1:0"))
	go server.Start()
	t.Cleanup(server.Stop)

	const phone = "188765431001"
	terminal := dialBareTerminal(t, server.listener.Addr().String(), phone, "京A91001", "MEDIA-E2E-01")

	session, err := runtime.Default().Conn.Get(context.Background(), terminal.conn.LocalAddr().String())
	require.NoError(t, err)
	require.NotNil(t, session)

	udpAddr, err := net.ResolveUDPAddr("udp", mediaServer.Addr())
	require.NoError(t, err)
	require.NotZero(t, udpAddr.Port)

	request := &model.Msg9101{
		Header:         model.GenMsgHeader(&model.Device{Phone: phone, VersionDesc: model.Version2013}, 0x9101, session.GetNextSerialNum()),
		ServerIP:       "127.0.0.1",
		UDPPort:        uint16(udpAddr.Port),
		LogicChannelID: 1,
		DataType:       uint8(media.DataTypeIFrame),
		StreamType:     1,
	}
	server.Send(terminal.conn.LocalAddr().String(), request)

	var received model.Msg9101
	terminal.recvInto(t, 3*time.Second, 0x9101, &received)
	require.Equal(t, uint16(0x9101), received.Header.MsgID)
	require.Equal(t, phone, received.Header.PhoneNumber)
	require.Equal(t, model.Version2013, received.Header.Attr.VersionDesc)
	require.Equal(t, "127.0.0.1", received.ServerIP)
	require.Equal(t, uint16(udpAddr.Port), received.UDPPort)
	require.Equal(t, uint8(1), received.LogicChannelID)
	require.Equal(t, uint8(media.DataTypeIFrame), received.DataType)
	require.NotZero(t, received.Header.SerialNumber)

	// 终端按 0x9101 指示的地址，经真实 UDP socket 推送 PS 封装的 H.264 ES。
	es := []byte{
		0x00, 0x00, 0x00, 0x01, 0x67, 0x42, 0x00, 0x1E, 0xAB, 0x40, 0x28,
		0x00, 0x00, 0x00, 0x01, 0x68, 0xCE, 0x3C, 0x80,
		0x00, 0x00, 0x00, 0x01, 0x65, 0x88, 0x84, 0x00, 0x33, 0xFF, 0xEE,
	}
	udpConn, err := net.Dial("udp", mediaServer.Addr())
	require.NoError(t, err)
	t.Cleanup(func() { _ = udpConn.Close() })

	packet := e2eJT1078Packet(media.DataTypeIFrame, media.SubpackageAtomic, phone, 1,
		e2ePSVideoPayload(es))
	_, err = udpConn.Write(packet)
	require.NoError(t, err)

	path, content := waitForH264File(t, mediaDir, int64(len(es)))
	require.Contains(t, path, filepath.Join(phone, "channel_1"))
	require.Regexp(t, `^\d+\.h264$`, filepath.Base(path))
	require.Equal(t, es, content, "persisted H.264 ES must match the pushed payload")
	require.Equal(t, md5.Sum(es), md5.Sum(content))
}

func TestE2E1078SignalingAckPairingOverJT808Session(t *testing.T) {
	server := NewTCPServer()
	require.NoError(t, server.Listen("127.0.0.1:0"))
	go server.Start()
	t.Cleanup(server.Stop)

	const phone = "188765431002"
	terminal := dialBareTerminal(t, server.listener.Addr().String(), phone, "京A91002", "MEDIA-E2E-02")

	session, err := runtime.Default().Conn.Get(context.Background(), terminal.conn.LocalAddr().String())
	require.NoError(t, err)
	require.NotNil(t, session)

	device := &model.Device{Phone: phone, VersionDesc: model.Version2013}

	// 平台下发 0x9101，终端按真实报文解析并应答 0x0001。
	request := &model.Msg9101{
		Header:         model.GenMsgHeader(device, 0x9101, session.GetNextSerialNum()),
		ServerIP:       "10.0.0.1",
		TCPPort:        0x238D,
		UDPPort:        0x238E,
		LogicChannelID: 2,
		DataType:       uint8(media.DataTypePFrame),
		StreamType:     0,
	}
	server.Send(session.ID, request)

	var received model.Msg9101
	terminal.recvInto(t, 3*time.Second, 0x9101, &received)
	require.Equal(t, uint16(0x9101), received.Header.MsgID)
	require.Equal(t, phone, received.Header.PhoneNumber)
	require.Equal(t, model.Version2013, received.Header.Attr.VersionDesc)
	require.Equal(t, "10.0.0.1", received.ServerIP)
	require.Equal(t, uint16(0x238D), received.TCPPort)
	require.Equal(t, uint16(0x238E), received.UDPPort)
	require.Equal(t, uint8(2), received.LogicChannelID)
	require.Equal(t, uint8(media.DataTypePFrame), received.DataType)
	require.Equal(t, uint8(0), received.StreamType)
	serial9101 := received.Header.SerialNumber
	waitForPending(t, session, 1)

	// 错误流水号应答不得清除待确认项，证明按流水号精确配对。
	terminal.send(t, &model.Msg0001{
		Header:             uplinkHeader(0x0001, phone, 101),
		AnswerSerialNumber: serial9101 + 1,
		AnswerMessageID:    0x9101,
		Result:             0,
	})
	time.Sleep(200 * time.Millisecond)
	require.Equal(t, 1, session.PendingCount(), "mismatched ack must not clear pending entry")

	// 正确流水号应答后待确认项清零。
	terminal.send(t, &model.Msg0001{
		Header:             uplinkHeader(0x0001, phone, 102),
		AnswerSerialNumber: serial9101,
		AnswerMessageID:    0x9101,
		Result:             0,
	})
	waitForPending(t, session, 0)

	// 平台下发 0x9102，同样完成编码/应答配对。
	control := &model.Msg9102{
		Header:         model.GenMsgHeader(device, 0x9102, session.GetNextSerialNum()),
		LogicChannelID: 2,
		ControlCmd:     1,
		CloseMediaType: 1,
		StreamType:     1,
	}
	server.Send(session.ID, control)

	var receivedControl model.Msg9102
	terminal.recvInto(t, 3*time.Second, 0x9102, &receivedControl)
	require.Equal(t, uint16(0x9102), receivedControl.Header.MsgID)
	require.Equal(t, phone, receivedControl.Header.PhoneNumber)
	require.Equal(t, model.Version2013, receivedControl.Header.Attr.VersionDesc)
	require.Equal(t, uint8(2), receivedControl.LogicChannelID)
	require.Equal(t, uint8(1), receivedControl.ControlCmd)
	require.Equal(t, uint8(1), receivedControl.CloseMediaType)
	require.Equal(t, uint8(1), receivedControl.StreamType)
	serial9102 := receivedControl.Header.SerialNumber
	waitForPending(t, session, 1)

	terminal.send(t, &model.Msg0001{
		Header:             uplinkHeader(0x0001, phone, 103),
		AnswerSerialNumber: serial9102,
		AnswerMessageID:    0x9102,
		Result:             0,
	})
	waitForPending(t, session, 0)
}

func waitForPending(t *testing.T, session *model.Session, want int) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if session.PendingCount() == want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("pending count = %d, want %d", session.PendingCount(), want)
}

// e2eJT1078Packet 构造 1078 RTP 包（视频头 30 字节，V=1）。
func e2eJT1078Packet(dataType, subpackage uint8, phone string, channel uint8, body []byte) []byte {
	packet := make([]byte, 0, 30+len(body))
	packet = append(packet, 0x30, 0x31, 0x63, 0x64)
	packet = append(packet, 0x41, media.PayloadTypeH264)
	packet = binary.BigEndian.AppendUint16(packet, 7)
	for i := 0; i < 12; i += 2 {
		hi := phone[i] - '0'
		lo := phone[i+1] - '0'
		packet = append(packet, hi<<4|lo)
	}
	packet = append(packet, channel, dataType<<4|subpackage)
	packet = binary.BigEndian.AppendUint64(packet, 1000)
	packet = binary.BigEndian.AppendUint16(packet, 500)
	packet = binary.BigEndian.AppendUint16(packet, 100)
	packet = binary.BigEndian.AppendUint16(packet, uint16(len(body)))
	return append(packet, body...)
}

// e2ePSVideoPayload 构造 pack header + 视频 PES，模拟终端侧 PS 封装。
func e2ePSVideoPayload(es []byte) []byte {
	pack := []byte{
		0x00, 0x00, 0x01, 0xBA,
		0x44, 0x00, 0x04, 0x00, 0x04, 0x01, 0x00, 0x00, 0x03, 0xF0,
	}
	pesLen := 3 + len(es)
	pes := []byte{
		0x00, 0x00, 0x01, 0xE0,
		byte(pesLen >> 8), byte(pesLen),
		0x80, 0x00, 0x00,
	}
	payload := append(append(pack, pes...), es...)
	return payload
}

func waitForH264File(t *testing.T, root string, wantSize int64) (string, []byte) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		var files []string
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if !info.IsDir() && strings.HasSuffix(info.Name(), ".h264") {
				files = append(files, path)
			}
			return nil
		})
		require.NoError(t, err)
		if len(files) > 0 {
			info, err := os.Stat(files[0])
			require.NoError(t, err)
			if info.Size() == wantSize {
				content, err := os.ReadFile(files[0])
				require.NoError(t, err)
				return files[0], content
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("media file with size %d was not persisted under %s", wantSize, root)
	return "", nil
}
