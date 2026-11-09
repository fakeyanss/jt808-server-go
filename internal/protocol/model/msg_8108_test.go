package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestMsg8108MakerIDByVersion 校验 MakerID 按协议版本使用 5/11 字节定长编码。
func TestMsg8108MakerIDByVersion(t *testing.T) {
	const maker = "12345678901"
	const version = "v1.2.3"
	data := []byte{0x01, 0x02, 0x03}

	tests := []struct {
		name      string
		version   VersionType
		makerLen  int
		wantMaker string
	}{
		{name: "2019", version: Version2019, makerLen: 11, wantMaker: maker},
		{name: "2013", version: Version2013, makerLen: 5, wantMaker: maker[:5]},
		{name: "2011", version: Version2011, makerLen: 5, wantMaker: maker[:5]},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			header := genMsgHeader(0x8108)
			header.Attr.VersionDesc = tt.version
			header.Attr.VersionSign = versionDecode(tt.version)
			msg := &Msg8108{Header: header, UpgradeType: 0, MakerID: maker, Version: version, Data: data}

			decoded := &Msg8108{}
			pkt := roundTrip(t, msg, decoded)

			// 消息体长度 = 升级类型(1) + MakerID(定长) + 版本号长度(1) + 版本号 + 数据长度(4) + 数据。
			require.Len(t, pkt.Body, 1+tt.makerLen+1+len(version)+4+len(data))
			require.Equal(t, tt.wantMaker, decoded.MakerID)
			require.Equal(t, uint8(0), decoded.UpgradeType)
			require.Equal(t, version, decoded.Version)
			require.Equal(t, data, decoded.Data)
		})
	}
}

// TestMsg8108RejectsShortBody 校验消息体不足定长字段时拒绝解码，避免越界读取。
func TestMsg8108RejectsShortBody(t *testing.T) {
	header2019 := genMsgHeader(0x8108)
	require.Error(t, (&Msg8108{}).Decode(&PacketData{Header: header2019, Body: make([]byte, 10)}))

	header2013 := genMsgHeader(0x8108)
	header2013.Attr.VersionDesc = Version2013
	require.Error(t, (&Msg8108{}).Decode(&PacketData{Header: header2013, Body: make([]byte, 10)}))
	require.NoError(t, (&Msg8108{}).Decode(&PacketData{Header: header2013, Body: make([]byte, 11)}))
}
