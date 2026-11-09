package media

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// testAudioPES 构造音频 PES（stream_id 0xC0）并附带 ES 数据。
func testAudioPES(es []byte, declaredLength int) []byte {
	pes := []byte{0x00, 0x00, 0x01, 0xC0, 0x00, 0x00, 0x80, 0x00, 0x00}
	if declaredLength < 0 {
		declaredLength = len(es) + 3
	}
	pes[4] = byte(declaredLength >> 8)
	pes[5] = byte(declaredLength)
	return append(pes, es...)
}

// 裸 G.711 载荷原样返回；即使采样数据里出现 00 00 01 BA 也不能当成 PS 容器。
func TestExtractAudioRawPassthrough(t *testing.T) {
	raw := []byte{0xD5, 0xD5, 0x00, 0x00, 0x01, 0xBA, 0x11, 0x22}
	got, err := ExtractAudio(raw)
	require.NoError(t, err)
	require.Equal(t, raw, got)

	// 空载荷不落盘。
	got, err = ExtractAudio(nil)
	require.NoError(t, err)
	require.Nil(t, got)
}

// PS 封装中只取音频 PES，视频 PES 必须被跳过。
func TestExtractAudioFromPSSkipsVideo(t *testing.T) {
	audio := []byte{0x01, 0x02, 0x03}
	video := []byte{0x00, 0x00, 0x00, 0x01, 0x65, 0x01}

	payload := append([]byte(nil), testPSPackHeader()...)
	payload = append(payload, testVideoPES(video, -1)...)
	payload = append(payload, testAudioPES(audio, -1)...)

	got, err := ExtractAudio(payload)
	require.NoError(t, err)
	require.Equal(t, audio, got)
}

// 长度为 0 的音频 PES 需要向后扫描到下一个容器起始码。
func TestExtractAudioUnboundedPES(t *testing.T) {
	audio := []byte{0x0A, 0x0B}
	payload := append([]byte(nil), testPSPackHeader()...)
	payload = append(payload, testAudioPES(audio, 0)...)
	payload = append(payload, []byte{0x00, 0x00, 0x01, 0xB9}...)

	got, err := ExtractAudio(payload)
	require.NoError(t, err)
	require.Equal(t, audio, got)
}

// 只有视频 PES 的 PS 载荷没有音频 ES，应返回损坏错误。
func TestExtractAudioRejectsPSWithoutAudio(t *testing.T) {
	payload := append([]byte(nil), testPSPackHeader()...)
	payload = append(payload, testVideoPES([]byte{0x00, 0x00, 0x00, 0x01, 0x65, 0x01}, -1)...)

	got, err := ExtractAudio(payload)
	require.ErrorIs(t, err, ErrCorruptPayload)
	require.Nil(t, got)
}

// listFilesWithExt 列出目录下指定扩展名的文件。
func listFilesWithExt(t *testing.T, dir, ext string) []string {
	t.Helper()
	var files []string
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		require.NoError(t, err)
	}
	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ext {
			files = append(files, filepath.Join(dir, entry.Name()))
		}
	}
	return files
}

