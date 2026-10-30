package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMsg0702RoundTrip(t *testing.T) {
	in := &Msg0702{Header: genMsgHeader(0x0702), State: 1, Time: "261028120000", ReadResult: 0, Name: "测试司机", QualificationCode: "123456", IssuingAuthority: "交通局", ValidUntil: "20301231", IdentityNumber: "110101199001010011"}
	out := &Msg0702{}
	roundTrip(t, in, out)
	require.Equal(t, in.Name, out.Name)
	require.Equal(t, in.QualificationCode, out.QualificationCode)
	require.Equal(t, in.IssuingAuthority, out.IssuingAuthority)
	require.Equal(t, in.IdentityNumber, out.IdentityNumber)
	clockOut := &Msg0702{}
	roundTrip(t, &Msg0702{Header: genMsgHeader(0x0702), State: 2, Time: "261028120000"}, clockOut)
	require.Equal(t, uint8(2), clockOut.State)
}
