package api

import (
	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/server"
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

// directSend 供豁免清单内的下行入口直连发送。
// 未登记的报文会记错误日志：新增下行必须走 outbox，或在此登记豁免理由。
func directSend(serv *server.TCPServer, session *model.Session, msg model.JT808Msg) {
	if header := msg.GetHeader(); header != nil {
		if _, ok := downlinkExemptions[header.MsgID]; !ok {
			log.Error().Uint16("msg_id", header.MsgID).Msg("Downlink message is not covered by outbox or exemption list")
		}
	}
	serv.Send(session.ID, msg)
}
