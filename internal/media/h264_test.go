package media

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// 测试用参数集：SPS(0x67)、PPS(0x68) 与 IDR(0x65)，均带 4 字节起始码。
var (
	testSPS = []byte{0x00, 0x00, 0x00, 0x01, 0x67, 0x42, 0x00, 0x1E}
	testPPS = []byte{0x00, 0x00, 0x00, 0x01, 0x68, 0xCE, 0x3C, 0x80}
	testIDR = []byte{0x00, 0x00, 0x00, 0x01, 0x65, 0x88, 0x84}
	testP   = []byte{0x00, 0x00, 0x00, 0x01, 0x41, 0x9A}
)

func concatBytes(parts ...[]byte) []byte {
	var out []byte
	for _, part := range parts {
		out = append(out, part...)
	}
	return out
}

// 帧内自带 SPS/PPS 时原样输出；后续裸 IDR 需要补齐缓存的参数集。
func TestParamSetCachePrependsBeforeIDR(t *testing.T) {
	var cache paramSetCache

	frame := concatBytes(testSPS, testPPS, testIDR)
	out, changed := cache.apply(frame)
	require.True(t, changed, "first parameter sets must be reported as changed")
	require.Equal(t, frame, out)

	out, changed = cache.apply(testIDR)
	require.False(t, changed)
	require.Equal(t, concatBytes(testSPS, testPPS, testIDR), out)

	// P 帧不前置参数集。
	out, changed = cache.apply(testP)
	require.False(t, changed)
	require.Equal(t, testP, out)
}

// 参数集变化时必须刷新缓存，后续 IDR 使用新参数集。
func TestParamSetCacheRefreshesOnChange(t *testing.T) {
	var cache paramSetCache
	_, changed := cache.apply(concatBytes(testSPS, testPPS, testIDR))
	require.True(t, changed)

	newSPS := []byte{0x00, 0x00, 0x00, 0x01, 0x67, 0x4D, 0x00, 0x1F}
	_, changed = cache.apply(concatBytes(newSPS, testPPS, testIDR))
	require.True(t, changed, "changed SPS must be detected")

	out, changed := cache.apply(testIDR)
	require.False(t, changed)
	require.Equal(t, concatBytes(newSPS, testPPS, testIDR), out)
}

// 只带 PPS 的 IDR 帧需要前置缓存中的 SPS，PPS 保留帧内版本。
func TestParamSetCachePartialInlinePPSOnly(t *testing.T) {
	var cache paramSetCache
	cache.apply(concatBytes(testSPS, testPPS, testIDR))

	newPPS := []byte{0x00, 0x00, 0x00, 0x01, 0x68, 0xEE, 0x3C, 0x80}
	out, changed := cache.apply(concatBytes(newPPS, testIDR))
	require.True(t, changed)
	require.Equal(t, concatBytes(testSPS, newPPS, testIDR), out)

	// 缓存已更新为新 PPS。
	out, _ = cache.apply(testIDR)
	require.Equal(t, concatBytes(testSPS, newPPS, testIDR), out)
}

// 只带 SPS 的 IDR 帧前置完整参数集，保证 SPS 在 PPS 之前。
func TestParamSetCachePartialInlineSPSOnly(t *testing.T) {
	var cache paramSetCache
	cache.apply(concatBytes(testSPS, testPPS, testIDR))

	newSPS := []byte{0x00, 0x00, 0x00, 0x01, 0x67, 0x4D, 0x00, 0x1F}
	out, changed := cache.apply(concatBytes(newSPS, testIDR))
	require.True(t, changed)
	require.Equal(t, concatBytes(newSPS, testPPS, newSPS, testIDR), out)
}

// 参数集帧本身不含 IDR 时不前置，只更新缓存。
func TestParamSetCacheUpdatesWithoutIDR(t *testing.T) {
	var cache paramSetCache
	out, changed := cache.apply(concatBytes(testSPS, testPPS))
	require.True(t, changed)
	require.Equal(t, concatBytes(testSPS, testPPS), out)

	out, _ = cache.apply(testIDR)
	require.Equal(t, concatBytes(testSPS, testPPS, testIDR), out)
}

