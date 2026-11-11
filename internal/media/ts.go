package media

import (
	"encoding/binary"
	"io"
)

const (
	tsPacketSize     = 188
	tsPIDPAT         = 0x0000
	tsPIDPMT         = 0x1000
	tsPIDVideo       = 0x0100
	tsStreamTypeH264 = 0x1B
)

// tsMuxer 把 Annex B H.264 封装为 MPEG-TS。
//
// 只做点播/归档所需的最小集合：PAT + PMT + 单路 H.264；每个关键帧带 PCR
// 与 random_access_indicator，并在切片起点重发 PAT/PMT，保证任意切片可独立播放。
type tsMuxer struct {
	w       io.Writer
	written int64
	ccPAT   byte
	ccPMT   byte
	ccVideo byte
}

func newTSMuxer(w io.Writer) *tsMuxer { return &tsMuxer{w: w} }

// Offset 返回已写入字节数，用于记录关键帧在切片内的偏移。
func (m *tsMuxer) Offset() int64 { return m.written }

// WriteTables 写 PAT 与 PMT。
func (m *tsMuxer) WriteTables() error {
	pat := make([]byte, 0, 16)
	pat = append(pat, 0x00, 0xB0, 0x0D, 0x00, 0x01, 0xC1, 0x00, 0x00)
	pat = append(pat, 0x00, 0x01, 0xF0, 0x00)
	pat = append(pat, mpegCRC32(pat)...)
	if err := m.writePSI(tsPIDPAT, &m.ccPAT, pat); err != nil {
		return err
	}

	pmt := make([]byte, 0, 24)
	pmt = append(pmt, 0x02, 0xB0, 0x12, 0x00, 0x01, 0xC1, 0x00, 0x00)
	pmt = append(pmt, 0xE1, 0x00, 0xF0, 0x00) // PCR_PID=0x100, program_info_length=0
	pmt = append(pmt, tsStreamTypeH264, 0xE1, 0x00, 0xF0, 0x00)
	pmt = append(pmt, mpegCRC32(pmt)...)
	return m.writePSI(tsPIDPMT, &m.ccPMT, pmt)
}

func (m *tsMuxer) writePSI(pid uint16, cc *byte, section []byte) error {
	payload := make([]byte, tsPacketSize)
	payload[0] = 0x00 // pointer_field
	copy(payload[1:], section)
	for i := 1 + len(section); i < len(payload); i++ {
		payload[i] = 0xFF
	}
	packet := m.packetHeader(true, pid, *cc)
	*cc = (*cc + 1) & 0x0F
	copy(packet[4:], payload)
	return m.write(packet)
}

// WriteFrame 把一个访问单元写成 PES/TS；key 为真时携带 PCR。
func (m *tsMuxer) WriteFrame(ptsMS uint64, key bool, annexB []byte) error {
	pts := ptsMS * 90
	pes := make([]byte, 0, 14+len(annexB))
	pes = append(pes, 0x00, 0x00, 0x01, 0xE0)
	pes = append(pes, 0x00, 0x00) // PES_packet_length=0（视频允许）
	pes = append(pes, 0x80, 0x80, 0x05)
	pes = append(pes, encodePTS(pts)...)
	pes = append(pes, annexB...)

	offset := 0
	first := true
	for offset < len(pes) {
		packet := m.packetHeader(first, tsPIDVideo, m.ccVideo)
		m.ccVideo = (m.ccVideo + 1) & 0x0F
		remaining := len(pes) - offset
		if first && key {
			// 首包带 7 字节 adaptation field：随机访问标志 + PCR。
			take := min(remaining, tsPacketSize-4-8)
			afLength := tsPacketSize - 5 - take
			packet[3] = 0x30
			packet[4] = byte(afLength)
			packet[5] = 0x50
			writePCR(packet[6:12], pts)
			for i := 12; i < 5+afLength; i++ {
				packet[i] = 0xFF
			}
			copy(packet[5+afLength:], pes[offset:offset+take])
			offset += take
		} else if remaining >= tsPacketSize-4 {
			packet[3] = 0x10
			copy(packet[4:], pes[offset:offset+tsPacketSize-4])
			offset += tsPacketSize - 4
		} else {
			// 最后一包用 adaptation field 填充，避免把 0xFF 当成 ES 数据。
			afLength := tsPacketSize - 5 - remaining
			packet[3] = 0x30
			packet[4] = byte(afLength)
			if afLength > 0 {
				packet[5] = 0x00
				for i := 6; i < 5+afLength; i++ {
					packet[i] = 0xFF
				}
			}
			copy(packet[5+afLength:], pes[offset:])
			offset = len(pes)
		}
		first = false
		if err := m.write(packet); err != nil {
			return err
		}
	}
	return nil
}

func (m *tsMuxer) packetHeader(pusi bool, pid uint16, cc byte) []byte {
	packet := make([]byte, tsPacketSize)
	packet[0] = 0x47
	packet[1] = byte(pid >> 8)
	if pusi {
		packet[1] |= 0x40
	}
	packet[2] = byte(pid)
	packet[3] = 0x10 | (cc & 0x0F)
	return packet
}

func (m *tsMuxer) write(packet []byte) error {
	n, err := m.w.Write(packet)
	m.written += int64(n)
	return err
}

func encodePTS(pts uint64) []byte {
	p := pts & ((1 << 33) - 1)
	return []byte{
		byte(0x21 | ((p >> 29) & 0x0E)),
		byte(p >> 22),
		byte(((p >> 14) & 0xFE) | 0x01),
		byte(p >> 7),
		byte(((p << 1) & 0xFE) | 0x01),
	}
}

func writePCR(dst []byte, pts uint64) {
	base := pts & ((1 << 33) - 1)
	dst[0] = byte(base >> 25)
	dst[1] = byte(base >> 17)
	dst[2] = byte(base >> 9)
	dst[3] = byte(base >> 1)
	dst[4] = byte((base&1)<<7) | 0x7E
	dst[5] = 0x00
}

// mpegCRC32 是 MPEG-2 PSI 使用的 CRC32（多项式 0x04C11DB7，不反射）。
func mpegCRC32(data []byte) []byte {
	crc := uint32(0xFFFFFFFF)
	for _, b := range data {
		crc ^= uint32(b) << 24
		for i := 0; i < 8; i++ {
			if crc&0x80000000 != 0 {
				crc = (crc << 1) ^ 0x04C11DB7
			} else {
				crc <<= 1
			}
		}
	}
	var out [4]byte
	binary.BigEndian.PutUint32(out[:], crc)
	return out[:]
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
