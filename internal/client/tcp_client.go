package client

import (
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/protocol"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
	"github.com/fakeyanss/jt808-server-go/internal/storage"
)

type TCPClient struct {
	Session *model.Session
}

func NewTCPClient() *TCPClient {
	return &TCPClient{}
}

func (cli *TCPClient) Dial(addr string) error {
	return cli.DialTransport(addr, model.TCPProto)
}

func (cli *TCPClient) DialTransport(addr string, transport model.TransportProtocol) error {
	network := strings.ToLower(string(transport))
	if transport != model.TCPProto && transport != model.UDPProto {
		return errors.New("unsupported transport protocol: " + network)
	}
	conn, err := net.Dial(network, addr)

	if err == nil {
		remoteAddr := conn.RemoteAddr().String()
		cli.Session = &model.Session{
			Conn:      conn,
			ID:        remoteAddr, // using remote addr default
			Transport: transport,
		}
		log.Debug().Str("addr", addr).Msg("Dialed to server")
	}

	return err
}

func (cli *TCPClient) Start() {
	pg := protocol.NewPipeline(cli.Session.Conn)

	for {
		_ = cli.Session.Conn.SetReadDeadline(time.Now().Add(2 * time.Minute))
		// 记录value ctx
		ctx := context.WithValue(context.Background(), model.SessionCtxKey{}, cli.Session)

		err := pg.ProcessConnRead(ctx)

		if err == nil {
			continue
		}
		if errors.Is(err, protocol.ErrFrameReadEmpty) {
			continue
		}

		log.Error().Err(err).Str("id", cli.Session.ID).Msg("Failed to serve session")

		switch {
		case errors.Is(err, io.EOF), errors.Is(err, io.ErrClosedPipe), errors.Is(err, net.ErrClosed), errors.Is(err, protocol.ErrActiveClose), isTimeout(err):
			return // close connection when EOF or closed
		default:
			time.Sleep(1 * time.Second)
		}
	}
}

func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func (cli *TCPClient) Stop() {
	if cli.Session != nil {
		cli.Session.ClosePending()
	}
	cli.Session.Conn.Close()
}

func (cli *TCPClient) Send(msg model.JT808Msg) {
	pg := protocol.NewPipeline(cli.Session.Conn)

	// 记录value ctx
	ctx := context.WithValue(context.Background(), model.SessionCtxKey{}, cli.Session)
	ctx = context.WithValue(ctx, model.ProcessDataCtxKey{}, &model.ProcessData{Outgoing: msg})

	err := pg.ProcessConnWrite(ctx)

	if err == nil {
		return
	}

	if errors.Is(err, io.ErrClosedPipe) || errors.Is(err, net.ErrClosed) {
		cli.Stop()
	}

	log.Error().Err(err).Msg("Failed to send jtmsg to server")
}

// SendFragments splits a JT808 message body into independently encoded long
// message packets. The client simulator uses it to exercise server reassembly.
func (cli *TCPClient) SendFragments(msg model.JT808Msg, fragmentSize int) error {
	return cli.SendFragmentsWithOrder(msg, fragmentSize, false)
}

func (cli *TCPClient) SendFragmentsOutOfOrder(msg model.JT808Msg, fragmentSize int) error {
	return cli.SendFragmentsWithOrder(msg, fragmentSize, true)
}

func (cli *TCPClient) SendFragmentsWithOrder(msg model.JT808Msg, fragmentSize int, reverse bool) error {
	return cli.SendFragmentsWithFault(msg, fragmentSize, reverse, 0)
}