// 3 字节起始码的输入也要被接受，缓存统一保存为 4 字节起始码。
func TestParamSetCacheHandlesThreeByteStartCode(t *testing.T) {
	sps3 := []byte{0x00, 0x00, 0x01, 0x67, 0x42, 0x00, 0x1E}
	pps3 := []byte{0x00, 0x00, 0x01, 0x68, 0xCE, 0x3C, 0x80}
	idr3 := []byte{0x00, 0x00, 0x01, 0x65, 0x88}

	var cache paramSetCache
	cache.apply(concatBytes(sps3, pps3, idr3))

	out, _ := cache.apply(idr3)
	require.Equal(t, concatBytes(testSPS, testPPS, idr3), out)
}

// 没有参数集的缓存不会凭空补数据，也不会 panic。
func TestParamSetCacheEmptyPrefix(t *testing.T) {
	var cache paramSetCache
	require.True(t, cache.empty())

	out, changed := cache.apply(testIDR)
	require.False(t, changed)
	require.Equal(t, testIDR, out)
}

// 服务级最后参数集：流结束后保存，新流按手机号+通道继承。
func TestServerRememberAndLoadLastParams(t *testing.T) {
	server := NewServer(Config{Dir: t.TempDir()})
	t.Cleanup(server.Stop)

	st := &streamState{phone: "188765490001", channel: 1}
	st.params.sps = append([]byte(nil), testSPS...)
	st.params.pps = append([]byte(nil), testPPS...)
	server.rememberParams(st)

	sps, pps := server.loadLastParams("188765490001", 1)
	require.Equal(t, testSPS, sps)
	require.Equal(t, testPPS, pps)

	// 其他通道不能继承。
	sps, pps = server.loadLastParams("188765490001", 2)
	require.Nil(t, sps)
	require.Nil(t, pps)

	// 新流建立时继承服务级参数集。
	stream := server.streamFor(&Packet{Phone: "188765490001", LogicChannel: 1}, testUDPSource("127.0.0.1:20001"))
	require.NotNil(t, stream)
	require.Equal(t, testSPS, stream.params.sps)
	require.Equal(t, testPPS, stream.params.pps)

	// 返回的是副本，调用方修改不会污染缓存。
	sps, _ = server.loadLastParams("188765490001", 1)
	sps[0] = 0xFF
	sps, _ = server.loadLastParams("188765490001", 1)
	require.Equal(t, testSPS, sps)
}

// 服务级参数集超过 TTL 后失效，并会被定期清理。
func TestServerLastParamsExpire(t *testing.T) {
	server := NewServer(Config{Dir: t.TempDir()})
	t.Cleanup(server.Stop)

	st := &streamState{phone: "188765490002", channel: 1}
	st.params.sps = append([]byte(nil), testSPS...)
	st.params.pps = append([]byte(nil), testPPS...)
	server.rememberParams(st)

	key := streamKeyBase("188765490002", 1)
	server.paramMu.Lock()
	expired := server.lastParams[key]
	expired.updatedAt = time.Now().Add(-paramSetTTL - time.Minute)
	server.lastParams[key] = expired
	server.paramMu.Unlock()

	sps, pps := server.loadLastParams("188765490002", 1)
	require.Nil(t, sps)
	require.Nil(t, pps)

	server.paramMu.Lock()
	_, ok := server.lastParams[key]
	server.paramMu.Unlock()
	require.False(t, ok, "expired entry must be removed on read")

	// 定期清理同样按 TTL 删除。
	server.rememberParams(st)
	server.pruneLastParams(time.Now().Add(paramSetTTL + time.Minute))
	server.paramMu.Lock()
	_, ok = server.lastParams[key]
	server.paramMu.Unlock()
	require.False(t, ok)
}
