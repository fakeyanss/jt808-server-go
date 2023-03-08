package client

import (
	"context"
	"errors"
	"io"
	"net"
	"time"

	"github.com/rs/zerolog/log"

	"github.com/fakeyanss/jt808-server-go/internal/protocol"
	"github.com/fakeyanss/jt808-server-go/internal/protocol/model"
)

type TCPClient struct {
	Session *model.Session
}

func NewTCPClient() *TCPClient {
	return &TCPClient{}
}

func (cli *TCPClient) Dial(addr string) error {
	conn, err := net.Dial("tcp", addr)

	if err == nil {
		remoteAddr := conn.RemoteAddr().String()
		cli.Session = &model.Session{
			Conn: conn,
			ID:   remoteAddr, // using remote addr default
		}
		log.Debug().Str("addr", addr).Msg("Dialed to server")
	}

	return err
}

func (cli *TCPClient) Start() {
	pg := protocol.NewPipeline(cli.Session.Conn)

	for {
		// 记录value ctx
		ctx := context.WithValue(context.Background(), model.SessionCtxKey{}, cli.Session)

		err := pg.ProcessConnRead(ctx)

		if err == nil {
			continue
		}

		log.Error().Err(err).Str("id", cli.Session.ID).Msg("Failed to serve session")

		switch {
		case errors.Is(err, io.EOF), errors.Is(err, io.ErrClosedPipe), errors.Is(err, net.ErrClosed), errors.Is(err, protocol.ErrActiveClose):
			return // close connection when EOF or closed
		default:
			time.Sleep(1 * time.Second)
		}
	}
}

func (cli *TCPClient) Stop() {
	cli.Session.Conn.Close()
}

func (cli *TCPClient) Send(msg model.JT808Msg) {
	pg := protocol.NewPipeline(cli.Session.Conn)

	// 记录value ctx
	ctx := context.WithValue(context.Background(), model.ProcessDataCtxKey{}, &model.ProcessData{Outgoing: msg})

	err := pg.ProcessConnWrite(ctx)

	if err == nil {
		return
	}

	if errors.Is(err, io.ErrClosedPipe) || errors.Is(err, net.ErrClosed) {
		cli.Stop()
	}

	log.Error().Err(err).Msg("Failed to send jtmsg to server")
}
