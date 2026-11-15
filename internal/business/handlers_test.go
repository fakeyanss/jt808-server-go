package business

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	codechex "github.com/fakeyanss/jt808-server-go/internal/codec/hex"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository/database"
)

// 0x0802 是 0x8802 检索命令的应答，必须按 (phone, answerSerial) 落库并可查询。
func TestProcessMsg0802PersistsSearchResults(t *testing.T) {
	const phone = "188765432802"
	ctx := context.Background()
	msg := &model.Msg0802{
		Header:       &model.MsgHeader{MsgID: 0x0802, PhoneNumber: phone, Attr: &model.MsgBodyAttr{}},
		AnswerSerial: 21,
		Items: []model.MediaSearchItem{{
			MediaID: 9001, MediaType: 0, ChannelID: 1, EventID: 2,
			Location: model.Msg0200{AlarmSign: 4, Latitude: 31000000, Time: "261028103000"},
		}},
	}
	p := New(Options{Repositories: database.Default().Repositories})
	require.NoError(t, p.processMsg0802(ctx, &model.ProcessData{Incoming: msg}))

	got, err := database.Default().MediaSearches.List(ctx, phone, 10)
	require.NoError(t, err)
	require.Len(t, got, 1)
	require.Equal(t, uint16(21), got[0].AnswerSerial)
	require.Equal(t, uint32(9001), got[0].MediaID)
	require.Equal(t, uint32(31000000), got[0].Latitude)
	require.True(t, got[0].LocationTime.Equal(time.Date(2026, 10, 28, 10, 30, 0, 0, codechex.ProtocolTimeZone)))

	// 终端重传同一条应答但结果变空时，旧结果必须被覆盖清空。
	msg.Items = nil
	require.NoError(t, p.processMsg0802(ctx, &model.ProcessData{Incoming: msg}))
	got, err = database.Default().MediaSearches.List(ctx, phone, 10)
	require.NoError(t, err)
	require.Empty(t, got)
}

func TestParseMediaSearchLocationTime(t *testing.T) {
	require.True(t, parseMediaSearchLocationTime("261028103000").Equal(
		time.Date(2026, 10, 28, 10, 30, 0, 0, codechex.ProtocolTimeZone)))
	require.True(t, parseMediaSearchLocationTime("26102810300").IsZero())
	require.True(t, parseMediaSearchLocationTime("261340103000").IsZero())
}
