package alarmserver

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/protocol"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

func buildJT808Frame(t *testing.T, msg model.JT808Msg) []byte {
	t.Helper()
	pkt, err := protocol.NewJT808PacketCodec().Encode(msg)
	require.NoError(t, err)
	return pkt
}

func readJT808Message(t *testing.T, reader *bufio.Reader) *model.PacketData {
	t.Helper()
	frame, err := readJT808Frame(reader)
	require.NoError(t, err)
	pkt, err := protocol.NewJT808PacketCodec().Decode(frame)
	require.NoError(t, err)
	return pkt
}

func buildDataFrame(fileName string, offset uint32, data []byte) []byte {
	header := make([]byte, FrameHeaderSize)
	copy(header[:len(FrameMagic)], FrameMagic[:])
	copy(header[len(FrameMagic):len(FrameMagic)+FileNameFieldSize], []byte(fileName))
	binary.BigEndian.PutUint32(header[len(FrameMagic)+FileNameFieldSize:], offset)
	binary.BigEndian.PutUint32(header[len(FrameMagic)+FileNameFieldSize+4:], uint32(len(data)))
	return append(header, data...)
}

func newAlarmHeader(msgID uint16, serial uint16) *model.MsgHeader {
	return model.GenMsgHeader(&model.Device{
		Phone:       "013800000001",
		VersionDesc: model.Version2013,
	}, msgID, serial)
}

func TestReadJT808FrameHandlesStickyAndHalfPackets(t *testing.T) {
	stream := []byte{0x7e, 0x11, 0x22, 0x7e, 0x7e, 0x33, 0x7e}
	reader := bufio.NewReader(bytes.NewReader(stream))

	first, err := readJT808Frame(reader)
	require.NoError(t, err)
	require.Equal(t, []byte{0x7e, 0x11, 0x22, 0x7e}, first)

	second, err := readJT808Frame(reader)
	require.NoError(t, err)
	require.Equal(t, []byte{0x7e, 0x33, 0x7e}, second)

	pr, pw := io.Pipe()
	go func() {
		_, _ = pw.Write(second[:2])
		time.Sleep(10 * time.Millisecond)
		_, _ = pw.Write(second[2:])
		_ = pw.Close()
	}()
	half, err := readJT808Frame(bufio.NewReader(pr))
	require.NoError(t, err)
	require.Equal(t, second, half)
}

func TestReadDataFrameParsesFixedHeader(t *testing.T) {
	payload := []byte("hello-attachment")
	frame := buildDataFrame("alarm.jpg", 12345, payload)
	reader := bufio.NewReader(bytes.NewReader(frame))
	fileName, offset, data, err := readDataFrame(reader)
	require.NoError(t, err)
	require.Equal(t, "alarm.jpg", fileName)
	require.Equal(t, uint32(12345), offset)
	require.Equal(t, payload, data)

	_, _, _, err = readDataFrame(bufio.NewReader(bytes.NewReader(frame[:FrameHeaderSize-1])))
	require.Error(t, err)
}

