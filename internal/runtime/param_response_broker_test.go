package runtime

import (
	"testing"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/stretchr/testify/require"
)

func TestParamResponseBrokerCorrelatesPhoneAndPlatformSerial(t *testing.T) {
	broker := newMemoryParamResponseBroker()
	ch, cancel, err := broker.Register("18800000001", 42)
	require.NoError(t, err)
	defer cancel()

	require.False(t, broker.Deliver(&model.Msg0104{Header: &model.MsgHeader{PhoneNumber: "18800000002"}, AnswerSerialNumber: 42}))
	require.False(t, broker.Deliver(&model.Msg0104{Header: &model.MsgHeader{PhoneNumber: "18800000001"}, AnswerSerialNumber: 41}))

	response := &model.Msg0104{Header: &model.MsgHeader{PhoneNumber: "18800000001"}, AnswerSerialNumber: 42}
	require.True(t, broker.Deliver(response))
	select {
	case got := <-ch:
		require.Same(t, response, got)
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for matching parameter response")
	}
	require.False(t, broker.Deliver(response), "a delivered response must not be delivered twice")
}
