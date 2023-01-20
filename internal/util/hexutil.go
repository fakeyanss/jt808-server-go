package util

import (
	"fmt"
	"strconv"
	"strings"
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
func Bcd2NumberStr(bcd []byte) string {
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
func NumberStr2bcd(number string) []byte {
	var rNumber = number
	for i := 0; i < 8-len(number); i++ {
		rNumber = "f" + rNumber
	}
	bcd := Hex2Byte(rNumber)
	return bcd
}

func Hex2Byte(str string) []byte {
	slen := len(str)
	bHex := make([]byte, len(str)/2)
	ii := 0
	for i := 0; i < len(str); i = i + 2 {
		if slen != 1 {
			ss := string(str[i]) + string(str[i+1])
			bt, _ := strconv.ParseInt(ss, 16, 32)
			bHex[ii] = byte(bt)
			ii = ii + 1
			slen = slen - 2
		}
	}
	return bHex
}
