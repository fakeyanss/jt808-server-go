package model

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
	"github.com/fakeyanss/jt808-server-go/internal/domain"
)

// TestMsg0104DecodesSubiaoParams 验证 0x0104 能把苏标复合参数解码为领域参数。
func TestMsg0104DecodesSubiaoParams(t *testing.T) {
	params := &domain.DeviceParams{Params: []*domain.ParamData{{
		ParamID:    0xF367,
		ParamValue: domain.SubiaoBSDParam{RearApproachAlarmTimeThreshold: 4, SideRearApproachAlarmTimeThreshold: 6},
	}}}
	encoded, err := params.Encode()
	require.NoError(t, err)

	body := hex.WriteWord(nil, 7)
	body = append(body, encoded...)

	msg := &Msg0104{}
	err = msg.Decode(&PacketData{Header: genMsgHeader(0x0104), Body: body})
	require.NoError(t, err)
	require.Equal(t, uint16(7), msg.AnswerSerialNumber)
	require.Len(t, msg.Parameters.Params, 1)
	require.Equal(t, domain.SubiaoBSDParam{RearApproachAlarmTimeThreshold: 4, SideRearApproachAlarmTimeThreshold: 6}, msg.Parameters.Params[0].ParamValue)
}
