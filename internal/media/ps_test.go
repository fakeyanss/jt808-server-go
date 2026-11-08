package media

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func testPSPackHeader() []byte {
	return []byte{
		0x00, 0x00, 0x01, 0xBA,
		0x44, 0x00, 0x04, 0x00, 0x04, 0x01, 0x00, 0x00, 0x03, 0xF0,
	}
}

func testVideoPES(es []byte, declaredLength int) []byte {
	pes := []byte{0x00, 0x00, 0x01, 0xE0, 0x00, 0x00, 0x80, 0x00, 0x00}
	if declaredLength < 0 {
		declaredLength = len(es) + 3
	}
	pes[4] = byte(declaredLength >> 8)
	pes[5] = byte(declaredLength)
	return append(pes, es...)
}

func TestExtractH264FromPS(t *testing.T) {
	es := []byte{0x00, 0x00, 0x00, 0x01, 0x67, 0x42, 0x00, 0x1E, 0x00, 0x00, 0x00, 0x01, 0x65, 0x88}
	systemHeader := append([]byte{0x00, 0x00, 0x01, 0xBB, 0x00, 0x0C}, make([]byte, 12)...)

	payload := append([]byte(nil), testPSPackHeader()...)
	payload = append(payload, systemHeader...)
	payload = append(payload, testVideoPES(es, -1)...)

	got, err := ExtractH264(payload)
	require.NoError(t, err)
	require.Equal(t, es, got)
}

func TestExtractH264SkipsAudioAndUnboundedPES(t *testing.T) {
	videoES := []byte{0x00, 0x00, 0x00, 0x01, 0x65, 0x01}
	audioPES := []byte{0x00, 0x00, 0x01, 0xC0, 0x00, 0x04, 0x80, 0x00, 0x00, 0x11}

	payload := append([]byte(nil), testPSPackHeader()...)
	payload = append(payload, testVideoPES(videoES, 0)...)
	payload = append(payload, audioPES...)

	got, err := ExtractH264(payload)
	require.NoError(t, err)
	require.Equal(t, videoES, got)
}

func TestExtractH264RawAnnexBPassthrough(t *testing.T) {
	es := []byte{0x00, 0x00, 0x00, 0x01, 0x09, 0xF0, 0x00, 0x00, 0x00, 0x01, 0x67}
	got, err := ExtractH264(es)
	require.NoError(t, err)
	require.Equal(t, es, got)

	threeByteStart := []byte{0x00, 0x00, 0x01, 0x65, 0x01}
	got, err = ExtractH264(threeByteStart)
	require.NoError(t, err)
	require.Equal(t, threeByteStart, got)
}

func TestExtractH264RejectsUnsupportedPayload(t *testing.T) {
	_, err := ExtractH264([]byte{0xde, 0xad, 0xbe, 0xef})
	require.Error(t, err)

	// PS 中只有音频 PES 时没有视频 ES。
	payload := append([]byte(nil), testPSPackHeader()...)
	payload = append(payload, []byte{0x00, 0x00, 0x01, 0xC0, 0x00, 0x04, 0x80, 0x00, 0x00, 0x11}...)
	_, err = ExtractH264(payload)
	require.Error(t, err)
}

func TestExtractH264TrimsPESPrefix(t *testing.T) {
	es := []byte{0x00, 0x00, 0x00, 0x01, 0x65, 0x01}
	withPrefix := append([]byte{0xAA, 0xBB, 0xCC, 0xDD}, es...)
	payload := append([]byte(nil), testPSPackHeader()...)
	payload = append(payload, testVideoPES(withPrefix, -1)...)

	got, err := ExtractH264(payload)
	require.NoError(t, err)
	require.Equal(t, es, got)
}

// testLengthPrefixed 构造“起始码 + 长度 + 内容”的 PS 容器，用于 PSM/padding/system header/audio PES。
func testLengthPrefixed(code byte, body []byte) []byte {
	h := []byte{0x00, 0x00, 0x01, code, 0x00, byte(len(body))}
	return append(h, body...)
}

// testPSPackHeaderWithStuffing 构造带 stuffing 字节的 pack header。
func testPSPackHeaderWithStuffing(stuffing int) []byte {
	pack := []byte{
		0x00, 0x00, 0x01, 0xBA,
		0x44, 0x00, 0x04, 0x00, 0x04, 0x01, 0x00, 0x00, 0x03, byte(0xF0 | (stuffing & 0x07)),
	}
	return append(pack, make([]byte, stuffing&0x07)...)
}

