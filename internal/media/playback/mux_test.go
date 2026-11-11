package playback

import (
	"bytes"
	"testing"
)

// 真实终端常见的 Baseline SPS/PPS（Annex B，含 4 字节起始码）。
var (
	testSPS = []byte{0x00, 0x00, 0x00, 0x01, 0x67, 0x42, 0xc0, 0x1e, 0xda, 0x02, 0x80, 0xf6, 0x80, 0x6d, 0x0a, 0x13, 0x50}
	testPPS = []byte{0x00, 0x00, 0x00, 0x01, 0x68, 0xce, 0x3c, 0x80}
)

func TestAVCDecoderConfigurationRecord(t *testing.T) {
	record, err := AVCDecoderConfigurationRecord(ParamSets{SPS: testSPS, PPS: testPPS})
	if err != nil {
		t.Fatalf("build record: %v", err)
	}
	if record[0] != 0x01 {
		t.Fatalf("configurationVersion = %#x, want 1", record[0])
	}
	if record[1] != 0x42 || record[2] != 0xc0 || record[3] != 0x1e {
		t.Fatalf("profile/compat/level = %#x/%#x/%#x, want 42/c0/1e", record[1], record[2], record[3])
	}
	if record[4] != 0xFF {
		t.Fatalf("lengthSizeMinusOne = %#x, want 0xFF", record[4])
	}
	spsLen := int(record[6])<<8 | int(record[7])
	if spsLen != len(testSPS)-4 {
		t.Fatalf("sps length = %d, want %d", spsLen, len(testSPS)-4)
	}
	if !bytes.Equal(record[8:8+spsLen], testSPS[4:]) {
		t.Fatalf("sps body mismatch")
	}
	ppsLen := int(record[8+spsLen+1])<<8 | int(record[8+spsLen+2])
	if ppsLen != len(testPPS)-4 {
		t.Fatalf("pps length = %d, want %d", ppsLen, len(testPPS)-4)
	}
}

func TestVideoTagBodyConvertsAnnexBToAVCC(t *testing.T) {
	frame := append(append([]byte{}, testSPS...), testPPS...)
	frame = append(frame, 0x00, 0x00, 0x00, 0x01, 0x65, 0x88, 0x84, 0x00)
	body, err := VideoTagBody(frame, true)
	if err != nil {
		t.Fatalf("video tag body: %v", err)
	}
	if body[0] != 0x17 || body[1] != 0x01 {
		t.Fatalf("frame header = %#x %#x, want 0x17 0x01", body[0], body[1])
	}
	// SPS/PPS 不应出现在 NALU 负载中，只剩 IDR。
	if len(body) != 5+4+4 {
		t.Fatalf("body length = %d, want %d", len(body), 5+4+4)
	}
	if !bytes.Equal(body[5:9], []byte{0x00, 0x00, 0x00, 0x04}) {
		t.Fatalf("nal length prefix = %x, want 00000004", body[5:9])
	}
}

func TestParseADTSBuildsAudioSpecificConfig(t *testing.T) {
	// AAC LC, 44.1kHz(index 4), stereo(2), 负载 8 字节，总长 15。
	frame := []byte{0xFF, 0xF1, 0x50, 0x80, 0x01, 0xFF, 0xFC,
		0x21, 0x10, 0x05, 0x60, 0x7A, 0x3C, 0x00, 0x00}
	raw, config, ok := ParseADTS(frame)
	if !ok {
		t.Fatal("ParseADTS rejected a valid ADTS frame")
	}
	if len(raw) != 8 {
		t.Fatalf("raw length = %d, want 8", len(raw))
	}
	if !bytes.Equal(config, []byte{0x12, 0x10}) {
		t.Fatalf("AudioSpecificConfig = %x, want 1210", config)
	}
}
