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
