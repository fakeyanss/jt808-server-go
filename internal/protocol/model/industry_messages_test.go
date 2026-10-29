package model

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func roundTrip(t *testing.T, msg JT808Msg, target JT808Msg) *PacketData {
	t.Helper()
	encoded, err := msg.Encode()
	require.NoError(t, err)
	bodyLength := int(msg.GetHeader().Attr.BodyLength)
	pkt := &PacketData{Header: msg.GetHeader(), Body: encoded[len(encoded)-bodyLength:]}
	require.NoError(t, target.Decode(pkt))
	return pkt
}

func TestDriverIdentityMessageRoundTrip(t *testing.T) {
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

func TestCANAndRecorderMessagesRoundTrip(t *testing.T) {
	can := &Msg0705{Header: genMsgHeader(0x0705), FirstTime: "1234560100", Items: []CANDataItem{{CANID: 0x60000123, Data: [8]byte{1, 2, 3}}}}
	decodedCAN := &Msg0705{}
	roundTrip(t, can, decodedCAN)
	require.Equal(t, can.Items, decodedCAN.Items)
	require.Equal(t, can.FirstTime, decodedCAN.FirstTime)
	command := &MsgRecordCommand{Header: genMsgHeader(0x8700), Command: 3, Data: []byte{0x11, 0x22}}
	decodedCommand := &MsgRecordCommand{}
	roundTrip(t, command, decodedCommand)
	require.Equal(t, command.Command, decodedCommand.Command)
	require.Equal(t, command.Data, decodedCommand.Data)
	response := &Msg0700{Header: genMsgHeader(0x0700), AnswerSerial: 7, Command: 3, Data: []byte{4, 5}}
	decodedResponse := &Msg0700{}
	roundTrip(t, response, decodedResponse)
	require.Equal(t, response.AnswerSerial, decodedResponse.AnswerSerial)
	require.Equal(t, response.Data, decodedResponse.Data)
}

func TestMultimediaUploadAndAckRoundTrip(t *testing.T) {
	upload := &Msg0801{Header: genMsgHeader(0x0801), MultiMediaID: 91, MultiMediaType: 0, MultiMediaContainer: 0, EventID: 1, LogicChannelID: 2, GeoAlarmBody: make([]byte, 28), FragmentData: []byte{0xff, 0xd8, 0xff}}
	decoded := &Msg0801{}
	roundTrip(t, upload, decoded)
	require.Equal(t, upload.MultiMediaID, decoded.MultiMediaID)
	require.Equal(t, upload.GeoAlarmBody, decoded.GeoAlarmBody)
	require.Equal(t, upload.FragmentData, decoded.FragmentData)
	ack := &Msg8800{Header: genMsgHeader(0x8800), MultiMediaID: 91, RetransmitIDs: []uint16{2, 9}}
	decodedAck := &Msg8800{}
	roundTrip(t, ack, decodedAck)
	require.Equal(t, ack.RetransmitIDs, decodedAck.RetransmitIDs)
}

func TestRegionRouteQueryRoundTrip(t *testing.T) {
	query := &Msg8608{Header: genMsgHeader(0x8608), QueryType: 4, IDs: []uint32{10, 20}}
	decoded := &Msg8608{}
	roundTrip(t, query, decoded)
	require.Equal(t, query.QueryType, decoded.QueryType)
	require.Equal(t, query.IDs, decoded.IDs)
	response := &Msg0608{Header: genMsgHeader(0x0608), QueryType: 4}
	decodedResponse := &Msg0608{}
	roundTrip(t, response, decodedResponse)
	require.Zero(t, decodedResponse.Count)
}

func TestRegionSetCameraCaptureAndMultimediaSearchRoundTrip(t *testing.T) {
	set := &MsgRegionRouteSet{
		Header:      genMsgHeader(0x8600),
		SettingAttr: 1,
		Circles: []CircleArea{{
			ID: 1, Attr: AreaAttrTime | AreaAttrSpeed,
			Latitude: 31000000, Longitude: 121000000, Radius: 500,
			StartTime: "261028000000", EndTime: "261028235959",
			MaxSpeed: 600, Duration: 10, Name: "测试圆",
		}},
	}
	decodedSet := &MsgRegionRouteSet{}
	roundTrip(t, set, decodedSet)
	require.Equal(t, set.SettingAttr, decodedSet.SettingAttr)
	require.Equal(t, set.Circles[0].ID, decodedSet.Circles[0].ID)
	require.Equal(t, set.Circles[0].Name, decodedSet.Circles[0].Name)
	require.Equal(t, set.Circles[0].StartTime, decodedSet.Circles[0].StartTime)
	empty := &MsgRegionRouteSet{Header: genMsgHeader(0x8600), SettingAttr: 1}
	_, err := empty.Encode()
	require.Error(t, err)

	capture := &Msg8801{Header: genMsgHeader(0x8801), ChannelID: 1, Command: 3, Interval: 2, Save: 1, Resolution: 2, Quality: 5, Brightness: 10, Contrast: 20, Saturation: 30, Chroma: 40}
	decodedCapture := &Msg8801{}
	roundTrip(t, capture, decodedCapture)
	require.Equal(t, capture.Command, decodedCapture.Command)
	require.Equal(t, capture.Chroma, decodedCapture.Chroma)
	search := &Msg8802{Header: genMsgHeader(0x8802), MediaType: 2, ChannelID: 1, EventID: 3, StartTime: "261028000000", EndTime: "261028235959"}
	decodedSearch := &Msg8802{}
	roundTrip(t, search, decodedSearch)
	require.Equal(t, search.StartTime, decodedSearch.StartTime)
	require.Equal(t, search.EndTime, decodedSearch.EndTime)
	result := &Msg0802{Header: genMsgHeader(0x0802), AnswerSerial: 7, Items: []MediaSearchItem{{MediaID: 10, MediaType: 2, ChannelID: 1, EventID: 3, Location: Msg0200{Latitude: 31000000, Longitude: 121000000, Time: search.StartTime}}}}
	decodedResult := &Msg0802{}
	roundTrip(t, result, decodedResult)
	require.Equal(t, result.Items[0].MediaID, decodedResult.Items[0].MediaID)
	require.Equal(t, result.Items[0].Location.Latitude, decodedResult.Items[0].Location.Latitude)
}

func TestTypedMediaCommandRoundTrips(t *testing.T) {
	cases := []struct {
		in  JT808Msg
		out JT808Msg
	}{
		{&Msg8803{Header: genMsgHeader(0x8803), MediaType: 1, ChannelID: 2, EventID: 3, StartTime: "261028000000", EndTime: "261028235959", DeleteFlag: 1}, &Msg8803{}},
		{&Msg8804{Header: genMsgHeader(0x8804), Command: 1, Duration: 30, Save: 1, SampleRate: 2}, &Msg8804{}},
		{&Msg8805{Header: genMsgHeader(0x8805), MediaID: 11, DeleteFlag: 1}, &Msg8805{}},
	}
	for _, tc := range cases {
		roundTrip(t, tc.in, tc.out)
	}
}
