package protocol

import (
	"context"
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
// 事件 ID 只使用手机号、消息 ID、流水号与发生时间，保证终端重传时稳定。
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
	sessionID := ""
	transport := ""
	if header != nil {
		phone = header.PhoneNumber
		serial = header.SerialNumber
		if header.Frag != nil && header.Frag.Total > 0 {
			packetCount = int(header.Frag.Total)
		}
	}
	if session, ok := ctx.Value(model.SessionCtxKey{}).(*model.Session); ok && session != nil {
		sessionID = session.ID
		transport = string(session.Transport)
	}

	occurredAt := uplinkOccurredAt(in)
	occurredAtMS := int64(0)
	if !occurredAt.IsZero() {
		occurredAtMS = occurredAt.UnixMilli()
	}

	event := contracts.UplinkEvent{
		EventID:     fmt.Sprintf("%s|0x%04X|%d|%d", phone, msgID, serial, occurredAtMS),
		Phone:       phone,
		MsgID:       msgID,
		Serial:      serial,
		OccurredAt:  occurredAt,
		GatewayID:   gatewayID,
		SessionID:   sessionID,
		Transport:   transport,
		Reliability: reliability,
		Payload:     in,
		Meta:        map[string]any{"packetCount": packetCount},
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

// uplinkReliability 把既有可靠性分类映射到出口契约的两档语义。
func uplinkReliability(policy *uplink.Policy, msgID uint16, incoming model.JT808Msg) contracts.Reliability {
	if policy != nil && policy.AtLeastOnce(msgID, incoming) {
		return contracts.ReliabilityConfirmBeforeAck
	}
	return contracts.ReliabilityBestEffort
}
