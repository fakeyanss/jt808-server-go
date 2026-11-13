package protocol

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/fakeyanss/jt808-server-go/internal/codec/hex"
	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

// 每个注册的消息都必须在归属表里有明确归属。
func TestOwnershipCoversAllRegisteredMessages(t *testing.T) {
	registry := (&JT808MsgProcessor{}).initProcessOption()
	require.NotEmpty(t, registry)
	for msgID := range registry {
		_, ok := OwnershipOf(msgID)
		require.Truef(t, ok, "msg 0x%04X has no ownership entry", msgID)
	}
}

// 两边归属的消息必须有业务半边，且不得被误标记为纯网关消息。
func TestOwnershipBothSidesHaveBusinessHalf(t *testing.T) {
	for _, msgID := range []uint16{0x0100, 0x0102} {
		owner, ok := OwnershipOf(msgID)
		require.True(t, ok)
		require.Equal(t, OwnerBoth, owner)
		require.NotEmpty(t, BusinessHalves[msgID], "msg 0x%04X must declare a business half", msgID)
	}
}

// 事件信封必须保留原始报文元数据，供管控重建消息头与排查。
func TestBuildUplinkEventCarriesRawPacketMetadata(t *testing.T) {
	header := &model.MsgHeader{
		MsgID:        0x0200,
		PhoneNumber:  "188765499022",
		SerialNumber: 9,
		Attr:         &model.MsgBodyAttr{VersionDesc: model.Version2019},
	}
	body := locationTestBody()
	ctx := context.WithValue(context.Background(), model.PacketDecodeCtxKey{}, &model.PacketData{
		Header:       header,
		Body:         body,
		SegCompleted: true,
	})
	data := &model.ProcessData{Incoming: &model.Msg0200{Header: header, Time: "261112103000"}}

	event := buildUplinkEvent(ctx, 0x0200, data, contracts.ReliabilityBestEffort, "gw-test", "")
	require.Equal(t, uint16(0x0200), event.RawMsgID)
	require.Equal(t, "2019", event.ProtocolVersion)
	require.Equal(t, header.Attr.Encode(), event.HeaderAttr)
	require.True(t, event.FragmentReassembled)
	require.Equal(t, body, event.Body)
	require.Len(t, event.EventID, 16)
}

func locationTestBody() []byte {
	body := hex.WriteDoubleWord(nil, 0x00000002)
	body = hex.WriteDoubleWord(body, 0x00000001)
	body = hex.WriteDoubleWord(body, 30000000)
	body = hex.WriteDoubleWord(body, 120000000)
	body = hex.WriteWord(body, 100)
	body = hex.WriteWord(body, 600)
	body = hex.WriteWord(body, 90)
	return hex.WriteBCD(body, "261112103000")
}

func TestDecodeMessageBodyMatchesProcessorDecode(t *testing.T) {
	header := &model.MsgHeader{
		MsgID:           0x0200,
		PhoneNumber:     "188765499021",
		SerialNumber:    7,
		Attr:            &model.MsgBodyAttr{VersionDesc: model.Version2013},
		ProtocolVersion: 0,
	}
	body := hex.WriteDoubleWord(nil, 0x00000002)
	body = hex.WriteDoubleWord(body, 0x00000001)
	body = hex.WriteDoubleWord(body, 30000000)
	body = hex.WriteDoubleWord(body, 120000000)
	body = hex.WriteWord(body, 100)
	body = hex.WriteWord(body, 600)
	body = hex.WriteWord(body, 90)
	body = hex.WriteBCD(body, "261112103000")

	processor := NewJT808MsgProcessor(nil)
	data, err := processor.Decode(context.Background(), &model.PacketData{Header: header, Body: body})
	require.NoError(t, err)

	direct, err := DecodeMessageBody(0x0200, header, body)
	require.NoError(t, err)
	require.Equal(t, data.Incoming, direct)
}
