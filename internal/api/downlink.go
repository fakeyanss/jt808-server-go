package api

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/contracts"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

// downlinkExemptions 记录不接 outbox 的下行入口及豁免理由。
// 判定标准只有两类：
//   - 查询类：HTTP 请求同步等待应答，重投无法重新绑定等待方；且只读、可安全重发。
//   - 确认/通知类：终端按业务键幂等，重复下发无副作用。
//
// 其余有副作用的下行必须走 dispatchDownlink（outbox）。
var downlinkExemptions = map[uint16]string{
	0x8104: "查询终端参数：HTTP 同步等待应答，重投无法重新绑定等待方",
	0x8106: "查询指定终端参数：HTTP 同步等待应答，重投无法重新绑定等待方",
	0x8107: "查询终端属性：HTTP 同步等待应答，重投无法重新绑定等待方",
	0x8201: "查询位置：HTTP 同步等待应答，重投无法重新绑定等待方",
	0x8608: "查询区域/线路：HTTP 同步等待应答，重投无法重新绑定等待方",
	0x8702: "查询驾驶员身份：只读查询，可安全重发",
	0x8802: "查询媒体资源：只读查询，可安全重发",
	0x9205: "查询指定媒体资源：只读查询，可安全重发",
	0x8203: "报警确认：终端按 alarmSerial 幂等，重复下发无副作用",
}

// directSend 供豁免清单内的下行入口单发。
//
// 走 DownlinkSender 而不是本地 serv.Send：control-only 没有本地会话，
// 只有网关侧能完成实际发送；standalone 下 sender 是 LocalSender，行为不变。
// 这些报文不进 outbox，也不产生“永远 accepted”的记录。
func directSend(c *gin.Context, sender contracts.DownlinkSender, msg model.JT808Msg) {
	header := msg.GetHeader()
	if header == nil {
		log.Error().Msg("Downlink message header is required")
		return
	}
	if _, ok := downlinkExemptions[header.MsgID]; !ok {
		log.Error().Uint16("msg_id", header.MsgID).Msg("Downlink message is not covered by outbox or exemption list")
	}
	if sender == nil {
		log.Error().Msg("Downlink sender is unavailable")
		return
	}
	ctx, cancel := context.WithTimeout(c.Request.Context(), 10*time.Second)
	defer cancel()
	receipt, err := sender.Send(ctx, contracts.DownlinkRequest{
		Phone:   header.PhoneNumber,
		Serial:  header.SerialNumber,
		Message: msg,
	})
	if err != nil {
		log.Warn().Err(err).
			Str("phone", header.PhoneNumber).
			Uint16("msg_id", header.MsgID).
			Msg("Exempt downlink send failed")
		return
	}
	if receipt.Status == contracts.DownlinkStatusOffline || receipt.Status == contracts.DownlinkStatusRejected {
		log.Warn().
			Str("phone", header.PhoneNumber).
			Uint16("msg_id", header.MsgID).
			Str("status", receipt.Status).
			Str("reason", receipt.Reason).
			Msg("Exempt downlink was not delivered")
	}
}
