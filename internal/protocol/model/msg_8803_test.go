package model

import "testing"

func TestMsg8803RoundTrip(t *testing.T) {
	msg := &Msg8803{Header: genMsgHeader(0x8803), MediaType: 1, ChannelID: 2, EventID: 3, StartTime: "261028000000", EndTime: "261028235959", DeleteFlag: 1}
	roundTrip(t, msg, &Msg8803{})
}
