package protocol

import (
	"context"
	"testing"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/stretchr/testify/require"
)

func TestTerminalControlProducesGenericAcknowledgement(t *testing.T) {
	header := &model.MsgHeader{MsgID: 0x8105, SerialNumber: 17, PhoneNumber: "188765432109", Attr: &model.MsgBodyAttr{}}
	result, err := NewJT808MsgProcessor().Process(context.Background(), &model.PacketData{Header: header, Body: []byte{2, 'h', 'o', 's', 't'}})
	require.NoError(t, err)
	require.IsType(t, &model.Msg0001{}, result.Outgoing)
	ack := result.Outgoing.(*model.Msg0001)
	require.Equal(t, uint16(17), ack.AnswerSerialNumber)
	require.Equal(t, uint16(0x8105), ack.AnswerMessageID)
	require.Equal(t, uint8(0), ack.Result)
}
