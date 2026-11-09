package media

import (
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// 粘包与半包：任意切分点都应还原出两个完整包。
func TestFrameScannerCoalescedAndSplit(t *testing.T) {
	phone := "188765480001"
	first := buildTestPacket(DataTypeIFrame, SubpackageAtomic, PayloadTypeH264, 1, phone, 1, []byte("first"))
	second := buildTestPacket(DataTypePFrame, SubpackageAtomic, PayloadTypeH264, 2, phone, 1, []byte("second"))
	stream := append(append([]byte(nil), first...), second...)

	scanner := newFrameScanner(maxFrameBytes)
	frames := scanner.Push(stream)
	require.Len(t, frames, 2)
	require.Equal(t, first, frames[0])
	require.Equal(t, second, frames[1])

	for cut := 0; cut <= len(stream); cut++ {
		scanner := newFrameScanner(maxFrameBytes)
		frames := scanner.Push(stream[:cut])
		frames = append(frames, scanner.Push(stream[cut:])...)
		require.Len(t, frames, 2, "split at %d", cut)
		require.Equal(t, first, frames[0], "split at %d", cut)
		require.Equal(t, second, frames[1], "split at %d", cut)
	}
}

// magic 前的垃圾数据需要被丢弃并重新同步。
func TestFrameScannerSkipsGarbageAndResyncs(t *testing.T) {
	packet := buildTestPacket(DataTypeIFrame, SubpackageAtomic, PayloadTypeH264, 1, "188765480001", 1, []byte("x"))

	scanner := newFrameScanner(maxFrameBytes)
	require.Empty(t, scanner.Push([]byte{0xDE, 0xAD, 0xBE, 0xEF}))
	frames := scanner.Push(packet)
	require.Len(t, frames, 1)
	require.Equal(t, packet, frames[0])

	// 垃圾数据与包体恰好粘在一起也要能切出完整包。
	scanner = newFrameScanner(maxFrameBytes)
	frames = scanner.Push(append([]byte{0x01, 0x02, 0x03}, packet...))
	require.Len(t, frames, 1)
	require.Equal(t, packet, frames[0])
}

// 声明长度超过上限时丢弃当前字节并重同步，后续正常包仍可解析。
func TestFrameScannerResyncsAfterOversizedBody(t *testing.T) {
	oversized := buildTestPacket(DataTypeIFrame, SubpackageAtomic, PayloadTypeH264, 1, "188765480001", 1, make([]byte, 32))
	valid := buildTestPacket(DataTypePFrame, SubpackageAtomic, PayloadTypeH264, 2, "188765480001", 1, []byte("ok"))

	scanner := newFrameScanner(16)
	require.Empty(t, scanner.Push(oversized))
	frames := scanner.Push(valid)
	require.Len(t, frames, 1)
	require.Equal(t, valid, frames[0])
}

// 10 字节 SIM（20 位）的终端也要能正确分帧。
func TestFrameScannerSupportsLongSIM(t *testing.T) {
	phone := "00000000020232609559"
	packet := buildTestPacket(DataTypeAudio, SubpackageAtomic, PayloadTypeG711A, 1, phone, 1, []byte{0xD5, 0xD5})

	scanner := newFrameScanner(maxFrameBytes)
	frames := scanner.Push(packet)
	require.Len(t, frames, 1)

	parsed, err := ParsePacketWithSIM(frames[0], simLengthLong)
	require.NoError(t, err)
	require.Equal(t, phone, parsed.Phone)
	require.Equal(t, DataTypeAudio, parsed.DataType)
}

// 一条 TCP 连接可以合法承载多个通道，分帧器必须按包内通道号区分。
func TestFrameScannerMultiChannelSingleConnection(t *testing.T) {
	phone := "188765480001"
	ch1 := buildTestPacket(DataTypeIFrame, SubpackageAtomic, PayloadTypeH264, 1, phone, 1, []byte("channel-one"))
	ch2 := buildTestPacket(DataTypePFrame, SubpackageAtomic, PayloadTypeH264, 2, phone, 2, []byte("channel-two"))

	scanner := newFrameScanner(maxFrameBytes)
	frames := scanner.Push(append(append([]byte(nil), ch1...), ch2...))
	require.Len(t, frames, 2)

	first, err := ParsePacket(frames[0])
	require.NoError(t, err)
	require.Equal(t, uint8(1), first.LogicChannel)

	second, err := ParsePacket(frames[1])
	require.NoError(t, err)
	require.Equal(t, uint8(2), second.LogicChannel)
}

// 空闲回收按流判定：同一连接上另一个通道仍活跃时不能被误杀。
func TestEvictIdleKeepsActiveChannelOnSameConnection(t *testing.T) {
	server := NewServer(Config{Dir: t.TempDir(), IdleTimeout: time.Minute})
	t.Cleanup(server.Stop)

	source := packetSource{transport: transportTCP, connID: 7, remote: "127.0.0.1:12345"}
	active := server.streamFor(&Packet{Phone: "188765480002", LogicChannel: 1}, source)
	idle := server.streamFor(&Packet{Phone: "188765480002", LogicChannel: 2}, source)
	require.NotNil(t, active)
	require.NotNil(t, idle)

	idle.lastSeen.Store(time.Now().Add(-2 * time.Minute).UnixNano())
	server.evictIdle(time.Now())

	server.mu.Lock()
	_, activeOK := server.streams[streamKey("188765480002", 1, source)]
	_, idleOK := server.streams[streamKey("188765480002", 2, source)]
	server.mu.Unlock()
	require.True(t, activeOK, "active channel must survive")
	require.False(t, idleOK, "idle channel must be evicted")
}

// 单条 TCP 连接同时承载两个通道时，应落出两个独立目录与文件。
func TestServerTCPMultiChannelSingleConnection(t *testing.T) {
	dir := t.TempDir()
	server := NewServer(Config{Dir: dir, MaxTCPConns: 4})
	require.NoError(t, server.ListenTCP("127.0.0.1:0"))
	go server.Start()
	t.Cleanup(server.Stop)

	phone := "188765480003"
	es1 := []byte{0x00, 0x00, 0x00, 0x01, 0x67, 0x42, 0x00, 0x1E, 0x00, 0x00, 0x00, 0x01, 0x65, 0x88}
	es2 := []byte{0x00, 0x00, 0x00, 0x01, 0x67, 0x42, 0x00, 0x1E, 0x00, 0x00, 0x00, 0x01, 0x65, 0x99}
	packet1 := buildTestPacket(DataTypeIFrame, SubpackageAtomic, PayloadTypeH264, 1, phone, 1, es1)
	packet2 := buildTestPacket(DataTypeIFrame, SubpackageAtomic, PayloadTypeH264, 2, phone, 2, es2)

	conn, err := net.Dial("tcp", server.TCPAddr())
	require.NoError(t, err)
	defer conn.Close()

	stream := append(append([]byte(nil), packet1...), packet2...)
	// 故意拆成两段写入，覆盖半包路径。
	_, err = conn.Write(stream[:17])
	require.NoError(t, err)
	_, err = conn.Write(stream[17:])
	require.NoError(t, err)

	require.Eventually(t, func() bool {
		return hasFileWithExt(filepath.Join(dir, phone, "channel_1"), ".h264") &&
			hasFileWithExt(filepath.Join(dir, phone, "channel_2"), ".h264")
	}, 3*time.Second, 10*time.Millisecond)

	require.NoError(t, conn.Close())
	require.Eventually(t, func() bool { return server.Streams() == 0 }, 2*time.Second, 10*time.Millisecond)
}

// TCP 连接数上限独立于流数上限，超限连接应被立即关闭。
func TestServerLimitsTCPConnections(t *testing.T) {
	server := NewServer(Config{Dir: t.TempDir(), MaxTCPConns: 1})
	require.NoError(t, server.ListenTCP("127.0.0.1:0"))
	go server.Start()
	t.Cleanup(server.Stop)

	first, err := net.Dial("tcp", server.TCPAddr())
	require.NoError(t, err)
	defer first.Close()
	require.Eventually(t, func() bool {
		server.mu.Lock()
		defer server.mu.Unlock()
		return len(server.conns) == 1
	}, 2*time.Second, 10*time.Millisecond)

	second, err := net.Dial("tcp", server.TCPAddr())
	require.NoError(t, err)
	defer second.Close()
	_ = second.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, err = second.Read(make([]byte, 1))
	require.Error(t, err, "connection over the limit must be closed by server")
}

func hasFileWithExt(dir, ext string) bool {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ext {
			return true
		}
	}
	return false
}
