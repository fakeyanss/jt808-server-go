package model

import "github.com/pkg/errors"

var (
	ErrDecodeDeviceArgs = errors.New("Fail to decode device args")
)

type DeviceArgs struct {
	ArgCnt uint8      `json:"argCnt"` // 参数项个数
	Args   []*ArgData `json:"args"`   // 参数项列表
}

func (a *DeviceArgs) Decode(cnt uint8, pkt []byte) error {
	a.ArgCnt = cnt
	for i := 0; i < int(cnt); i++ {
		arg := &ArgData{}
		err := arg.Decode(pkt)
		if err != nil {
			return ErrDecodeDeviceArgs
		}

		a.Args = append(a.Args, arg)
	}
	return nil
}

func (a *DeviceArgs) Encode() (pkt []byte, err error) {
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
