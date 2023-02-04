package protocol

import (
	"context"
	"net"

	"github.com/fakeYanss/jt808-server-go/internal/protocol/model"
)

// tcp/udp 消息处理组
type ProcessGroup struct {
	fh *JT808FrameHandler // FrameHandler instance
	pc *JT808PacketCodec  // PacketCodec instance
	mh *JT808MsgProcessor // MsgHandler instance
}

func NewProcessGroup(conn net.Conn) *ProcessGroup {
	return &ProcessGroup{
		fh: NewJT808FrameHandler(conn),
		pc: NewJT808PacketCodec(),
		mh: NewJT808MsgHandler(),
	}
}

// 处理函数封装
type processFunc func(context.Context, *ProcessGroup) (context.Context, error)

func (pg *ProcessGroup) ProcessConnRead(ctx context.Context) error {
	actions := []processFunc{
		recv(),
		decode(),
		processPacket(),
		processMsg(),
		encode(),
		send(),
	}
	return pg.callWithBlocking(ctx, actions)
}

func (pg *ProcessGroup) ProcessConnWrite(ctx context.Context) error {
	actions := []processFunc{
		encode(),
		send(),
	}
	return pg.callWithBlocking(ctx, actions)
}

func (pg *ProcessGroup) callWithBlocking(ctx context.Context, funcs []processFunc) error {
	// todo: 重构err定义，通过errors.Cause, 区分breakErr, continueErr
	curCtx := ctx
	var err error
	for _, f := range funcs {
		curCtx, err = f(curCtx, pg)
		if curCtx == nil || err != nil {
			break
		}
	}
	return err
}

func recv() processFunc {
	return func(ctx context.Context, pg *ProcessGroup) (context.Context, error) {
		framePayload, err := pg.fh.Recv(ctx)
		nxtCtx := context.WithValue(ctx, model.FrameCtxKey{}, framePayload)
		return nxtCtx, err
	}
}

func decode() processFunc {
	return func(ctx context.Context, pg *ProcessGroup) (context.Context, error) {
		framePayload := ctx.Value(model.FrameCtxKey{}).(FramePayload)
		packet, err := pg.pc.Decode(framePayload)
		nxtCtx := context.WithValue(ctx, model.PacketDecodeCtxKey{}, packet)
		return nxtCtx, err
	}
}

func processPacket() processFunc {
	return func(ctx context.Context, pg *ProcessGroup) (context.Context, error) {
		packet := ctx.Value(model.PacketDecodeCtxKey{}).(*model.PacketData)
		jtmsg, err := pg.mh.ProcessPacket(ctx, packet)
		nxtCtx := context.WithValue(ctx, model.MsgCtxKey{}, jtmsg)
		return nxtCtx, err
	}
}

func processMsg() processFunc {
	return func(ctx context.Context, pg *ProcessGroup) (context.Context, error) {
		jtmsg := ctx.Value(model.MsgCtxKey{}).(model.JT808Msg)
		jtcmd, err := pg.mh.ProcessMsg(ctx, jtmsg)
		nxtCtx := context.WithValue(ctx, model.CmdCtxKey{}, jtcmd)
		return nxtCtx, err
	}
}

func process() processFunc {
	return func(ctx context.Context, pg *ProcessGroup) (context.Context, error) {
		packet := ctx.Value(model.ProcessDataCtxKey{}).(*model.PacketData)
		pd, err := pg.mh.ProcessMsg1(ctx, packet)
		nxtCtx := context.WithValue(ctx, model.ProcessDataCtxKey{}, pd)
		return nxtCtx, err
	}
}

func encode() processFunc {
	return func(ctx context.Context, pg *ProcessGroup) (context.Context, error) {
		pd := ctx.Value(model.CmdCtxKey{}).(*model.ProcessData)
		if pd == nil || pd.Cmd == nil { // 不需要回复cmd，不用后续处理
			return nil, nil
		}
		// jtcmd := ctx.Value(model.CmdCtxKey{}).(model.JT808Cmd)
		// if jtcmd == nil { // 不需要回复cmd，不用后续处理
		// 	return nil, nil
		// }
		pkt, err := pg.pc.Encode(pd.Cmd)
		nxtCtx := context.WithValue(ctx, model.PacketEncodeCtxKey{}, pkt)
		return nxtCtx, err
	}
}

func send() processFunc {
	return func(ctx context.Context, pg *ProcessGroup) (context.Context, error) {
		packet := ctx.Value(model.PacketEncodeCtxKey{}).([]byte)
		err := pg.fh.Send(packet)
		return ctx, err
	}
}
