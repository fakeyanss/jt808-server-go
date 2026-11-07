package alarmserver

import (
	"bufio"
	"crypto/md5"
	"fmt"
	"math/rand"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/protocol"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

func startTestServer(t *testing.T) (*Server, net.Conn, *bufio.Reader) {
	t.Helper()
	srv := NewServer(Config{
		Dir:            t.TempDir(),
		MaxFileSize:    16 * 1024 * 1024,
		MaxConcurrent:  4,
		SessionTimeout: time.Minute,
	})
	require.NoError(t, srv.Listen("127.0.0.1:0"))
	go srv.Start()
	t.Cleanup(srv.Stop)

	conn, err := net.Dial("tcp", srv.Addr())
	require.NoError(t, err)
	t.Cleanup(func() { _ = conn.Close() })
	return srv, conn, bufio.NewReader(conn)
}

func sendMsg(t *testing.T, conn net.Conn, msg model.JT808Msg) {
	t.Helper()
	pkt, err := protocol.NewJT808PacketCodec().Encode(msg)
	require.NoError(t, err)
	_, err = conn.Write(pkt)
	require.NoError(t, err)
}

func sendChunk(t *testing.T, conn net.Conn, fileName string, offset uint32, data []byte) {
	t.Helper()
	_, err := conn.Write(buildDataFrame(fileName, offset, data))
	require.NoError(t, err)
}

func receive9212(t *testing.T, reader *bufio.Reader) *model.Msg9212 {
	t.Helper()
	pkt := readJT808Message(t, reader)
	require.Equal(t, uint16(0x9212), pkt.Header.MsgID)
	msg := &model.Msg9212{}
	require.NoError(t, msg.Decode(pkt))
	return msg
}

func TestServerEndToEndRetransmissionAndMD5(t *testing.T) {
	srv, conn, reader := startTestServer(t)
	phone := "013800000001"
	fileName := "00_64_6401_01_ALARM.jpg"
	size := 11*DefaultBlockSize + 4321 // 12 块，最后一块 4321 字节
	source := buildSource(size)

	sendMsg(t, conn, &model.Msg1210{
		Header:     newAlarmHeader(0x1210, 1),
		TerminalID: "ABC1234",
		AlarmSign:  testAlarmSign(),
		AlarmNo:    "ALARM-0001",
		Attachments: []model.AlarmAttachmentEntry{
			{FileName: fileName, FileSize: uint32(size)},
		},
	})
	ack := readJT808Message(t, reader)
	require.Equal(t, uint16(0x0001), ack.Header.MsgID)

	dropped := map[int]bool{2: true, 6: true, 11: true}
	order := make([]int, 0, 12)
	for i := 0; i < 12; i++ {
		if !dropped[i] {
			order = append(order, i)
		}
	}
	rand.New(rand.NewSource(20261107)).Shuffle(len(order), func(i, j int) { order[i], order[j] = order[j], order[i] })
	for _, index := range order {
		offset := index * DefaultBlockSize
		end := offset + DefaultBlockSize
		if end > size {
			end = size
		}
		sendChunk(t, conn, fileName, uint32(offset), source[offset:end])
	}

	sendMsg(t, conn, &model.Msg1211{
		Header:   newAlarmHeader(0x1211, 2),
		FileName: fileName,
		FileType: 0,
		FileSize: uint32(size),
	})
	ack = readJT808Message(t, reader)
	require.Equal(t, uint16(0x0001), ack.Header.MsgID)

	retransmit := receive9212(t, reader)
	require.Equal(t, uint8(1), retransmit.Result)
	require.Equal(t, fileName, retransmit.FileName)
	require.Equal(t, []model.RetransmitPacket{
		{Offset: 2 * DefaultBlockSize, Length: DefaultBlockSize},
		{Offset: 6 * DefaultBlockSize, Length: DefaultBlockSize},
		{Offset: 11 * DefaultBlockSize, Length: 4321},
	}, retransmit.RetransmitPackets)

	for index := range dropped {
		offset := index * DefaultBlockSize
		end := offset + DefaultBlockSize
		if end > size {
			end = size
		}
		sendChunk(t, conn, fileName, uint32(offset), source[offset:end])
	}

	sendMsg(t, conn, &model.Msg1212{
		Header:   newAlarmHeader(0x1212, 3),
		FileName: fileName,
		FileType: 0,
		FileSize: uint32(size),
	})
	completed := receive9212(t, reader)
	require.Equal(t, uint8(0), completed.Result)
	require.Empty(t, completed.RetransmitPackets)

	finalPath := filepath.Join(srv.Assembler().Config().Dir, phone, "20260107", "ALARM-0001", fileName)
	onDisk, err := os.ReadFile(finalPath)
	require.NoError(t, err)
	require.Equal(t, fmt.Sprintf("%x", md5.Sum(source)), fmt.Sprintf("%x", md5.Sum(onDisk)))
	require.Equal(t, source, onDisk)

	meta, err := srv.Assembler().FindMetadata(phone, fileName)
	require.NoError(t, err)
	require.Equal(t, fmt.Sprintf("%x", md5.Sum(source)), meta.MD5)
	require.Equal(t, []string{fileName}, []string{meta.FileName})
}

func TestServerEndToEndTwoFilesConcurrent(t *testing.T) {
	srv, conn, reader := startTestServer(t)
	phone := "013800000001"
	type fileCase struct {
		name   string
		source []byte
		drop   int
	}
	cases := []fileCase{
		{name: "front.jpg", source: buildSource(5*DefaultBlockSize + 777), drop: 2},
		{name: "driver.wav", source: buildSource(4*DefaultBlockSize + 123), drop: 3},
	}

	sendMsg(t, conn, &model.Msg1210{
		Header:     newAlarmHeader(0x1210, 1),
		TerminalID: "ABC1234",
		AlarmSign:  testAlarmSign(),
		AlarmNo:    "ALARM-0002",
		Attachments: []model.AlarmAttachmentEntry{
			{FileName: cases[0].name, FileSize: uint32(len(cases[0].source))},
			{FileName: cases[1].name, FileSize: uint32(len(cases[1].source))},
		},
	})
	ack := readJT808Message(t, reader)
	require.Equal(t, uint16(0x0001), ack.Header.MsgID)

	// 两个文件按块交错发送，模拟同一终端并发上传。
	maxBlocks := 0
	for _, item := range cases {
		total := (len(item.source) + DefaultBlockSize - 1) / DefaultBlockSize
		if total > maxBlocks {
			maxBlocks = total
		}
	}
	for index := 0; index < maxBlocks; index++ {
		for _, item := range cases {
			total := (len(item.source) + DefaultBlockSize - 1) / DefaultBlockSize
			if index >= total || index == item.drop {
				continue
			}
			offset := index * DefaultBlockSize
			end := offset + DefaultBlockSize
			if end > len(item.source) {
				end = len(item.source)
			}
			sendChunk(t, conn, item.name, uint32(offset), item.source[offset:end])
		}
	}

	for serial, item := range cases {
		sendMsg(t, conn, &model.Msg1211{
			Header:   newAlarmHeader(0x1211, uint16(2+serial)),
			FileName: item.name,
			FileType: 0,
			FileSize: uint32(len(item.source)),
		})
		ack = readJT808Message(t, reader)
		require.Equal(t, uint16(0x0001), ack.Header.MsgID)
		retransmit := receive9212(t, reader)
		require.Equal(t, uint8(1), retransmit.Result)
		require.Len(t, retransmit.RetransmitPackets, 1)
		require.Equal(t, uint32(item.drop*DefaultBlockSize), retransmit.RetransmitPackets[0].Offset)

		offset := item.drop * DefaultBlockSize
		end := offset + DefaultBlockSize
		if end > len(item.source) {
			end = len(item.source)
		}
		sendChunk(t, conn, item.name, uint32(offset), item.source[offset:end])

		sendMsg(t, conn, &model.Msg1212{
			Header:   newAlarmHeader(0x1212, uint16(10+serial)),
			FileName: item.name,
			FileType: 0,
			FileSize: uint32(len(item.source)),
		})
		completed := receive9212(t, reader)
		require.Equal(t, uint8(0), completed.Result)

		finalPath := filepath.Join(srv.Assembler().Config().Dir, phone, "20260107", "ALARM-0002", item.name)
		onDisk, err := os.ReadFile(finalPath)
		require.NoError(t, err)
		require.Equal(t, fmt.Sprintf("%x", md5.Sum(item.source)), fmt.Sprintf("%x", md5.Sum(onDisk)))
	}
}
