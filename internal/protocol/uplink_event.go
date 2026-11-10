package protocol

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/uplink"
)

// uplinkTimeLayout 与 0x0200 位置体的 BCD 时间一致。
const uplinkTimeLayout = "060102150405"

// uplinkZone 固定为东八区：协议里的位置时间均为北京时间。
var uplinkZone = time.FixedZone("CST", 8*3600)

// buildUplinkEvent 把已解码的上行数据转换为出口事件信封。
//
// EventID 只用手机号、消息 ID、流水号与消息体计算，终端重传同一条报文时稳定；
// 不掺入收包时间，避免重传被当成两条不同事件。
func buildUplinkEvent(
	ctx context.Context,
	msgID uint16,
	data *model.ProcessData,
	reliability contracts.Reliability,
	gatewayID string,
) contracts.UplinkEvent {
	in := data.Incoming
	header := in.GetHeader()
	phone := ""
	serial := uint16(0)
	packetCount := 1
	reassembled := true
	sessionID := ""
	transport := ""
	protocolVersion := ""
	headerAttr := uint16(0)
	body := []byte(nil)
	if header != nil {
		phone = header.PhoneNumber
		serial = header.SerialNumber
		protocolVersion = protocolVersionLabel(header.GetVersionDesc())
		if header.Attr != nil {
			headerAttr = header.Attr.Encode()
		}
		if header.Frag != nil {
			if header.Frag.Total > 0 {
				packetCount = int(header.Frag.Total)
			}
			reassembled = false
		}
	}
	if packet, ok := ctx.Value(model.PacketDecodeCtxKey{}).(*model.PacketData); ok && packet != nil {
		body = append([]byte(nil), packet.Body...)
		if packet.SegCompleted || packet.Header == nil || packet.Header.Frag == nil {
			reassembled = true
		}
	} else if media, ok := in.(*model.Msg0801); ok && media != nil {
		body = append([]byte(nil), media.FragmentData...)
	}
	if session, ok := ctx.Value(model.SessionCtxKey{}).(*model.Session); ok && session != nil {
		sessionID = session.ID
		transport = string(session.Transport)
	}

	occurredAt := uplinkOccurredAt(in)

	event := contracts.UplinkEvent{
		EventID:             uplinkEventID(phone, msgID, serial, body),
		Phone:               phone,
		MsgID:               msgID,
		RawMsgID:            msgID,
		Serial:              serial,
		OccurredAt:          occurredAt,
		GatewayID:           gatewayID,
		SessionID:           sessionID,
		Transport:           transport,
		Reliability:         reliability,
		ProtocolVersion:     protocolVersion,
		HeaderAttr:          headerAttr,
		FragmentReassembled: reassembled,
		FragmentTotal:       uint16(packetCount),
		Body:                body,
		Payload:             in,
		Meta:                map[string]any{"packetCount": packetCount},
	}
	if media, ok := in.(*model.Msg0801); ok && media != nil {
		// 0x0801 的图片本体单独走 payloadBase64，避免把大块字节塞进结构化载荷。
		event.Payload = map[string]any{
			"multiMediaId":        media.MultiMediaID,
			"multiMediaType":      media.MultiMediaType,
			"multiMediaContainer": media.MultiMediaContainer,
			"eventId":             media.EventID,
			"logicChannelId":      media.LogicChannelID,
		}
		event.PayloadRaw = media.FragmentData
		event.Meta["mediaId"] = media.MultiMediaID
		event.Meta["multiMediaType"] = media.MultiMediaType
	}
	if reply, ok := in.(*model.Msg0001); ok && reply != nil {
		// 0x0001 是终端对下行命令的应答，消费方据此调用管控的应答入口。
		event.Reply = &contracts.TerminalReply{
			EventID:      event.EventID,
			Phone:        phone,
			AnswerMsgID:  reply.AnswerMessageID,
			AnswerSerial: reply.AnswerSerialNumber,
			Result:       reply.Result,
			GatewayID:    gatewayID,
			SessionID:    sessionID,
			ReceivedAt:   time.Now(),
			Source:       "http",
		}
	}
	return event
}

// uplinkOccurredAt 从协议体中提取发生时间；没有时间字段的消息返回零值，
// 让 eventId 保持稳定，而不是退化为网关收包时间。
func uplinkOccurredAt(in model.JT808Msg) time.Time {
	switch msg := in.(type) {
	case *model.Msg0200:
		if msg != nil {
			return parseUplinkTime(msg.Time)
		}
	case *model.Msg0704:
		if msg != nil && len(msg.Items) > 0 {
			return parseUplinkTime(msg.Items[0].Loc.Time)
		}
	}
	return time.Time{}
}

func parseUplinkTime(value string) time.Time {
	if len(value) != len(uplinkTimeLayout) {
		return time.Time{}
	}
	parsed, err := time.ParseInLocation(uplinkTimeLayout, value, uplinkZone)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

// uplinkEventID 用手机号、消息 ID、流水号与原始消息体派生稳定 ID。
func uplinkEventID(phone string, msgID, serial uint16, body []byte) string {
	sum := sha256.New()
	fmt.Fprintf(sum, "%s|%04X|%d|", phone, msgID, serial)
	sum.Write(body)
	return hex.EncodeToString(sum.Sum(nil))[:16]
}

// protocolVersionLabel 把协议版本枚举转换为事件里的可读标签。
func protocolVersionLabel(version model.VersionType) string {
	switch version {
	case model.Version2019:
		return "2019"
	case model.Version2013:
		return "2013"
	default:
		return "2011"
	}
}

// uplinkReliability 把既有可靠性分类映射到出口契约的两档语义。
func uplinkReliability(policy *uplink.Policy, msgID uint16, incoming model.JT808Msg) contracts.Reliability {
	if policy != nil && policy.AtLeastOnce(msgID, incoming) {
		return contracts.ReliabilityConfirmBeforeAck
	}
	return contracts.ReliabilityBestEffort
}
