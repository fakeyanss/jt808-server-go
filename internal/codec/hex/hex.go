package hex

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	GBK "github.com/fakeyanss/jt808-server-go/internal/codec/gbk"
)

const (
	doubeWordLen  = 4
	timeBCDLen    = 6
	timeBCDLayout = "060102150405"
	timeBCDFormat = "%02d%02d%02d%02d%02d%02d"
)

func Str2Byte(src string) []byte {
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

func Byte2Str(src []byte) string {
	return hex.EncodeToString(src)
}

// 十进制数 <- BCD 8421码
//
//	0 <- 0000
//	1 <- 0001
//	2 <- 0010
//	3 <- 0011
//	4 <- 0100
//	5 <- 0101
//	6 <- 0110
//	7 <- 0111
//	8 <- 1000
//	9 <- 1001
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

// 十进制数 -> BCD 8421码
//
//	0 -> 0000
//	1 -> 0001
//	2 -> 0010
//	3 -> 0011
//	4 -> 0100
//	5 -> 0101
//	6 -> 0110
//	7 -> 0111
//	8 -> 1000
//	9 -> 1001
func NumberStr2BCD(number string) []byte {
	var rNumber = number
	for i := 0; i < 8-len(number); i++ {
		rNumber = "f" + rNumber
	}
	bcd := Str2Byte(rNumber)
	return bcd
}

func ReadByte(pkt []byte, idx *int) uint8 {
	ans := pkt[*idx]
	*idx++
	return ans
}

func WriteByte(pkt []byte, num uint8) []byte {
	return append(pkt, num)
}

func ReadWord(pkt []byte, idx *int) uint16 {
	ans := binary.BigEndian.Uint16(pkt[*idx : *idx+2])
	*idx += 2
	return ans
}

func WriteWord(pkt []byte, num uint16) []byte {
	numPkt := make([]byte, 2)
	binary.BigEndian.PutUint16(numPkt, num)
	return append(pkt, numPkt...)
}

func ReadDoubleWord(pkt []byte, idx *int) uint32 {
	ans := binary.BigEndian.Uint32(pkt[*idx : *idx+4])
	*idx += doubeWordLen
	return ans
}

func WriteDoubleWord(pkt []byte, num uint32) []byte {
	numPkt := make([]byte, doubeWordLen)
	binary.BigEndian.PutUint32(numPkt, num)
	return append(pkt, numPkt...)
}

func ReadBytes(pkt []byte, idx *int, n int) []byte {
	ans := pkt[*idx : *idx+n]
	*idx += n
	return ans
}

func WriteBytes(pkt []byte, arr []byte) []byte {
	return append(pkt, arr...)
}

func ReadString(pkt []byte, idx *int, n int) string {
	return string(ReadBytes(pkt, idx, n))
}

func WriteString(pkt []byte, str string) []byte {
	arr := []byte(str)
	return WriteBytes(pkt, arr)
}

func ReadBCD(pkt []byte, idx *int, n int) string {
	ans := BCD2NumberStr(pkt[*idx : *idx+n])
	*idx += n
	return ans
}

func WriteBCD(pkt []byte, bcd string) []byte {
	return append(pkt, NumberStr2BCD(bcd)...)
}

func ReadGBK(pkt []byte, idx *int, n int) string {
	gbk, err := GBK.GBK2UTF8(pkt[*idx : *idx+n])
	*idx += n
	if err != nil {
		return ""
	}
	return string(gbk)
}

func WriteGBK(pkt []byte, str string) []byte {
	gbk, err := GBK.UTF82GBK([]byte(str))
	if err != nil {
		return []byte{}
	}
	return append(pkt, gbk...)
}

// 输入JT808协议定义的时间format, 转换为time.Time
func ReadTime(pkt []byte, idx *int) *time.Time {
	timeStr := ReadBCD(pkt, idx, timeBCDLen)
	timeIns := ParseTime(timeStr)
	return &timeIns
}

func WriteTime(pkt []byte, timeIns time.Time) []byte {
	return WriteBCD(pkt, FormatTime(timeIns))
}

func ParseTime(timeStr string) time.Time {
	timeIns, err := time.Parse(timeBCDLayout, timeStr)
	if err != nil {
		log.Warn().Msg("Fail to parse time str")
		timeIns = time.Now()
	}
	return timeIns
}

func FormatTime(timeIns time.Time) string {
	year := timeIns.Year()     // 年
	month := timeIns.Month()   // 月
	day := timeIns.Day()       // 日
	hour := timeIns.Hour()     // 小时
	minute := timeIns.Minute() // 分钟
	second := timeIns.Second() // 秒
	yearDivision := 100        // 年取后两位
	return fmt.Sprintf(timeBCDFormat, year%yearDivision, month, day, hour, minute, second)
}
