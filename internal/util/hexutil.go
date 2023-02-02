package util

import (
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"github.com/rs/zerolog/log"
)

// 十进制数 <-> BCD 8421码
//
//	0 <-> 0000
//	1 <-> 0001
//	2 <-> 0010
//	3 <-> 0011
//	4 <-> 0100
//	5 <-> 0101
//	6 <-> 0110
//	7 <-> 0111
//	8 <-> 1000
//	9 <-> 1001
func BCD2NumberStr(bcd []byte) string {
	var number string
	for _, i := range bcd {
		number += fmt.Sprintf("%02X", i)
	}
	pos := strings.LastIndex(number, "F")
	if pos == 8 {
		return "0"
	}
	return number[pos+1:]
}

// 十进制数 <-> BCD 8421码
//
//	0 <-> 0000
//	1 <-> 0001
//	2 <-> 0010
//	3 <-> 0011
//	4 <-> 0100
//	5 <-> 0101
//	6 <-> 0110
//	7 <-> 0111
//	8 <-> 1000
//	9 <-> 1001
func NumberStr2BCD(number string) []byte {
	var rNumber = number
	for i := 0; i < 8-len(number); i++ {
		rNumber = "f" + rNumber
	}
	bcd := Hex2Byte(rNumber)
	return bcd
}

func Hex2Byte(src string) []byte {
	dst, err := hex.DecodeString(src)
	if err != nil {
		if errors.Is(err, hex.ErrLength) {
			log.Warn().
				Err(err).
				Str("src", src).
				Msg("Source str invalid, will ignore extra byte")
		} else {
			log.Error().
				Err(err).
				Msg("Fail to transform hex str to byte array")
		}
	}
	return dst
}
