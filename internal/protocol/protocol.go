package protocol

import (
	"context"

	"github.com/fakeYanss/jt808-server-go/internal/protocol/model"
)

type (
	frameCtxKey struct{}

	packetDecodeCtxKey struct{}

	msgCtxKey struct{}

	cmdCtxKey struct{}

	packetEncodeCtxKey struct{}
)

// tcp/udp 消息处理组
type ProcessGroup struct {
	FH *JT808FrameHandler
	PC *JT808PacketCodec
	MH *JT808MsgHandler
}

// 处理函数封装
type processFunc func(context.Context, *ProcessGroup) (context.Context, error)

func ProcessConn(ctx context.Context, pg *ProcessGroup) error {
	actions := []processFunc{
		recv(),
		decode(),
		processPacket(),
		processMsg(),
		encode(),
		send(),
	}
	return callWithBlocking(ctx, pg, actions)
}

func callWithBlocking(ctx context.Context, pg *ProcessGroup, funcs []processFunc) error {
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
		framePayload, err := pg.FH.Recv(ctx)
		nxtCtx := context.WithValue(ctx, frameCtxKey{}, framePayload)
		return nxtCtx, err
	}
}

func decode() processFunc {
	return func(ctx context.Context, pg *ProcessGroup) (context.Context, error) {
		framePayload := ctx.Value(frameCtxKey{}).(FramePayload)
		packet, err := pg.PC.Decode(framePayload)
		nxtCtx := context.WithValue(ctx, packetDecodeCtxKey{}, packet)
		return nxtCtx, err
	}
}

func processPacket() processFunc {
	return func(ctx context.Context, pg *ProcessGroup) (context.Context, error) {
		packet := ctx.Value(packetDecodeCtxKey{}).(*model.Packet)
		jtmsg, err := pg.MH.ProcessPacket(ctx, packet)
		nxtCtx := context.WithValue(ctx, msgCtxKey{}, jtmsg)
		return nxtCtx, err
	}
}

func processMsg() processFunc {
	return func(ctx context.Context, pg *ProcessGroup) (context.Context, error) {
		jtmsg := ctx.Value(msgCtxKey{}).(model.JT808Msg)
		jtcmd, err := pg.MH.ProcessMsg(ctx, jtmsg)
		nxtCtx := context.WithValue(ctx, cmdCtxKey{}, jtcmd)
		return nxtCtx, err
	}
}

func encode() processFunc {
	return func(ctx context.Context, pg *ProcessGroup) (context.Context, error) {
		jtcmd := ctx.Value(cmdCtxKey{}).(model.JT808Cmd)
		if jtcmd == nil { // 不需要回复cmd，不用后续处理
			return nil, nil
		}
		pkt, err := pg.PC.Encode(jtcmd)
		nxtCtx := context.WithValue(ctx, packetEncodeCtxKey{}, pkt)
		return nxtCtx, err
	}
}

func send() processFunc {
	return func(ctx context.Context, pg *ProcessGroup) (context.Context, error) {
		packet := ctx.Value(packetEncodeCtxKey{}).([]byte)
		err := pg.FH.Send(packet)
		return ctx, err
	}
}
