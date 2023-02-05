package protocol

import (
	"context"
	"net"

	"github.com/fakeYanss/jt808-server-go/internal/protocol/model"
)

// tcp/udp 消息处理组
type Pipeline struct {
	fh *JT808FrameHandler // FrameHandler instance
	pc *JT808PacketCodec  // PacketCodec instance
	mp *JT808MsgProcessor // MsgHandler instance
}

func NewPipeline(conn net.Conn) *Pipeline {
	return &Pipeline{
		fh: NewJT808FrameHandler(conn),
		pc: NewJT808PacketCodec(),
		mp: NewJT808MsgProcessor(),
	}
}

// 处理函数封装
type delegateFunc func(context.Context, *Pipeline) (context.Context, error)

func (p *Pipeline) ProcessConnRead(ctx context.Context) error {
	actions := []delegateFunc{
		recv(),
		decode(),
		process(),
		encode(),
		send(),
	}
	return p.callWithBlocking(ctx, actions)
}

func (p *Pipeline) ProcessConnWrite(ctx context.Context) error {
	actions := []delegateFunc{
		encode(),
		send(),
	}
	return p.callWithBlocking(ctx, actions)
}

func (p *Pipeline) callWithBlocking(ctx context.Context, funcs []delegateFunc) error {
	// todo: 重构err定义，通过errors.Cause, 区分breakErr, continueErr
	curCtx := ctx
	var err error
	for _, f := range funcs {
		curCtx, err = f(curCtx, p)
		if curCtx == nil || err != nil {
			break
		}
	}
	return err
}

func recv() delegateFunc {
	return func(ctx context.Context, p *Pipeline) (context.Context, error) {
		framePayload, err := p.fh.Recv(ctx)
		nxtCtx := context.WithValue(ctx, model.FrameCtxKey{}, framePayload)
		return nxtCtx, err
	}
}

func decode() delegateFunc {
	return func(ctx context.Context, p *Pipeline) (context.Context, error) {
		framePayload := ctx.Value(model.FrameCtxKey{}).(FramePayload)
		packet, err := p.pc.Decode(framePayload)
		nxtCtx := context.WithValue(ctx, model.PacketDecodeCtxKey{}, packet)
		return nxtCtx, err
	}
}

func process() delegateFunc {
	return func(ctx context.Context, p *Pipeline) (context.Context, error) {
		packet := ctx.Value(model.PacketDecodeCtxKey{}).(*model.PacketData)
		pd, err := p.mp.Process(ctx, packet)
		nxtCtx := context.WithValue(ctx, model.ProcessDataCtxKey{}, pd)
		return nxtCtx, err
	}
}

func encode() delegateFunc {
	return func(ctx context.Context, p *Pipeline) (context.Context, error) {
		pd := ctx.Value(model.ProcessDataCtxKey{}).(*model.ProcessData)
		if pd == nil || pd.Cmd == nil { // 不需要回复cmd，不用后续处理
			return nil, nil
		}
		pkt, err := p.pc.Encode(pd.Cmd)
		nxtCtx := context.WithValue(ctx, model.PacketEncodeCtxKey{}, pkt)
		return nxtCtx, err
	}
}

func send() delegateFunc {
	return func(ctx context.Context, p *Pipeline) (context.Context, error) {
		packet := ctx.Value(model.PacketEncodeCtxKey{}).([]byte)
		err := p.fh.Send(packet)
		return ctx, err
	}
}