func TestServerHandlesAttachmentFlow(t *testing.T) {
	dir := t.TempDir()
	server := NewServer(Config{
		Dir:            dir,
		MaxFileSize:    16 * 1024 * 1024,
		MaxConcurrent:  4,
		SessionTimeout: time.Minute,
	})
	t.Cleanup(server.Stop)

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	go server.serveConn(serverConn)
	defer serverConn.Close()

	fileName := "00_64_6401_01_ALARM.jpg"
	size := 3 * DefaultBlockSize
	source := buildSource(size)
	phone := "013800000001"

	alarm := &model.Msg1210{
		Header:     newAlarmHeader(0x1210, 1),
		TerminalID: "ABC1234",
		AlarmSign:  testAlarmSign(),
		AlarmNo:    "ALARM-0001",
		InfoType:   0,
		Attachments: []model.AlarmAttachmentEntry{
			{FileName: fileName, FileSize: uint32(size)},
		},
	}
	if _, err := clientConn.Write(buildJT808Frame(t, alarm)); err != nil {
		require.NoError(t, err)
	}
	reader := bufio.NewReader(clientConn)
	ack := readJT808Message(t, reader)
	require.Equal(t, uint16(0x0001), ack.Header.MsgID)
	require.Equal(t, uint8(0), ack.Body[len(ack.Body)-1])

	// 乱序发送并故意丢掉第 1 块，数据帧按半包方式写出。
	order := []int{2, 0}
	for _, index := range order {
		offset := index * DefaultBlockSize
		frame := buildDataFrame(fileName, uint32(offset), source[offset:offset+DefaultBlockSize])
		half := len(frame) / 2
		_, err := clientConn.Write(frame[:half])
		require.NoError(t, err)
		time.Sleep(5 * time.Millisecond)
		_, err = clientConn.Write(frame[half:])
		require.NoError(t, err)
	}

	fileInfo := &model.Msg1211{
		Header:   newAlarmHeader(0x1211, 2),
		FileName: fileName,
		FileType: 0,
		FileSize: uint32(size),
	}
	_, err := clientConn.Write(buildJT808Frame(t, fileInfo))
	require.NoError(t, err)
	ack = readJT808Message(t, reader)
	require.Equal(t, uint16(0x0001), ack.Header.MsgID)

	retransmit := readJT808Message(t, reader)
	require.Equal(t, uint16(0x9212), retransmit.Header.MsgID)
	msg9212 := &model.Msg9212{}
	require.NoError(t, msg9212.Decode(retransmit))
	require.Equal(t, uint8(1), msg9212.Result)
	require.Equal(t, fileName, msg9212.FileName)
	require.Equal(t, []model.RetransmitPacket{{
		Offset: DefaultBlockSize,
		Length: DefaultBlockSize,
	}}, msg9212.RetransmitPackets)

	offset := DefaultBlockSize
	_, err = clientConn.Write(buildDataFrame(fileName, uint32(offset), source[offset:offset+DefaultBlockSize]))
	require.NoError(t, err)

	complete := &model.Msg1212{
		Header:   newAlarmHeader(0x1212, 3),
		FileName: fileName,
		FileType: 0,
		FileSize: uint32(size),
	}
	_, err = clientConn.Write(buildJT808Frame(t, complete))
	require.NoError(t, err)
	final := readJT808Message(t, reader)
	require.Equal(t, uint16(0x9212), final.Header.MsgID)
	msg9212 = &model.Msg9212{}
	require.NoError(t, msg9212.Decode(final))
	require.Equal(t, uint8(0), msg9212.Result)
	require.Empty(t, msg9212.RetransmitPackets)

	finalPath := filepath.Join(dir, phone, "20260107", "ALARM-0001", fileName)
	onDisk, err := os.ReadFile(finalPath)
	require.NoError(t, err)
	require.Equal(t, source, onDisk)
}

func TestServerRejectsDataBeforeRegistration(t *testing.T) {
	dir := t.TempDir()
	server := NewServer(Config{Dir: dir, MaxFileSize: 1024, MaxConcurrent: 2, SessionTimeout: time.Minute})
	t.Cleanup(server.Stop)

	clientConn, serverConn := net.Pipe()
	defer clientConn.Close()
	go server.serveConn(serverConn)
	defer serverConn.Close()

	_, err := clientConn.Write(buildDataFrame("orphan.jpg", 0, make([]byte, 16)))
	require.NoError(t, err)
	_, err = clientConn.Write(buildJT808Frame(t, &model.Msg1211{
		Header:   newAlarmHeader(0x1211, 1),
		FileName: "orphan.jpg",
		FileSize: 16,
	}))
	require.NoError(t, err)
	// 未形成 0x1210 登记上下文时，0x1211 会用消息头手机号补登记，但数据帧已被丢弃。
	ack := readJT808Message(t, bufio.NewReader(clientConn))
	require.Equal(t, uint16(0x0001), ack.Header.MsgID)
}
