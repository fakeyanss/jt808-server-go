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
			log.Warn().Err(err).Str("src", src).Msg("Source str invalid, will ignore extra byte")
		} else {
			log.Error().Err(err).Msg("Fail to transform hex str to byte array")
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
func bcd2NumberStr(bcd []byte) string {
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
func numberStr2BCD(number string) []byte {
	var rNumber = number
	for i := 0; i < 8-len(number); i++ {
		rNumber = "f" + rNumber
	}
	bcd := Str2Byte(rNumber)
	return bcd
}

// 对应JT808类型BYTE
func ReadByte(pkt []byte, idx *int) uint8 {
	ans := pkt[*idx]
	*idx++
	return ans
}

// 对应JT808类型BYTE
func WriteByte(pkt []byte, num uint8) []byte {
	return append(pkt, num)
}

func any2uint8(a any) uint8 {
	if b, ok := a.(float64); ok {
		return uint8(b)
	}
	return a.(uint8)
}

func WriteByteAny(pkt []byte, num any) []byte {
	return WriteByte(pkt, any2uint8(num))
}

// 对应JT808类型WORD
func ReadWord(pkt []byte, idx *int) uint16 {
	ans := binary.BigEndian.Uint16(pkt[*idx : *idx+2])
	*idx += 2
	return ans
}

// 对应JT808类型WORD
func WriteWord(pkt []byte, num uint16) []byte {
	numPkt := make([]byte, 2)
	binary.BigEndian.PutUint16(numPkt, num)
	return append(pkt, numPkt...)
}

func any2uint16(a any) uint16 {
	if b, ok := a.(float64); ok {
		return uint16(b)
	}
	return a.(uint16)
}

func WriteWordAny(pkt []byte, num any) []byte {
	return WriteWord(pkt, any2uint16(num))
}

// 对应JT808类型DWORD
func ReadDoubleWord(pkt []byte, idx *int) uint32 {
	ans := binary.BigEndian.Uint32(pkt[*idx : *idx+4])
	*idx += doubeWordLen
	return ans
}

// 对应JT808类型DWORD
func WriteDoubleWord(pkt []byte, num uint32) []byte {
	numPkt := make([]byte, doubeWordLen)
	binary.BigEndian.PutUint32(numPkt, num)
	return append(pkt, numPkt...)
}

func any2uint32(a any) uint32 {
	if b, ok := a.(float64); ok {
		return uint32(b)
	}
	return a.(uint32)
}

func WriteDoubleWordAny(pkt []byte, num any) []byte {
	return WriteDoubleWord(pkt, any2uint32(num))
}

// 对应JT808类型BYTE[n]
func ReadBytes(pkt []byte, idx *int, n int) []byte {
	ans := pkt[*idx : *idx+n]
	*idx += n
	return ans
}

// 对应JT808类型BYTE[n]
func WriteBytes(pkt, arr []byte) []byte {
	return append(pkt, arr...)
}

func any2bytes(a any) []byte {
	if b, ok := a.([]any); ok {
		ans := make([]byte, len(b))
		for i, v := range b {
			ans[i] = v.(byte)
		}
		return ans
	}
	return a.([]byte)
}

func WriteBytesAny(pkt []byte, arr any) []byte {
	return WriteBytes(pkt, any2bytes(arr))
}

// 对应JT808类型BYTE[n]
func ReadString(pkt []byte, idx *int, n int) string {
	return string(ReadBytes(pkt, idx, n))
}

// 对应JT808类型BYTE[n]
func WriteString(pkt []byte, str string) []byte {
	arr := []byte(str)
	return WriteBytes(pkt, arr)
}

// 对应JT808类型BCD[n]
func ReadBCD(pkt []byte, idx *int, n int) string {
	ans := bcd2NumberStr(pkt[*idx : *idx+n])
	*idx += n
	return ans
}

// 对应JT808类型BCD[n]
func WriteBCD(pkt []byte, bcd string) []byte {
	return append(pkt, numberStr2BCD(bcd)...)
}

// 对应JT808类型String
func ReadGBK(pkt []byte, idx *int, n int) string {
	gbk, err := GBK.GBK2UTF8(pkt[*idx : *idx+n])
	*idx += n
	if err != nil {
		return ""
	}
	return string(gbk)
}

// 对应JT808类型String
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

// 输入time.Time, 转换为JT808协议定义的时间format
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
