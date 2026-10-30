package model

import "testing"

func TestMsg8804RoundTrip(t *testing.T) {
	msg := &Msg8804{Header: genMsgHeader(0x8804), Command: 1, Duration: 30, Save: 1, SampleRate: 2}
	roundTrip(t, msg, &Msg8804{})
}
