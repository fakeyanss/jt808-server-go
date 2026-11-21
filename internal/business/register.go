package business

import (
	"context"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

// processTerminalReplyBusiness 是 0x0001 的管控半边占位实现。
//
// 应答不在这里落库：管控的 uplink HTTP 入口在解析出 TerminalReply 后直接调用
// TerminalReplyHandler（outbox），此处只用于归属声明与注册表一致性校验。
func (p *Processor) processTerminalReplyBusiness(context.Context, *model.ProcessData) error {
	return nil
}
