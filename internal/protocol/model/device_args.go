package model

import (
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
)

var (
	ErrDecodeDeviceArgs = errors.New("Fail to decode device args")
)

type DeviceArgs struct {
	devicePhone string     `json:"-"`      // 关联device phone
	ArgCnt      uint8      `json:"argCnt"` // 参数项个数
	Args        []*ArgData `json:"args"`   // 参数项列表
}

func (a *DeviceArgs) Decode(phone string, cnt uint8, pkt []byte) error {
	a.devicePhone = phone
	a.ArgCnt = cnt
	for i := 0; i < int(cnt); i++ {
		arg := &ArgData{}
		err := arg.Decode(pkt)
		if err != nil {
			return err
		}

		a.Args = append(a.Args, arg)
	}
	return nil
}

func (a *DeviceArgs) Encode() (pkt []byte, err error) {
	pkt = hex.WriteByte(pkt, a.ArgCnt)
	for _, arg := range a.Args {
		argBytes, err := arg.Encode()
		if err != nil {
			// skip this err
			log.Error().Err(err).Str("device", a.devicePhone).Msg("Fail to encode device arg")
			continue
		}
		pkt = hex.WriteBytes(pkt, argBytes)
	}
	return nil, nil
}

type ArgData struct {
	ArgID    uint32 `json:"argID"`    // 参数ID
	ArgLen   uint8  `json:"argLen"`   // 参数长度
	ArgValue any    `json:"argValue"` // 参数值
}

func (a *ArgData) Decode(pkt []byte) error {
	return nil
}

func (a *ArgData) Encode() (pkt []byte, err error) {
	return nil, nil
}
