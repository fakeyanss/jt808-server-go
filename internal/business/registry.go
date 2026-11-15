package business

import "github.com/fakeyanss/jt808-server-go/internal/protocol/model"

// action 只保留业务处理函数；消息类型与应答构造仍由 protocol 的注册表负责。
// 字段形状与 protocol 的 action 保持一致，便于 tools/registrydoc 解析。
type action struct {
	genData func() *model.ProcessData
	process handlerFunc
}

// initHandlers 注册管控侧业务消息。
// 这张表只包含 OwnerControl 的消息；注册/鉴权业务半边由 ProcessControl 直接分派，
// 0x0001 的管控半边不落库，因此都不在表内。
func (p *Processor) initHandlers() map[uint16]handlerFunc {
	options := make(map[uint16]*action)

	options[0x0200] = &action{process: p.processMsg0200} // 位置信息上报
	options[0x0704] = &action{process: p.processMsg0704} // 批量定位数据上传

	options[0x0108] = &action{process: p.processIndustryReport} // 升级结果通知
	options[0x0301] = &action{process: p.processIndustryReport} // 事件上报
	options[0x0302] = &action{process: p.processIndustryReport} // 提问应答
	options[0x0303] = &action{process: p.processIndustryReport} // 信息点播/取消
	options[0x0500] = &action{process: p.processIndustryReport} // 车辆控制应答
	options[0x0700] = &action{process: p.processIndustryReport}
	options[0x0701] = &action{process: p.processIndustryReport} // 电子运单上报
	options[0x0702] = &action{process: p.processIndustryReport}
	options[0x0705] = &action{process: p.processIndustryReport}
	options[0x0800] = &action{process: p.processIndustryReport}
	options[0x0900] = &action{process: p.processIndustryReport} // 数据上行透传
	options[0x0901] = &action{process: p.processIndustryReport} // 数据压缩上报

	options[0x0801] = &action{process: p.processMsg0801} // 多媒体数据上传
	options[0x0802] = &action{process: p.processMsg0802} // 多媒体检索应答
	options[0x0805] = &action{process: p.processMsg0805} // 摄像头立即拍摄应答
	options[0x1205] = &action{process: p.processMediaResourceList}
	options[0x1206] = &action{process: p.processFileUploadNotice}
	options[0x1210] = &action{process: p.processAttachmentSignaling}
	options[0x1211] = &action{process: p.processAttachmentSignaling}
	options[0x1212] = &action{process: p.processAttachmentSignaling}

	handlers := make(map[uint16]handlerFunc, len(options))
	for msgID, option := range options {
		if option.process != nil {
			handlers[msgID] = option.process
		}
	}
	return handlers
}
