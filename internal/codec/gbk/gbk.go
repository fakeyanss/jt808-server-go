package gbk

import (
	"bytes"
	"io"

	"github.com/rs/zerolog/log"
	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
)

// GBK 转 UTF-8
func GBK2UTF8(src []byte) ([]byte, error) {
	dst, err := io.ReadAll(transform.NewReader(bytes.NewBuffer(src), simplifiedchinese.GBK.NewDecoder()))
	if err != nil {
		log.Error().
			Bytes("src", src).
			Msg("Fail to transform gbk to utf8")
		return nil, err
	}
	return dst, nil
}

// UTF-8 转 GBK
func UTF82GBK(src []byte) ([]byte, error) {
	dst, err := io.ReadAll(transform.NewReader(bytes.NewBuffer(src), simplifiedchinese.GBK.NewEncoder()))
	if err != nil {
		log.Error().
			Err(err).
			Bytes("src", src).
			Msg("Fail to transform utf8 to gbk")
		return nil, err
	}
	return dst, nil
}
