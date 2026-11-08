package media

import (
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// startTestServer 在随机端口启动媒体服务器，返回服务器与 UDP 目标地址。
func startTestServer(t *testing.T, dir string, maxConcurrent int) (*Server, string) {
	t.Helper()
	server := NewServer(Config{Dir: dir, MaxConcurrent: maxConcurrent})
	require.NoError(t, server.Listen("127.0.0.1:0"))
	go server.Start()
	return server, server.Addr()
}

// 落盘文件按手机号/通道分目录，文件名为纳秒时间戳 + .h264。
func TestFileSinkDirectoryAndFileName(t *testing.T) {
	dir := t.TempDir()
	sink, err := newFileSink(dir, "188765430101", 3)
	require.NoError(t, err)

	wantDir := filepath.Join(dir, "188765430101", "channel_3")
	require.Equal(t, wantDir, filepath.Dir(sink.Path()))
	require.Regexp(t, `^\d+\.h264$`, filepath.Base(sink.Path()))

	es := []byte{0x00, 0x00, 0x00, 0x01, 0x65, 0x88}
	require.NoError(t, sink.Write(es))
	require.NoError(t, sink.Close())

	content, err := os.ReadFile(sink.Path())
	require.NoError(t, err)
	require.Equal(t, es, content)
}

func TestNewFileSinkRejectsNonNumericPhone(t *testing.T) {
	for _, phone := range []string{"", "18876543010a", "京A12345", "-188765430101"} {
		_, err := newFileSink(t.TempDir(), phone, 1)
		require.Error(t, err, "phone %q must be rejected", phone)
	}
}

// 并发上限按“手机号+通道”计流，超限时直接拒绝新流，但已有流仍复用同一状态。
func TestServerStreamForEnforcesConcurrencyLimit(t *testing.T) {
	server := NewServer(Config{Dir: t.TempDir(), MaxConcurrent: 1})
	t.Cleanup(server.Stop)

	first := &Packet{Phone: "188765430111", LogicChannel: 1}
	st := server.streamFor(first)
	require.NotNil(t, st)
	require.Same(t, st, server.streamFor(first), "existing stream must be reused")

	require.Nil(t, server.streamFor(&Packet{Phone: "188765430112", LogicChannel: 1}), "second phone must be rejected")
	require.Nil(t, server.streamFor(&Packet{Phone: "188765430111", LogicChannel: 2}), "second channel of same phone must be rejected")

	server.mu.Lock()
	defer server.mu.Unlock()
	require.Len(t, server.streams, 1)
}

// 空闲回收必须关闭文件句柄，但已写内容保留在磁盘上。
func TestServerIdleEvictionClosesFileHandle(t *testing.T) {
	dir := t.TempDir()
	server := NewServer(Config{Dir: dir, MaxConcurrent: 1, IdleTimeout: 10 * time.Minute})
	t.Cleanup(server.Stop)

	pkt := &Packet{Phone: "188765430121", LogicChannel: 1, DataType: DataTypeIFrame, SubpackageType: SubpackageAtomic}
	st := server.streamFor(pkt)
	require.NotNil(t, st)

	es := []byte{0x00, 0x00, 0x00, 0x01, 0x65, 0x01}
	require.NoError(t, st.sink.Write(es))
	path := st.sink.Path()

	server.mu.Lock()
	st.lastSeen = time.Now().Add(-11 * time.Minute)
	server.mu.Unlock()
	server.evictIdle(time.Now())

	require.Empty(t, server.streams)
	require.Error(t, st.sink.Write([]byte{0xFF}), "evicted stream must release its file handle")

	content, err := os.ReadFile(path)
	require.NoError(t, err)
	require.Equal(t, es, content)
}

// 仍活跃的流不能被空闲回收误杀。
func TestServerIdleEvictionKeepsActiveStream(t *testing.T) {
	server := NewServer(Config{Dir: t.TempDir(), MaxConcurrent: 1, IdleTimeout: 10 * time.Minute})
	t.Cleanup(server.Stop)

	pkt := &Packet{Phone: "188765430131", LogicChannel: 1, DataType: DataTypeIFrame}
	require.NotNil(t, server.streamFor(pkt))
	server.evictIdle(time.Now())
	require.Len(t, server.streams, 1)
}

func sendUDP(t *testing.T, addr string, packets ...[]byte) {
	t.Helper()
	conn, err := net.Dial("udp", addr)
	require.NoError(t, err)
	defer conn.Close()
	for _, pkt := range packets {
		_, err = conn.Write(pkt)
		require.NoError(t, err)
	}
}

func listH264Files(t *testing.T, dir string) []string {
	t.Helper()
	var files []string
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.HasSuffix(info.Name(), ".h264") {
			files = append(files, path)
		}
		return nil
	})
	require.NoError(t, err)
	return files
}

// waitForMediaBytes 等待目录中 H.264 文件总字节数达到期望值。
func waitForMediaBytes(t *testing.T, dir string, want int64) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var total int64
		for _, path := range listH264Files(t, dir) {
			info, err := os.Stat(path)
			if err == nil {
				total += info.Size()
			}
		}
		if total >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("media output did not reach %d bytes within timeout", want)
}

