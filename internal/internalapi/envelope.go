package internalapi

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
)

// UplinkEnvelope 是网关到 control 的上行 JSON 信封，字段与设计文档第 4 节一致。
type UplinkEnvelope struct {
	EventID             string                   `json:"eventId"`
	Event               string                   `json:"event,omitempty"`
	Reason              string                   `json:"reason,omitempty"`
	Phone               string                   `json:"phone"`
	MsgID               uint16                   `json:"msgId"`
	RawMsgID            uint16                   `json:"rawMsgId"`
	Serial              uint16                   `json:"serial"`
	OccurredAtMS        int64                    `json:"occurredAtMs"`
	GatewayID           string                   `json:"gatewayId"`
	InstanceID          string                   `json:"instanceId,omitempty"`
	SessionID           string                   `json:"sessionId"`
	Generation          uint64                   `json:"generation,omitempty"`
	Transport           string                   `json:"transport"`
	Reliability         string                   `json:"reliability"`
	ProtocolVersion     string                   `json:"protocolVersion,omitempty"`
	HeaderAttr          uint16                   `json:"headerAttr,omitempty"`
	FragmentReassembled bool                     `json:"fragmentReassembled"`
	FragmentTotal       uint16                   `json:"fragmentTotal,omitempty"`
	ReplyMsgID          uint16                   `json:"replyMsgId,omitempty"`
	ReplyResult         uint8                    `json:"replyResult,omitempty"`
	BodyBase64          string                   `json:"bodyBase64,omitempty"`
	Payload             json.RawMessage          `json:"payload,omitempty"`
	PayloadBase64       string                   `json:"payloadBase64,omitempty"`
	Meta                map[string]any           `json:"meta,omitempty"`
	Profile             *contracts.DeviceProfile `json:"profile,omitempty"`
	Reply               *contracts.TerminalReply `json:"reply,omitempty"`
}

// BuildUplinkEnvelope 把契约事件转换为线上 JSON 信封。
func BuildUplinkEnvelope(event contracts.UplinkEvent) (UplinkEnvelope, error) {
	envelope := UplinkEnvelope{
		EventID:             event.EventID,
		Event:               event.Event,
		Reason:              event.Reason,
		Phone:               event.Phone,
		MsgID:               event.MsgID,
		RawMsgID:            event.RawMsgID,
		Serial:              event.Serial,
		GatewayID:           event.GatewayID,
		InstanceID:          event.InstanceID,
		SessionID:           event.SessionID,
		Generation:          event.Generation,
		Transport:           event.Transport,
		Reliability:         string(event.Reliability),
		ProtocolVersion:     event.ProtocolVersion,
		HeaderAttr:          event.HeaderAttr,
		FragmentReassembled: event.FragmentReassembled,
		FragmentTotal:       event.FragmentTotal,
		ReplyMsgID:          event.ReplyMsgID,
		ReplyResult:         event.ReplyResult,
		Meta:                event.Meta,
		Profile:             event.Profile,
		Reply:               event.Reply,
	}
	if !event.OccurredAt.IsZero() {
		envelope.OccurredAtMS = event.OccurredAt.UnixMilli()
	}
	if len(event.Body) > 0 {
		envelope.BodyBase64 = base64.StdEncoding.EncodeToString(event.Body)
	}
	if event.Payload != nil {
		payload, err := json.Marshal(event.Payload)
		if err != nil {
			return UplinkEnvelope{}, fmt.Errorf("marshal uplink payload: %w", err)
		}
		envelope.Payload = payload
	}
	if len(event.PayloadRaw) > 0 {
		envelope.PayloadBase64 = base64.StdEncoding.EncodeToString(event.PayloadRaw)
	}
	return envelope, nil
}

// ToEvent 把 HTTP 信封还原成契约事件。
// Payload 保留为 json.RawMessage，由具体 consumer 决定如何解码。
func (envelope UplinkEnvelope) ToEvent() (contracts.UplinkEvent, error) {
	event := contracts.UplinkEvent{
		EventID:             envelope.EventID,
		Event:               envelope.Event,
		Reason:              envelope.Reason,
		Phone:               envelope.Phone,
		MsgID:               envelope.MsgID,
		RawMsgID:            envelope.RawMsgID,
		Serial:              envelope.Serial,
		GatewayID:           envelope.GatewayID,
		InstanceID:          envelope.InstanceID,
		SessionID:           envelope.SessionID,
		Generation:          envelope.Generation,
		Transport:           envelope.Transport,
		Reliability:         contracts.Reliability(envelope.Reliability),
		ProtocolVersion:     envelope.ProtocolVersion,
		HeaderAttr:          envelope.HeaderAttr,
		FragmentReassembled: envelope.FragmentReassembled,
		FragmentTotal:       envelope.FragmentTotal,
		ReplyMsgID:          envelope.ReplyMsgID,
		ReplyResult:         envelope.ReplyResult,
		Payload:             envelope.Payload,
		PayloadRaw:          nil,
		Meta:                envelope.Meta,
		Profile:             envelope.Profile,
		Reply:               envelope.Reply,
	}
	if envelope.RawMsgID == 0 {
		event.RawMsgID = envelope.MsgID
	}
	if envelope.OccurredAtMS > 0 {
		event.OccurredAt = time.UnixMilli(envelope.OccurredAtMS)
	}
	if envelope.BodyBase64 != "" {
		decoded, err := base64.StdEncoding.DecodeString(envelope.BodyBase64)
		if err != nil {
			return contracts.UplinkEvent{}, fmt.Errorf("bodyBase64 is not valid base64")
		}
		event.Body = decoded
	}
	if envelope.PayloadBase64 != "" {
		decoded, err := base64.StdEncoding.DecodeString(envelope.PayloadBase64)
		if err != nil {
			return contracts.UplinkEvent{}, fmt.Errorf("payloadBase64 is not valid base64")
		}
		event.PayloadRaw = decoded
	}
	return event, nil
}
