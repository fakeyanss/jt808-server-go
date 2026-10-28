package protocol

import (
	"context"
	"net"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
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
		assignSerialNumber(),
		encode(),
		send(),
	}
	return p.callWithBlocking(ctx, actions)
}

func (p *Pipeline) ProcessConnWrite(ctx context.Context) error {
	actions := []delegateFunc{
		assignSerialNumber(),
		encode(),
		send(),
	}
	return p.callWithBlocking(ctx, actions)
}

func assignSerialNumber() delegateFunc {
	return delegateFunc(func(ctx context.Context, _ *Pipeline) (context.Context, error) {
		pd, _ := ctx.Value(model.ProcessDataCtxKey{}).(*model.ProcessData)
		if pd == nil || pd.Outgoing == nil {
			return nil, nil
		}
		session, _ := ctx.Value(model.SessionCtxKey{}).(*model.Session)
		if session != nil {
			pd.Outgoing.GetHeader().SerialNumber = session.GetNextSerialNum()
		}
		return ctx, nil
	})
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
	return delegateFunc(func(ctx context.Context, p *Pipeline) (context.Context, error) {
		framePayload, err := p.fh.Recv(ctx)
		nxtCtx := context.WithValue(ctx, model.FrameCtxKey{}, framePayload)
		return nxtCtx, err
	})
}

func decode() delegateFunc {
	return delegateFunc(func(ctx context.Context, p *Pipeline) (context.Context, error) {
		framePayload := ctx.Value(model.FrameCtxKey{}).(FramePayload)
		packet, err := p.pc.Decode(framePayload)
		nxtCtx := context.WithValue(ctx, model.PacketDecodeCtxKey{}, packet)
		return nxtCtx, err
	})
}

func process() delegateFunc {
	return delegateFunc(func(ctx context.Context, p *Pipeline) (context.Context, error) {
		packet := ctx.Value(model.PacketDecodeCtxKey{}).(*model.PacketData)
		if packet == nil { // 不需要处理
			return nil, nil
		}
		pd, err := p.mp.Process(ctx, packet)
		nxtCtx := context.WithValue(ctx, model.ProcessDataCtxKey{}, pd)
		return nxtCtx, err
	})
}

func encode() delegateFunc {
	return delegateFunc(func(ctx context.Context, p *Pipeline) (context.Context, error) {
		pd := ctx.Value(model.ProcessDataCtxKey{}).(*model.ProcessData)
		if pd == nil || pd.Outgoing == nil { // 不需要回复，不用后续处理
			return nil, nil
		}
		pkt, err := p.pc.Encode(pd.Outgoing)
		nxtCtx := context.WithValue(ctx, model.PacketEncodeCtxKey{}, pkt)
		return nxtCtx, err
	})
}

func send() delegateFunc {
	return delegateFunc(func(ctx context.Context, p *Pipeline) (context.Context, error) {
		packet := ctx.Value(model.PacketEncodeCtxKey{}).([]byte)
		session, _ := ctx.Value(model.SessionCtxKey{}).(*model.Session)
		if session != nil {
			session.WriteMu.Lock()
			defer session.WriteMu.Unlock()
			_ = session.Conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
			if data, ok := ctx.Value(model.ProcessDataCtxKey{}).(*model.ProcessData); ok && data != nil && data.Outgoing != nil {
				header := data.Outgoing.GetHeader()
				log.Debug().Str("session", session.ID).Uint16("msg_id", header.MsgID).Uint16("serial", header.SerialNumber).Msg("Sending JT808 message")
			}
		}
		data, _ := ctx.Value(model.ProcessDataCtxKey{}).(*model.ProcessData)
		var header *model.MsgHeader
		if session != nil && data != nil && data.Outgoing != nil {
			header = data.Outgoing.GetHeader()
			session.TrackReliable(header.MsgID, header.SerialNumber, header.PhoneNumber, packet)
		}
		err := p.fh.Send(packet)
		if err != nil && session != nil && header != nil {
			session.CancelReliable(header.MsgID, header.SerialNumber)
		}
		return ctx, err
	})
}