// 发送 PS 封装的 I 帧后应落出 H.264 ES；同一通道的后续裸 Annex B 帧追加到同一文件。
func TestServerWritesH264FromPSAndRawFrames(t *testing.T) {
	dir := t.TempDir()
	server, addr := startTestServer(t, dir, 1)
	t.Cleanup(server.Stop)

	es := []byte{0x00, 0x00, 0x00, 0x01, 0x67, 0x42, 0x00, 0x1E, 0x00, 0x00, 0x00, 0x01, 0x65, 0x88}
	psPayload := append([]byte(nil), testPSPackHeader()...)
	psPayload = append(psPayload, testVideoPES(es, -1)...)

	iFrame := buildTestPacket(DataTypeIFrame, SubpackageAtomic, PayloadTypeH264, 1, "188765470001", 1, psPayload)
	pFrameES := []byte{0x00, 0x00, 0x00, 0x01, 0x41, 0x9A}
	pFrame := buildTestPacket(DataTypePFrame, SubpackageAtomic, PayloadTypeH264, 2, "188765470001", 1, pFrameES)

	sendUDP(t, addr, iFrame)
	waitForMediaBytes(t, dir, int64(len(es)))

	sendUDP(t, addr, pFrame)
	waitForMediaBytes(t, dir, int64(len(es)+len(pFrameES)))

	server.Stop()

	files := listH264Files(t, dir)
	require.Len(t, files, 1)
	content, err := os.ReadFile(files[0])
	require.NoError(t, err)
	require.Equal(t, append(append([]byte(nil), es...), pFrameES...), content)
	// 落盘内容必须是不含 PS pack 起始码的裸流。
	require.NotContains(t, string(content[:4]), "\x00\x00\x01\xba")
}

// 并发通道达到上限后，新通道的帧被丢弃，不创建文件。
func TestServerEnforcesConcurrentStreamLimit(t *testing.T) {
	dir := t.TempDir()
	server, addr := startTestServer(t, dir, 1)
	t.Cleanup(server.Stop)

	es := []byte{0x00, 0x00, 0x00, 0x01, 0x65, 0x01}
	first := buildTestPacket(DataTypeIFrame, SubpackageAtomic, PayloadTypeH264, 1, "188765470002", 1, es)
	second := buildTestPacket(DataTypeIFrame, SubpackageAtomic, PayloadTypeH264, 2, "188765470003", 1, es)

	sendUDP(t, addr, first)
	waitForMediaBytes(t, dir, int64(len(es)))
	sendUDP(t, addr, second)
	time.Sleep(100 * time.Millisecond)

	server.Stop()
	require.Len(t, listH264Files(t, dir), 1)
}

// 非 1078 包与 H.265 载荷不创建文件。
func TestServerIgnoresUnsupportedPackets(t *testing.T) {
	dir := t.TempDir()
	server, addr := startTestServer(t, dir, 1)
	t.Cleanup(server.Stop)

	sendUDP(t, addr, []byte{0xde, 0xad, 0xbe, 0xef})
	h265 := append([]byte(nil), esFrame()...)
	packet := buildTestPacket(DataTypeIFrame, SubpackageAtomic, 99, 1, "188765470004", 1, h265)
	sendUDP(t, addr, packet)
	time.Sleep(100 * time.Millisecond)

	server.Stop()
	require.Empty(t, listH264Files(t, dir))
}

func esFrame() []byte {
	return []byte{0x00, 0x00, 0x00, 0x01, 0x65, 0x01}
}

func TestPruneExpiredMediaFiles(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	old := filepath.Join(dir, "old.h264")
	newFile := filepath.Join(dir, "new.h264")
	running := filepath.Join(dir, "running.h264")
	for _, path := range []string{old, newFile, running} {
		require.NoError(t, os.WriteFile(path, []byte("data"), 0o644))
	}
	past := now.Add(-8 * 24 * time.Hour)
	require.NoError(t, os.Chtimes(old, past, past))
	require.NoError(t, os.Chtimes(running, past, past))

	server := NewServer(Config{Dir: dir, MaxConcurrent: 2, Retention: 7 * 24 * time.Hour})
	defer server.Stop()
	server.mu.Lock()
	server.streams["running"] = &streamState{sink: &fileSink{path: running}, lastSeen: now}
	server.mu.Unlock()

	server.pruneExpired(now)

	_, err := os.Stat(old)
	require.True(t, os.IsNotExist(err), "expired file must be pruned")
	_, err = os.Stat(newFile)
	require.NoError(t, err, "file within retention must be kept")
	_, err = os.Stat(running)
	require.NoError(t, err, "active stream file must be kept")
}

func TestPruneExpiredKeepsFilesWhenRetentionDisabled(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "old.h264")
	require.NoError(t, os.WriteFile(old, []byte("data"), 0o644))
	past := time.Now().Add(-8 * 24 * time.Hour)
	require.NoError(t, os.Chtimes(old, past, past))

	server := NewServer(Config{Dir: dir})
	defer server.Stop()
	server.pruneExpired(time.Now())

	_, err := os.Stat(old)
	require.NoError(t, err)
}
