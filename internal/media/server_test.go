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
