package model

import (
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
)

// readOptionalTime reads a 6-byte BCD time and returns nil when all bytes are
// zero, which the JT808 standard treats as "no time constraint".
func readOptionalTime(pkt []byte, idx *int) *time.Time {
	if *idx+6 > len(pkt) {
		return nil
	}
	allZero := true
	for _, b := range pkt[*idx : *idx+6] {
		if b != 0 {
			allZero = false
			break
		}
	}
	if allZero {
		*idx += 6
		return nil
	}
	return hex.ReadTime(pkt, idx)
}