// PSM、padding、system header 与 pack stuffing 都需要被跳过，不能混入 H.264 输出。
func TestExtractH264SkipsPackStuffingPSMAndPadding(t *testing.T) {
	es := []byte{0x00, 0x00, 0x00, 0x01, 0x67, 0x42, 0x00, 0x1E, 0x00, 0x00, 0x00, 0x01, 0x65, 0x88}

	payload := testPSPackHeaderWithStuffing(3)
	payload = append(payload, testLengthPrefixed(psSystemHeaderCode, make([]byte, 12))...)
	payload = append(payload, testLengthPrefixed(psProgramStreamMap, make([]byte, 8))...)
	payload = append(payload, testLengthPrefixed(psPaddingCode, make([]byte, 4))...)
	payload = append(payload, testVideoPES(es, -1)...)

	got, err := ExtractH264(payload)
	require.NoError(t, err)
	require.Equal(t, es, got)
}

// 音频 PES 在视频 PES 之前时必须被完整跳过，不能混进视频 ES。
func TestExtractH264SkipsAudioPESBeforeVideo(t *testing.T) {
	videoES := []byte{0x00, 0x00, 0x00, 0x01, 0x65, 0x01}
	audioPayload := []byte{0x11, 0x22, 0x33}

	payload := append([]byte(nil), testPSPackHeader()...)
	payload = append(payload, testLengthPrefixed(pesAudioStreamMin, audioPayload)...)
	payload = append(payload, testVideoPES(videoES, -1)...)

	got, err := ExtractH264(payload)
	require.NoError(t, err)
	require.Equal(t, videoES, got)
}

// PES 长度为 0 时应向后扫描到下一个容器起始码，且不把后续音频数据带入 ES。
func TestExtractH264UnboundedPESStopsAtNextContainer(t *testing.T) {
	videoES := []byte{0x00, 0x00, 0x00, 0x01, 0x65, 0x02, 0x03}
	audioPayload := []byte{0xAA, 0xBB}

	payload := append([]byte(nil), testPSPackHeader()...)
	payload = append(payload, testVideoPES(videoES, 0)...)
	payload = append(payload, testLengthPrefixed(pesAudioStreamMin, audioPayload)...)

	got, err := ExtractH264(payload)
	require.NoError(t, err)
	require.Equal(t, videoES, got)
}

// 缺少 SPS/PPS 的裸 IDR 仍应原样提取，不 panic、不报错。
func TestExtractH264WithoutSPSPPSDoesNotPanic(t *testing.T) {
	idrOnly := []byte{0x00, 0x00, 0x00, 0x01, 0x65, 0x88, 0x84, 0x00, 0x33}
	payload := append([]byte(nil), testPSPackHeader()...)
	payload = append(payload, testVideoPES(idrOnly, -1)...)

	var got []byte
	require.NotPanics(t, func() {
		var err error
		got, err = ExtractH264(payload)
		require.NoError(t, err)
	})
	require.Equal(t, idrOnly, got)
}

// 声明长度超过实际数据时按实际数据截断，不 panic。
func TestExtractH264ClampsPESLengthBeyondPayload(t *testing.T) {
	es := []byte{0x00, 0x00, 0x00, 0x01, 0x41, 0x9A}
	payload := append([]byte(nil), testPSPackHeader()...)
	payload = append(payload, testVideoPES(es, len(es)+50)...)

	got, err := ExtractH264(payload)
	require.NoError(t, err)
	require.Equal(t, es, got)
}

// 各种截断/损坏的 PS 载荷必须返回错误而不是 panic。
func TestExtractH264RejectsTruncatedPSWithoutPanic(t *testing.T) {
	cases := map[string][]byte{
		"pack start code only": {0x00, 0x00, 0x01, 0xBA},
		"short pack header":    {0x00, 0x00, 0x01, 0xBA, 0x44, 0x00},
		"short video pes":      append(append([]byte(nil), testPSPackHeader()...), 0x00, 0x00, 0x01, 0xE0, 0x00),
		"psm declares too much": append(append([]byte(nil), testPSPackHeader()...),
			0x00, 0x00, 0x01, 0xBC, 0x00, 0x08),
	}
	for name, payload := range cases {
		t.Run(name, func(t *testing.T) {
			require.NotPanics(t, func() {
				out, err := ExtractH264(payload)
				if err != nil {
					require.Nil(t, out)
					return
				}
				require.Empty(t, out, "truncated PS payload must not produce a partial ES")
			})
		})
	}

	require.NotPanics(t, func() {
		_, err := ExtractH264(nil)
		require.Error(t, err)
	})
}

// findPSStart 只识别 >= 0xB0 的容器码，H.264 NAL 起始码不能被当作容器边界。
func TestFindPSStartSkipsNALStartCode(t *testing.T) {
	payload := []byte{0x00, 0x00, 0x01, 0x65, 0x00, 0x00, 0x01, 0xBA}
	require.Equal(t, 4, findPSStart(payload, 0))
	require.Equal(t, -1, findPSStart([]byte{0x00, 0x00, 0x01, 0x65}, 0))
}
