package protocol

import (
	"context"
	"net"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/storage"
)

const maxJT808BodyLength = 1023

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
		encoded, err := pd.Outgoing.Encode()
		if err != nil {
			return nil, err
		}
		header := pd.Outgoing.GetHeader()
		headerLen := 12
		if header.Attr.VersionDesc == model.Version2019 {
			headerLen = 17
		}
		if header.IsFragmented() {
			headerLen += 4
		}
		if len(encoded) < headerLen {
			return nil, ErrMalformedFrame
		}
		body := encoded[headerLen:]
		if len(body) <= maxJT808BodyLength {
			pkt, err := p.pc.Encode(pd.Outgoing)
			return context.WithValue(ctx, model.PacketEncodeCtxKey{}, [][]byte{pkt}), err
		}
		if header.IsFragmented() || header.Frag != nil {
			return nil, ErrMalformedFrame
		}
		total := (len(body) + maxJT808BodyLength - 1) / maxJT808BodyLength
		if total > 65535 {
			return nil, ErrMalformedFrame
		}
		serials := make([]uint16, total)
		serials[0] = header.SerialNumber
		if session, ok := ctx.Value(model.SessionCtxKey{}).(*model.Session); ok && total > 1 {
			copy(serials[1:], session.GetNextSerialNums(total-1))
		}
		frames := make([][]byte, total)
		for i := 0; i < total; i++ {
			start, end := i*maxJT808BodyLength, (i+1)*maxJT808BodyLength
			if end > len(body) {
				end = len(body)
			}
			fragmentHeader := *header
			fragmentAttr := *header.Attr
			fragmentAttr.PacketFragmented = 1
			fragmentAttr.PacketFragmentedDesc = model.PacketFragmentedTrue
			fragmentHeader.Attr = &fragmentAttr
			fragmentHeader.SerialNumber = serials[i]
			fragmentHeader.Frag = &model.MsgFragmentation{Total: uint16(total), Index: uint16(i + 1)}
			frames[i], err = p.pc.EncodePacketData(&fragmentHeader, body[start:end])
			if err != nil {
				return nil, err
			}
			storage.CacheSentSegmentFrame(header.PhoneNumber, serials[0], uint16(i+1), uint16(total), frames[i])
		}
		return context.WithValue(ctx, model.PacketEncodeCtxKey{}, frames), nil
	})
}

func send() delegateFunc {
	return delegateFunc(func(ctx context.Context, p *Pipeline) (context.Context, error) {
		packets := ctx.Value(model.PacketEncodeCtxKey{}).([][]byte)
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
		if data != nil && data.Outgoing != nil {
			header = data.Outgoing.GetHeader()
		}
		for i, packet := range packets {
			serial := uint16(0)
			if session != nil && header != nil {
				serial = header.SerialNumber
				for n := 0; n < i; n++ {
					serial = incrementJT808Serial(serial)
				}
				session.TrackReliable(header.MsgID, serial, header.PhoneNumber, packet)
			}
			if err := p.fh.Send(packet); err != nil {
				if session != nil && header != nil {
					for j := 0; j <= i; j++ {
						failedSerial := header.SerialNumber
						for n := 0; n < j; n++ {
							failedSerial = incrementJT808Serial(failedSerial)
						}
						session.CancelReliable(header.MsgID, failedSerial)
					}
				}
				return nil, err
			}
		}
		return ctx, nil
	})
}

func incrementJT808Serial(serial uint16) uint16 {
	if serial == 65535 {
		return 1
	}
	return serial + 1
}
