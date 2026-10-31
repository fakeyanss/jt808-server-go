package model

import "testing"

func TestMsg8805RoundTrip(t *testing.T) {
	msg := &Msg8805{Header: genMsgHeader(0x8805), MediaID: 11, DeleteFlag: 1}
	roundTrip(t, msg, &Msg8805{})
}