// waitForFileWithExt 等待目录下出现指定扩展名文件。
func waitForFileWithExt(t *testing.T, dir, ext string) string {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if files := listFilesWithExt(t, dir, ext); len(files) > 0 {
			return files[0]
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no %s file appeared in %s within timeout", ext, dir)
	return ""
}

// 音频与视频共用同一个文件名主幹，分别落成独立文件。
func TestServerWritesAudioWithSameStemAsVideo(t *testing.T) {
	dir := t.TempDir()
	server, addr := startTestServer(t, dir, 2)
	t.Cleanup(server.Stop)

	const phone = "188765470011"
	videoES := []byte{0x00, 0x00, 0x00, 0x01, 0x67, 0x42, 0x00, 0x1E, 0x00, 0x00, 0x00, 0x01, 0x65, 0x88}
	video := buildTestPacket(DataTypeIFrame, SubpackageAtomic, PayloadTypeH264, 1, phone, 1, videoES)
	audioPayload := []byte{0xD5, 0xD5, 0x00, 0x00, 0x01, 0xBA, 0xD5}
	audio := buildTestPacket(DataTypeAudio, SubpackageAtomic, PayloadTypeG711A, 2, phone, 1, audioPayload)

	sendUDP(t, addr, video)
	waitForMediaBytes(t, dir, int64(len(videoES)))
	sendUDP(t, addr, audio)

	channelDir := filepath.Join(dir, phone, "channel_1")
	audioPath := waitForFileWithExt(t, channelDir, ".g711a")

	content, err := os.ReadFile(audioPath)
	require.NoError(t, err)
	require.Equal(t, audioPayload, content, "G.711 应当按裸流落盘")

	h264Files := listFilesWithExt(t, channelDir, ".h264")
	require.Len(t, h264Files, 1)
	require.Equal(t, strings.TrimSuffix(filepath.Base(h264Files[0]), ".h264"),
		strings.TrimSuffix(filepath.Base(audioPath), ".g711a"), "音视频文件名主幹必须一致")
}

// G.711U 落成 .g711u；AAC 必须带 ADTS 头才落盘。
func TestServerWritesG711UAndAAC(t *testing.T) {
	dir := t.TempDir()
	server, addr := startTestServer(t, dir, 3)
	t.Cleanup(server.Stop)

	const phone = "188765470012"
	ulaw := buildTestPacket(DataTypeAudio, SubpackageAtomic, PayloadTypeG711U, 1, phone, 1, []byte{0xAA, 0xBB})
	adts := []byte{0xFF, 0xF1, 0x50, 0x80, 0x01, 0x7F, 0xFC, 0x01, 0x02}
	aac := buildTestPacket(DataTypeAudio, SubpackageAtomic, PayloadTypeAAC, 2, phone, 1, adts)
	badAAC := buildTestPacket(DataTypeAudio, SubpackageAtomic, PayloadTypeAAC, 3, phone, 2, []byte{0x01, 0x02, 0x03})

	sendUDP(t, addr, ulaw)
	sendUDP(t, addr, aac)
	sendUDP(t, addr, badAAC)

	channelDir := filepath.Join(dir, phone, "channel_1")
	waitForFileWithExt(t, channelDir, ".g711u")
	waitForFileWithExt(t, channelDir, ".aac")
	time.Sleep(100 * time.Millisecond)

	// 无 ADTS 头的 AAC 通道不应产生文件。
	require.Empty(t, listFilesWithExt(t, filepath.Join(dir, phone, "channel_2"), ".aac"))
}

// 清理白名单必须覆盖音频文件扩展名。
func TestPruneExpiredRemovesAudioFiles(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	paths := []string{
		filepath.Join(dir, "a.h264"),
		filepath.Join(dir, "a.g711a"),
		filepath.Join(dir, "a.g711u"),
		filepath.Join(dir, "a.aac"),
	}
	for _, path := range paths {
		require.NoError(t, os.WriteFile(path, []byte("data"), 0o644))
		past := now.Add(-8 * 24 * time.Hour)
		require.NoError(t, os.Chtimes(path, past, past))
	}
	// 不属于白名单的文件不能被清理。
	other := filepath.Join(dir, "keep.txt")
	require.NoError(t, os.WriteFile(other, []byte("data"), 0o644))
	past := now.Add(-8 * 24 * time.Hour)
	require.NoError(t, os.Chtimes(other, past, past))

	server := NewServer(Config{Dir: dir, Retention: 7 * 24 * time.Hour})
	defer server.Stop()
	server.pruneExpired(now)

	for _, path := range paths {
		_, err := os.Stat(path)
		require.True(t, os.IsNotExist(err), "%s must be pruned", filepath.Base(path))
	}
	_, err := os.Stat(other)
	require.NoError(t, err)
}

// 正在写入的音频文件不能被清理。
func TestPruneExpiredKeepsActiveAudioSink(t *testing.T) {
	dir := t.TempDir()
	now := time.Now()

	path := filepath.Join(dir, "running.g711a")
	require.NoError(t, os.WriteFile(path, []byte("data"), 0o644))
	past := now.Add(-8 * 24 * time.Hour)
	require.NoError(t, os.Chtimes(path, past, past))

	server := NewServer(Config{Dir: dir, Retention: 7 * 24 * time.Hour})
	defer server.Stop()
	state := &streamState{audioSink: &fileSink{path: path}}
	state.touch()
	server.mu.Lock()
	server.streams["audio"] = state
	server.mu.Unlock()

	server.pruneExpired(now)

	_, err := os.Stat(path)
	require.NoError(t, err)
}
