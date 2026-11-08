package protocol

import (
	"context"
	stderrors "errors"
	"net"
	"time"

	"github.com/rs/zerolog/log"

	actionpkg "github.com/fakeyanss/jt808-server-go/internal/action"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/repository"
)

const maxJT808BodyLength = 1023

// earlyAckSentCtxKey 标记本轮应答已在业务处理前发出，后续链路不再重复编码发送。
type earlyAckSentCtxKey struct{}

// tcp/udp 消息处理组
type Pipeline struct {
	fh *JT808FrameHandler // FrameHandler instance
	pc *JT808PacketCodec  // PacketCodec instance
	mp *JT808MsgProcessor // MsgHandler instance
}

// PipelineOption 配置 Pipeline 的生命周期钩子与依赖。
type PipelineOption func(*pipelineSettings)

type pipelineSettings struct {
	hooks actionpkg.Hooks
	deps  ProcessorDeps
}

// WithHooks 指定生命周期钩子，默认 action.Noop。
func WithHooks(hooks actionpkg.Hooks) PipelineOption {
	return func(s *pipelineSettings) {
		s.hooks = hooks
	}
}

// WithDeps 指定处理器依赖；未注入的字段回退到包级默认单例。
func WithDeps(deps ProcessorDeps) PipelineOption {
	return func(s *pipelineSettings) {
		s.deps = deps
	}
}

func NewPipeline(conn net.Conn, opts ...PipelineOption) *Pipeline {
	settings := pipelineSettings{}
	for _, opt := range opts {
		opt(&settings)
	}
	return &Pipeline{
		fh: NewJT808FrameHandler(conn),
		pc: NewJT808PacketCodec(),
		mp: NewJT808MsgProcessor(settings.hooks, WithProcessorDeps(settings.deps)),
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

// preassignedSerialCtxKey 标记下行报文流水号已由调用方预分配，编码阶段不再改写。
// outbox 需要在下发前用确定的流水号落库，才能把终端应答精确回填到命令记录。
type preassignedSerialCtxKey struct{}

// WithPreassignedSerial 标记本次下行发送使用报文头中已设置的流水号。
func WithPreassignedSerial(ctx context.Context) context.Context {
	return context.WithValue(ctx, preassignedSerialCtxKey{}, true)
}

func assignSerialNumber() delegateFunc {
	return delegateFunc(func(ctx context.Context, _ *Pipeline) (context.Context, error) {
		if ctx.Value(earlyAckSentCtxKey{}) != nil || ctx.Value(preassignedSerialCtxKey{}) != nil {
			return ctx, nil
		}
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
		// Decode 内部可能改写 Header.MsgID，先捕获协议原始值。
		msgID := packet.Header.MsgID
		pd, err := p.mp.Decode(ctx, packet)
		nxtCtx := context.WithValue(ctx, model.ProcessDataCtxKey{}, pd)
		if err != nil {
			return nxtCtx, err
		}
		// Incoming 为空表示分包尚未收齐，本轮没有可处理的业务。
		// Outgoing 为空只说明该消息不需要应答（如 0x0001/0x0608 等应答类），
		// 这些消息仍必须执行 process，否则应答登记与运行时状态不会更新。
		if pd == nil || pd.Incoming == nil {
			return nxtCtx, nil
		}

		ackFirst := pd.Outgoing != nil && p.mp.AckFirst(msgID)
		var spoolIDs []string
		if ackFirst && p.mp.deps.Uplink != nil {
			// at_least_once 分类先落盘再应答；落盘失败直接中断，让终端重传。
			ids, spoolErr := p.mp.deps.Uplink.BeforeAck(ctx, msgID, pd)
			if spoolErr != nil {
				return nxtCtx, spoolErr
			}
			spoolIDs = ids
		}
		if ackFirst {
			// best_effort 分类仍是先应答后落库（at-most-once）；
			// at_least_once 分类此时记录已落盘，崩溃后由启动重放兜底。
			if sendErr := p.sendOutgoingNow(nxtCtx); sendErr != nil {
				return nxtCtx, sendErr
			}
			nxtCtx = context.WithValue(nxtCtx, earlyAckSentCtxKey{}, true)
		}

		businessErr := p.mp.ApplyBusiness(ctx, msgID, pd)
		if businessErr != nil {
			// 未先应答的消息保持旧语义：业务错误中断本轮应答。
			// 已先应答的消息只记录日志，连接继续；设备不存在仍作为例外向上返回。
			// spool 记录保留不删，重启后会重放，保证 at-least-once。
			if !ackFirst || stderrors.Is(businessErr, repository.ErrDeviceNotFound) {
				return nxtCtx, businessErr
			}
			log.Error().Err(businessErr).
				Uint16("msg_id", msgID).
				Str("phone", packet.Header.PhoneNumber).
				Msg("Business processing failed after acknowledgement was sent")
			return nxtCtx, nil
		}
		if len(spoolIDs) > 0 {
			p.mp.deps.Uplink.AfterBusiness(spoolIDs)
		}
		return nxtCtx, nil
	})
}

// sendOutgoingNow 在当前会话内立即完成流水号分配、编码与写回，
// 用于「先应答后业务」的消息。后续 encode/send 通过 earlyAckSentCtxKey 跳过。
func (p *Pipeline) sendOutgoingNow(ctx context.Context) error {
	curCtx, err := assignSerialNumber()(ctx, p)
	if err != nil || curCtx == nil {
		return err
	}
	curCtx, err = encode()(curCtx, p)
	if err != nil || curCtx == nil {
		return err
	}
	_, err = send()(curCtx, p)
	return err
}

func encode() delegateFunc {
	return delegateFunc(func(ctx context.Context, p *Pipeline) (context.Context, error) {
		if ctx.Value(earlyAckSentCtxKey{}) != nil {
			return ctx, nil
		}
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
			p.mp.deps.Runtime.Retrans.CacheFrame(context.Background(), header.PhoneNumber, serials[0], uint16(i+1), uint16(total), frames[i])
		}
		return context.WithValue(ctx, model.PacketEncodeCtxKey{}, frames), nil
	})
}

func send() delegateFunc {
	return delegateFunc(func(ctx context.Context, p *Pipeline) (context.Context, error) {
		if ctx.Value(earlyAckSentCtxKey{}) != nil {
			return ctx, nil
		}
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