// SendFragmentsWithFault optionally omits one 1-based fragment index from the
// initial transmission while retaining it for a later 0x8003 retransmission.
func (cli *TCPClient) SendFragmentsWithFault(msg model.JT808Msg, fragmentSize int, reverse bool, dropIndex uint16) error {
	if cli.Session == nil || msg == nil || fragmentSize <= 0 || fragmentSize > 1023 {
		return errors.New("invalid fragmented message arguments")
	}
	header := msg.GetHeader()
	if header == nil || header.Attr == nil || header.IsFragmented() || header.Frag != nil {
		return errors.New("message must have a non-fragmented header")
	}
	encoded, err := msg.Encode()
	if err != nil {
		return err
	}
	headerLen := 12
	if header.Attr.VersionDesc == model.Version2019 {
		headerLen = 17
	}
	if len(encoded) < headerLen {
		return errors.New("encoded JT808 message is shorter than its header")
	}
	body := encoded[headerLen:]
	total := (len(body) + fragmentSize - 1) / fragmentSize
	if total == 0 || total > 65535 {
		return errors.New("fragment count is outside the JT808 range")
	}
	if dropIndex > uint16(total) {
		return errors.New("dropped fragment index exceeds fragment count")
	}
	codec := protocol.NewJT808PacketCodec()
	serials := cli.Session.GetNextSerialNums(total)
	if media, ok := msg.(*model.Msg0801); ok {
		storage.Default().Retrans.LinkMedia(context.Background(), header.PhoneNumber, media.MultiMediaID, serials[0])
	}
	frames := make([][]byte, total)
	for idx := 0; idx < total; idx++ {
		start := idx * fragmentSize
		end := start + fragmentSize
		if end > len(body) {
			end = len(body)
		}
		fragmentHeader := *header
		fragmentAttr := *header.Attr
		fragmentAttr.PacketFragmented = 1
		fragmentAttr.PacketFragmentedDesc = model.PacketFragmentedTrue
		fragmentHeader.Attr = &fragmentAttr
		fragmentHeader.Frag = &model.MsgFragmentation{Total: uint16(total), Index: uint16(idx + 1)}
		fragmentHeader.SerialNumber = serials[idx]
		frame, err := codec.EncodePacketData(&fragmentHeader, body[start:end])
		if err != nil {
			return err
		}
		frames[idx] = frame
		storage.Default().Retrans.CacheFrame(context.Background(), header.PhoneNumber, serials[0], uint16(idx+1), uint16(total), frame)
	}
	for sent := 0; sent < total; sent++ {
		idx := sent
		if reverse {
			idx = total - sent - 1
		}
		if uint16(idx+1) == dropIndex {
			continue
		}
		serial := serials[idx]
		cli.Session.TrackReliable(header.MsgID, serial, header.PhoneNumber, frames[idx])
		if err = cli.Session.WriteFrame(frames[idx]); err != nil {
			cli.Session.CancelReliable(header.MsgID, serial)
			return err
		}
	}
	return nil
}

// SendMalformedFrame sends one deliberately invalid frame before a valid
// message. It is intended for the protocol test client to verify that a server
// rejects malformed input and can continue processing the following frame.
func (cli *TCPClient) SendMalformedFrame(msg model.JT808Msg, fault string) error {
	if cli.Session == nil || msg == nil {
		return errors.New("invalid malformed-frame test arguments")
	}
	encoded, err := protocol.NewJT808PacketCodec().Encode(msg)
	if err != nil {
		return err
	}
	var malformed []byte
	switch fault {
	case "bad-checksum":
		if len(encoded) < 4 {
			return errors.New("encoded frame is too short")
		}
		malformed = append([]byte(nil), encoded...)
		malformed[len(malformed)-2] ^= 0xff
	case "bad-escape":
		malformed = []byte{0x7e, 0x7d, 0x03, 0x7e}
	case "oversize":
		malformed = make([]byte, protocol.MaxFrameLen+1)
		malformed[0], malformed[len(malformed)-1] = 0x7e, 0x7e
		for i := 1; i < len(malformed)-1; i++ {
			malformed[i] = 0x01
		}
	default:
		return errors.New("fault must be bad-checksum, bad-escape, or oversize")
	}
	if err := cli.Session.WriteFrame(malformed); err != nil {
		return err
	}
	return cli.SendWithError(msg)
}

// SendWithError writes a normally encoded message and returns transport or
// encoding failures to test tooling.
func (cli *TCPClient) SendWithError(msg model.JT808Msg) error {
	if cli.Session == nil || msg == nil {
		return errors.New("invalid message send arguments")
	}
	pg := protocol.NewPipeline(cli.Session.Conn)
	ctx := context.WithValue(context.Background(), model.SessionCtxKey{}, cli.Session)
	ctx = context.WithValue(ctx, model.ProcessDataCtxKey{}, &model.ProcessData{Outgoing: msg})
	return pg.ProcessConnWrite(ctx)
}

// RequestFragmentRetransmission sends a terminal-side 0x0005 request. It is
// intended for simulator scenarios that verify platform resend behavior.
func (cli *TCPClient) RequestFragmentRetransmission(device *model.Device, originalSerial uint16, packetIDs []uint16) error {
	if cli.Session == nil || device == nil || device.Phone == "" || len(packetIDs) == 0 || len(packetIDs) > 65535 {
		return errors.New("invalid fragment retransmission request")
	}
	header := model.GenMsgHeader(device, 0x0005, cli.Session.GetNextSerialNum())
	return cli.SendWithError(&model.Msg0005{Header: header, OriginalSerialNumber: originalSerial, PacketIDs: packetIDs})
}
