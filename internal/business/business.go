// Package business 承载管控侧的业务半边：注册/鉴权落库、定位与报警、
// 行业上报、多媒体与附件元数据。
//
// 依赖方向：business → protocol（消息模型与钩子派发）/ repository / mediaupload。
// 协议内核不反向依赖本包：单机进程通过 ProcessorDeps.Business 注入，
// 集群网关不注入，管控侧由 control.BusinessExecutor 直接调用 ProcessControl。
package business

import (
	"context"

	actionpkg "github.com/fakeyanss/jt808-server-go/internal/action"
	"github.com/fakeyanss/jt808-server-go/internal/mediaupload"
	"github.com/fakeyanss/jt808-server-go/internal/protocol"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

// Options 是业务处理器的构造参数。
type Options struct {
	// Repositories 是业务落库所需的仓储接口集合。
	Repositories repository.Repositories
	// Hooks 是控制面识别到注册/鉴权/位置/扩展事件时的动作出口。
	Hooks actionpkg.Hooks
	// MediaUpload 是 0x0801 的本地媒体目录；为空时只保留行业内存快照。
	MediaUpload *mediaupload.Store
}

// Processor 执行控制面业务，不持有活连接。
type Processor struct {
	repos       repository.Repositories
	hooks       actionpkg.Hooks
	mediaUpload *mediaupload.Store
	handlers    map[uint16]handlerFunc
}

type handlerFunc func(context.Context, *model.ProcessData) error

// New 创建业务处理器。
func New(opts Options) *Processor {
	hooks := opts.Hooks
	if hooks == nil {
		hooks = actionpkg.Noop{}
	}
	p := &Processor{
		repos:       opts.Repositories,
		hooks:       hooks,
		mediaUpload: opts.MediaUpload,
	}
	p.handlers = p.initHandlers()
	return p
}

// Process 只执行业务处理，不派发钩子。
// 单机进程由 protocol.ApplyBusiness 统一派发生命周期钩子，避免重复触发。
func (p *Processor) Process(ctx context.Context, msgID uint16, data *model.ProcessData) error {
	if data == nil || data.Incoming == nil {
		return nil
	}
	if handler := p.handlers[msgID]; handler != nil {
		return handler(ctx, data)
	}
	return nil
}

// ProcessControl 是管控侧入口：在业务处理之外补齐与事件语义相关的钩子。
//
// 0x0100/0x0102 使用网关随事件带来的应答结果派发注册/鉴权事件；
// 0x0200/0x0704 只派发位置/报警事件，位置历史的落库由 Process 内的 handler 完成。
func (p *Processor) ProcessControl(ctx context.Context, msgID uint16, data *model.ProcessData) error {
	switch msgID {
	case 0x0001:
		// 0x0001 的管控半边不落库：应答由 uplink 入口直接交给 outbox 收尾。
		return p.processTerminalReplyBusiness(ctx, data)
	case 0x0100:
		err := p.processRegisterBusiness(ctx, data)
		protocol.ApplyHookReply(ctx, msgID, data)
		protocol.DispatchHooks(ctx, p.hooks, data)
		return err
	case 0x0102:
		err := p.processAuthBusiness(ctx, data)
		protocol.ApplyHookReply(ctx, msgID, data)
		protocol.DispatchHooks(ctx, p.hooks, data)
		return err
	case 0x0200:
		// 控制面也需要报警事件：自动 0x9208 与报警动作回调在这里触发。
		protocol.DispatchHooks(ctx, p.hooks, data)
		return p.processMsg0200(ctx, data)
	case 0x0704:
		// 0x0704 在集群控制面只派发位置事件；历史写入保持既有归属不变。
		protocol.DispatchHooks(ctx, p.hooks, data)
		return nil
	}
	if data == nil || data.Incoming == nil {
		return nil
	}
	if handler := p.handlers[msgID]; handler != nil {
		err := handler(ctx, data)
		protocol.DispatchExtendedHooks(ctx, p.hooks, data)
		return err
	}
	return nil
}
