package hex

import (
	"encoding/hex"
	"errors"

	"github.com/rs/zerolog/log"
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
